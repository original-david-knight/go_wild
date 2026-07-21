package claudellm

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExtractBalancedJSONObject(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantJSON bool
	}{
		{
			name:     "plain object",
			input:    `{"status":"succeeded"}`,
			want:     `{"status":"succeeded"}`,
			wantJSON: true,
		},
		{
			name:     "object with prose prefix",
			input:    "Here is the result: " + `{"status":"succeeded","result":{"x":1}}`,
			want:     `{"status":"succeeded","result":{"x":1}}`,
			wantJSON: true,
		},
		{
			name:     "nested objects",
			input:    `prefix {"a":{"b":{"c":1}}} suffix`,
			want:     `{"a":{"b":{"c":1}}}`,
			wantJSON: true,
		},
		{
			name:     "string containing braces is skipped",
			input:    `{"msg":"this has { and } inside"}`,
			want:     `{"msg":"this has { and } inside"}`,
			wantJSON: true,
		},
		{
			name:     "escaped quote inside string",
			input:    `{"msg":"say \"hi\" now"}`,
			want:     `{"msg":"say \"hi\" now"}`,
			wantJSON: true,
		},
		{
			name:  "no object returns empty",
			input: "no json here at all",
			want:  "",
		},
		{
			name:  "empty input returns empty",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBalancedJSONObject(tt.input)
			if got != tt.want {
				t.Fatalf("extractBalancedJSONObject() = %q, want %q", got, tt.want)
			}
			if tt.wantJSON {
				var v map[string]any
				if err := json.Unmarshal([]byte(got), &v); err != nil {
					t.Fatalf("expected valid JSON, got error: %v", err)
				}
			}
		})
	}
}

// TestExtractBalancedJSONObject_FirstBalancedWins pins that the scanner
// returns the first balanced `{...}` region, even if a later region in the
// same input is the "real" JSON.
func TestExtractBalancedJSONObject_FirstBalancedWins(t *testing.T) {
	input := `prefix {not json} {"real":1}`
	want := `{not json}`
	if got := extractBalancedJSONObject(input); got != want {
		t.Fatalf("extractBalancedJSONObject() = %q, want %q", got, want)
	}
}

func TestExtractFenceJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty input returns empty", input: "", want: ""},
		{name: "no fence returns empty", input: `{"status":"ok"}`, want: ""},
		{name: "bare fence with content", input: "```\n{\"status\":\"ok\"}\n```", want: `{"status":"ok"}`},
		{name: "json-labelled fence strips label", input: "```json\n{\"status\":\"ok\"}\n```", want: `{"status":"ok"}`},
		{name: "JSON label is case-insensitive", input: "```JSON\n{\"status\":\"ok\"}\n```", want: `{"status":"ok"}`},
		{name: "fence surrounded by prose", input: "Here:\n```json\n{\"a\":1}\n```\nDone.", want: `{"a":1}`},
		{name: "first non-empty fence wins", input: "```json\n{\"first\":1}\n```\n```json\n{\"second\":2}\n```", want: `{"first":1}`},
		{name: "empty fence is skipped, next fence used", input: "```\n\n```\n```json\n{\"real\":1}\n```", want: `{"real":1}`},
		{name: "unterminated fence returns empty", input: "```json\n{\"status\":\"ok\"}", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFenceJSON(tt.input); got != tt.want {
				t.Fatalf("extractFenceJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecodeStreamEvents pins claudellm's strict decoder behaviour: the first
// malformed line aborts the whole stream and returns nil. This differs from
// codexllm's lenient decoder. ParseResult/ExtractFinalResponse rely on this:
// a partial trailing line therefore drops earlier events too.
func TestDecodeStreamEvents(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNil   bool
		wantCount int
		wantTypes []string
	}{
		{
			name:      "empty input returns empty slice",
			input:     "",
			wantCount: 0,
		},
		{
			name:      "whitespace-only returns empty slice",
			input:     "   \n\n\t\n",
			wantCount: 0,
		},
		{
			name:      "blank lines between events are skipped",
			input:     `{"type":"a"}` + "\n\n" + `{"type":"b"}` + "\n",
			wantCount: 2,
			wantTypes: []string{"a", "b"},
		},
		{
			name: "all valid events decoded",
			input: `{"type":"system"}` + "\n" +
				`{"type":"assistant"}` + "\n" +
				`{"type":"result"}`,
			wantCount: 3,
			wantTypes: []string{"system", "assistant", "result"},
		},
		{
			name:    "malformed line aborts decode entirely (returns nil)",
			input:   `{"type":"a"}` + "\n" + `not json` + "\n" + `{"type":"b"}`,
			wantNil: true,
		},
		{
			name:    "truncated trailing line aborts decode (returns nil)",
			input:   `{"type":"a"}` + "\n" + `{"type":"b","incomple`,
			wantNil: true,
		},
		{
			// Pinning a behaviour difference vs. codexllm, which explicitly
			// skips bare `null` lines. claudellm accepts them: json.Unmarshal
			// of `null` into a map[string]any succeeds with a nil map, and
			// the decoder appends it as an event. If this ever changes, the
			// `looksLikeEnvelope`/`extractStreamMissionResult` callers will
			// see one fewer (nil) entry and behaviour will shift silently.
			name:      "bare null line decodes to a nil-map event (NOT skipped, unlike codexllm)",
			input:     `null` + "\n" + `{"type":"a"}`,
			wantCount: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeStreamEvents(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("decodeStreamEvents = %+v, want nil on parse failure", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("decodeStreamEvents returned nil, want non-nil slice (len=%d)", tt.wantCount)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("decodeStreamEvents returned %d events, want %d: %+v", len(got), tt.wantCount, got)
			}
			for i, wantType := range tt.wantTypes {
				if gotType, _ := got[i]["type"].(string); gotType != wantType {
					t.Errorf("event[%d].type = %q, want %q", i, gotType, wantType)
				}
			}
		})
	}
}

// TestDecodeStreamEvents_NestedPayloadsPreserved guards against a regression
// where events decode but lose nested fields. Inspects every level so a
// decoder that preserved the outer slice but zeroed inner maps would fail.
func TestDecodeStreamEvents_NestedPayloadsPreserved(t *testing.T) {
	input := `{"type":"system","subtype":"init","model":"opus","cwd":"/tmp"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}` + "\n" +
		`{"type":"result","subtype":"success","result":"final","duration_ms":42}`
	got := decodeStreamEvents(input)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if model, _ := got[0]["model"].(string); model != "opus" {
		t.Errorf("event[0].model = %v, want opus", got[0]["model"])
	}
	if cwd, _ := got[0]["cwd"].(string); cwd != "/tmp" {
		t.Errorf("event[0].cwd = %v, want /tmp", got[0]["cwd"])
	}
	msg, ok := got[1]["message"].(map[string]any)
	if !ok {
		t.Fatalf("event[1].message not a map: %#v", got[1]["message"])
	}
	content, _ := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("event[1].message.content has %d items, want 1", len(content))
	}
	innerBlock, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("event[1].message.content[0] not a map: %#v", content[0])
	}
	if itype, _ := innerBlock["type"].(string); itype != "text" {
		t.Errorf("event[1].message.content[0].type = %v, want text (inner map must survive decode)", innerBlock["type"])
	}
	if itext, _ := innerBlock["text"].(string); itext != "hello" {
		t.Errorf("event[1].message.content[0].text = %v, want hello", innerBlock["text"])
	}
	if got[2]["result"] != "final" {
		t.Errorf("event[2].result = %v, want final", got[2]["result"])
	}
	if dur, _ := got[2]["duration_ms"].(float64); dur != 42 {
		t.Errorf("event[2].duration_ms = %v, want 42", got[2]["duration_ms"])
	}
}

