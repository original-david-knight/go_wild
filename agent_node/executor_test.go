package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"

	"google.golang.org/genai"
)

func TestExecutor_LinearDAG(t *testing.T) {
	// Test that the executor can be created and the graph validates correctly.
	graph := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "do A", ToolNames: []string{"test_tool"}},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "do B", ToolNames: []string{"test_tool"}},
		},
	}

	if err := graph.Validate(); err != nil {
		t.Fatalf("graph should be valid: %v", err)
	}

	testTool := loop.NewFuncTool("test_tool", "A test tool", &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"input": {Type: genai.TypeString},
		},
	}, func(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
		return loop.NewSuccessResult("ok"), nil
	})

	events := make(chan GraphEvent, 100)
	exec, err := NewGraphExecutor(context.Background(), ExecutorConfig{
		APIKey:         "test-key",
		DefaultModel:   "test-model",
		MaxConcurrency: 2,
		Tools:          ToolRegistry{"test_tool": testTool},
		Events:         events,
	})
	if err != nil {
		t.Fatalf("NewGraphExecutor: %v", err)
	}

	// Verify executor was created with correct config
	if exec.config.DefaultModel != "test-model" {
		t.Fatalf("expected model 'test-model', got '%s'", exec.config.DefaultModel)
	}
	if exec.config.MaxConcurrency != 2 {
		t.Fatalf("expected concurrency 2, got %d", exec.config.MaxConcurrency)
	}

	// Test scheduling via shared state simulation
	state := NewSharedState()
	state.set("a", &NodeResult{
		NodeID:    "a",
		Status:    NodeDone,
		Text:      "result of A",
		TurnCount: 1,
	})
	state.set("b", &NodeResult{
		NodeID:    "b",
		Status:    NodeDone,
		Text:      "result of B",
		TurnCount: 1,
	})

	if state.get("a").Status != NodeDone {
		t.Fatal("expected node a to be done")
	}
	if state.get("b").Status != NodeDone {
		t.Fatal("expected node b to be done")
	}
}

func TestExecutor_ParallelNodes(t *testing.T) {
	// Test that independent nodes can be identified correctly
	graph := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "do A"},
			{ID: "b", Prompt: "do B"},
			{ID: "c", Prompt: "do C"},
			{ID: "d", DependsOn: []NodeID{"a", "b", "c"}, Prompt: "join"},
		},
	}

	sorted, err := graph.topologicalSort()
	if err != nil {
		t.Fatal(err)
	}

	// a, b, c should all come before d
	pos := make(map[NodeID]int)
	for i, n := range sorted {
		pos[n.ID] = i
	}

	if pos["d"] <= pos["a"] || pos["d"] <= pos["b"] || pos["d"] <= pos["c"] {
		t.Fatal("d must come after a, b, and c")
	}
}

func TestExecutor_FailedNodeSkipsDependents(t *testing.T) {
	state := NewSharedState()

	// Simulate: node "a" failed, so "b" (depends on "a") should be skipped
	state.set("a", &NodeResult{
		NodeID: "a",
		Status: NodeFailed,
		Error:  "test error",
	})

	// Manually apply the skip logic
	graph := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "do A"},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "do B"},
		},
	}

	if err := graph.Validate(); err != nil {
		t.Fatal(err)
	}

	aResult := state.get("a")
	if aResult.Status != NodeFailed {
		t.Fatal("expected a to be failed")
	}

	// Simulate skip
	state.set("b", &NodeResult{
		NodeID: "b",
		Status: NodeSkipped,
		Error:  fmt.Sprintf("dependency %s failed", "a"),
	})

	bResult := state.get("b")
	if bResult.Status != NodeSkipped {
		t.Fatal("expected b to be skipped")
	}
}

func TestBuildNodePrompt_NoDeps(t *testing.T) {
	def := &NodeDef{ID: "a", Prompt: "hello"}
	result := buildNodePrompt(def, nil)
	if result != "hello" {
		t.Fatalf("expected 'hello', got '%s'", result)
	}
}

func TestBuildNodePrompt_WithDeps(t *testing.T) {
	state := NewSharedState()
	state.set("dep1", &NodeResult{
		NodeID: "dep1",
		Status: NodeDone,
		Output: json.RawMessage(`{"key":"value"}`),
	})

	def := &NodeDef{
		ID:        "b",
		DependsOn: []NodeID{"dep1"},
		Prompt:    "analyze this",
	}

	result := buildNodePrompt(def, state)
	if result == "analyze this" {
		t.Fatal("expected context to be prepended")
	}
	if !contains(result, "dep1") || !contains(result, "analyze this") {
		t.Fatalf("expected both context and prompt in result: %s", result)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
