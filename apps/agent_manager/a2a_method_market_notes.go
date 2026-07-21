package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

const autoMethodMarketNoteMaxLength = 1900

// maybeSetCompletionMarketProperties extracts method/status/conditionID/companyID
// from an A2A completion result and delegates to the shared setCompletionMarketProperties.
func (h *BrokerToolsHandler) maybeSetCompletionMarketProperties(ctx context.Context, agentID string, completed map[string]any) {
	if h == nil || h.db == nil || len(completed) == 0 {
		return
	}
	method := completedMethodName(completed)
	status := strings.TrimSpace(firstDeepString([]any{completed}, "status"))
	conditionID := extractCompletionConditionID(completed)
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, strings.TrimSpace(agentID))
	if err != nil || member == nil {
		return
	}
	setCompletionMarketProperties(ctx, h.db, method, status, conditionID, strings.TrimSpace(member.CompanyID))
}

// setCompletionMarketProperties is the core logic shared by both A2A and
// claude-code completion paths. It requires a db, method name, status,
// condition_id, and company_id.
func setCompletionMarketProperties(ctx context.Context, db gowild_data.Database, method, status, conditionID, companyID string) {
	if db == nil {
		return
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return
	}
	methodDef, err := data.NewAgentService(db, "system").GetA2AMethod(ctx, method)
	if err != nil || methodDef == nil {
		return
	}
	tsKey := strings.TrimSpace(methodDef.CompletionTimestampKey)
	okKey := strings.TrimSpace(methodDef.CompletionSuccessKey)
	if tsKey == "" && okKey == "" {
		return
	}
	conditionID = strings.TrimSpace(conditionID)
	companyID = strings.TrimSpace(companyID)
	if conditionID == "" || companyID == "" {
		return
	}
	succeeded := strings.ToLower(strings.TrimSpace(status)) == "succeeded"
	if tsKey != "" {
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := data.SetMarketProperty(ctx, db, companyID, conditionID, tsKey, now, data.MarketPropertyTypeDatetime); err != nil {
			log.Printf("Completion timestamp property failed for method %s key %s: %v", method, tsKey, err)
		}
	}
	if okKey != "" {
		if _, err := data.SetMarketProperty(ctx, db, companyID, conditionID, okKey, fmt.Sprintf("%t", succeeded), data.MarketPropertyTypeBool); err != nil {
			log.Printf("Completion success property failed for method %s key %s: %v", method, okKey, err)
		}
	}
}

// extractCompletionConditionID pulls condition_id from a completed job result,
// searching the top level, request.params, and the result payload.
func extractCompletionConditionID(completed map[string]any) string {
	conditionID := firstDeepString([]any{completed}, "condition_id")
	if conditionID != "" {
		return conditionID
	}
	request, _ := completed["request"].(map[string]any)
	params, _ := request["params"].(map[string]any)
	return firstDeepString([]any{params, request}, "condition_id")
}

func (h *BrokerToolsHandler) maybeAddAutomaticMethodMarketNote(ctx context.Context, agentID string, completed map[string]any) {
	if h == nil || h.db == nil || len(completed) == 0 {
		return
	}

	method := completedMethodName(completed)
	if method == "" {
		return
	}

	methodDef, err := data.NewAgentService(h.db, "system").GetA2AMethod(ctx, method)
	if err != nil {
		log.Printf("A2A auto market note skipped for method %s: failed to load method config: %v", method, err)
		return
	}
	if methodDef == nil || !methodDef.AutoMarketNote {
		return
	}

	member, err := data.GetCompanyMemberForAgent(ctx, h.db, strings.TrimSpace(agentID))
	if err != nil {
		log.Printf("A2A auto market note skipped for method %s: failed to resolve company for agent %s: %v", method, agentID, err)
		return
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		log.Printf("A2A auto market note skipped for method %s: agent %s is not in a company", method, agentID)
		return
	}
	if automaticMethodMarketVolumeTooLow(completed) {
		log.Printf("A2A auto market note skipped for method %s job %s: low market volume", method, completedMethodJobID(completed))
		return
	}

	conditionID, note := buildAutomaticMethodMarketNote(method, strings.TrimSpace(agentID), completed)
	if conditionID == "" {
		log.Printf("A2A auto market note skipped for method %s job %s: no condition_id found", method, completedMethodJobID(completed))
		return
	}
	if note == "" {
		log.Printf("A2A auto market note skipped for method %s job %s: empty note content", method, completedMethodJobID(completed))
		return
	}

	metadata := buildAutomaticMethodMarketNoteMetadata(method, completed)
	if _, err := data.AddMarketNoteWithMetadata(ctx, h.db, strings.TrimSpace(member.CompanyID), strings.TrimSpace(agentID), conditionID, note, metadata); err != nil {
		log.Printf("A2A auto market note failed for method %s job %s: %v", method, completedMethodJobID(completed), err)
	}
}

