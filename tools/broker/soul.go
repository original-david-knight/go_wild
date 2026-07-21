package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SoulTools proxies soul operations through the broker API.
type SoulTools struct {
	client *Client
}

func NewSoulTools(client *Client) *SoulTools {
	return &SoulTools{client: client}
}

func (s *SoulTools) ReadSoulTool(ctx context.Context, input tools.ReadSoulInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "read_soul", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SoulTools) UpdateSoulTool(ctx context.Context, input tools.UpdateSoulInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "update_soul", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SoulTools) DescribeTool(name string) string {
	return tools.NewSoulTools(nil).DescribeTool(name)
}
