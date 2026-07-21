package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

var jsonCodeFencePattern = regexp.MustCompile("(?is)```(?:json)?\\s*(\\{.*\\})\\s*```")

type claimedMethodCallConfig struct {
	JobID        string
	Method       string
	FreshContext bool
}

func claimedMethodCallConfigFromHeartbeat(message string) (claimedMethodCallConfig, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return claimedMethodCallConfig{}, false
	}
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "this is a heartbeat for a company method call.") &&
		!strings.HasPrefix(lower, "this is a heartbeat for a claimed company method call.") {
		return claimedMethodCallConfig{}, false
	}
	cfg := claimedMethodCallConfig{}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		const jobPrefix = "job id:"
		lowerLine := strings.ToLower(line)
		if strings.HasPrefix(lowerLine, jobPrefix) {
			cfg.JobID = strings.TrimSpace(line[len(jobPrefix):])
			continue
		}
		const freshContextPrefix = "fresh context:"
		if strings.HasPrefix(lowerLine, freshContextPrefix) {
			value := strings.TrimSpace(line[len(freshContextPrefix):])
			cfg.FreshContext = strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || strings.EqualFold(value, "1")
			continue
		}
		const methodPrefix = "method:"
		if strings.HasPrefix(lowerLine, methodPrefix) {
			cfg.Method = strings.TrimSpace(line[len(methodPrefix):])
		}
	}
	if cfg.JobID == "" {
		return claimedMethodCallConfig{}, false
	}
	return cfg, true
}

func claimedMethodJobIDFromHeartbeat(message string) (string, bool) {
	cfg, ok := claimedMethodCallConfigFromHeartbeat(message)
	if !ok {
		return "", false
	}
	return cfg.JobID, true
}

func historyForClaimedMethodCall(history []loop.Message, prompt string, freshContext bool) []loop.Message {
	if freshContext {
		history = nil
	}
	return append(history, loop.NewUserMessage(prompt))
}

func finalizeClaimedMethodCallHistory(previous, updated []loop.Message, freshContext bool) []loop.Message {
	if freshContext {
		return previous
	}
	return updated
}

func autoCompleteClaimedMethodJob(ctx context.Context, jobID, finalText, lastError string) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" || globalBrokerClient == nil {
		return
	}

	resultPayload, parseErr := parseMethodResultJSONMap(finalText)
	if parseErr != nil {
		msg := fmt.Sprintf("method response must be a JSON object: %v", parseErr)
		if lastError != "" {
			msg = fmt.Sprintf("agent error: %s (method response must be a JSON object: %v)", lastError, parseErr)
		}
		completeClaimedMethodJobFailed(ctx, jobID, msg, finalText)
		return
	}
	if failed, reason := methodResultMarkedFailed(resultPayload); failed {
		if reason == "" {
			reason = "method returned status=FAILED"
		}
		completeClaimedMethodJobFailed(ctx, jobID, reason, finalText)
		return
	}
	resultPayload = normalizeMethodSuccessPayload(resultPayload)

	_, err := globalBrokerClient.CallTool(ctx, "job_result", map[string]any{
		"job_id": jobID,
		"status": "succeeded",
		"result": resultPayload,
	})
	if err != nil {
		if isMethodJobAlreadyFinalizedError(err) {
			output.System("Method call %s already finalized; skipping completion update", jobID)
			return
		}
		completeClaimedMethodJobFailed(ctx, jobID, fmt.Sprintf("method result rejected: %v", err), finalText)
		return
	}

	output.System("Completed method call %s", jobID)
}

func completeClaimedMethodJobFailed(ctx context.Context, jobID, message, finalText string) {
	errorPayload := map[string]any{
		"message": strings.TrimSpace(message),
	}
	if preview := strings.TrimSpace(finalText); preview != "" {
		errorPayload["details"] = map[string]any{
			"response_preview": truncate(preview, 1200),
		}
	}

	_, err := globalBrokerClient.CallTool(ctx, "job_result", map[string]any{
		"job_id": jobID,
		"status": "failed",
		"error":  errorPayload,
	})
	if err != nil {
		if isMethodJobAlreadyFinalizedError(err) {
			output.System("Method call %s already finalized; skipping failed update", jobID)
			return
		}
		output.SystemWarning("Failed to complete method call %s: %s (failed to mark failed: %v)", jobID, message, err)
		return
	}
	output.SystemWarning("Marked method call %s as failed: %s", jobID, message)
}

func isMethodJobAlreadyFinalizedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if !strings.Contains(msg, "invalid job state") {
		return false
	}
	return strings.Contains(msg, "\"failed\"") || strings.Contains(msg, "\"succeeded\"")
}

func parseMethodResultJSONMap(text string) (map[string]any, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("empty response")
	}

	candidates := []string{trimmed}
	if m := jsonCodeFencePattern.FindStringSubmatch(trimmed); len(m) > 1 {
		candidates = append(candidates, strings.TrimSpace(m[1]))
	}
	if firstBrace := strings.Index(trimmed, "{"); firstBrace >= 0 {
		if lastBrace := strings.LastIndex(trimmed, "}"); lastBrace > firstBrace {
			candidates = append(candidates, strings.TrimSpace(trimmed[firstBrace:lastBrace+1]))
		}
	}

	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}

		var payload map[string]any
		if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
			continue
		}
		if payload == nil {
			continue
		}
		return payload, nil
	}

	return nil, fmt.Errorf("could not decode JSON object from final response")
}

func methodResultMarkedFailed(payload map[string]any) (bool, string) {
	if len(payload) == 0 {
		return false, ""
	}
	return methodResultMarkedFailedInValue(payload, 0)
}

func methodResultMarkedFailedInValue(value any, depth int) (bool, string) {
	if depth > 3 {
		return false, ""
	}
	switch v := value.(type) {
	case map[string]any:
		return methodResultMarkedFailedInMap(v, depth)
	case string:
		trimmed := strings.TrimSpace(v)
		if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
			return false, ""
		}
		var nested map[string]any
		if err := json.Unmarshal([]byte(trimmed), &nested); err != nil {
			return false, ""
		}
		return methodResultMarkedFailedInMap(nested, depth+1)
	default:
		return false, ""
	}
}

func methodResultMarkedFailedInMap(payload map[string]any, depth int) (bool, string) {
	for key, raw := range payload {
		if !strings.EqualFold(strings.TrimSpace(key), "status") {
			continue
		}
		status, ok := raw.(string)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(status), "failed") {
			return true, methodResultFailureReason(payload)
		}
	}

	for _, wrapperKey := range []string{"result", "output", "payload", "response", "data"} {
		child, ok := payload[wrapperKey]
		if !ok {
			continue
		}
		if failed, reason := methodResultMarkedFailedInValue(child, depth+1); failed {
			return true, reason
		}
	}

	return false, ""
}

func methodResultFailureReason(payload map[string]any) string {
	for _, key := range []string{"reason", "message"} {
		if msg, ok := payload[key].(string); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				return msg
			}
		}
	}

	if rawErr, ok := payload["error"]; ok {
		switch e := rawErr.(type) {
		case string:
			if msg := strings.TrimSpace(e); msg != "" {
				return msg
			}
		case map[string]any:
			if msg, ok := e["message"].(string); ok {
				if msg = strings.TrimSpace(msg); msg != "" {
					return msg
				}
			}
		}
	}

	return ""
}

func normalizeMethodSuccessPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return payload
	}
	statusRaw, ok := payload["status"]
	if !ok {
		return payload
	}
	status, ok := statusRaw.(string)
	if !ok || !strings.EqualFold(strings.TrimSpace(status), "succeeded") {
		return payload
	}

	for _, wrapperKey := range []string{"result", "output", "payload", "response", "data"} {
		child, ok := payload[wrapperKey]
		if !ok {
			continue
		}
		if inner, ok := decodeJSONMapLike(child); ok {
			return inner
		}
	}
	return payload
}

func decodeJSONMapLike(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case string:
		trimmed := strings.TrimSpace(v)
		if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
			return nil, false
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil || parsed == nil {
			return nil, false
		}
		return parsed, true
	default:
		return nil, false
	}
}
