package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SoulTools provides tools for reading and updating the agent's soul.
// The soul contains the agent's persistent identity, values, and self-knowledge.
type SoulTools struct {
	service *data.AgentService
}

// NewSoulTools creates a new SoulTools instance.
func NewSoulTools(service *data.AgentService) *SoulTools {
	return &SoulTools{
		service: service,
	}
}

// DescribeTool returns the description for a tool by name.
func (t *SoulTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"read_soul":   "Read your soul - your persistent identity, values, goals, and self-knowledge. This is already included in your system prompt, but use this to get the raw content if needed.",
		"update_soul": "Update your soul. Use this to evolve your identity, add new insights about yourself, update your goals, or refine your values. Changes persist across sessions. Be thoughtful - this is your core identity.",
	}
	return descriptions[name]
}

// ReadSoulInput defines input for reading the soul.
type ReadSoulInput struct{}

// ReadSoulTool reads the soul.
func (t *SoulTools) ReadSoulTool(ctx context.Context, input ReadSoulInput) (*loop.ToolResult, error) {
	soul, err := t.service.GetSoul(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to read soul: %v", err)), nil
	}

	if soul == nil {
		return loop.NewSuccessResult(map[string]any{
			"exists":  false,
			"content": "",
			"message": "Soul does not exist yet. Use update_soul to create it.",
		}), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"exists":  true,
		"content": soul.Content,
	}), nil
}

// UpdateSoulInput defines input for updating the soul.
type UpdateSoulInput struct {
	Content string `json:"content" description:"The complete new content for the soul. This replaces the entire content." required:"true"`
	Reason  string `json:"reason,omitempty" description:"Brief reason for this update (logged for your reference)"`
}

// UpdateSoulTool updates the soul.
func (t *SoulTools) UpdateSoulTool(ctx context.Context, input UpdateSoulInput) (*loop.ToolResult, error) {
	if strings.TrimSpace(input.Content) == "" {
		return loop.NewErrorResult("content cannot be empty - your soul must contain something"), nil
	}

	// Get existing soul to check if this is create or update
	existing, err := t.service.GetSoul(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to check existing soul: %v", err)), nil
	}

	wasCreated := existing == nil
	contentChanged := existing == nil || existing.Content != input.Content

	if err := t.service.SaveSoul(ctx, input.Content); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to save soul: %v", err)), nil
	}

	result := map[string]any{
		"success":    true,
		"created":    wasCreated,
		"updated":    contentChanged && !wasCreated,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	if input.Reason != "" {
		result["reason"] = input.Reason
	}

	if wasCreated {
		result["message"] = "Soul created successfully. This will be included in your system prompt on next session."
	} else if contentChanged {
		result["message"] = "Soul updated successfully. Changes will be reflected in your system prompt on next session."
	} else {
		result["message"] = "Soul content unchanged."
	}

	return loop.NewSuccessResult(result), nil
}
