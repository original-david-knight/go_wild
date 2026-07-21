package gowild_agentic_loop

import (
	"testing"
)

func TestNewSuccessResult(t *testing.T) {
	result := NewSuccessResult("test content")

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.Content != "test content" {
		t.Errorf("expected content 'test content', got %v", result.Content)
	}
	if result.Error != "" {
		t.Errorf("expected empty error, got %s", result.Error)
	}
}

func TestNewErrorResult(t *testing.T) {
	result := NewErrorResult("something went wrong")

	if result.Success {
		t.Error("expected Success to be false")
	}
	if result.Error != "something went wrong" {
		t.Errorf("expected error 'something went wrong', got %s", result.Error)
	}
}

func TestToolResult_ToMap_Success_String(t *testing.T) {
	result := NewSuccessResult("hello")
	m := result.ToMap()

	if m["result"] != "hello" {
		t.Errorf("expected result 'hello', got %v", m["result"])
	}
}

func TestToolResult_ToMap_Success_Map(t *testing.T) {
	content := map[string]any{
		"temperature": 72,
		"condition":   "sunny",
	}
	result := NewSuccessResult(content)
	m := result.ToMap()

	if m["temperature"] != 72 {
		t.Errorf("expected temperature 72, got %v", m["temperature"])
	}
	if m["condition"] != "sunny" {
		t.Errorf("expected condition 'sunny', got %v", m["condition"])
	}
}

func TestToolResult_ToMap_Error(t *testing.T) {
	result := NewErrorResult("failed")
	m := result.ToMap()

	if m["error"] != "failed" {
		t.Errorf("expected error 'failed', got %v", m["error"])
	}
}

func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("Hello, world!")

	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", msg.Role)
	}
	if msg.Content == nil {
		t.Fatal("expected non-nil content")
	}
}

func TestNewToolResultMessage(t *testing.T) {
	result := map[string]any{"data": "value"}
	msg := NewToolResultMessage("my_tool", result)

	if msg.Role != RoleTool {
		t.Errorf("expected RoleTool, got %v", msg.Role)
	}
	if msg.Content == nil {
		t.Fatal("expected non-nil content")
	}
}

func TestNewToolResultMessageWithCallID(t *testing.T) {
	result := map[string]any{"data": "value"}
	msg := NewToolResultMessageWithCallID("my_tool", "call_123", result)

	if msg.Content == nil || len(msg.Content.Parts) != 1 {
		t.Fatal("expected one tool result part")
	}
	if msg.Content.Parts[0].FunctionResponse == nil {
		t.Fatal("expected function response")
	}
	if msg.Content.Parts[0].FunctionResponse.ID != "call_123" {
		t.Fatalf("expected call ID call_123, got %q", msg.Content.Parts[0].FunctionResponse.ID)
	}
}

func TestEventMarkers(t *testing.T) {
	// Verify all event types implement Event interface
	var _ Event = TextDeltaEvent{}
	var _ Event = ToolCallEvent{}
	var _ Event = ToolResultEvent{}
	var _ Event = DoneEvent{}
	var _ Event = ErrorEvent{}
}

func TestTextDeltaEvent(t *testing.T) {
	event := TextDeltaEvent{Text: "hello"}
	if event.Text != "hello" {
		t.Errorf("expected text 'hello', got %s", event.Text)
	}
}

func TestToolCallEvent(t *testing.T) {
	event := ToolCallEvent{
		ID:    "call_1",
		Name:  "get_weather",
		Input: map[string]any{"location": "SF"},
	}

	if event.ID != "call_1" {
		t.Errorf("expected ID 'call_1', got %s", event.ID)
	}
	if event.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %s", event.Name)
	}
	if event.Input["location"] != "SF" {
		t.Errorf("expected location 'SF', got %v", event.Input["location"])
	}
}

func TestDoneEvent(t *testing.T) {
	event := DoneEvent{
		Usage: ModelUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
		FinalText:  "The answer is 42",
		TurnCount:  3,
		StopReason: "stop",
	}

	if event.Usage.TotalTokens != 150 {
		t.Errorf("expected total tokens 150, got %d", event.Usage.TotalTokens)
	}
	if event.TurnCount != 3 {
		t.Errorf("expected turn count 3, got %d", event.TurnCount)
	}
}

