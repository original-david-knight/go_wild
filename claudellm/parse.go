package claudellm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExecutionError represents a Claude Code execution failure with captured output.
type ExecutionError struct {
	Message  string
	ExitCode int
	Stdout   string
	Stderr   string
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

// ParsedResult holds the parsed output from a Claude Code mission.
type ParsedResult struct {
	Status        string
	Payload       map[string]any
	FailureReason string
	FormatError   bool // true when the failure is due to malformed output, not an explicit LLM failure
}

// ParseResult extracts the step status and payload from Claude Code JSON output.
func ParseResult(output string) ParsedResult {
	output = strings.TrimSpace(output)
	if output == "" {
		return invalidResult("claude-code returned empty output")
	}

	if rawResult, ok := extractStreamMissionResult(output); ok {
		return parseMissionResult(rawResult)
	}

	var claudeOutput map[string]any
	if err := json.Unmarshal([]byte(output), &claudeOutput); err == nil {
		if _, hasStatus := claudeOutput["status"]; hasStatus {
			return normalizeMissionMap(claudeOutput)
		}
		if rawResult, ok := claudeOutput["result"]; ok && looksLikeEnvelope(claudeOutput) {
			return parseMissionResult(rawResult)
		}
	}

	return parseMissionResult(output)
}

// decodeStreamEvents parses Claude Code stream-json NDJSON output into events.
func decodeStreamEvents(output string) []map[string]any {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil
		}
		events = append(events, event)
	}
	return events
}

// FormatEventLog formats Claude Code stream-json output into a human-readable event log.
func FormatEventLog(output string) string {
	events := decodeStreamEvents(output)
	if len(events) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, event := range events {
		switch strings.TrimSpace(fmt.Sprint(event["type"])) {
		case "system":
			appendSystemEventLog(&sb, event)
		case "assistant":
			appendAssistantEventLog(&sb, event)
		case "user":
			appendUserEventLog(&sb, event)
		case "result":
			appendResultEventLog(&sb, event)
		}
	}
	return strings.TrimSpace(sb.String())
}

// BuildFailureArtifacts constructs result and error payloads from a Claude Code failure.
func BuildFailureArtifacts(output string, err error) (map[string]any, map[string]any) {
	message := ""
	if err != nil {
		message = strings.TrimSpace(err.Error())
	}
	stdout := strings.TrimSpace(output)
	stderr := ""
	exitCode := 0
	if execErr, ok := err.(*ExecutionError); ok && execErr != nil {
		if stdout == "" {
			stdout = strings.TrimSpace(execErr.Stdout)
		}
		stderr = strings.TrimSpace(execErr.Stderr)
		exitCode = execErr.ExitCode
	}

	result := map[string]any{
		"status":         "failed",
		"failure_reason": message,
	}
	if stdout != "" {
		result["stdout"] = stdout
		result["raw_output"] = stdout
		if eventLog := strings.TrimSpace(FormatEventLog(stdout)); eventLog != "" {
			result["event_log"] = eventLog
		}
	}
	if stderr != "" {
		result["stderr"] = stderr
	}
	if exitCode != 0 {
		result["exit_code"] = exitCode
	}

	errorPayload := map[string]any{
		"message": message,
	}
	if stdout != "" {
		errorPayload["stdout"] = stdout
	}
	if stderr != "" {
		errorPayload["stderr"] = stderr
	}
	if exitCode != 0 {
		errorPayload["exit_code"] = exitCode
	}
	return result, errorPayload
}

func extractStreamMissionResult(output string) (any, bool) {
	events := decodeStreamEvents(output)
	if len(events) == 0 {
		return nil, false
	}

	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if strings.TrimSpace(fmt.Sprint(event["type"])) != "result" {
			continue
		}
		raw, exists := event["result"]
		return raw, exists
	}

	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if strings.TrimSpace(fmt.Sprint(event["type"])) != "assistant" {
			continue
		}
		if text := extractAssistantText(event); text != "" {
			return text, true
		}
	}

	return nil, false
}

func parseMissionResult(raw any) ParsedResult {
	switch typed := raw.(type) {
	case map[string]any:
		return normalizeMissionMap(typed)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return invalidResult("claude-code returned empty output")
		}
		if parsed, ok := decodeJSONMap(text); ok {
			return normalizeMissionMap(parsed)
		}
		return invalidResult("claude-code returned a non-JSON final response")
	default:
		return invalidResult("claude-code returned a malformed final response")
	}
}

