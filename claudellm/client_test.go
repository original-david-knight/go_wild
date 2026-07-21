package claudellm

import (
	"testing"
)

func TestParseStreamLineResult(t *testing.T) {
	line := `{"type":"result","result":"Hello, world!"}`
	text, ok := parseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true for result message")
	}
	if text != "Hello, world!" {
		t.Fatalf("expected %q, got %q", "Hello, world!", text)
	}
}

func TestParseStreamLineAssistant(t *testing.T) {
	line := `{"type":"assistant","content":[{"type":"text","text":"Generated output"}]}`
	text, ok := parseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true for assistant message")
	}
	if text != "Generated output" {
		t.Fatalf("expected %q, got %q", "Generated output", text)
	}
}

func TestParseStreamLineDeltaIgnored(t *testing.T) {
	line := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`
	_, ok := parseStreamLine(line)
	if ok {
		t.Fatal("expected ok=false for delta messages")
	}
}

func TestParseStreamLineInvalidJSON(t *testing.T) {
	_, ok := parseStreamLine("not json")
	if ok {
		t.Fatal("expected ok=false for invalid JSON")
	}
}

func TestParseStreamLineEmptyResult(t *testing.T) {
	line := `{"type":"result","result":""}`
	_, ok := parseStreamLine(line)
	if ok {
		t.Fatal("expected ok=false for empty result")
	}
}

func TestParseStreamLineAssistantNoText(t *testing.T) {
	line := `{"type":"assistant","content":[{"type":"tool_use","text":""}]}`
	_, ok := parseStreamLine(line)
	if ok {
		t.Fatal("expected ok=false for non-text content block")
	}
}

func TestFindExecutableMissing(t *testing.T) {
	// This test verifies FindExecutable returns an error when the binary isn't found.
	// It will pass if claude is not in PATH, skip if it is.
	_, err := FindExecutable()
	if err == nil {
		t.Skip("claude binary found in PATH, skipping missing-binary test")
	}
	if err.Error() != "claude executable not found in PATH (tried: claude, claude-code)" {
		t.Fatalf("unexpected error: %v", err)
	}
}