func TestNewUserMessageWithImage(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	msg := NewUserMessageWithImage("describe this image", imageData, "image/png")

	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", msg.Role)
	}
	if msg.Content == nil {
		t.Fatal("expected non-nil content")
	}
	if len(msg.Content.Parts) != 2 {
		t.Fatalf("expected 2 parts (text + image), got %d", len(msg.Content.Parts))
	}
	if msg.Content.Parts[0].Text != "describe this image" {
		t.Errorf("expected text 'describe this image', got %q", msg.Content.Parts[0].Text)
	}
	if msg.Content.Parts[1].InlineData == nil {
		t.Fatal("expected inline data for image part")
	}
	if msg.Content.Parts[1].InlineData.MIMEType != "image/png" {
		t.Errorf("expected MIME type 'image/png', got %q", msg.Content.Parts[1].InlineData.MIMEType)
	}
	if string(msg.Content.Parts[1].InlineData.Data) != string(imageData) {
		t.Error("image data not preserved")
	}
}

func TestNewModelTextMessage(t *testing.T) {
	msg := NewModelTextMessage("The answer is 42")

	if msg.Role != RoleModel {
		t.Errorf("expected RoleModel, got %v", msg.Role)
	}
	if msg.Content == nil {
		t.Fatal("expected non-nil content")
	}
	if len(msg.Content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Content.Parts))
	}
	if msg.Content.Parts[0].Text != "The answer is 42" {
		t.Errorf("expected text 'The answer is 42', got %q", msg.Content.Parts[0].Text)
	}
}

func TestNewSuccessResultWithImage(t *testing.T) {
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG header
	result := NewSuccessResultWithImage("screenshot taken", imageData, "image/jpeg")

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.Content != "screenshot taken" {
		t.Errorf("expected content 'screenshot taken', got %v", result.Content)
	}
	if result.Image == nil {
		t.Fatal("expected non-nil Image")
	}
	if string(result.Image.Data) != string(imageData) {
		t.Error("image data not preserved")
	}
	if result.Image.MIMEType != "image/jpeg" {
		t.Errorf("expected MIME type 'image/jpeg', got %q", result.Image.MIMEType)
	}
}

func TestHasImage(t *testing.T) {
	// No image
	result := NewSuccessResult("text only")
	if result.HasImage() {
		t.Error("expected HasImage() to be false for text-only result")
	}

	// With image
	result = NewSuccessResultWithImage("with image", []byte{0x01, 0x02}, "image/png")
	if !result.HasImage() {
		t.Error("expected HasImage() to be true for result with image")
	}

	// Nil image field
	result = &ToolResult{Success: true, Image: nil}
	if result.HasImage() {
		t.Error("expected HasImage() to be false for nil Image")
	}

	// Empty image data
	result = &ToolResult{Success: true, Image: &ImageData{Data: []byte{}, MIMEType: "image/png"}}
	if result.HasImage() {
		t.Error("expected HasImage() to be false for empty image data")
	}
}

func TestToolResult_ToMap_DefaultType(t *testing.T) {
	// Content that is neither string nor map
	result := NewSuccessResult(42)
	m := result.ToMap()
	if m["result"] != 42 {
		t.Errorf("expected result 42, got %v", m["result"])
	}

	// Boolean content
	result = NewSuccessResult(true)
	m = result.ToMap()
	if m["result"] != true {
		t.Errorf("expected result true, got %v", m["result"])
	}

	// Nil content
	result = NewSuccessResult(nil)
	m = result.ToMap()
	if _, ok := m["result"]; !ok {
		t.Error("expected 'result' key in map")
	}
}

func TestCompactionEvent(t *testing.T) {
	event := CompactionEvent{
		PromptTokensBefore: 100000,
		PromptTokensAfter:  50000,
		MessagesCompacted:  15,
	}

	if event.PromptTokensBefore != 100000 {
		t.Errorf("expected PromptTokensBefore 100000, got %d", event.PromptTokensBefore)
	}
	if event.MessagesCompacted != 15 {
		t.Errorf("expected MessagesCompacted 15, got %d", event.MessagesCompacted)
	}
}

func TestContextLimitEvent(t *testing.T) {
	event := ContextLimitEvent{
		PromptTokens: 500000,
		MaxTokens:    400000,
	}

	if event.PromptTokens != 500000 {
		t.Errorf("expected PromptTokens 500000, got %d", event.PromptTokens)
	}
}

func TestThinkingEvent(t *testing.T) {
	event := ThinkingEvent{Turn: 3}
	if event.Turn != 3 {
		t.Errorf("expected Turn 3, got %d", event.Turn)
	}
}

func TestAllEventTypesImplementEvent(t *testing.T) {
	// Verify all event types including newer ones implement Event
	events := []Event{
		TextDeltaEvent{},
		ToolCallEvent{},
		ToolResultEvent{},
		DoneEvent{},
		ErrorEvent{},
		CompactionEvent{},
		ContextLimitEvent{},
		ThinkingEvent{},
	}
	if len(events) != 8 {
		t.Errorf("expected 8 event types, got %d", len(events))
	}
}
