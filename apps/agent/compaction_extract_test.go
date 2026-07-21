package main

import (
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

func TestExtractMessageText(t *testing.T) {
	// Test user message
	userMsg := loop.NewUserMessage("Test message")
	text := extractMessageText(userMsg)
	if text != "Test message" {
		t.Errorf("Expected 'Test message', got '%s'", text)
	}

	// Test model message
	modelMsg := loop.NewModelTextMessage("Model response")
	text = extractMessageText(modelMsg)
	if text != "Model response" {
		t.Errorf("Expected 'Model response', got '%s'", text)
	}
}

func TestExtractMessageText_FunctionCall(t *testing.T) {
	msg := loop.Message{
		Role: loop.RoleModel,
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{
					Name: "read_file",
					Args: map[string]any{"path": "/data/test.go"},
				}},
			},
		},
	}
	text := extractMessageText(msg)
	if !contains(text, "[Called tool: read_file]") {
		t.Errorf("expected tool call text, got: %s", text)
	}
}

func TestExtractMessageText_FunctionResponse(t *testing.T) {
	// With response
	msg := createToolResultMessage("search", map[string]any{"result": "found 3 items"})
	text := extractMessageText(msg)
	if !contains(text, "[Tool search result:") {
		t.Errorf("expected tool result text, got: %s", text)
	}

	// With nil response
	msg2 := loop.Message{
		Role: loop.RoleTool,
		Content: &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{FunctionResponse: &genai.FunctionResponse{
					Name:     "test_tool",
					Response: nil,
				}},
			},
		},
	}
	text2 := extractMessageText(msg2)
	if !contains(text2, "[Tool test_tool completed]") {
		t.Errorf("expected tool completed text, got: %s", text2)
	}
}

func TestExtractMessageText_InlineData(t *testing.T) {
	msg := loop.Message{
		Role: loop.RoleUser,
		Content: &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{
					MIMEType: "image/png",
					Data:     []byte{0x89, 0x50},
				}},
			},
		},
	}
	text := extractMessageText(msg)
	if !contains(text, "[Image attached]") {
		t.Errorf("expected '[Image attached]', got: %s", text)
	}
}

func TestExtractMessageText_ThoughtSkipped(t *testing.T) {
	msg := loop.Message{
		Role: loop.RoleModel,
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: "visible text", Thought: false},
				{Text: "hidden thought", Thought: true},
			},
		},
	}
	text := extractMessageText(msg)
	if !contains(text, "visible text") {
		t.Errorf("expected visible text, got: %s", text)
	}
	if contains(text, "hidden thought") {
		t.Error("thought text should be skipped")
	}
}

func TestExtractMessageText_NilContent(t *testing.T) {
	msg := loop.Message{Role: loop.RoleUser, Content: nil}
	text := extractMessageText(msg)
	if text != "" {
		t.Errorf("expected empty string, got: %s", text)
	}
}

func TestGetRoleLabel(t *testing.T) {
	if getRoleLabel(loop.RoleUser) != "USER" {
		t.Error("Expected USER for RoleUser")
	}
	if getRoleLabel(loop.RoleModel) != "ASSISTANT" {
		t.Error("Expected ASSISTANT for RoleModel")
	}
	if getRoleLabel(loop.RoleTool) != "TOOL RESULT" {
		t.Error("Expected TOOL RESULT for RoleTool")
	}
}