// TestParseResult exercises the public ParseResult through the renamed
// helpers (decodeStreamEvents, extractFenceJSON, extractBalancedJSONObject).
// Each success case inspects Payload so a regression in the wrong helper
// returning the wrong object would fail the test, not silently pass.
func TestParseResult(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantStatus      string
		wantFormatError bool
		wantFailReason  string
		checkPayload    func(t *testing.T, payload map[string]any)
	}{
		{
			name:            "empty input is a format error",
			input:           "",
			wantStatus:      "failed",
			wantFormatError: true,
			wantFailReason:  "claude-code returned empty output",
		},
		{
			name:       "direct JSON object with succeeded status and result",
			input:      `{"status":"succeeded","result":{"answer":42}}`,
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["answer"].(float64); v != 42 {
					t.Errorf("payload[answer] = %v, want 42", payload["answer"])
				}
			},
		},
		{
			name:           "direct JSON object with failed status and reason",
			input:          `{"status":"failed","reason":"bad input"}`,
			wantStatus:     "failed",
			wantFailReason: "bad input",
		},
		{
			name:       "fenced JSON body is extracted via extractFenceJSON",
			input:      "```json\n{\"status\":\"succeeded\",\"result\":{\"ok\":true,\"kind\":\"fenced\"}}\n```",
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["ok"].(bool); !v {
					t.Errorf("payload[ok] = %v, want true", payload["ok"])
				}
				if v, _ := payload["kind"].(string); v != "fenced" {
					t.Errorf("payload[kind] = %v, want fenced (proves extractFenceJSON returned the right block)", payload["kind"])
				}
			},
		},
		{
			name:       "JSON in prose is extracted via extractBalancedJSONObject",
			input:      `Here you go: {"status":"succeeded","result":{"n":1,"kind":"prose"}} (done)`,
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["n"].(float64); v != 1 {
					t.Errorf("payload[n] = %v, want 1", payload["n"])
				}
				if v, _ := payload["kind"].(string); v != "prose" {
					t.Errorf("payload[kind] = %v, want prose (proves extractBalancedJSONObject returned the right object)", payload["kind"])
				}
			},
		},
		{
			name: "stream events: result event carries final JSON",
			input: `{"type":"system","subtype":"init"}` + "\n" +
				`{"type":"result","result":"{\"status\":\"succeeded\",\"result\":{\"kind\":\"stream\"}}"}`,
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["kind"].(string); v != "stream" {
					t.Errorf("payload[kind] = %v, want stream (proves the stream-result branch fired)", payload["kind"])
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseResult(tt.input)
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.FormatError != tt.wantFormatError {
				t.Errorf("FormatError = %v, want %v", got.FormatError, tt.wantFormatError)
			}
			if tt.wantFailReason != "" && got.FailureReason != tt.wantFailReason {
				t.Errorf("FailureReason = %q, want %q", got.FailureReason, tt.wantFailReason)
			}
			if tt.checkPayload != nil {
				if got.Payload == nil {
					t.Fatalf("Payload is nil; want a payload to inspect")
				}
				tt.checkPayload(t, got.Payload)
			}
		})
	}
}

// TestParseResult_StrictDecoderPropagatesThroughPublicAPI pins that the
// strict decoder behaviour (first malformed line → nil) propagates through
// ParseResult: a stream with a partial trailing line is treated as
// "no events at all", which falls through to parseMissionResult on the raw
// string and produces a format error. If the decoder ever started skipping
// bad lines instead, the earlier valid result event would be picked up and
// this test would fail — exactly the public-API drift codex flagged.
func TestParseResult_StrictDecoderPropagatesThroughPublicAPI(t *testing.T) {
	input := `{"type":"system","subtype":"init"}` + "\n" +
		`{"type":"result","result":"{\"status\":\"succeeded\",\"result\":{\"kind\":\"earlier\"}}"}` + "\n" +
		`{"type":"turn.completed","incomple`
	got := ParseResult(input)
	if got.Status != "failed" {
		t.Errorf("Status = %q, want failed (strict decoder must drop the whole stream on a partial trailing line)", got.Status)
	}
	if !got.FormatError {
		t.Errorf("FormatError = false, want true (no events → parseMissionResult on raw string → format error)")
	}
	if v, _ := got.Payload["kind"].(string); v == "earlier" {
		t.Errorf("Payload[kind] = %q, want NOT 'earlier' — strict decoder must NOT silently surface the prior result event", v)
	}
}

// TestExtractFinalResponse_StrictDecoderPropagatesThroughPublicAPI is the
// twin of the above: a partial trailing line drops earlier events, so
// ExtractFinalResponse falls back to the raw output. Pins the contract
// that downstream callers in apps/agent_manager rely on.
func TestExtractFinalResponse_StrictDecoderPropagatesThroughPublicAPI(t *testing.T) {
	input := `{"type":"result","result":"earlier answer"}` + "\n" + `{"type":"turn.completed","incomple`
	got := ExtractFinalResponse(input)
	if strings.Contains(got, "earlier answer") && !strings.Contains(got, "incomple") {
		t.Errorf("ExtractFinalResponse returned the earlier event %q without the malformed tail; the strict decoder must have dropped earlier events too", got)
	}
	if got != strings.TrimSpace(input) {
		t.Errorf("ExtractFinalResponse = %q, want raw input (trimmed) — strict decoder failure must fall through to raw fallback", got)
	}
}

