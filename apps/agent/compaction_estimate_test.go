package main

import (
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

func TestEstimateHistorySize(t *testing.T) {
	// Empty history
	size := estimateHistorySize(nil)
	if size != 0 {
		t.Errorf("expected 0 for empty history, got %d", size)
	}

	// With text message
	history := []loop.Message{
		loop.NewUserMessage("Hello world"),
	}
	size = estimateHistorySize(history)
	if size != len("Hello world") {
		t.Errorf("expected %d, got %d", len("Hello world"), size)
	}

	// With nil content message
	history2 := []loop.Message{
		{Role: loop.RoleUser, Content: nil},
	}
	size2 := estimateHistorySize(history2)
	if size2 != 0 {
		t.Errorf("expected 0 for nil content, got %d", size2)
	}

	// With function call
	history3 := []loop.Message{
		{
			Role: loop.RoleModel,
			Content: &genai.Content{
				Role: "model",
				Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{
						Name: "test",
						Args: map[string]any{"key": "value"},
					}},
				},
			},
		},
	}
	size3 := estimateHistorySize(history3)
	if size3 <= 0 {
		t.Errorf("expected positive size for function call, got %d", size3)
	}

	// With function response
	history4 := []loop.Message{
		createToolResultMessage("test_tool", map[string]any{"result": "some data"}),
	}
	size4 := estimateHistorySize(history4)
	if size4 <= 0 {
		t.Errorf("expected positive size for function response, got %d", size4)
	}
}

func TestEstimateTokenSavings(t *testing.T) {
	original := []loop.Message{
		loop.NewUserMessage("Hello world"),
		createToolResultMessage("tool1", map[string]any{"result": "a very long response with lots of data that represents tool output"}),
	}
	masked := []loop.Message{
		loop.NewUserMessage("Hello world"),
		createToolResultMessage("tool1", map[string]any{"_masked": "[tool1: completed]"}),
	}

	savings := estimateTokenSavings(original, masked)
	if savings <= 0 {
		t.Errorf("expected positive token savings, got %d", savings)
	}
}