func buildAutomaticMethodMarketNote(method, agentID string, completed map[string]any) (string, string) {
	request, _ := completed["request"].(map[string]any)
	params, _ := request["params"].(map[string]any)
	result, _ := completed["result"].(map[string]any)
	errPayload, _ := completed["error"].(map[string]any)
	responsePreview := completedMethodResponsePreview(errPayload)
	previewPayload := decodeLooseJSONObject(responsePreview)

	searchSpace := []any{
		completed,
		request,
		params,
		result,
		errPayload,
		previewPayload,
	}

	conditionID := firstDeepString(searchSpace, "condition_id")
	question := firstDeepString(searchSpace, "question", "title")
	actionTaken := firstDeepString(searchSpace, "action_taken", "action", "recommendation")
	reason := firstDeepString([]any{result, previewPayload, params, request}, "reason", "reasoning", "assessment", "summary", "message")
	errorMessage := firstDeepString([]any{errPayload}, "message")
	if reason == "" {
		reason = errorMessage
	}
	status := strings.ToUpper(firstDeepString([]any{completed, result, errPayload}, "status"))
	if status == "" {
		status = strings.ToUpper(stringParam(completed, "status"))
	}
	if status == "" {
		status = "COMPLETED"
	}
	if strings.EqualFold(status, "SUCCEEDED") || strings.EqualFold(status, "FAILED") {
		// Keep plain terminal states as-is.
	} else if result == nil && errPayload != nil {
		status = "FAILED"
	}

	if shouldSuppressAutomaticMethodMarketNote(status, reason, errorMessage, responsePreview) {
		return conditionID, ""
	}

	lines := []string{
		fmt.Sprintf("Method %s completed", strings.TrimSpace(method)),
		fmt.Sprintf("Status: %s", status),
	}
	if agentID != "" {
		lines = append(lines, fmt.Sprintf("Agent: %s", agentID))
	}
	if jobID := completedMethodJobID(completed); jobID != "" {
		lines = append(lines, fmt.Sprintf("Job ID: %s", jobID))
	}
	if question != "" {
		lines = append(lines, fmt.Sprintf("Question: %s", question))
	}
	if actionTaken != "" {
		lines = append(lines, fmt.Sprintf("Action: %s", actionTaken))
	}

	if value, ok := firstDeepFloat(searchSpace, "probability", "estimated_probability"); ok {
		lines = append(lines, fmt.Sprintf("Probability: %s", formatAutoMarketNoteFloat(value)))
	}
	if value, ok := firstDeepFloat(searchSpace, "current_position"); ok {
		lines = append(lines, fmt.Sprintf("Current position: %s", formatAutoMarketNoteFloat(value)))
	}
	if value, ok := firstDeepFloat(searchSpace, "max_allowed"); ok {
		lines = append(lines, fmt.Sprintf("Max allowed: %s", formatAutoMarketNoteFloat(value)))
	}
	if value, ok := firstDeepFloat(searchSpace, "remaining_capacity"); ok {
		lines = append(lines, fmt.Sprintf("Remaining capacity: %s", formatAutoMarketNoteFloat(value)))
	}
	if value, ok := firstDeepFloat(searchSpace, "aum"); ok {
		lines = append(lines, fmt.Sprintf("AUM: %s", formatAutoMarketNoteFloat(value)))
	}

	if reason != "" {
		lines = append(lines, fmt.Sprintf("Reason: %s", reason))
	}
	if errorMessage != "" {
		lines = append(lines, fmt.Sprintf("Error: %s", errorMessage))
	}
	if responsePreview != "" && previewShouldBeIncluded(responsePreview, reason) {
		lines = append(lines, fmt.Sprintf("Response preview: %s", responsePreview))
	}

	note := strings.Join(lines, "\n")
	note = strings.TrimSpace(note)
	if len(note) > autoMethodMarketNoteMaxLength {
		note = strings.TrimSpace(note[:autoMethodMarketNoteMaxLength-3]) + "..."
	}
	return conditionID, note
}

