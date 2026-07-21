// Package gowild_agentic_loop provides an agentic loop for LLM interactions with tool support.
package gowild_agentic_loop

import "google.golang.org/genai"

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleUser  MessageRole = "user"
	RoleModel MessageRole = "model"
	RoleTool  MessageRole = "tool"
)

// Message represents a conversation message.
type Message struct {
	Role    MessageRole
	Content *genai.Content
}

// NewUserMessage creates a new user message from text.
func NewUserMessage(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: genai.NewContentFromText(text, genai.RoleUser),
	}
}

// NewUserMessageWithImage creates a new user message with text and an image.
func NewUserMessageWithImage(text string, imageData []byte, mimeType string) Message {
	parts := []*genai.Part{
		{Text: text},
		genai.NewPartFromBytes(imageData, mimeType),
	}
	return Message{
		Role: RoleUser,
		Content: &genai.Content{
			Role:  string(genai.RoleUser),
			Parts: parts,
		},
	}
}

// NewModelTextMessage creates a new model message from text.
func NewModelTextMessage(text string) Message {
	return Message{
		Role:    RoleModel,
		Content: genai.NewContentFromText(text, genai.RoleModel),
	}
}

// NewToolResultMessage creates a message containing a tool result.
func NewToolResultMessage(toolName string, result map[string]any) Message {
	return NewToolResultMessageWithCallID(toolName, "", result)
}

// NewToolResultMessageWithCallID creates a tool-result message linked to a prior
// function call ID when one is available.
func NewToolResultMessageWithCallID(toolName, callID string, result map[string]any) Message {
	return Message{
		Role: RoleTool,
		Content: &genai.Content{
			Role: string(genai.RoleUser),
			Parts: []*genai.Part{{
				FunctionResponse: &genai.FunctionResponse{
					ID:       callID,
					Name:     toolName,
					Response: result,
				},
			}},
		},
	}
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Success bool
	Content any
	Error   string
	// Image holds optional image data returned by the tool.
	// When set, the image is included in the function response to the model.
	Image *ImageData
}

// ImageData represents image data that can be returned from a tool.
type ImageData struct {
	Data     []byte // Raw image bytes
	MIMEType string // e.g., "image/png", "image/jpeg"
}

// NewSuccessResult creates a successful tool result.
func NewSuccessResult(content any) *ToolResult {
	return &ToolResult{
		Success: true,
		Content: content,
	}
}

// NewSuccessResultWithImage creates a successful tool result with an image.
func NewSuccessResultWithImage(content any, imageData []byte, mimeType string) *ToolResult {
	return &ToolResult{
		Success: true,
		Content: content,
		Image: &ImageData{
			Data:     imageData,
			MIMEType: mimeType,
		},
	}
}

// NewErrorResult creates an error tool result.
func NewErrorResult(err string) *ToolResult {
	return &ToolResult{
		Success: false,
		Error:   err,
	}
}

// HasImage returns true if the result contains image data.
func (r *ToolResult) HasImage() bool {
	return r.Image != nil && len(r.Image.Data) > 0
}

// ToMap converts the ToolResult to a map for Gemini API.
func (r *ToolResult) ToMap() map[string]any {
	if r.Success {
		switch v := r.Content.(type) {
		case map[string]any:
			return v
		case string:
			return map[string]any{"result": v}
		default:
			return map[string]any{"result": v}
		}
	}
	return map[string]any{"error": r.Error}
}

// ModelUsage tracks token usage from the model.
type ModelUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Event represents events streamed from the agentic loop.
type Event interface {
	eventMarker()
}

// TextDeltaEvent is emitted when new text is generated.
type TextDeltaEvent struct {
	Text string
}

func (TextDeltaEvent) eventMarker() {}

// ToolCallEvent is emitted when the model requests a tool call.
type ToolCallEvent struct {
	ID    string
	Name  string
	Input map[string]any
}

func (ToolCallEvent) eventMarker() {}

// ToolResultEvent is emitted after a tool execution completes.
type ToolResultEvent struct {
	ID     string
	Name   string
	Result *ToolResult
}

func (ToolResultEvent) eventMarker() {}

// DoneEvent is emitted when the loop completes.
type DoneEvent struct {
	Usage      ModelUsage
	FinalText  string
	TurnCount  int
	StopReason string
	History    []Message // Full conversation history including tool calls and results
}

func (DoneEvent) eventMarker() {}

// ErrorEvent is emitted when an error occurs.
type ErrorEvent struct {
	Err error
}

func (ErrorEvent) eventMarker() {}

// CompactionEvent is emitted when mid-run context compaction occurs.
type CompactionEvent struct {
	PromptTokensBefore int // Token count before compaction
	PromptTokensAfter  int // Estimated token count after compaction (0 if unknown)
	MessagesCompacted  int // Number of messages affected
}

func (CompactionEvent) eventMarker() {}

// ContextLimitEvent is emitted when the context size exceeds the configured limit.
// The loop stops after emitting this event to prevent API errors.
type ContextLimitEvent struct {
	PromptTokens int // Current prompt token count
	MaxTokens    int // Configured maximum tokens
}

func (ContextLimitEvent) eventMarker() {}

// ThinkingEvent is emitted when the agent is waiting for a response from the model.
type ThinkingEvent struct {
	Turn int // Current turn number (1-indexed)
}

func (ThinkingEvent) eventMarker() {}
