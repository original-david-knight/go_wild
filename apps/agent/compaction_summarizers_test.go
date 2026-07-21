package main

import (
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestFormatHistoryForCompaction(t *testing.T) {
	messages := []loop.Message{
		loop.NewUserMessage("Hello, how are you?"),
		loop.NewModelTextMessage("I'm doing well, thank you for asking!"),
		loop.NewUserMessage("Can you help me with a task?"),
	}

	result := formatHistoryForCompaction(messages)

	if !contains(result, "USER [1]") {
		t.Error("Expected USER [1] in output")
	}
	if !contains(result, "ASSISTANT [2]") {
		t.Error("Expected ASSISTANT [2] in output")
	}
	if !contains(result, "USER [3]") {
		t.Error("Expected USER [3] in output")
	}
	if !contains(result, "Hello, how are you?") {
		t.Error("Expected first user message in output")
	}
}

func TestFormatHistoryForCompaction_Truncation(t *testing.T) {
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "This is a very long message. "
	}

	messages := []loop.Message{
		loop.NewUserMessage(longContent),
	}

	result := formatHistoryForCompaction(messages)

	if !contains(result, "... [truncated]") {
		t.Error("Expected truncated message")
	}
	if len(result) > 600 {
		t.Error("Message should be truncated to around 500 chars")
	}
}

func TestBuildCompactionPrompt(t *testing.T) {
	formatted := "USER [1]: Hello\n\nASSISTANT [2]: Hi there\n\n"
	prompt := buildCompactionPrompt(formatted)
	if !contains(prompt, "CONVERSATION HISTORY TO SUMMARIZE") {
		t.Error("expected history header")
	}
	if !contains(prompt, "USER [1]: Hello") {
		t.Error("expected formatted history content")
	}
	if !contains(prompt, "SUMMARY:") {
		t.Error("expected SUMMARY: prompt end")
	}
}

func TestShortenPath(t *testing.T) {
	// Short path - unchanged
	short := "/home/file.go"
	if shortenPath(short) != short {
		t.Error("short path should be unchanged")
	}

	// Long path - shortened
	long := "/very/long/path/to/some/deeply/nested/directory/file.go"
	shortened := shortenPath(long)
	if len(shortened) > 45 {
		t.Error("long path should be shortened")
	}
	if !contains(shortened, "file.go") {
		t.Error("shortened path should contain filename")
	}
}

func TestSummarizeToolResponse(t *testing.T) {
	// Test simple result
	resp := map[string]any{"result": "success"}
	summary := summarizeToolResponse(resp)
	if summary != "success" {
		t.Errorf("Expected 'success', got '%s'", summary)
	}

	// Test error
	resp = map[string]any{"error": "something went wrong"}
	summary = summarizeToolResponse(resp)
	if summary != "error: something went wrong" {
		t.Errorf("Expected 'error: something went wrong', got '%s'", summary)
	}

	// Test complex response
	resp = map[string]any{"field1": "a", "field2": "b"}
	summary = summarizeToolResponse(resp)
	// Should contain the field names
	if !contains(summary, "field1") || !contains(summary, "field2") {
		t.Errorf("Expected field names in summary, got '%s'", summary)
	}
}

func TestSummarizeToolResponse_LongResult(t *testing.T) {
	longResult := ""
	for i := 0; i < 50; i++ {
		longResult += "long text "
	}
	resp := map[string]any{"result": longResult}
	summary := summarizeToolResponse(resp)
	if len(summary) > 210 {
		t.Errorf("expected truncation at ~200 chars, got length %d", len(summary))
	}
	if !contains(summary, "...") {
		t.Error("expected '...' at end of truncated summary")
	}
}

func TestShortenURL(t *testing.T) {
	// Short URL - unchanged
	short := "https://example.com/page"
	if shortenURL(short) != short {
		t.Error("short URL should be unchanged")
	}

	// Long URL - protocol stripped and truncated
	long := "https://www.example.com/very/long/path/to/some/resource/that/exceeds/fifty/characters"
	shortened := shortenURL(long)
	if contains(shortened, "https://") {
		t.Error("protocol should be stripped")
	}
	if len(shortened) > 50 {
		t.Errorf("shortened URL should be <= 50 chars, got %d", len(shortened))
	}
	if !contains(shortened, "...") {
		t.Error("long URL should end with ...")
	}

	// HTTP URL
	httpURL := "http://www.example.com/very/long/path/to/some/resource/that/exceeds/fifty/characters"
	shortened2 := shortenURL(httpURL)
	if contains(shortened2, "http://") {
		t.Error("http protocol should be stripped")
	}

	// URL that's short after stripping protocol
	medium := "https://example.com/short"
	result := shortenURL(medium)
	if contains(result, "...") {
		t.Error("medium URL should not be truncated after protocol strip")
	}
}

