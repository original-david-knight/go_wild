package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// MessagingTools proxies inter-agent messaging operations through the broker API.
type MessagingTools struct {
	client *Client
}

// NewMessagingTools creates broker-backed messaging tools.
func NewMessagingTools(client *Client) *MessagingTools {
	return &MessagingTools{client: client}
}

func (m *MessagingTools) ListPeersTool(ctx context.Context, input tools.ListPeersInput) (*loop.ToolResult, error) {
	result, err := m.client.CallTool(ctx, "list_peers", map[string]any{})
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (m *MessagingTools) SendMessageTool(ctx context.Context, input tools.SendMessageInput) (*loop.ToolResult, error) {
	result, err := m.client.CallTool(ctx, "send_agent_message", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (m *MessagingTools) ReadMessagesTool(ctx context.Context, input tools.ReadMessagesInput) (*loop.ToolResult, error) {
	result, err := m.client.CallTool(ctx, "read_agent_messages", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (m *MessagingTools) MarkMessagesReadTool(ctx context.Context, input tools.MarkMessagesReadInput) (*loop.ToolResult, error) {
	result, err := m.client.CallTool(ctx, "mark_agent_messages_read", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (m *MessagingTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"list_peers":         "List peer agents you can message and their unread message counts",
		"send_message":       "Send a message to a peer agent",
		"read_messages":      "Read messages exchanged with a peer agent",
		"mark_messages_read": "Mark all messages from a peer agent as read",
	}
	return descriptions[name]
}