func buildAutomaticMethodMarketNoteMetadata(method string, completed map[string]any) *data.MarketNoteMetadata {
	request, _ := completed["request"].(map[string]any)
	params, _ := request["params"].(map[string]any)
	result, _ := completed["result"].(map[string]any)
	errPayload, _ := completed["error"].(map[string]any)
	responsePreview := completedMethodResponsePreview(errPayload)
	previewPayload := decodeLooseJSONObject(responsePreview)

	searchSpace := []any{
		completed,
		request,
		params,
		result,
		errPayload,
		previewPayload,
	}

	status := strings.ToUpper(firstDeepString([]any{completed, result, errPayload}, "status"))
	if status == "" {
		status = strings.ToUpper(stringParam(completed, "status"))
	}
	if status == "" {
		status = "COMPLETED"
	}

	metadata := &data.MarketNoteMetadata{
		Kind:         "a2a_" + strings.TrimSpace(method),
		Status:       strings.TrimSpace(status),
		Action:       firstDeepString(searchSpace, "action_taken", "action", "recommendation"),
		Question:     firstDeepString(searchSpace, "question", "title"),
		Reasoning:    firstDeepString([]any{result, previewPayload, params, request}, "reason", "reasoning", "assessment", "summary", "message"),
		Invalidation: "Reevaluate when the method output, market state, or cited evidence changes materially.",
		CapturedAt:   time.Now().UTC(),
	}
	if value, ok := firstDeepFloat(searchSpace, "probability", "estimated_probability"); ok && value >= 0 && value <= 1 {
		metadata.EstimatedProbability = builtinPolymarketFloatPointer(value)
	}
	if value, ok := firstDeepFloat(searchSpace, "confidence"); ok && value >= 0 && value <= 1 {
		metadata.Confidence = builtinPolymarketFloatPointer(value)
	}
	if value, ok := firstDeepFloat(searchSpace, "current_position"); ok {
		metadata.CurrentPosition = builtinPolymarketFloatPointer(value)
	}
	if value, ok := firstDeepFloat(searchSpace, "max_allowed"); ok {
		metadata.MaxAllowed = builtinPolymarketFloatPointer(value)
	}
	if value, ok := firstDeepFloat(searchSpace, "remaining_capacity"); ok {
		metadata.RemainingCapacity = builtinPolymarketFloatPointer(value)
	}
	if resolutionDate := firstDeepString(searchSpace, "resolution_date", "end_date"); resolutionDate != "" {
		metadata.ResolutionDate = resolutionDate
	}
	metadata.ThesisHash = polymarketStableHash(
		strings.TrimSpace(method),
		firstDeepString(searchSpace, "condition_id"),
		metadata.Question,
		metadata.Reasoning,
		formatBuiltinPolymarketHashFloat(metadata.EstimatedProbability),
		formatBuiltinPolymarketHashFloat(metadata.Confidence),
		metadata.ResolutionDate,
	)
	return metadata
}