func TestSummarizeHTTPRequest(t *testing.T) {
	// With body keys and method
	resp := map[string]any{
		"status_code": float64(200),
		"method":      "POST",
		"body":        map[string]any{"key1": "v1", "key2": "v2", "key3": "v3"},
	}
	summary := summarizeHTTPRequest(resp)
	if !contains(summary, "POST") {
		t.Errorf("expected POST, got: %s", summary)
	}
	if !contains(summary, "200") {
		t.Errorf("expected 200, got: %s", summary)
	}
	if !contains(summary, "3 fields") {
		t.Errorf("expected 3 fields, got: %s", summary)
	}

	// Without body keys, default method
	resp2 := map[string]any{
		"status_code": float64(404),
	}
	summary2 := summarizeHTTPRequest(resp2)
	if !contains(summary2, "GET") {
		t.Errorf("expected default GET, got: %s", summary2)
	}
	if !contains(summary2, "404") {
		t.Errorf("expected 404, got: %s", summary2)
	}
}

func TestSummarizeRunShell(t *testing.T) {
	// Success with output
	resp := map[string]any{
		"stdout":    "line1\nline2\nline3",
		"stderr":    "",
		"exit_code": float64(0),
	}
	summary := summarizeRunShell(resp)
	if !contains(summary, "success") {
		t.Errorf("expected success, got: %s", summary)
	}
	if !contains(summary, "2 lines") {
		t.Errorf("expected 2 lines of output, got: %s", summary)
	}

	// Success with no output
	resp2 := map[string]any{
		"stdout":    "",
		"stderr":    "",
		"exit_code": float64(0),
	}
	summary2 := summarizeRunShell(resp2)
	if !contains(summary2, "success") {
		t.Errorf("expected success, got: %s", summary2)
	}

	// Error with long stderr
	longErr := ""
	for i := 0; i < 20; i++ {
		longErr += "error message part "
	}
	resp3 := map[string]any{
		"stdout":    "",
		"stderr":    longErr,
		"exit_code": float64(1),
	}
	summary3 := summarizeRunShell(resp3)
	if !contains(summary3, "exit 1") {
		t.Errorf("expected exit 1, got: %s", summary3)
	}
	if !contains(summary3, "...") {
		t.Errorf("expected truncated stderr, got: %s", summary3)
	}
}

func TestSummarizeListFiles(t *testing.T) {
	// With []any
	resp := map[string]any{
		"files": []any{"file1.go", "file2.go", "dir1"},
	}
	summary := summarizeListFiles(resp)
	if !contains(summary, "3 files") {
		t.Errorf("expected 3 files, got: %s", summary)
	}

	// With []string
	resp2 := map[string]any{
		"files": []string{"a.go", "b.go"},
	}
	summary2 := summarizeListFiles(resp2)
	if !contains(summary2, "2 files") {
		t.Errorf("expected 2 files, got: %s", summary2)
	}

	// Without files key
	resp3 := map[string]any{"status": "ok"}
	summary3 := summarizeListFiles(resp3)
	if !contains(summary3, "completed") {
		t.Errorf("expected completed, got: %s", summary3)
	}
}

func TestSummarizeReadWebpage(t *testing.T) {
	resp := map[string]any{
		"content": "Hello World content here with some text",
		"url":     "https://example.com/page",
	}
	summary := summarizeReadWebpage(resp)
	if !contains(summary, "read_webpage") {
		t.Errorf("expected read_webpage, got: %s", summary)
	}
	if !contains(summary, "39 chars") {
		t.Errorf("expected correct char count, got: %s", summary)
	}
	if !contains(summary, "example.com") {
		t.Errorf("expected domain, got: %s", summary)
	}

	// Empty content
	resp2 := map[string]any{
		"content": "",
		"url":     "",
	}
	summary2 := summarizeReadWebpage(resp2)
	if !contains(summary2, "0 chars") {
		t.Errorf("expected 0 chars, got: %s", summary2)
	}
}

