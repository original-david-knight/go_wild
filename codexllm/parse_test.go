package codexllm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExtractBalancedJSONObject(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantJSON bool // whether result is valid JSON
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
			name:     "escaped backslash then quote (even-backslash run exits string)",
			input:    `{"a":"x\\","b":"}"}`,
			want:     `{"a":"x\\","b":"}"}`,
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
					t.Fatalf("expected result to be valid JSON, got error: %v", err)
				}
			}
		})
	}
}

// TestExtractBalancedJSONObject_NonSpecInputs pins exact behaviour under
// non-spec JSON forms. These inputs are not expected in Codex output, but
// the tests document what the heuristic actually does today so regressions
// are visible. In particular, the "silent wrong extraction" cases below are
// NOT safe failures — they decode as valid JSON from the wrong region.
func TestExtractBalancedJSONObject_NonSpecInputs(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		want           string
		wantValidJSON  bool // true = json.Unmarshal succeeds on `want`
		silentWrongDoc string
	}{
		{
			name:           "block comment wrapping a JSON object — silent wrong extraction",
			input:          `/* {"status":"ok"} */ noise`,
			want:           `{"status":"ok"}`,
			wantValidJSON:  true,
			silentWrongDoc: "content from inside a comment is returned and accepted as valid JSON",
		},
		{
			name:           "prose example before real response — silent wrong extraction",
			input:          "example: " + `{"foo":1}` + "\nactual: unavailable",
			want:           `{"foo":1}`,
			wantValidJSON:  true,
			silentWrongDoc: "earlier illustrative object wins over later prose",
		},
		{
			name:          "single-quoted string with embedded brace — bracket miscount, fails Unmarshal",
			input:         `{"key": 'value with } inside'}`,
			want:          `{"key": 'value with }`,
			wantValidJSON: false,
		},
		{
			name:          "template literal with embedded ${ and brace — bracket miscount, fails Unmarshal",
			input:         "prefix `tpl ${ {\"status\":\"ok\"} }` suffix",
			want:          `{ {"status":"ok"} }`,
			wantValidJSON: false,
		},
		{
			name:          "block comment inside object — bracket miscount, fails Unmarshal",
			input:         `{/* has } in comment */"key":"v"}`,
			want:          `{/* has }`,
			wantValidJSON: false,
		},
		{
			name:          "line comment with trailing brace — bracket miscount, fails Unmarshal",
			input:         `{"key":"v" // trailing } comment` + "\n}",
			want:          `{"key":"v" // trailing }`,
			wantValidJSON: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBalancedJSONObject(tt.input)
			if got != tt.want {
				t.Fatalf("extractBalancedJSONObject() = %q, want %q", got, tt.want)
			}
			var v any
			err := json.Unmarshal([]byte(got), &v)
			gotValid := err == nil
			if gotValid != tt.wantValidJSON {
				t.Fatalf("json.Unmarshal validity = %v, want %v (err=%v)", gotValid, tt.wantValidJSON, err)
			}
		})
	}
}

func TestDecodeStreamEvents(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantTypes []string // "type" field of each decoded event in order
	}{
		{
			name:      "empty input returns empty slice",
			input:     "",
			wantCount: 0,
		},
		{
			name:      "whitespace-only input returns empty slice",
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
			name:      "all valid events decoded",
			input:     `{"type":"thread.started"}` + "\n" + `{"type":"item.completed"}` + "\n" + `{"type":"turn.completed"}`,
			wantCount: 3,
			wantTypes: []string{"thread.started", "item.completed", "turn.completed"},
		},
		{
			name:      "malformed line in middle is skipped, earlier and later events preserved",
			input:     `{"type":"a"}` + "\n" + `not json at all` + "\n" + `{"type":"b"}`,
			wantCount: 2,
			wantTypes: []string{"a", "b"},
		},
		{
			name:      "truncated final line is skipped, earlier events preserved",
			input:     `{"type":"a"}` + "\n" + `{"type":"b"}` + "\n" + `{"type":"c", "incomple`,
			wantCount: 2,
			wantTypes: []string{"a", "b"},
		},
		{
			name:      "malformed first line does not poison subsequent valid events",
			input:     `partial {garbage` + "\n" + `{"type":"a"}` + "\n" + `{"type":"b"}`,
			wantCount: 2,
			wantTypes: []string{"a", "b"},
		},
		{
			name:      "all malformed lines return empty slice, not nil",
			input:     "garbage\nmore garbage\nstill bad",
			wantCount: 0,
		},
		{
			name:      "non-object JSON lines (null, number, string, array) are skipped",
			input:     `null` + "\n" + `123` + "\n" + `"bare string"` + "\n" + `[1,2,3]` + "\n" + `{"type":"real"}`,
			wantCount: 1,
			wantTypes: []string{"real"},
		},
		{
			name:      "multiple interleaved malformed lines are all skipped",
			input:     `{"type":"a"}` + "\n" + `junk1` + "\n" + `{"type":"b"}` + "\n" + `junk2` + "\n" + `junk3` + "\n" + `{"type":"c"}` + "\n" + `junk4`,
			wantCount: 3,
			wantTypes: []string{"a", "b", "c"},
		},
		{
			name:      "CRLF line endings are handled (codex CLI on Windows or piped through tools that rewrite newlines)",
			input:     `{"type":"a"}` + "\r\n" + `{"type":"b"}` + "\r\n" + `{"type":"c"}` + "\r\n",
			wantCount: 3,
			wantTypes: []string{"a", "b", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeStreamEvents(tt.input)
			if got == nil {
				t.Fatalf("DecodeStreamEvents returned nil; expected non-nil slice (even if empty) so callers can distinguish 'no events' from 'parse error'")
			}
			if len(got) != tt.wantCount {
				t.Fatalf("DecodeStreamEvents returned %d events, want %d: %+v", len(got), tt.wantCount, got)
			}
			for i, wantType := range tt.wantTypes {
				gotType, _ := got[i]["type"].(string)
				if gotType != wantType {
					t.Errorf("event[%d].type = %q, want %q", i, gotType, wantType)
				}
			}
		})
	}
}