func normalizeMissionMap(raw map[string]any) ParsedResult {
	statusRaw, hasStatus := raw["status"]
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(statusRaw)))
	if !hasStatus || status == "" {
		return invalidResult("claude-code returned JSON without the required status field")
	}
	if status != "succeeded" && status != "failed" {
		return invalidResult(fmt.Sprintf("claude-code returned invalid status %q", fmt.Sprint(statusRaw)))
	}
	failureReason := extractFailureReason(raw)
	resultRaw, hasResult := raw["result"]
	if status == "succeeded" && !hasResult {
		return invalidResult("claude-code returned success without the required result field")
	}

	if payload, ok := decodePayloadObject(resultRaw); ok {
		if status == "failed" && failureReason == "" {
			failureReason = extractFailureReason(payload)
		}
		return ParsedResult{
			Status:        status,
			Payload:       payload,
			FailureReason: failureReason,
		}
	}
	if status == "failed" {
		return ParsedResult{
			Status:        status,
			Payload:       cloneResultMap(raw),
			FailureReason: failureReason,
		}
	}

	return invalidResult("claude-code returned a result that was not a JSON object")
}

func invalidResult(reason string) ParsedResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "claude-code returned a malformed final response"
	}
	return ParsedResult{
		Status:        "failed",
		Payload:       map[string]any{"reason": reason},
		FailureReason: reason,
		FormatError:   true,
	}
}

// ExtractFinalResponse returns the final text response from Claude Code
// stream-json output, suitable for inclusion in a correction prompt.
func ExtractFinalResponse(output string) string {
	raw, ok := extractStreamMissionResult(output)
	if !ok {
		return strings.TrimSpace(output)
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		blob, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return strings.TrimSpace(string(blob))
	}
}

func looksLikeEnvelope(raw map[string]any) bool {
	if len(raw) == 0 {
		return false
	}
	for _, key := range []string{"type", "subtype", "message"} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

func decodePayloadObject(raw any) (map[string]any, bool) {
	switch typed := raw.(type) {
	case map[string]any:
		return cloneResultMap(typed), true
	case string:
		return decodeJSONMap(typed)
	default:
		return nil, false
	}
}

func decodeJSONMap(text string) (map[string]any, bool) {
	candidates := []string{strings.TrimSpace(text)}
	if trimmed := trimLeadingJSONLabel(strings.TrimSpace(text)); trimmed != strings.TrimSpace(text) {
		candidates = append(candidates, trimmed)
	}
	if fenced := extractFenceJSON(strings.TrimSpace(text)); fenced != "" {
		candidates = append(candidates, fenced)
	}
	if extracted := extractBalancedJSONObject(strings.TrimSpace(text)); extracted != "" {
		candidates = append(candidates, extracted)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

func cloneResultMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// extractFailureReason extracts a failure reason from a result map
// by checking "reason", "message", and "error" keys.
func extractFailureReason(result map[string]any) string {
	for _, key := range []string{"reason", "message"} {
		if msg, ok := result[key].(string); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				return msg
			}
		}
	}
	if rawErr, ok := result["error"]; ok {
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

func trimLeadingJSONLabel(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= 4 {
		return trimmed
	}
	if !strings.EqualFold(trimmed[:4], "json") {
		return trimmed
	}
	switch trimmed[4] {
	case '\n', '\r', '\t', ' ':
		return strings.TrimSpace(trimmed[4:])
	default:
		return trimmed
	}
}

// extractFenceJSON extracts JSON from markdown code fences.
func extractFenceJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	for {
		start := strings.Index(trimmed, "```")
		if start < 0 {
			return ""
		}
		rest := trimmed[start+3:]
		end := strings.Index(rest, "```")
		if end < 0 {
			return ""
		}
		block := strings.TrimSpace(rest[:end])
		block = trimLeadingJSONLabel(block)
		if block != "" {
			return block
		}
		trimmed = rest[end+3:]
	}
}

// extractBalancedJSONObject finds the first balanced JSON object in text.
func extractBalancedJSONObject(text string) string {
	for start := 0; start < len(text); start++ {
		if text[start] != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(text); i++ {
			ch := text[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return text[start : i+1]
				}
			}
		}
	}
	return ""
}

// Event log formatting

func appendSystemEventLog(sb *strings.Builder, event map[string]any) {
	if sb == nil || len(event) == 0 || strings.TrimSpace(fmt.Sprint(event["subtype"])) != "init" {
		return
	}

	var details []string
	for _, field := range []struct{ key, label string }{
		{"model", "model"},
		{"session_id", "session_id"},
		{"permissionMode", "permission_mode"},
		{"cwd", "cwd"},
	} {
		if val := strings.TrimSpace(fmt.Sprint(event[field.key])); val != "" && val != "<nil>" {
			details = append(details, field.label+"="+val)
		}
	}
	if len(details) == 0 {
		return
	}

	appendLogSection(sb, "SYSTEM init", strings.Join(details, "\n"))
}

func appendAssistantEventLog(sb *strings.Builder, event map[string]any) {
	if sb == nil || len(event) == 0 {
		return
	}
	message, _ := event["message"].(map[string]any)
	content, _ := message["content"].([]any)
	for _, item := range content {
		part, _ := item.(map[string]any)
		switch strings.TrimSpace(fmt.Sprint(part["type"])) {
		case "tool_use":
			var title strings.Builder
			title.WriteString("ASSISTANT tool_use")
			if name := strings.TrimSpace(fmt.Sprint(part["name"])); name != "" && name != "<nil>" {
				title.WriteString(": ")
				title.WriteString(name)
			}
			body := formatLogBody(part["input"])
			if toolID := strings.TrimSpace(fmt.Sprint(part["id"])); toolID != "" && toolID != "<nil>" {
				if body != "" {
					body = "tool_use_id: " + toolID + "\n" + body
				} else {
					body = "tool_use_id: " + toolID
				}
			}
			appendLogSection(sb, title.String(), body)
		case "text":
			text := strings.TrimSpace(fmt.Sprint(part["text"]))
			if text != "" && text != "<nil>" {
				appendLogSection(sb, "ASSISTANT text", text)
			}
		}
	}
}

func appendUserEventLog(sb *strings.Builder, event map[string]any) {
	if sb == nil || len(event) == 0 {
		return
	}
	message, _ := event["message"].(map[string]any)
	content, _ := message["content"].([]any)
	for _, item := range content {
		part, _ := item.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(part["type"])) != "tool_result" {
			continue
		}

		body := formatLogBody(part["content"])
		if toolID := strings.TrimSpace(fmt.Sprint(part["tool_use_id"])); toolID != "" && toolID != "<nil>" {
			if body != "" {
				body = "tool_use_id: " + toolID + "\n" + body
			} else {
				body = "tool_use_id: " + toolID
			}
		}
		if isError, _ := part["is_error"].(bool); isError {
			if body != "" {
				body = "is_error: true\n" + body
			} else {
				body = "is_error: true"
			}
		}
		appendLogSection(sb, "USER tool_result", body)
	}
}

