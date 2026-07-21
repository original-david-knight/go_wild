package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

func TestClaudeCodeToolUsesCorrectToolName(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
		})
	}))

	claudeTools := NewClaudeCodeTools(c)
	result, err := claudeTools.ClaudeCodeTool(context.Background(), tools.ClaudeCodeInput{
		Prompt:          "print hello",
		TargetDirectory: "/data/project",
		Timeout:         30,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success result")
	}
	if gotPath != "/broker/v1/tools/claude_code" {
		t.Fatalf("expected claude_code tool path, got %s", gotPath)
	}
	if gotBody["prompt"] != "print hello" {
		t.Fatalf("expected prompt in request body, got %#v", gotBody["prompt"])
	}
}

func TestClaudeCodeDescribeTool(t *testing.T) {
	claudeTools := NewClaudeCodeTools(nil)
	if claudeTools.DescribeTool("claude_code") == "" {
		t.Fatalf("expected non-empty description")
	}
	if claudeTools.DescribeTool("unknown") != "" {
		t.Fatalf("expected empty description for unknown tool")
	}
}
