package main

import (
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

// Helper to create tool result messages for testing
func createToolResultMessage(toolName string, result map[string]any) loop.Message {
	return loop.Message{
		Role: loop.RoleTool,
		Content: &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						Name:     toolName,
						Response: result,
					},
				},
			},
		},
	}
}

// Helper to extract text from a message
func extractText(msg loop.Message) string {
	if msg.Content == nil {
		return ""
	}
	for _, part := range msg.Content.Parts {
		if part.Text != "" {
			return part.Text
		}
	}
	return ""
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
