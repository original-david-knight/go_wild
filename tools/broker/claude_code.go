package broker

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools"
)

// ClaudeCodeTools proxies Claude Code execution through the broker API.
type ClaudeCodeTools struct {
	client *Client
}

// NewClaudeCodeTools creates broker-backed Claude Code tools.
func NewClaudeCodeTools(client *Client) *ClaudeCodeTools {
	return &ClaudeCodeTools{client: client}
}

func (c *ClaudeCodeTools) ClaudeCodeTool(ctx context.Context, input tools.ClaudeCodeInput) (*loop.ToolResult, error) {
	result, err := c.client.CallTool(ctx, "claude_code", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (c *ClaudeCodeTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"claude_code": "Run Claude Code in one-shot mode with `claude --dangerously-skip-permissions -p`. Accepts a coding prompt and target_directory. The requested /data path is mapped to the agent volume on the manager host; execution is isolated with bubblewrap and scoped to that mapped working directory.",
	}
	return descriptions[name]
}