// TestDecodeStreamEvents_NestedPayloadsPreserved guards against a regression
// where events decode but lose nested fields. The type-only assertions in
// TestDecodeStreamEvents would still pass if, for example, the decoder
// re-marshalled through a typed struct that dropped unknown keys.
func TestDecodeStreamEvents_NestedPayloadsPreserved(t *testing.T) {
	input := `{"type":"thread.started","thread_id":"t-42"}` + "\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":123,"output_tokens":45}}`
	got := decodeStreamEvents(input)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if tid, _ := got[0]["thread_id"].(string); tid != "t-42" {
		t.Errorf("event[0].thread_id = %v, want t-42", got[0]["thread_id"])
	}
	item, ok := got[1]["item"].(map[string]any)
	if !ok {
		t.Fatalf("event[1].item not a map: %#v", got[1]["item"])
	}
	if text, _ := item["text"].(string); text != "hello" {
		t.Errorf("event[1].item.text = %v, want hello", item["text"])
	}
	usage, ok := got[2]["usage"].(map[string]any)
	if !ok {
		t.Fatalf("event[2].usage not a map: %#v", got[2]["usage"])
	}
	if v, _ := usage["input_tokens"].(float64); v != 123 {
		t.Errorf("event[2].usage.input_tokens = %v, want 123", usage["input_tokens"])
	}
}

// TestDecodeStreamEvents_ExtractFinalResponseSurvivesTrailingGarbage verifies
// that a malformed tail line does not swallow the agent_message event that
// extractCodexStreamResult (and therefore ExtractFinalResponse) needs.
func TestDecodeStreamEvents_ExtractFinalResponseSurvivesTrailingGarbage(t *testing.T) {
	input := `{"type":"thread.started","thread_id":"t1"}` + "\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"final answer"}}` + "\n" +
		`{"type":"turn.completed","usage":{"in`
	got := ExtractFinalResponse(input)
	if got != "final answer" {
		t.Fatalf("ExtractFinalResponse = %q, want %q — malformed trailing line should not suppress the agent_message", got, "final answer")
	}
}

// TestExtractBalancedJSONObject_FirstBalancedWins pins the known limitation
// that the scanner returns on the first balanced `{...}` region even if it
// is malformed — later valid JSON in the same string is not reached.
// decodeJSONMap does not re-try ExtractBalancedJSONObject, so the valid
// object is effectively lost.
func TestExtractBalancedJSONObject_FirstBalancedWins(t *testing.T) {
	input := `prefix {not json} {"real":1}`
	want := `{not json}`
	if got := extractBalancedJSONObject(input); got != want {
		t.Fatalf("extractBalancedJSONObject() = %q, want %q (pinning first-balanced-wins)", got, want)
	}
	// The valid later object {"real":1} is NOT returned. If this assertion
	// ever fails because the scanner was improved, update the comment on
	// ExtractBalancedJSONObject to reflect the new behaviour.
}

func TestExtractFenceJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "no fence returns empty",
			input: `{"status":"ok"}`,
			want:  "",
		},
		{
			name:  "bare fence with content",
			input: "```\n{\"status\":\"ok\"}\n```",
			want:  `{"status":"ok"}`,
		},
		{
			name:  "json-labelled fence strips label",
			input: "```json\n{\"status\":\"ok\"}\n```",
			want:  `{"status":"ok"}`,
		},
		{
			name:  "JSON-labelled fence is case-insensitive",
			input: "```JSON\n{\"status\":\"ok\"}\n```",
			want:  `{"status":"ok"}`,
		},
		{
			name:  "fence with surrounding prose",
			input: "Here is the output:\n```json\n{\"a\":1}\n```\nDone.",
			want:  `{"a":1}`,
		},
		{
			name:  "first non-empty fence wins when multiple fences present",
			input: "```json\n{\"first\":1}\n```\n```json\n{\"second\":2}\n```",
			want:  `{"first":1}`,
		},
		{
			name:  "empty fence block is skipped, next fence used",
			input: "```\n\n```\n```json\n{\"real\":1}\n```",
			want:  `{"real":1}`,
		},
		{
			// Pins known edge case: trimLeadingJSONLabel only strips "json"
			// when followed by whitespace and len > 4, so a labelled fence
			// whose body is just the label plus a newline ends up returning
			// the bare "json" string. Downstream json.Unmarshal fails on it,
			// which is acceptable (decodeJSONMap just moves on), but if the
			// scanner ever starts skipping such blocks this test fails and
			// the comment on trimLeadingJSONLabel should be updated.
			name:  "labelled empty fence returns bare 'json' (pinned edge case)",
			input: "```json\n```",
			want:  "json",
		},
		{
			name:  "unterminated fence returns empty",
			input: "```json\n{\"status\":\"ok\"}",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFenceJSON(tt.input)
			if got != tt.want {
				t.Fatalf("extractFenceJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

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
			name:            "empty output is a format error",
			input:           "",
			wantStatus:      "failed",
			wantFormatError: true,
			wantFailReason:  "codex returned empty output",
		},
		{
			name:            "whitespace-only output is a format error",
			input:           "   \n\t  ",
			wantStatus:      "failed",
			wantFormatError: true,
			wantFailReason:  "codex returned empty output",
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
			name:            "direct JSON object with invalid status",
			input:           `{"status":"unknown","result":{}}`,
			wantStatus:      "failed",
			wantFormatError: true,
		},
		{
			name:            "direct JSON object missing status falls through to mission parse",
			input:           `{"result":{"a":1}}`,
			wantStatus:      "failed",
			wantFormatError: true,
		},
		{
			name:            "succeeded without result field is a format error",
			input:           `{"status":"succeeded"}`,
			wantStatus:      "failed",
			wantFormatError: true,
		},
		{
			name:            "succeeded with non-object result is a format error",
			input:           `{"status":"succeeded","result":"not an object"}`,
			wantStatus:      "failed",
			wantFormatError: true,
		},
		{
			name:       "fenced JSON body is extracted",
			input:      "```json\n{\"status\":\"succeeded\",\"result\":{\"ok\":true}}\n```",
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["ok"].(bool); !v {
					t.Errorf("payload[ok] = %v, want true", payload["ok"])
				}
			},
		},
		{
			name:       "JSON embedded in prose is extracted via balanced scanner",
			input:      `Here you go: {"status":"succeeded","result":{"n":1}} (done)`,
			wantStatus: "succeeded",
		},
		{
			name: "stream events: agent_message carrying JSON result wins",
			input: `{"type":"thread.started","thread_id":"t1"}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":"{\"status\":\"succeeded\",\"result\":{\"kind\":\"stream\"}}"}}` + "\n" +
				`{"type":"turn.completed","usage":{}}`,
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["kind"].(string); v != "stream" {
					t.Errorf("payload[kind] = %v, want stream", payload["kind"])
				}
			},
		},
		{
			name:            "non-JSON plain text is a format error",
			input:           "just a prose reply from the model",
			wantStatus:      "failed",
			wantFormatError: true,
		},
		{
			name:           "failed with error.message object sets failure reason",
			input:          `{"status":"failed","error":{"message":"tool crashed"}}`,
			wantStatus:     "failed",
			wantFailReason: "tool crashed",
		},
		{
			name:       "result is a raw JSON string, decoded via decodeJSONMap",
			input:      `{"status":"succeeded","result":"{\"kind\":\"str-result\"}"}`,
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["kind"].(string); v != "str-result" {
					t.Errorf("payload[kind] = %v, want str-result", payload["kind"])
				}
			},
		},
		{
			name:       "result is a fenced JSON string, decoded via ExtractFenceJSON fallback",
			input:      "{\"status\":\"succeeded\",\"result\":\"```json\\n{\\\"kind\\\":\\\"fenced\\\"}\\n```\"}",
			wantStatus: "succeeded",
			checkPayload: func(t *testing.T, payload map[string]any) {
				if v, _ := payload["kind"].(string); v != "fenced" {
					t.Errorf("payload[kind] = %v, want fenced", payload["kind"])
				}
			},
		},
		{
			name:           "failed with reason nested inside result payload",
			input:          `{"status":"failed","result":{"reason":"nested reason"}}`,
			wantStatus:     "failed",
			wantFailReason: "nested reason",
		},
		{
			name:           "failed with error.message nested inside result payload",
			input:          `{"status":"failed","result":{"error":{"message":"nested crash"}}}`,
			wantStatus:     "failed",
			wantFailReason: "nested crash",
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

func TestExtractFinalResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "no stream events falls back to raw output",
			input: "plain text response",
			want:  "plain text response",
		},
		{
			name: "agent_message text is returned",
			input: `{"type":"thread.started","thread_id":"t1"}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":"final answer"}}` + "\n" +
				`{"type":"turn.completed","usage":{}}`,
			want: "final answer",
		},
		{
			name: "last agent_message wins when multiple present",
			input: `{"type":"item.completed","item":{"type":"agent_message","text":"first"}}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":"second"}}`,
			want: "second",
		},
		{
			name: "non-agent_message items are ignored",
			input: `{"type":"item.completed","item":{"type":"reasoning","text":"ignored"}}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":"kept"}}`,
			want: "kept",
		},
		{
			name:  "stream events with no agent_message falls back to raw output",
			input: `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.completed","usage":{}}`,
			want:  `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.completed","usage":{}}`,
		},
		{
			name:  "fallback path trims surrounding whitespace",
			input: "   \n\tplain text response\n  ",
			want:  "plain text response",
		},
		{
			// extractCodexStreamResult walks backward and requires the text to
			// be non-empty, so a trailing empty agent_message is skipped in
			// favour of the earlier non-empty one.
			name: "final empty agent_message is skipped; earlier non-empty wins",
			input: `{"type":"item.completed","item":{"type":"agent_message","text":"earlier"}}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":""}}`,
			want: "earlier",
		},
		{
			// All agent_messages empty → no result → fall through to raw.
			name: "all agent_messages empty falls back to raw output",
			input: `{"type":"item.completed","item":{"type":"agent_message","text":""}}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":""}}`,
			want: `{"type":"item.completed","item":{"type":"agent_message","text":""}}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":""}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFinalResponse(tt.input)
			if got != tt.want {
				t.Fatalf("ExtractFinalResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatEventLog(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantEmpty      bool
		wantContainAll []string
	}{
		{
			name:      "empty input returns empty string",
			input:     "",
			wantEmpty: true,
		},
		{
			name:      "no recognised event types returns empty string",
			input:     `{"type":"unknown"}`,
			wantEmpty: true,
		},
		{
			name:           "thread.started includes thread id",
			input:          `{"type":"thread.started","thread_id":"abc123"}`,
			wantContainAll: []string{"THREAD started", "abc123"},
		},
		{
			name:           "item.completed shows item type and text",
			input:          `{"type":"item.completed","item":{"type":"agent_message","text":"hello there"}}`,
			wantContainAll: []string{"ITEM agent_message", "hello there"},
		},
		{
			name:           "error event surfaces message",
			input:          `{"type":"error","message":"something broke"}`,
			wantContainAll: []string{"ERROR", "something broke"},
		},
		{
			name:           "turn.failed surfaces error.message",
			input:          `{"type":"turn.failed","error":{"message":"upstream timeout"}}`,
			wantContainAll: []string{"TURN FAILED", "upstream timeout"},
		},
		{
			name: "multiple events are joined with a blank line (double newline) between sections",
			input: `{"type":"thread.started","thread_id":"t1"}` + "\n" +
				`{"type":"item.completed","item":{"type":"agent_message","text":"answer"}}`,
			wantContainAll: []string{"THREAD started", "t1", "ITEM agent_message", "answer", "\n\n"},
		},
		{
			name:           "turn.completed renders usage body via formatLogBody",
			input:          `{"type":"turn.completed","usage":{"input_tokens":123,"output_tokens":45}}`,
			wantContainAll: []string{"TURN completed", "input_tokens", "123", "output_tokens", "45"},
		},
		{
			name:           "turn.completed with nil usage still emits title",
			input:          `{"type":"turn.completed"}`,
			wantContainAll: []string{"TURN completed"},
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

func TestFormatEventLog_TruncatesLongItemText(t *testing.T) {
	const inputLen = 600
	const keepLen = 500 // matches parse.go FormatEventLog truncation threshold
	long := strings.Repeat("x", inputLen)
	event := map[string]any{
		"type": "item.completed",
		"item": map[string]any{"type": "agent_message", "text": long},
	}
	blob, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := FormatEventLog(string(blob))
	// Exact body shape: title "ITEM agent_message", newline, 500 x's, then "...".
	wantBody := strings.Repeat("x", keepLen) + "..."
	if !strings.HasSuffix(got, wantBody) {
		t.Errorf("expected output to end with %d x's + '...'; got suffix %q",
			keepLen, got[max(0, len(got)-(keepLen+3)):])
	}
	// Guard the truncation threshold itself: dropping exactly one x would still
	// end in "x...", so we also check the middle character at position keepLen-1
	// from the end of the x-run is still x and position keepLen+0 is '.'.
	// A simpler, equivalent pin: the full length equals title + "\n" + 500 + "...".
	wantLen := len("ITEM agent_message") + 1 + keepLen + 3
	if len(got) != wantLen {
		t.Errorf("formatted length = %d, want %d (title + newline + %d body chars + '...')",
			len(got), wantLen, keepLen)
	}
}

func TestBuildFailureArtifacts(t *testing.T) {
	t.Run("nil error and empty output produces minimal payloads", func(t *testing.T) {
		result, errPayload := BuildFailureArtifacts("", nil)
		if result["status"] != "failed" {
			t.Errorf("result.status = %v, want failed", result["status"])
		}
		if result["failure_reason"] != "" {
			t.Errorf("result.failure_reason = %v, want empty string", result["failure_reason"])
		}
		if _, has := result["stdout"]; has {
			t.Errorf("result should not have stdout key when output empty")
		}
		if _, has := result["stderr"]; has {
			t.Errorf("result should not have stderr key when unset")
		}
		if _, has := result["exit_code"]; has {
			t.Errorf("result should not have exit_code key when zero")
		}
		if errPayload["message"] != "" {
			t.Errorf("errPayload.message = %v, want empty string", errPayload["message"])
		}
	})

	t.Run("non-ExecutionError with raw output populates stdout and raw_output", func(t *testing.T) {
		// Uses a bare errors.New so the *ExecutionError type-assertion branch
		// is skipped — this exercises the plain-error path in BuildFailureArtifacts.
		output := "some raw stdout"
		err := errors.New("codex exited non-zero")
		result, errPayload := BuildFailureArtifacts(output, err)
		if result["failure_reason"] != "codex exited non-zero" {
			t.Errorf("failure_reason = %v", result["failure_reason"])
		}
		if result["stdout"] != output {
			t.Errorf("stdout = %v, want %q", result["stdout"], output)
		}
		if result["raw_output"] != output {
			t.Errorf("raw_output = %v, want %q", result["raw_output"], output)
		}
		if _, has := result["stderr"]; has {
			t.Errorf("stderr should be absent for non-ExecutionError")
		}
		if _, has := result["exit_code"]; has {
			t.Errorf("exit_code should be absent for non-ExecutionError")
		}
		if errPayload["stdout"] != output {
			t.Errorf("errPayload.stdout = %v, want %q", errPayload["stdout"], output)
		}
		if errPayload["message"] != "codex exited non-zero" {
			t.Errorf("errPayload.message = %v", errPayload["message"])
		}
	})

	t.Run("ExecutionError fields are propagated", func(t *testing.T) {
		execErr := &ExecutionError{
			Message:  "boom",
			ExitCode: 42,
			Stdout:   "fallback stdout",
			Stderr:   "stderr content",
		}
		result, errPayload := BuildFailureArtifacts("", execErr)
		if result["stdout"] != "fallback stdout" {
			t.Errorf("result.stdout = %v, want fallback stdout", result["stdout"])
		}
		if result["stderr"] != "stderr content" {
			t.Errorf("result.stderr = %v, want stderr content", result["stderr"])
		}
		if result["exit_code"] != 42 {
			t.Errorf("result.exit_code = %v, want 42", result["exit_code"])
		}
		if errPayload["exit_code"] != 42 {
			t.Errorf("errPayload.exit_code = %v, want 42", errPayload["exit_code"])
		}
		if errPayload["stderr"] != "stderr content" {
			t.Errorf("errPayload.stderr = %v", errPayload["stderr"])
		}
	})

	t.Run("caller-provided output takes precedence over ExecutionError.Stdout", func(t *testing.T) {
		execErr := &ExecutionError{Message: "x", Stdout: "never used"}
		result, _ := BuildFailureArtifacts("caller output", execErr)
		if result["stdout"] != "caller output" {
			t.Errorf("stdout = %v, want caller output (caller-provided output takes precedence)", result["stdout"])
		}
	})

	t.Run("event log is attached when stdout contains decodable JSONL", func(t *testing.T) {
		stream := `{"type":"thread.started","thread_id":"t1"}` + "\n" +
			`{"type":"error","message":"bad"}`
		result, _ := BuildFailureArtifacts(stream, &ExecutionError{Message: "failed"})
		log, ok := result["event_log"].(string)
		if !ok {
			t.Fatalf("event_log missing or not a string: %#v", result["event_log"])
		}
		if !strings.Contains(log, "THREAD started") || !strings.Contains(log, "ERROR") {
			t.Errorf("event_log missing expected sections: %q", log)
		}
	})

	t.Run("event_log key omitted when stdout has no recognised events", func(t *testing.T) {
		result, _ := BuildFailureArtifacts("plain prose", &ExecutionError{Message: "failed"})
		if _, has := result["event_log"]; has {
			t.Errorf("event_log should be omitted when no events present")
		}
	})
}

// TestExecutionErrorError pins the two branches of ExecutionError.Error():
// nil receiver returns "" (callers may hold a typed-nil and still call Error),
// and a non-nil receiver returns the Message with surrounding whitespace
// trimmed so a message with a trailing newline doesn't render as two lines
// when concatenated with other error text.
func TestExecutionErrorError(t *testing.T) {
	var nilErr *ExecutionError
	if got := nilErr.Error(); got != "" {
		t.Errorf("(*ExecutionError)(nil).Error() = %q, want empty", got)
	}
	trimmed := (&ExecutionError{Message: "  boom\n"}).Error()
	if trimmed != "boom" {
		t.Errorf("Error() = %q, want %q (leading/trailing whitespace must be trimmed)", trimmed, "boom")
	}
}

func TestTrimLeadingJSONLabel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"shorter than label (len 3) returns unchanged", "jso", "jso"},
		{"exactly four chars with no separator returns unchanged", "json", "json"},
		{"json label followed by newline strips label", "json\n{\"a\":1}", `{"a":1}`},
		{"json label followed by space strips label", `json {"a":1}`, `{"a":1}`},
		{"json label followed by tab strips label", "json\t{\"a\":1}", `{"a":1}`},
		{"json label followed by CR strips label", "json\r{\"a\":1}", `{"a":1}`},
		{"JSON label is case-insensitive", "JSON\n{\"a\":1}", `{"a":1}`},
		{"json-prefixed identifier is NOT stripped (no whitespace separator)", "jsonified", "jsonified"},
		{"non-json prefix returns unchanged", "yaml\n{\"a\":1}", "yaml\n{\"a\":1}"},
		{"leading whitespace is trimmed before label check", "   json\n{\"a\":1}", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimLeadingJSONLabel(tt.in); got != tt.want {
				t.Errorf("trimLeadingJSONLabel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestInvalidResultEmptyReasonFallsBackToDefault(t *testing.T) {
	const wantReason = "codex returned a malformed final response"
	got := invalidResult("   \n  ") // all whitespace → trimmed to empty → fallback
	if got.FailureReason != wantReason {
		t.Errorf("FailureReason = %q, want %q", got.FailureReason, wantReason)
	}
	if v, _ := got.Payload["reason"].(string); v != wantReason {
		t.Errorf("Payload[reason] = %v, want %q", got.Payload["reason"], wantReason)
	}
	if got.Status != "failed" {
		t.Errorf("Status = %q, want failed", got.Status)
	}
	if !got.FormatError {
		t.Errorf("FormatError = false, want true")
	}
}

// TestCloneResultMapEmpty pins the empty-input branch: nil and empty maps
// both return a non-nil empty map so callers can unconditionally write into
// the result without a nil-guard.
func TestCloneResultMapEmpty(t *testing.T) {
	for name, in := range map[string]map[string]any{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			got := cloneResultMap(in)
			if got == nil {
				t.Errorf("cloneResultMap(%s) returned nil; want non-nil empty map", name)
			}
			if len(got) != 0 {
				t.Errorf("cloneResultMap(%s) returned %d entries; want 0", name, len(got))
			}
			got["new"] = 1 // must be writable
			if len(got) != 1 {
				t.Errorf("returned map not writable")
			}
		})
	}
}

// TestCloneResultMapIsShallowCopy pins that cloneResultMap is a shallow copy.
// Top-level keys can be mutated on the clone without affecting the input,
// but nested containers are shared by reference — callers relying on deep
// isolation must copy nested maps themselves.
func TestCloneResultMapIsShallowCopy(t *testing.T) {
	nested := map[string]any{"k": "v"}
	in := map[string]any{"a": 1, "nested": nested}
	out := cloneResultMap(in)

	out["a"] = 999
	if in["a"] != 1 {
		t.Errorf("top-level mutation leaked back to input: in[a] = %v", in["a"])
	}

	outNested, _ := out["nested"].(map[string]any)
	outNested["k"] = "mutated"
	if nested["k"] != "mutated" {
		t.Errorf("nested map not shared (expected shallow copy semantics)")
	}
}

func TestExtractFailureReasonPrefersReasonOverMessage(t *testing.T) {
	in := map[string]any{"reason": "first", "message": "second", "error": "third"}
	if got := extractFailureReason(in); got != "first" {
		t.Errorf("extractFailureReason = %q, want 'first' (reason outranks message and error)", got)
	}
}

// TestExtractFailureReasonMessageOutranksError pins the second-rank slot in
// the `[]string{"reason", "message"}` iteration: when `reason` is absent,
// `message` wins over `error` rather than falling through to the error block.
func TestExtractFailureReasonMessageOutranksError(t *testing.T) {
	in := map[string]any{"message": "from message", "error": "from error"}
	if got := extractFailureReason(in); got != "from message" {
		t.Errorf("extractFailureReason = %q, want 'from message' (message outranks error when reason is absent)", got)
	}
}

func TestExtractFailureReasonErrorAsString(t *testing.T) {
	// Covers the `case string:` branch in the error type switch — the map
	// has no `reason`/`message`, so the error string is the only source.
	got := extractFailureReason(map[string]any{"error": "  crash detail  "})
	if got != "crash detail" {
		t.Errorf("extractFailureReason = %q, want 'crash detail' (trimmed)", got)
	}
}

func TestExtractFailureReasonAllEmptyReturnsEmpty(t *testing.T) {
	in := map[string]any{
		"reason":  "   ",
		"message": "",
		"error":   map[string]any{"message": "  "},
	}
	if got := extractFailureReason(in); got != "" {
		t.Errorf("extractFailureReason = %q, want empty when all candidate fields are blank", got)
	}
}

func TestExtractFailureReasonIgnoresNonStringErrorShapes(t *testing.T) {
	// error is neither string nor map → the switch has no matching case
	// and the function falls through to return "".
	got := extractFailureReason(map[string]any{"error": 42})
	if got != "" {
		t.Errorf("extractFailureReason = %q, want empty for non-string/non-map error value", got)
	}
}

// parseMissionResult is exercised through ParseResult in TestParseResult, but
// the direct-map path, the empty-string path, and the unknown-type default
// branch have no observable route through the public API. Pinning them here
// keeps each branch's contract visible.
func TestParseMissionResultDirectMap(t *testing.T) {
	in := map[string]any{"status": "succeeded", "result": map[string]any{"x": 1}}
	got := parseMissionResult(in)
	if got.Status != "succeeded" {
		t.Errorf("Status = %q, want succeeded", got.Status)
	}
	// Accept either int or float64: the current implementation preserves int
	// because there is no JSON round-trip on this path, but a future refactor
	// that normalizes numbers to float64 is an externally-invisible change
	// (JSON numbers always decode as float64 for callers that enter via
	// ParseResult). Assert only that the value round-trips numerically.
	switch v := got.Payload["x"].(type) {
	case int:
		if v != 1 {
			t.Errorf("Payload[x] (int) = %d, want 1", v)
		}
	case float64:
		if v != 1 {
			t.Errorf("Payload[x] (float64) = %v, want 1", v)
		}
	default:
		t.Errorf("Payload[x] = %v (type %T), want numeric 1", got.Payload["x"], got.Payload["x"])
	}
}

func TestParseMissionResultEmptyString(t *testing.T) {
	got := parseMissionResult("   \n\t  ")
	if !got.FormatError {
		t.Errorf("FormatError = false, want true for empty-string input")
	}
	if got.FailureReason != "codex returned empty output" {
		t.Errorf("FailureReason = %q, want 'codex returned empty output'", got.FailureReason)
	}
}

func TestParseMissionResultNonJSONString(t *testing.T) {
	// Non-empty, non-JSON string hits the `decodeJSONMap` failure branch.
	got := parseMissionResult("not json at all")
	if !got.FormatError {
		t.Errorf("FormatError = false, want true for non-JSON string")
	}
	if got.FailureReason != "codex returned a non-JSON final response" {
		t.Errorf("FailureReason = %q, want 'codex returned a non-JSON final response'", got.FailureReason)
	}
}

func TestParseMissionResultUnknownType(t *testing.T) {
	// bool triggers the default branch of the type switch. No public caller
	// passes non-string/non-map values today, but the branch exists so
	// future refactors that widen the input type fail closed.
	got := parseMissionResult(true)
	if !got.FormatError {
		t.Errorf("FormatError = false, want true for unknown-type input")
	}
	if got.FailureReason != "codex returned a malformed final response" {
		t.Errorf("FailureReason = %q, want 'codex returned a malformed final response'", got.FailureReason)
	}
}

// TestDecodeJSONMapAcceptsLabelPrefixedJSON pins that a "json\n" prefix does
// not block decoding. It exercises the label-trim branch at parse.go:279 for
// coverage purposes, but it does NOT prove the label-trim candidate was the
// one that won: the balanced-scan fallback also returns the same {"status":"ok"}
// object for this input, so either candidate would succeed. The label-trim
// branch's own contract is pinned directly in TestTrimLeadingJSONLabel; this
// test's sole job is to guard the end-to-end "json"-labelled-input path.
func TestDecodeJSONMapAcceptsLabelPrefixedJSON(t *testing.T) {
	in := "json\n" + `{"status":"ok"}`
	got, ok := decodeJSONMap(in)
	if !ok {
		t.Fatalf("decodeJSONMap returned ok=false; want true")
	}
	if got["status"] != "ok" {
		t.Errorf("got[status] = %v, want ok", got["status"])
	}
}

func TestDecodeJSONMapWhitespaceOnlyReturnsFalse(t *testing.T) {
	// Covers the empty-candidate skip: the only candidate is the trimmed
	// input (empty string), which the loop skips, yielding (nil, false).
	got, ok := decodeJSONMap("   \n\t  ")
	if ok || got != nil {
		t.Errorf("decodeJSONMap(whitespace) = (%v, %v); want (nil, false)", got, ok)
	}
}

func TestDecodeJSONMapFallsThroughAllCandidatesOnInvalidJSON(t *testing.T) {
	// "json\n" label + non-JSON body. Every candidate (original, label-trimmed,
	// fence-extracted=empty, balanced-extracted=empty) fails to unmarshal.
	if _, ok := decodeJSONMap("json\nstill not json"); ok {
		t.Errorf("decodeJSONMap accepted input where no candidate was valid JSON")
	}
}

// TestExtractCodexStreamResultSkipsItemWithoutMap pins the defensive branch
// where an item.completed event's `item` field is not a map[string]any. The
// backward walker must skip it and continue looking for a valid
// agent_message rather than bail — this branch would only fire if Codex
// (or a proxy between us and Codex) emits a malformed item payload.
func TestExtractCodexStreamResultSkipsItemWithoutMap(t *testing.T) {
	// Ordering note: walker goes BACKWARD. Put the malformed event AFTER
	// the valid one so the walker hits the malformed event first and must
	// skip it to reach the valid agent_message.
	input := `{"type":"item.completed","item":{"type":"agent_message","text":"final"}}` + "\n" +
		`{"type":"item.completed","item":"not a map"}`
	got := ExtractFinalResponse(input)
	if got != "final" {
		t.Errorf("ExtractFinalResponse = %q, want 'final' (malformed item must not suppress valid earlier agent_message)", got)
	}
}

func TestFormatLogBodyDirect(t *testing.T) {
	t.Run("nil returns empty", func(t *testing.T) {
		if got := formatLogBody(nil); got != "" {
			t.Errorf("formatLogBody(nil) = %q, want empty", got)
		}
	})
	t.Run("string is trimmed", func(t *testing.T) {
		if got := formatLogBody("  trimmed\n"); got != "trimmed" {
			t.Errorf("formatLogBody(string) = %q, want 'trimmed'", got)
		}
	})
	t.Run("map renders as indented JSON", func(t *testing.T) {
		got := formatLogBody(map[string]any{"k": 1})
		if !strings.Contains(got, `"k": 1`) {
			t.Errorf("formatLogBody(map) = %q, want output containing indented JSON", got)
		}
	})
	t.Run("unmarshalable value falls back to fmt.Sprint", func(t *testing.T) {
		// json.MarshalIndent returns UnsupportedTypeError for channels; the
		// function must then fall back to strings.TrimSpace(fmt.Sprint(v)).
		// Pin the exact string rather than just non-empty so a regression to
		// any placeholder (e.g., returning "" or a literal "<error>") fails
		// visibly. fmt.Sprint on a chan prints its runtime pointer, so the
		// expected value is computed from the same value rather than hardcoded.
		ch := make(chan int)
		want := strings.TrimSpace(fmt.Sprint(ch))
		if got := formatLogBody(ch); got != want {
			t.Errorf("formatLogBody(chan) = %q, want %q", got, want)
		}
	})
}

func TestAppendLogSectionNilBuilderIsNoOp(t *testing.T) {
	// The nil-builder guard is there for callers that pass a freshly-declared
	// pointer they forgot to initialise — this must not panic.
	appendLogSection(nil, "title", "body")
}

func TestAppendLogSectionEmptyTitleAndBodyIsNoOp(t *testing.T) {
	var sb strings.Builder
	appendLogSection(&sb, "  ", "\t\n")
	if sb.Len() != 0 {
		t.Errorf("builder written when both title and body empty after trim: %q", sb.String())
	}
}

func TestAppendLogSectionTitleAndBodyCombinations(t *testing.T) {
	t.Run("title only", func(t *testing.T) {
		var sb strings.Builder
		appendLogSection(&sb, "TITLE", "")
		if got := sb.String(); got != "TITLE" {
			t.Errorf("got %q, want TITLE", got)
		}
	})
	t.Run("body only", func(t *testing.T) {
		var sb strings.Builder
		appendLogSection(&sb, "", "body")
		if got := sb.String(); got != "body" {
			t.Errorf("got %q, want body", got)
		}
	})
	t.Run("consecutive sections are separated by blank line", func(t *testing.T) {
		var sb strings.Builder
		appendLogSection(&sb, "A", "a")
		appendLogSection(&sb, "B", "b")
		want := "A\na\n\nB\nb"
		if got := sb.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