func TestSummarizeGeneric(t *testing.T) {
	// String result - short
	resp := map[string]any{"result": "done"}
	summary := summarizeGeneric("custom_tool", resp)
	if !contains(summary, "done") {
		t.Errorf("expected 'done', got: %s", summary)
	}

	// String result - long (>50 chars)
	longStr := "This is a very long result string that exceeds the fifty character limit for display"
	resp2 := map[string]any{"result": longStr}
	summary2 := summarizeGeneric("custom_tool", resp2)
	if !contains(summary2, "...") {
		t.Errorf("expected truncation, got: %s", summary2)
	}

	// Bool result
	resp3 := map[string]any{"result": true}
	summary3 := summarizeGeneric("custom_tool", resp3)
	if !contains(summary3, "true") {
		t.Errorf("expected true, got: %s", summary3)
	}

	// Float result
	resp4 := map[string]any{"result": float64(42.5)}
	summary4 := summarizeGeneric("custom_tool", resp4)
	if !contains(summary4, "42.5") {
		t.Errorf("expected 42.5, got: %s", summary4)
	}

	// Map result
	resp5 := map[string]any{"result": map[string]any{"a": 1, "b": 2}}
	summary5 := summarizeGeneric("custom_tool", resp5)
	if !contains(summary5, "2 fields") {
		t.Errorf("expected 2 fields, got: %s", summary5)
	}

	// Array result
	resp6 := map[string]any{"result": []any{1, 2, 3}}
	summary6 := summarizeGeneric("custom_tool", resp6)
	if !contains(summary6, "3 items") {
		t.Errorf("expected 3 items, got: %s", summary6)
	}

	// Fallback - no result key
	resp7 := map[string]any{"field1": "v1", "field2": "v2"}
	summary7 := summarizeGeneric("custom_tool", resp7)
	if !contains(summary7, "2 fields") {
		t.Errorf("expected 2 fields fallback, got: %s", summary7)
	}
}

func TestSummarizeReadFile_FileTypes(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/data/main.py", "Python code"},
		{"/data/app.js", "JavaScript/TypeScript"},
		{"/data/index.ts", "JavaScript/TypeScript"},
		{"/data/README.md", "Markdown"},
		{"/data/config.json", "JSON"},
		{"/data/config.yaml", "YAML"},
		{"/data/config.yml", "YAML"},
		{"/data/unknown.txt", "text"},
	}
	for _, tt := range tests {
		resp := map[string]any{"content": "line1\nline2", "path": tt.path}
		summary := summarizeReadFile(resp)
		if !contains(summary, tt.expected) {
			t.Errorf("summarizeReadFile(%s): expected %q in summary, got: %s", tt.path, tt.expected, summary)
		}
	}
}

func TestSummarizeReadFile_FilePathKey(t *testing.T) {
	// Uses "file_path" key instead of "path"
	resp := map[string]any{"content": "line1", "file_path": "/data/main.go"}
	summary := summarizeReadFile(resp)
	if !contains(summary, "main.go") {
		t.Errorf("expected main.go in summary, got: %s", summary)
	}
}

func TestSummarizeWebSearch_LongQuery(t *testing.T) {
	resp := map[string]any{
		"query":   "This is a very long search query that exceeds thirty characters easily",
		"results": []any{"r1"},
	}
	summary := summarizeWebSearch(resp)
	if !contains(summary, "...") {
		t.Errorf("expected truncated query, got: %s", summary)
	}
	if !contains(summary, "1 results") {
		t.Errorf("expected 1 results, got: %s", summary)
	}
}

func TestSummarizeWebSearch_NoResults(t *testing.T) {
	resp := map[string]any{"status": "ok"}
	summary := summarizeWebSearch(resp)
	if !contains(summary, "completed") {
		t.Errorf("expected completed, got: %s", summary)
	}
}

func TestSummarizeRunPython_NoOutput(t *testing.T) {
	resp := map[string]any{
		"stdout":    "",
		"stderr":    "",
		"exit_code": float64(0),
	}
	summary := summarizeRunPython(resp)
	if !contains(summary, "no output") {
		t.Errorf("expected 'no output', got: %s", summary)
	}
}
