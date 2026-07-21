package gowild_agentic_loop

import (
	"context"

	"google.golang.org/genai"
)

// RunSync is a convenience method that runs the loop and collects all events.
// It blocks until the loop completes and returns the final result.
func (l *AgenticLoop) RunSync(ctx context.Context, history []Message) (*DoneEvent, error) {
	var done *DoneEvent
	var lastErr error

	for event := range l.Run(ctx, history) {
		switch e := event.(type) {
		case DoneEvent:
			done = &e
		case ErrorEvent:
			lastErr = e.Err
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return done, nil
}

// Prompt is a convenience method for single-turn interactions.
// It creates a user message and runs the loop.
func (l *AgenticLoop) Prompt(ctx context.Context, text string) (<-chan Event, error) {
	history := []Message{NewUserMessage(text)}
	return l.Run(ctx, history), nil
}

// buildToolResultContent creates a Content for a tool result, optionally including images.
func buildToolResultContent(name, callID string, result *ToolResult) *genai.Content {
	return NewToolResultMessageWithCallID(name, callID, result.ToMap()).Content
}

// contentsToMessages converts []*genai.Content back to []Message with proper roles.
func contentsToMessages(contents []*genai.Content) []Message {
	messages := make([]Message, 0, len(contents))
	for _, c := range contents {
		if c == nil {
			continue
		}
		var role MessageRole
		switch c.Role {
		case string(genai.RoleModel):
			role = RoleModel
		case string(genai.RoleUser):
			// Distinguish tool results from user messages by checking for FunctionResponse parts
			role = RoleUser
			for _, part := range c.Parts {
				if part.FunctionResponse != nil {
					role = RoleTool
					break
				}
			}
		default:
			role = RoleUser
		}
		messages = append(messages, Message{Role: role, Content: c})
	}
	return messages
}
