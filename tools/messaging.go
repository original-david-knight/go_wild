package tools

import (
	"context"
	"fmt"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ListPeersInput defines the input for listing peer agents.
type ListPeersInput struct{}

// SendMessageInput defines the input for sending a message to a peer agent.
type SendMessageInput struct {
	ToAgentID string `json:"to_agent_id" description:"The ID of the peer agent to send the message to" required:"true"`
	Content   string `json:"content" description:"The message content to send" required:"true"`
}

// ReadMessagesInput defines the input for reading messages with a peer.
type ReadMessagesInput struct {
	PeerAgentID string `json:"peer_agent_id" description:"The ID of the peer agent to read messages with" required:"true"`
	UnreadOnly  bool   `json:"unread_only" description:"If true, only return unread messages. Default false." required:"false"`
	Limit       int    `json:"limit" description:"Maximum number of messages to return. Default 20." required:"false"`
}

// MarkMessagesReadInput defines the input for marking messages from a peer as read.
type MarkMessagesReadInput struct {
	PeerAgentID string `json:"peer_agent_id" description:"The ID of the peer agent whose messages to mark as read" required:"true"`
}

// MessagingTools provides inter-agent messaging tools.
type MessagingTools struct {
	service *data.AgentService
}

// NewMessagingTools creates a new MessagingTools instance.
func NewMessagingTools(service *data.AgentService) *MessagingTools {
	if service == nil {
		return nil
	}
	return &MessagingTools{service: service}
}

// ListPeersTool lists agents you can message along with unread message counts.
func (m *MessagingTools) ListPeersTool(ctx context.Context, input ListPeersInput) (*loop.ToolResult, error) {
	peers, err := m.service.GetPeerAgents(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to list peers: %v", err)), nil
	}
	if len(peers) == 0 {
		return loop.NewSuccessResult(map[string]any{"peers": []any{}, "message": "No peers available"}), nil
	}

	counts, err := m.service.GetUnreadMessageCounts(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get unread counts: %v", err)), nil
	}

	peerList := make([]map[string]any, len(peers))
	for i, p := range peers {
		peerList[i] = map[string]any{
			"agent_id":     p.ID,
			"name":         p.Name,
			"unread_count": counts[p.ID],
		}
	}
	return loop.NewSuccessResult(map[string]any{"peers": peerList}), nil
}

// SendMessageTool sends a message to a peer agent.
func (m *MessagingTools) SendMessageTool(ctx context.Context, input SendMessageInput) (*loop.ToolResult, error) {
	if input.ToAgentID == "" {
		return loop.NewErrorResult("to_agent_id is required"), nil
	}
	if input.Content == "" {
		return loop.NewErrorResult("content is required"), nil
	}

	msg, err := m.service.SendAgentMessage(ctx, input.ToAgentID, input.Content)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to send message: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"message_id": msg.ID,
		"to":         msg.ToAgentID,
		"sent_at":    msg.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}), nil
}

// ReadMessagesTool reads messages with a peer agent.
func (m *MessagingTools) ReadMessagesTool(ctx context.Context, input ReadMessagesInput) (*loop.ToolResult, error) {
	if input.PeerAgentID == "" {
		return loop.NewErrorResult("peer_agent_id is required"), nil
	}

	messages, err := m.service.GetAgentMessages(ctx, input.PeerAgentID, input.Limit, input.UnreadOnly)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to read messages: %v", err)), nil
	}

	msgList := make([]map[string]any, len(messages))
	for i, msg := range messages {
		entry := map[string]any{
			"id":         msg.ID,
			"from":       msg.FromAgentID,
			"to":         msg.ToAgentID,
			"content":    msg.Content,
			"created_at": msg.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if msg.ReadAt != nil {
			entry["read_at"] = msg.ReadAt.Format("2006-01-02T15:04:05Z07:00")
		}
		msgList[i] = entry
	}

	return loop.NewSuccessResult(map[string]any{"messages": msgList}), nil
}

// MarkMessagesReadTool marks all messages from a peer as read.
func (m *MessagingTools) MarkMessagesReadTool(ctx context.Context, input MarkMessagesReadInput) (*loop.ToolResult, error) {
	if input.PeerAgentID == "" {
		return loop.NewErrorResult("peer_agent_id is required"), nil
	}

	if err := m.service.MarkAllMessagesRead(ctx, input.PeerAgentID); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to mark messages read: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{"status": "ok"}), nil
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