func appendResultEventLog(sb *strings.Builder, event map[string]any) {
	if sb == nil || len(event) == 0 {
		return
	}

	title := "RESULT"
	if subtype := strings.TrimSpace(fmt.Sprint(event["subtype"])); subtype != "" && subtype != "<nil>" {
		title += " " + subtype
	}

	var details []string
	for _, field := range []string{"stop_reason", "duration_ms", "total_cost_usd"} {
		if val := strings.TrimSpace(fmt.Sprint(event[field])); val != "" && val != "<nil>" {
			details = append(details, field+": "+val)
		}
	}
	if result := formatLogBody(event["result"]); result != "" {
		details = append(details, "result:\n"+result)
	}

	appendLogSection(sb, title, strings.Join(details, "\n"))
}

func extractAssistantText(event map[string]any) string {
	message, _ := event["message"].(map[string]any)
	content, _ := message["content"].([]any)
	var parts []string
	for _, item := range content {
		part, _ := item.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(part["type"])) != "text" {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(part["text"]))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func formatLogBody(raw any) string {
	switch typed := raw.(type) {
	case nil:
		return ""
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return ""
		}
		if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			var decoded any
			if err := json.Unmarshal([]byte(text), &decoded); err == nil {
				blob, err := json.MarshalIndent(decoded, "", "  ")
				if err == nil {
					return strings.TrimSpace(string(blob))
				}
			}
		}
		return text
	default:
		blob, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(raw))
		}
		return strings.TrimSpace(string(blob))
	}
}

func appendLogSection(sb *strings.Builder, title, body string) {
	if sb == nil {
		return
	}
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return
	}
	if sb.Len() > 0 {
		sb.WriteString("\n\n")
	}
	if title != "" {
		sb.WriteString(title)
	}
	if body != "" {
		if title != "" {
			sb.WriteString("\n")
		}
		sb.WriteString(body)
	}
}
