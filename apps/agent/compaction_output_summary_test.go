package main

import "testing"

func TestCreateOutputSummary_ReadFile(t *testing.T) {
	resp := map[string]any{
		"content": "line1\nline2\nline3\nline4\nline5",
		"path":    "/home/user/project/main.go",
	}
	summary := createOutputSummary("read_file", resp)

	if !contains(summary, "read_file") {
		t.Error("summary should contain tool name")
	}
	if !contains(summary, "5 lines") {
		t.Error("summary should contain line count")
	}
	if !contains(summary, "Go code") {
		t.Error("summary should identify Go code")
	}
}

func TestCreateOutputSummary_RunPython(t *testing.T) {
	// Success case
	resp := map[string]any{
		"stdout":    "result1\nresult2\nresult3",
		"stderr":    "",
		"exit_code": float64(0),
	}
	summary := createOutputSummary("run_python", resp)
	if !contains(summary, "success") {
		t.Error("success case should mention success")
	}
	// Note: strings.Count("\n") returns 2 for this string (number of newlines)
	if !contains(summary, "2 lines") {
		t.Errorf("should mention output lines, got: %s", summary)
	}

	// Error case
	resp = map[string]any{
		"stdout":    "",
		"stderr":    "NameError: name 'foo' is not defined",
		"exit_code": float64(1),
	}
	summary = createOutputSummary("run_python", resp)
	if !contains(summary, "exit 1") {
		t.Error("error case should mention exit code")
	}
}

func TestCreateOutputSummary_Error(t *testing.T) {
	resp := map[string]any{
		"error": "something went wrong",
	}
	summary := createOutputSummary("any_tool", resp)

	if !contains(summary, "ERROR") {
		t.Error("should indicate error")
	}
	if !contains(summary, "something went wrong") {
		t.Error("should include error message")
	}
}

func TestCreateOutputSummary_WebSearch(t *testing.T) {
	resp := map[string]any{
		"query":   "AI agents programming",
		"results": []any{"result1", "result2", "result3"},
	}
	summary := createOutputSummary("web_search", resp)

	if !contains(summary, "3 results") {
		t.Error("should mention result count")
	}
	if !contains(summary, "AI agents") {
		t.Error("should include query")
	}
}

func TestCreateOutputSummary_NilResponse(t *testing.T) {
	summary := createOutputSummary("test_tool", nil)
	if !contains(summary, "no output") {
		t.Errorf("expected 'no output', got: %s", summary)
	}
}

func TestCreateOutputSummary_LongError(t *testing.T) {
	longErr := ""
	for i := 0; i < 30; i++ {
		longErr += "error! "
	}
	resp := map[string]any{"error": longErr}
	summary := createOutputSummary("any_tool", resp)
	if !contains(summary, "...") {
		t.Errorf("expected truncated error, got: %s", summary)
	}
}
