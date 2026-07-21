package main

import (
	"testing"

	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// =============================================================================
// Observation Masking Tests
// =============================================================================

func TestMaskObservations_KeepsRecentOutputs(t *testing.T) {
	// Create history with 5 tool outputs
	history := []loop.Message{
		loop.NewUserMessage("Do task 1"),
		createToolResultMessage("tool1", map[string]any{"result": "output1 with lots of data"}),
		loop.NewUserMessage("Do task 2"),
		createToolResultMessage("tool2", map[string]any{"result": "output2 with lots of data"}),
		loop.NewUserMessage("Do task 3"),
		createToolResultMessage("tool3", map[string]any{"result": "output3 with lots of data"}),
		loop.NewUserMessage("Do task 4"),
		createToolResultMessage("tool4", map[string]any{"result": "output4 with lots of data"}),
		loop.NewUserMessage("Do task 5"),
		createToolResultMessage("tool5", map[string]any{"result": "output5 with lots of data"}),
	}

	result := maskObservations(history, 2) // Keep last 2 outputs

	if result.MaskedCount != 3 {
		t.Errorf("expected 3 masked, got %d", result.MaskedCount)
	}
	if result.KeptFullCount != 2 {
		t.Errorf("expected 2 kept full, got %d", result.KeptFullCount)
	}

	// Verify the last 2 tool outputs are NOT masked
	// Tool outputs are at indices 1, 3, 5, 7, 9
	// Last 2 are at indices 7 and 9
	for i, msg := range result.MaskedHistory {
		if msg.Role == loop.RoleTool && msg.Content != nil {
			for _, part := range msg.Content.Parts {
				if part.FunctionResponse != nil {
					isMasked := part.FunctionResponse.Response["_masked"] != nil
					isLastTwo := (i == 7 || i == 9)
					if isLastTwo && isMasked {
						t.Errorf("message at index %d should NOT be masked", i)
					}
					if !isLastTwo && !isMasked {
						t.Errorf("message at index %d should be masked", i)
					}
				}
			}
		}
	}
}

func TestMaskObservations_PreservesUserMessages(t *testing.T) {
	history := []loop.Message{
		loop.NewUserMessage("First user message"),
		createToolResultMessage("tool1", map[string]any{"result": "data"}),
		loop.NewUserMessage("Second user message"),
		createToolResultMessage("tool2", map[string]any{"result": "data"}),
	}

	result := maskObservations(history, 1)

	// Check user messages are unchanged
	if extractText(result.MaskedHistory[0]) != "First user message" {
		t.Error("First user message was modified")
	}
	if extractText(result.MaskedHistory[2]) != "Second user message" {
		t.Error("Second user message was modified")
	}
}

func TestMaskObservations_PreservesModelMessages(t *testing.T) {
	history := []loop.Message{
		loop.NewUserMessage("Hello"),
		loop.NewModelTextMessage("I'll help you with that. Let me call a tool."),
		createToolResultMessage("tool1", map[string]any{"result": "data"}),
		loop.NewModelTextMessage("Based on the result, here's what I found."),
	}

	result := maskObservations(history, 0) // Mask all tool outputs

	// Check model messages are unchanged
	if extractText(result.MaskedHistory[1]) != "I'll help you with that. Let me call a tool." {
		t.Error("First model message was modified")
	}
	if extractText(result.MaskedHistory[3]) != "Based on the result, here's what I found." {
		t.Error("Second model message was modified")
	}
}

func TestMaskObservations_NoToolOutputs(t *testing.T) {
	history := []loop.Message{
		loop.NewUserMessage("Hello"),
		loop.NewModelTextMessage("Hi there!"),
		loop.NewUserMessage("How are you?"),
		loop.NewModelTextMessage("I'm doing well!"),
	}

	result := maskObservations(history, 3)

	if result.MaskedCount != 0 {
		t.Errorf("expected 0 masked, got %d", result.MaskedCount)
	}
	if result.KeptFullCount != 0 {
		t.Errorf("expected 0 kept full, got %d", result.KeptFullCount)
	}
	if len(result.MaskedHistory) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result.MaskedHistory))
	}
}

func TestMaskObservations_AllKept(t *testing.T) {
	history := []loop.Message{
		loop.NewUserMessage("Task"),
		createToolResultMessage("tool1", map[string]any{"result": "data1"}),
		createToolResultMessage("tool2", map[string]any{"result": "data2"}),
	}

	result := maskObservations(history, 5) // Keep more than we have

	if result.MaskedCount != 0 {
		t.Errorf("expected 0 masked, got %d", result.MaskedCount)
	}
	if result.KeptFullCount != 2 {
		t.Errorf("expected 2 kept full, got %d", result.KeptFullCount)
	}
}

func TestMaskObservations_NegativeKeepRecent(t *testing.T) {
	history := []loop.Message{
		loop.NewUserMessage("task"),
		createToolResultMessage("t1", map[string]any{"result": "d1"}),
		createToolResultMessage("t2", map[string]any{"result": "d2"}),
		createToolResultMessage("t3", map[string]any{"result": "d3"}),
		createToolResultMessage("t4", map[string]any{"result": "d4"}),
	}
	// Negative keepRecentOutputs defaults to 3
	result := maskObservations(history, -1)
	if result.KeptFullCount != 3 {
		t.Errorf("expected default 3 kept, got %d", result.KeptFullCount)
	}
	if result.MaskedCount != 1 {
		t.Errorf("expected 1 masked, got %d", result.MaskedCount)
	}
}

func TestCreateMaskedToolMessage_NilContent(t *testing.T) {
	msg := loop.Message{Role: loop.RoleTool, Content: nil}
	result := createMaskedToolMessage(msg)
	if result.Content != nil {
		t.Error("expected nil content to pass through")
	}
}

func TestCreateMaskedToolMessage_NoParts(t *testing.T) {
	msg := loop.Message{
		Role: loop.RoleTool,
		Content: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{},
		},
	}
	result := createMaskedToolMessage(msg)
	// Returns original message unchanged when no parts
	if len(result.Content.Parts) != 0 {
		t.Error("expected empty parts to pass through unchanged")
	}
}

func TestCreateMaskedToolMessage_PreservesNonFunctionParts(t *testing.T) {
	msg := loop.Message{
		Role: loop.RoleTool,
		Content: &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "some text"},
				{FunctionResponse: &genai.FunctionResponse{
					ID:       "call_test",
					Name:     "test",
					Response: map[string]any{"result": "data"},
				}},
			},
		},
	}
	result := createMaskedToolMessage(msg)
	if len(result.Content.Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(result.Content.Parts))
	}
	// First part should be unchanged text
	if result.Content.Parts[0].Text != "some text" {
		t.Error("text part should be preserved")
	}
	// Second part should be masked
	if result.Content.Parts[1].FunctionResponse.Response["_masked"] == nil {
		t.Error("function response should be masked")
	}
	if result.Content.Parts[1].FunctionResponse.ID != "call_test" {
		t.Errorf("expected function response ID to be preserved, got %q", result.Content.Parts[1].FunctionResponse.ID)
	}
}
