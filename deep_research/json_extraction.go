package deepresearch

import (
	"bytes"
	"encoding/json"
	"strings"
)

// extractJSON attempts to extract a valid JSON object from LLM output that
// may contain markdown fences, prose preamble, or other non-JSON text.
// It tries in order: raw string, fence-stripped, brace-extracted.
//
// Shared by all deep-research provider paths (claude, codex, gemini). The
// paired prompt-suffix constants (claudeJSONSuffix / codexJSONSuffix) instruct
// the model to emit raw JSON; this function is the belt-and-braces recovery
// when the model ignores that instruction.
//
// Known limitation: "first syntactically valid JSON wins." If the model
// echoes an example/schema JSON block before the real answer, this returns
// the echoed block — the caller's json.Unmarshal then succeeds into a
// zero-valued struct, degrading quality silently rather than erroring. The
// primary defense is the prompt-suffix instruction ("Respond with ONLY the
// raw JSON object"); this extractor is only the fallback. If a caller needs
// stronger guarantees, shape-validate the decoded struct after Unmarshal.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Fast path: starts with JSON. This tolerates trailing prose after the
	// top-level object/array by decoding only the first JSON value.
	if candidate := extractLeadingJSONValue(s); candidate != "" {
		return candidate
	}

	// Strip markdown fences.
	stripped := stripMarkdownFences(s)
	if stripped != s {
		if candidate := extractLeadingJSONValue(stripped); candidate != "" {
			return candidate
		}
	}

	// Extract the first decodable JSON object/array from surrounding prose.
	// Try whichever delimiter appears first, then continue scanning if that
	// candidate was not actually the start of valid JSON.
	for _, candidate := range []string{s, stripped} {
		for start := nextJSONStart(candidate, 0); start >= 0; start = nextJSONStart(candidate, start+1) {
			if extracted := extractLeadingJSONValue(candidate[start:]); extracted != "" {
				return extracted
			}
		}
	}

	// Nothing worked — return the fence-stripped version and let the caller
	// get the parse error for diagnostics.
	return stripped
}

func extractLeadingJSONValue(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s[0] != '{' && s[0] != '[' {
		return ""
	}

	var raw json.RawMessage
	dec := json.NewDecoder(bytes.NewBufferString(s))
	if err := dec.Decode(&raw); err != nil {
		return ""
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] != '{' && raw[0] != '[' {
		return ""
	}
	return string(raw)
}

func nextJSONStart(s string, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '{', '[':
			return i
		}
	}
	return -1
}

// stripMarkdownFences removes markdown code fences from LLM output
// that may wrap the JSON despite instructions not to.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
