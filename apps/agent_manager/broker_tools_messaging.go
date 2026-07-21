package main

import (
	"context"
	"fmt"

	"github.com/original-david-knight/go_wild/agent_data"
)

type messagingToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, inputJSON []byte) (any, error)

type sendAgentMessageInput struct {
	ToAgentID string `json:"to_agent_id"`
	Content   string `json:"content"`
}

type readAgentMessagesInput struct {
	PeerAgentID string `json:"peer_agent_id"`
	UnreadOnly  bool   `json:"unread_only"`
	Limit       int    `json:"limit"`
}

type markAgentMessagesReadInput struct {
	PeerAgentID string `json:"peer_agent_id"`
}

var messagingToolHandlers = map[string]messagingToolHandlerFunc{
	"list_peers": func(_ *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, _ []byte) (any, error) {
		peers, err := svc.GetPeerAgents(ctx)
		if err != nil {
			return nil, err
		}
		counts, err := svc.GetUnreadMessageCounts(ctx)
		if err != nil {
			return nil, err
		}
		peerList := make([]map[string]any, len(peers))
		for i, p := range peers {
			peerList[i] = map[string]any{
				"agent_id":     p.ID,
				"name":         p.Name,
				"unread_count": counts[p.ID],
			}
		}
		return map[string]any{"peers": peerList}, nil
	},
	"send_agent_message": func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[sendAgentMessageInput](inputJSON, func(input sendAgentMessageInput) (any, error) {
			if input.ToAgentID == "" || input.Content == "" {
				return nil, fmt.Errorf("to_agent_id and content are required")
			}
			msg, err := svc.SendAgentMessage(ctx, input.ToAgentID, input.Content)
			if err != nil {
				return nil, err
			}
			// Send heartbeat to recipient if running.
			if h.workerManager != nil {
				senderName := agentID
				agent, err := svc.GetAgent(ctx)
				if err == nil && agent.Name != "" {
					senderName = agent.Name
				}
				heartbeatMsg := fmt.Sprintf("You have a new instant message from %s. Use read_messages to read and respond to their messages. Do not do any research or other work on this heartbeat unless needed to answer the message. You do not need to reply if the message has no question or request — only reply when there is something to respond to. Answer messages and then finish.", senderName)
				_ = h.workerManager.SendHeartbeat(input.ToAgentID, heartbeatMsg)
			}
			return map[string]any{
				"message_id": msg.ID,
				"to":         msg.ToAgentID,
				"sent_at":    msg.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			}, nil
		})
	},
	"read_agent_messages": func(_ *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[readAgentMessagesInput](inputJSON, func(input readAgentMessagesInput) (any, error) {
			if input.PeerAgentID == "" {
				return nil, fmt.Errorf("peer_agent_id is required")
			}
			messages, err := svc.GetAgentMessages(ctx, input.PeerAgentID, input.Limit, input.UnreadOnly)
			if err != nil {
				return nil, err
			}
			msgList := make([]map[string]any, len(messages))
			for i, m := range messages {
				entry := map[string]any{
					"id":         m.ID,
					"from":       m.FromAgentID,
					"to":         m.ToAgentID,
					"content":    m.Content,
					"created_at": m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				}
				if m.ReadAt != nil {
					entry["read_at"] = m.ReadAt.Format("2006-01-02T15:04:05Z07:00")
				}
				msgList[i] = entry
			}
			return map[string]any{"messages": msgList}, nil
		})
	},
	"mark_agent_messages_read": func(_ *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[markAgentMessagesReadInput](inputJSON, func(input markAgentMessagesReadInput) (any, error) {
			if input.PeerAgentID == "" {
				return nil, fmt.Errorf("peer_agent_id is required")
			}
			if err := svc.MarkAllMessagesRead(ctx, input.PeerAgentID); err != nil {
				return nil, err
			}
			return map[string]any{"status": "ok"}, nil
		})
	},
}

func isMessagingTool(toolName string) bool {
	_, ok := messagingToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callMessagingTools(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isMessagingTool(toolName) {
		return false, nil, nil
	}

	handler, ok := messagingToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, agentID, svc, inputJSON)
	return true, result, err
}
