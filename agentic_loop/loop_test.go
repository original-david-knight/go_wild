package gowild_agentic_loop

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/genai"
)

func TestAgenticLoop_AddTools(t *testing.T) {
	loop := &AgenticLoop{
		tools:   []Tool{},
		toolMap: make(map[string]Tool),
	}

	tool1 := NewFuncTool("tool1", "desc1", nil, nil)
	tool2 := NewFuncTool("tool2", "desc2", nil, nil)

	loop.AddTools(tool1, tool2)

	if len(loop.tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(loop.tools))
	}
	if loop.toolMap["tool1"] != tool1 {
		t.Error("tool1 not in toolMap")
	}
	if loop.toolMap["tool2"] != tool2 {
		t.Error("tool2 not in toolMap")
	}
}

func TestAgenticLoop_executeTool_NotFound(t *testing.T) {
	loop := &AgenticLoop{
		tools:   []Tool{},
		toolMap: make(map[string]Tool),
	}

	call := &genai.FunctionCall{
		Name: "nonexistent",
		Args: nil,
	}

	result := loop.executeTool(context.Background(), call)

	if result.Success {
		t.Error("expected failure for unknown tool")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestAgenticLoop_executeTool_Success(t *testing.T) {
	tool := NewFuncTool(
		"test_tool",
		"A test tool",
		nil,
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			return NewSuccessResult("executed"), nil
		},
	)

	loop := &AgenticLoop{
		tools:   []Tool{tool},
		toolMap: map[string]Tool{"test_tool": tool},
	}

	call := &genai.FunctionCall{
		Name: "test_tool",
		Args: map[string]any{},
	}

	result := loop.executeTool(context.Background(), call)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.Content != "executed" {
		t.Errorf("expected 'executed', got %v", result.Content)
	}
}

func TestAgenticLoop_executeToolsParallel(t *testing.T) {
	executed := make(chan string, 3)

	makeTool := func(name string) Tool {
		return NewFuncTool(
			name,
			"desc",
			nil,
			func(ctx context.Context, input map[string]any) (*ToolResult, error) {
				executed <- name
				return NewSuccessResult(name), nil
			},
		)
	}

	tool1 := makeTool("tool1")
	tool2 := makeTool("tool2")
	tool3 := makeTool("tool3")

	loop := &AgenticLoop{
		tools: []Tool{tool1, tool2, tool3},
		toolMap: map[string]Tool{
			"tool1": tool1,
			"tool2": tool2,
			"tool3": tool3,
		},
	}

	calls := []*genai.FunctionCall{
		{Name: "tool1", Args: nil},
		{Name: "tool2", Args: nil},
		{Name: "tool3", Args: nil},
	}

	events := make(chan Event, 10)
	results := loop.executeToolsParallel(context.Background(), calls, events, make(map[string]int))
	close(events)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// All tools should have been executed
	close(executed)
	executedTools := make(map[string]bool)
	for name := range executed {
		executedTools[name] = true
	}

	if !executedTools["tool1"] || !executedTools["tool2"] || !executedTools["tool3"] {
		t.Error("not all tools were executed")
	}

	// Check events were emitted
	eventCount := 0
	for range events {
		eventCount++
	}
	if eventCount != 3 {
		t.Errorf("expected 3 ToolResultEvents, got %d", eventCount)
	}
}

func TestAgenticLoop_executeToolsParallel_DoesNotCapNonDeepResearchToolsByDefault(t *testing.T) {
	var executed int32
	tool := NewFuncTool(
		"polymarket_get_market",
		"desc",
		nil,
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			atomic.AddInt32(&executed, 1)
			return NewSuccessResult("ok"), nil
		},
	)

	loop := &AgenticLoop{
		tools:   []Tool{tool},
		toolMap: map[string]Tool{"polymarket_get_market": tool},
	}

	calls := make([]*genai.FunctionCall, 0, DefaultMaxToolCalls+1)
	for i := 0; i < DefaultMaxToolCalls+1; i++ {
		calls = append(calls, &genai.FunctionCall{Name: "polymarket_get_market"})
	}

	results := loop.executeToolsParallel(context.Background(), calls, make(chan Event, len(calls)), make(map[string]int))
	if got := atomic.LoadInt32(&executed); got != int32(len(calls)) {
		t.Fatalf("expected %d executions, got %d", len(calls), got)
	}
	for i, result := range results {
		if !result.Result.Success {
			t.Fatalf("expected success for result %d, got error %q", i, result.Result.Error)
		}
	}
}

func TestAgenticLoop_executeToolsParallel_CapsDeepResearchToolsByDefault(t *testing.T) {
	var executed int32
	tool := NewFuncTool(
		"deep_research_answer",
		"desc",
		nil,
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			atomic.AddInt32(&executed, 1)
			return NewSuccessResult("ok"), nil
		},
	)

	loop := &AgenticLoop{
		tools:   []Tool{tool},
		toolMap: map[string]Tool{"deep_research_answer": tool},
	}

	calls := make([]*genai.FunctionCall, 0, DefaultMaxToolCalls+1)
	for i := 0; i < DefaultMaxToolCalls+1; i++ {
		calls = append(calls, &genai.FunctionCall{Name: "deep_research_answer"})
	}

	results := loop.executeToolsParallel(context.Background(), calls, make(chan Event, len(calls)), make(map[string]int))

	var failures int
	for _, result := range results {
		if result.Result.Success {
			continue
		}
		failures++
		if !strings.Contains(result.Result.Error, "limit: 10") {
			t.Fatalf("expected deep research loop error to mention limit, got %q", result.Result.Error)
		}
	}

	if got := atomic.LoadInt32(&executed); got != int32(DefaultMaxToolCalls) {
		t.Fatalf("expected %d deep research executions, got %d", DefaultMaxToolCalls, got)
	}
	if failures != 1 {
		t.Fatalf("expected 1 capped deep research call, got %d", failures)
	}
}

func TestOptions(t *testing.T) {
	loop := &AgenticLoop{
		tools:   []Tool{},
		toolMap: make(map[string]Tool),
	}

	// Test WithSystemPrompt
	WithSystemPrompt("custom prompt")(loop)
	if loop.systemPrompt != "custom prompt" {
		t.Errorf("expected 'custom prompt', got %s", loop.systemPrompt)
	}

	// Test WithMaxTurns
	WithMaxTurns(5)(loop)
	if loop.maxTurns != 5 {
		t.Errorf("expected 5, got %d", loop.maxTurns)
	}

	// Test WithTools
	tool := NewFuncTool("test", "desc", nil, nil)
	WithTools(tool)(loop)
	if len(loop.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(loop.tools))
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultModel == "" {
		t.Error("DefaultModel should not be empty")
	}
	if DefaultSystemPrompt == "" {
		t.Error("DefaultSystemPrompt should not be empty")
	}
	if DefaultMaxTurns <= 0 {
		t.Error("DefaultMaxTurns should be positive")
	}
}

func TestToolResultPair(t *testing.T) {
	pair := toolResultPair{
		Name:   "test_tool",
		Result: NewSuccessResult("data"),
	}

	if pair.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got %s", pair.Name)
	}
	if !pair.Result.Success {
		t.Error("expected success result")
	}
}
