package claudellm

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExecutionErrorMessage(t *testing.T) {
	if got := (&ExecutionError{Message: "  exit 2 \n"}).Error(); got != "exit 2" {
		t.Errorf("Error() = %q", got)
	}
	var nilErr *ExecutionError
	if got := nilErr.Error(); got != "" {
		t.Errorf("nil Error() = %q", got)
	}
}

func TestBuildFailureArtifactsFromAnExecutionError(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`
	err := &ExecutionError{Message: " claude exited 2 ", ExitCode: 2, Stdout: " " + stream + " ", Stderr: " boom \n"}

	result, errPayload := BuildFailureArtifacts("  ", err)

	wantResult := map[string]any{
		"status":         "failed",
		"failure_reason": "claude exited 2",
		"stdout":         stream,
		"raw_output":     stream,
		"event_log":      "ASSISTANT text\nworking",
		"stderr":         "boom",
		"exit_code":      2,
	}
	if !reflect.DeepEqual(result, wantResult) {
		t.Errorf("result = %#v\nwant %#v", result, wantResult)
	}
	wantErr := map[string]any{"message": "claude exited 2", "stdout": stream, "stderr": "boom", "exit_code": 2}
	if !reflect.DeepEqual(errPayload, wantErr) {
		t.Errorf("error payload = %#v\nwant %#v", errPayload, wantErr)
	}
}

func TestBuildFailureArtifactsFromAPlainError(t *testing.T) {
	result, errPayload := BuildFailureArtifacts(" not a stream ", errors.New("timed out"))
	wantResult := map[string]any{
		"status":         "failed",
		"failure_reason": "timed out",
		"stdout":         "not a stream",
		"raw_output":     "not a stream",
	}
	if !reflect.DeepEqual(result, wantResult) {
		t.Errorf("result = %#v", result)
	}
	if !reflect.DeepEqual(errPayload, map[string]any{"message": "timed out", "stdout": "not a stream"}) {
		t.Errorf("error payload = %#v", errPayload)
	}

	// The caller's output wins over the error's own stdout.
	result, _ = BuildFailureArtifacts("mine", &ExecutionError{Message: "x", Stdout: "theirs"})
	if result["stdout"] != "mine" {
		t.Errorf("stdout = %v, want the caller's output", result["stdout"])
	}

	result, errPayload = BuildFailureArtifacts("", nil)
	if !reflect.DeepEqual(result, map[string]any{"status": "failed", "failure_reason": ""}) ||
		!reflect.DeepEqual(errPayload, map[string]any{"message": ""}) {
		t.Errorf("nil error: result = %#v, payload = %#v", result, errPayload)
	}
}

func TestFormatEventLogToolTraffic(t *testing.T) {
	output := strings.Join([]string{
		`{"type":"system","subtype":"init","model":"opus","session_id":"s1","cwd":"/w"}`,
		`{"type":"system","subtype":"init"}`,
		`{"type":"system","subtype":"other","model":"x"}`,
		`{"type":"assistant","message":{"content":[` +
			`{"type":"tool_use","id":"tu1","name":"Read","input":{"path":"/a"}},` +
			`{"type":"tool_use","id":"tu2"},` +
			`{"type":"tool_use","name":"Bare"},` +
			`{"type":"text","text":"  "}]}}`,
		`{"type":"user","message":{"content":[` +
			`{"type":"tool_result","tool_use_id":"tu1","content":"{\"lines\":2}"},` +
			`{"type":"tool_result","tool_use_id":"tu2","is_error":true},` +
			`{"type":"tool_result","content":"[1, 2]","is_error":true},` +
			`{"type":"tool_result","content":"{not json"},` +
			`{"type":"text","text":"ignored"}]}}`,
		`{"type":"result","subtype":"success","stop_reason":"end_turn","result":{"ok":true}}`,
		`{"type":"unknown"}`,
	}, "\n")

	want := strings.Join([]string{
		"SYSTEM init\nmodel=opus\nsession_id=s1\ncwd=/w",
		"ASSISTANT tool_use: Read\ntool_use_id: tu1\n{\n  \"path\": \"/a\"\n}",
		"ASSISTANT tool_use\ntool_use_id: tu2",
		"ASSISTANT tool_use: Bare",
		"USER tool_result\ntool_use_id: tu1\n{\n  \"lines\": 2\n}",
		"USER tool_result\nis_error: true\ntool_use_id: tu2",
		"USER tool_result\nis_error: true\n[\n  1,\n  2\n]",
		"USER tool_result\n{not json",
		"RESULT success\nstop_reason: end_turn\nresult:\n{\n  \"ok\": true\n}",
	}, "\n\n")
	if got := FormatEventLog(output); got != want {
		t.Errorf("FormatEventLog =\n%s\n---want---\n%s", got, want)
	}
}

func TestFormatLogBodyShapes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"blank string", "  ", ""},
		{"plain string", " hi ", "hi"},
		{"json string", `{"a":1}`, "{\n  \"a\": 1\n}"},
		{"broken json string", `[1,`, "[1,"},
		{"number", 3.5, "3.5"},
	}
	for _, tc := range cases {
		if got := formatLogBody(tc.in); got != tc.want {
			t.Errorf("%s: formatLogBody = %q, want %q", tc.name, got, tc.want)
		}
	}
	// A value JSON cannot encode falls back to its fmt form.
	if got := formatLogBody(make(chan int)); !strings.HasPrefix(got, "0x") {
		t.Errorf("unmarshalable: formatLogBody = %q, want the fmt pointer form", got)
	}
}

func TestAppendLogSectionEdges(t *testing.T) {
	appendLogSection(nil, "t", "b") // must not panic
	var sb strings.Builder
	appendLogSection(&sb, " ", " ")
	appendLogSection(&sb, "", "body only")
	appendLogSection(&sb, "title only", "")
	if got := sb.String(); got != "body only\n\ntitle only" {
		t.Errorf("sections = %q", got)
	}
}

func TestExtractFinalResponseShapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"not a stream", "  plain text  ", "plain text"},
		{"object result", `{"type":"result","result":{"status":"succeeded"}}`, "{\n  \"status\": \"succeeded\"\n}"},
		{"string result", `{"type":"result","result":"  done  "}`, "done"},
		{"assistant text only", `{"type":"assistant","message":{"content":[{"type":"text","text":"a"},{"type":"tool_use"},{"type":"text","text":"b"}]}}`, "a\nb"},
		{"no result or text", `{"type":"system"}`, `{"type":"system"}`},
	}
	for _, tc := range cases {
		if got := ExtractFinalResponse(tc.in); got != tc.want {
			t.Errorf("%s: ExtractFinalResponse = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseResultEdgeShapes(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		want        ParsedResult
		wantPayload map[string]any
	}{
		{
			name: "envelope without a type carries the mission",
			in:   `{"subtype":"success","result":"{\"status\":\"succeeded\",\"result\":{\"n\":1}}"}`,
			want: ParsedResult{Status: "succeeded"}, wantPayload: map[string]any{"n": float64(1)},
		},
		{
			name:        "result event whose result is an object",
			in:          `{"type":"result","result":{"status":"failed","error":{"message":" quota "}}}`,
			want:        ParsedResult{Status: "failed", FailureReason: "quota"},
			wantPayload: map[string]any{"status": "failed", "error": map[string]any{"message": " quota "}},
		},
		{
			name: "result event whose result is a number",
			in:   `{"type":"result","result":7}`,
			want: ParsedResult{Status: "failed", FailureReason: "claude-code returned a malformed final response", FormatError: true},
		},
		{
			name: "blank result string",
			in:   `{"type":"result","result":"   "}`,
			want: ParsedResult{Status: "failed", FailureReason: "claude-code returned empty output", FormatError: true},
		},
		{
			name: "prose without JSON",
			in:   `no json here`,
			want: ParsedResult{Status: "failed", FailureReason: "claude-code returned a non-JSON final response", FormatError: true},
		},
		{
			name: "unknown status",
			in:   `{"status":"maybe"}`,
			want: ParsedResult{Status: "failed", FailureReason: `claude-code returned invalid status "maybe"`, FormatError: true},
		},
		{
			name: "blank status",
			in:   `{"status":" "}`,
			want: ParsedResult{Status: "failed", FailureReason: "claude-code returned JSON without the required status field", FormatError: true},
		},
		{
			name: "success without result",
			in:   `{"status":"succeeded"}`,
			want: ParsedResult{Status: "failed", FailureReason: "claude-code returned success without the required result field", FormatError: true},
		},
		{
			name: "success with a scalar result",
			in:   `{"status":"succeeded","result":5}`,
			want: ParsedResult{Status: "failed", FailureReason: "claude-code returned a result that was not a JSON object", FormatError: true},
		},
		{
			name: "failure reason from the payload's error string",
			in:   `{"status":"failed","result":{"error":" disk full "}}`,
			want: ParsedResult{Status: "failed", FailureReason: "disk full"}, wantPayload: map[string]any{"error": " disk full "},
		},
		{
			name: "failure reason from message",
			in:   `{"status":"FAILED","message":"nope","result":"json\n{}"}`,
			want: ParsedResult{Status: "failed", FailureReason: "nope"}, wantPayload: map[string]any{},
		},
		{
			name: "failure with no reason anywhere",
			in:   `{"status":"failed","result":{"error":{"message":" "}}}`,
			want: ParsedResult{Status: "failed"}, wantPayload: map[string]any{"error": map[string]any{"message": " "}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseResult(tc.in)
			if got.Status != tc.want.Status || got.FailureReason != tc.want.FailureReason || got.FormatError != tc.want.FormatError {
				t.Errorf("ParseResult = {%q %q %v}, want {%q %q %v}", got.Status, got.FailureReason, got.FormatError,
					tc.want.Status, tc.want.FailureReason, tc.want.FormatError)
			}
			if tc.wantPayload != nil && !reflect.DeepEqual(got.Payload, tc.wantPayload) {
				t.Errorf("payload = %#v, want %#v", got.Payload, tc.wantPayload)
			}
		})
	}
}

func TestInvalidResultDefaultsItsReason(t *testing.T) {
	got := invalidResult("  ")
	if got.FailureReason != "claude-code returned a malformed final response" || !got.FormatError {
		t.Errorf("invalidResult = %+v", got)
	}
}

func TestTrimLeadingJSONLabel(t *testing.T) {
	for in, want := range map[string]string{
		"json":       "json",
		"JSON {}":    "{}",
		"jsonx {}":   "jsonx {}",
		"text {}":    "text {}",
		"json\t\n{}": "{}",
	} {
		if got := trimLeadingJSONLabel(in); got != want {
			t.Errorf("trimLeadingJSONLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
