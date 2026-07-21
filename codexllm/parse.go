package codexllm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExecutionError represents a Codex execution failure with captured output.
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

// ParsedResult holds the parsed output from a Codex mission.
type ParsedResult struct {
	Status        string
	Payload       map[string]any
	FailureReason string
	FormatError   bool // true when the failure is due to malformed output, not an explicit LLM failure
}

// ParseResult extracts the step status and payload from Codex JSON output.
func ParseResult(output string) ParsedResult {
	output = strings.TrimSpace(output)
	if output == "" {
		return invalidResult("codex returned empty output")
	}

	// Try to extract the result from JSONL stream events first.
	if rawResult, ok := extractCodexStreamResult(output); ok {
		return parseMissionResult(rawResult)
	}

	// Try to parse as a direct JSON object.
	var directOutput map[string]any
	if err := json.Unmarshal([]byte(output), &directOutput); err == nil {
		if _, hasStatus := directOutput["status"]; hasStatus {
			return normalizeMissionMap(directOutput)
		}
	}

	return parseMissionResult(output)
}

// decodeStreamEvents parses Codex JSONL output into events. Malformed lines
// are skipped so a single bad line does not discard the rest of the stream —
// this matters when Codex writes a partial final line or interleaves a stray
// non-JSON diagnostic; the earlier events (including the agent_message we need
// for extractCodexStreamResult) still need to come through.
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
			continue
		}
		if event == nil {
			// A bare `null` line unmarshals into a nil map without error.
			// Treat as noise — callers expect real event objects.
			continue
		}
		events = append(events, event)
	}
	return events
}

// FormatEventLog formats Codex JSONL output into a human-readable event log.
func FormatEventLog(output string) string {
	events := decodeStreamEvents(output)
	if len(events) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, event := range events {
		eventType := strings.TrimSpace(fmt.Sprint(event["type"]))
		switch eventType {
		case "thread.started":
			appendLogSection(&sb, "THREAD started", fmt.Sprintf("thread_id: %v", event["thread_id"]))
		case "item.completed":
			item, _ := event["item"].(map[string]any)
			itemType := fmt.Sprint(item["type"])
			text, _ := item["text"].(string)
			if text != "" && len(text) > 500 {
				text = text[:500] + "..."
			}
			appendLogSection(&sb, "ITEM "+itemType, text)
		case "turn.completed":
			usage, _ := event["usage"].(map[string]any)
			appendLogSection(&sb, "TURN completed", formatLogBody(usage))
		case "error":
			appendLogSection(&sb, "ERROR", fmt.Sprint(event["message"]))
		case "turn.failed":
			errObj, _ := event["error"].(map[string]any)
			appendLogSection(&sb, "TURN FAILED", fmt.Sprint(errObj["message"]))
		}
	}
	return strings.TrimSpace(sb.String())
}

// BuildFailureArtifacts constructs result and error payloads from a Codex failure.
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

// ExtractFinalResponse returns the final text response from Codex JSONL output.
func ExtractFinalResponse(output string) string {
	raw, ok := extractCodexStreamResult(output)
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

func extractCodexStreamResult(output string) (any, bool) {
	events := decodeStreamEvents(output)
	if len(events) == 0 {
		return nil, false
	}

	// Find the last item.completed with type:"agent_message"
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if strings.TrimSpace(fmt.Sprint(event["type"])) != "item.completed" {
			continue
		}
		item, ok := event["item"].(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(item["type"])) == "agent_message" {
			if text, ok := item["text"].(string); ok && text != "" {
				return text, true
			}
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
			return invalidResult("codex returned empty output")
		}
		if parsed, ok := decodeJSONMap(text); ok {
			return normalizeMissionMap(parsed)
		}
		return invalidResult("codex returned a non-JSON final response")
	default:
		return invalidResult("codex returned a malformed final response")
	}
}

func normalizeMissionMap(raw map[string]any) ParsedResult {
	statusRaw, hasStatus := raw["status"]
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(statusRaw)))
	if !hasStatus || status == "" {
		return invalidResult("codex returned JSON without the required status field")
	}
	if status != "succeeded" && status != "failed" {
		return invalidResult(fmt.Sprintf("codex returned invalid status %q", fmt.Sprint(statusRaw)))
	}
	failureReason := extractFailureReason(raw)
	resultRaw, hasResult := raw["result"]
	if status == "succeeded" && !hasResult {
		return invalidResult("codex returned success without the required result field")
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

	return invalidResult("codex returned a result that was not a JSON object")
}

func invalidResult(reason string) ParsedResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "codex returned a malformed final response"
	}
	return ParsedResult{
		Status:        "failed",
		Payload:       map[string]any{"reason": reason},
		FailureReason: reason,
		FormatError:   true,
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
//
// This is a heuristic last-resort fallback in decodeJSONMap. It only tracks
// RFC-8259 double-quoted strings — single quotes, backtick template literals,
// `//` line comments, and `/* */` block comments are NOT recognised. Callers
// instruct Codex (via the system prompt built in apps/agent_manager/codex_runner.go)
// to emit raw JSON, so these non-spec forms should not appear in practice.
//
// Known limitations if they do appear:
//   - Embedded JSON-looking content inside a comment or template literal can
//     be returned as a balanced match (e.g. `/* {"k":1} */ noise` returns
//     `{"k":1}`), and json.Unmarshal will accept it — silent wrong-object
//     extraction, not a parse failure.
//   - The scanner returns on the FIRST balanced `{...}` region it finds. If
//     that region is malformed, the caller's json.Unmarshal fails and
//     decodeJSONMap does not try later `{...}` regions in the same string —
//     a valid JSON object after an invalid one is lost.
//
// Both behaviours are accepted because Codex is not expected to emit
// non-JSON content. See TestExtractBalancedJSONObject_* for the pinned
// outputs under these edge cases.
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

func formatLogBody(raw any) string {
	switch typed := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		blob, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
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