func automaticMethodMarketVolumeTooLow(completed map[string]any) bool {
	if len(completed) == 0 {
		return false
	}
	request, _ := completed["request"].(map[string]any)
	params, _ := request["params"].(map[string]any)
	result, _ := completed["result"].(map[string]any)
	errPayload, _ := completed["error"].(map[string]any)
	responsePreview := completedMethodResponsePreview(errPayload)
	previewPayload := decodeLooseJSONObject(responsePreview)

	volume, ok := firstDeepFloat([]any{
		completed,
		request,
		params,
		result,
		errPayload,
		previewPayload,
	}, "volume", "market_volume")
	return ok && volume < builtinPolymarketFindMarketsMinVolume
}

func shouldSuppressAutomaticMethodMarketNote(status, reason, errorMessage, responsePreview string) bool {
	if !strings.EqualFold(strings.TrimSpace(status), "FAILED") {
		return false
	}

	text := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(reason),
		strings.TrimSpace(errorMessage),
		strings.TrimSpace(responsePreview),
	}, "\n"))
	if text == "" {
		return false
	}
	return strings.Contains(text, "already been completely researched") ||
		strings.Contains(text, "redundant research is unnecessary") ||
		strings.Contains(text, "redundant re-evaluation")
}

func completedMethodName(completed map[string]any) string {
	request, _ := completed["request"].(map[string]any)
	return firstStringParam(request, "method")
}

func completedMethodJobID(completed map[string]any) string {
	return firstStringParam(completed, "job_id", "id")
}

func completedMethodResponsePreview(errPayload map[string]any) string {
	if errPayload == nil {
		return ""
	}
	details, _ := errPayload["details"].(map[string]any)
	return firstStringParam(details, "response_preview")
}

func firstDeepString(values []any, keys ...string) string {
	for _, value := range values {
		if found := deepStringValue(value, 0, keys...); found != "" {
			return found
		}
	}
	return ""
}

func firstDeepFloat(values []any, keys ...string) (float64, bool) {
	for _, value := range values {
		if found, ok := deepFloatValue(value, 0, keys...); ok {
			return found, true
		}
	}
	return 0, false
}

func deepStringValue(value any, depth int, keys ...string) string {
	if depth > 8 || value == nil {
		return ""
	}
	if found, ok := deepValueByKeys(value, depth, keys...); ok {
		switch typed := found.(type) {
		case string:
			return strings.TrimSpace(typed)
		case json.Number:
			return strings.TrimSpace(typed.String())
		case fmt.Stringer:
			return strings.TrimSpace(typed.String())
		}
	}
	return ""
}

func deepFloatValue(value any, depth int, keys ...string) (float64, bool) {
	if depth > 8 || value == nil {
		return 0, false
	}
	found, ok := deepValueByKeys(value, depth, keys...)
	if !ok {
		return 0, false
	}
	switch typed := found.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	case string:
		v, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return v, err == nil
	default:
		return 0, false
	}
}

func deepValueByKeys(value any, depth int, keys ...string) (any, bool) {
	if depth > 8 || value == nil {
		return nil, false
	}

	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			for actualKey, actualValue := range typed {
				if strings.EqualFold(strings.TrimSpace(actualKey), strings.TrimSpace(key)) {
					return actualValue, true
				}
			}
		}
		for _, child := range typed {
			if found, ok := deepValueByKeys(child, depth+1, keys...); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range typed {
			if found, ok := deepValueByKeys(child, depth+1, keys...); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func decodeLooseJSONObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	candidates := []string{raw}
	if firstBrace := strings.Index(raw, "{"); firstBrace >= 0 {
		if lastBrace := strings.LastIndex(raw, "}"); lastBrace > firstBrace {
			candidates = append(candidates, strings.TrimSpace(raw[firstBrace:lastBrace+1]))
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		var payload map[string]any
		if err := json.Unmarshal([]byte(candidate), &payload); err == nil && payload != nil {
			return payload
		}
	}
	return nil
}

func previewShouldBeIncluded(preview, reason string) bool {
	preview = strings.TrimSpace(preview)
	reason = strings.TrimSpace(reason)
	if preview == "" {
		return false
	}
	if reason == "" {
		return true
	}
	return !strings.Contains(strings.ToLower(preview), strings.ToLower(reason))
}

func formatAutoMarketNoteFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