// TestExtractFinalResponse exercises ExtractFinalResponse, which depends on
// decodeStreamEvents through extractStreamMissionResult.
func TestExtractFinalResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no stream events falls back to raw output",
			input: "plain text response",
			want:  "plain text response",
		},
		{
			name:  "result event text is returned",
			input: `{"type":"system","subtype":"init"}` + "\n" + `{"type":"result","result":"final answer"}`,
			want:  "final answer",
		},
		{
			name:  "fallback path trims whitespace",
			input: "   \n\tanswer\n  ",
			want:  "answer",
		},
		{
			name: "assistant text fallback when no result event",
			input: `{"type":"system","subtype":"init"}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"text","text":"from assistant"}]}}`,
			want: "from assistant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractFinalResponse(tt.input); got != tt.want {
				t.Fatalf("ExtractFinalResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFormatEventLog exercises FormatEventLog, which depends on
// decodeStreamEvents.
func TestFormatEventLog(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantEmpty      bool
		wantContainAll []string
	}{
		{name: "empty input returns empty", input: "", wantEmpty: true},
		{name: "unrecognised event types return empty", input: `{"type":"unknown"}`, wantEmpty: true},
		{
			name:           "system init renders selected fields",
			input:          `{"type":"system","subtype":"init","model":"opus","cwd":"/tmp"}`,
			wantContainAll: []string{"SYSTEM init", "model=opus", "cwd=/tmp"},
		},
		{
			name:           "assistant text section",
			input:          `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
			wantContainAll: []string{"ASSISTANT text", "hello"},
		},
		{
			name:           "result event carries result body",
			input:          `{"type":"result","subtype":"success","result":"final"}`,
			wantContainAll: []string{"RESULT success", "final"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatEventLog(tt.input)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("FormatEventLog() = %q, want empty", got)
				}
				return
			}
			for _, needle := range tt.wantContainAll {
				if !strings.Contains(got, needle) {
					t.Errorf("FormatEventLog() output missing %q; got %q", needle, got)
				}
			}
		})
	}
}

// TestResearchOutputStyleContent pins the renamed const's contract. The const
// is the body of a Claude Code output-style markdown file (NOT a --settings
// JSON file — the path to this file is what callers later embed in
// --settings). Pin the closing frontmatter delimiter, the
// keep-coding-instructions: false flag (this is the whole point of the file),
// and the body sentinel so a regression can't strip the body and pass.
func TestResearchOutputStyleContent(t *testing.T) {
	if !strings.HasPrefix(researchOutputStyleContent, "---\n") {
		t.Errorf("must start with opening frontmatter delimiter")
	}
	// Frontmatter must close before the body — two delimiters total.
	if strings.Count(researchOutputStyleContent, "\n---\n") < 1 {
		t.Errorf("missing closing frontmatter delimiter (---\\n on its own line after opener)")
	}
	if !strings.Contains(researchOutputStyleContent, "keep-coding-instructions: false") {
		t.Errorf("missing keep-coding-instructions: false — the entire purpose of the research style is to disable the SE prompt")
	}
	if !strings.Contains(researchOutputStyleContent, "Do not wrap JSON in markdown code fences") {
		t.Errorf("body sentinel missing — the prompt must instruct the model to emit raw JSON")
	}
}

// TestWriteResearchOutputStyle verifies the public writer round-trips the
// renamed const to a temp file on disk and returns a working cleanup. The
// returned path is the markdown style file itself (not a --settings file);
// downstream callers feed this path into --settings as
// {"outputStyle":"<path>"} (see apps/agent_manager/claude_code_runner.go).
func TestWriteResearchOutputStyle(t *testing.T) {
	path, cleanup, err := WriteResearchOutputStyle()
	if err != nil {
		t.Fatalf("WriteResearchOutputStyle: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != researchOutputStyleContent {
		t.Errorf("written content does not match researchOutputStyleContent — round-trip broken")
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("after cleanup, file %s still exists (err=%v); cleanup must remove the temp file", path, err)
	}
}
