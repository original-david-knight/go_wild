package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Chat Message operations

// SaveChatMessage saves a chat message for the agent.
func (s *AgentService) SaveChatMessage(ctx context.Context, role, content string) error {
	dao := s.db.Table(ChatMessage{})
	msg := &ChatMessage{
		ID:        newID(),
		AgentID:   s.agentID,
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	}
	return dao.Insert(ctx, msg)
}

// GetChatHistory retrieves recent chat messages for the agent in chronological order.
func (s *AgentService) GetChatHistory(ctx context.Context, limit int) ([]*ChatMessage, error) {
	if err := s.pruneChatHistory(ctx); err != nil {
		return nil, err
	}

	dao := s.db.Table(ChatMessage{})
	limitOpt := limit
	if limitOpt == 0 {
		limitOpt = 50
	} else if limitOpt < 0 {
		// Negative limit means "no limit" (return all).
		limitOpt = 0
	}
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limitOpt,
	})
	if err != nil {
		return nil, err
	}

	// Reverse to chronological order
	msgs := make([]*ChatMessage, len(results))
	for i, r := range results {
		msgs[len(results)-1-i] = r.(*ChatMessage)
	}
	return msgs, nil
}

func (s *AgentService) pruneChatHistory(ctx context.Context) error {
	retention := chatRetentionDuration()
	if retention <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-retention)
	dao := s.db.Table(ChatMessage{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		msg := r.(*ChatMessage)
		if msg.CreatedAt.Before(cutoff) {
			if err := dao.Delete(ctx, msg.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// deleteChatHistory deletes all chat messages for the agent.
func (s *AgentService) deleteChatHistory(ctx context.Context) error {
	dao := s.db.Table(ChatMessage{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		msg := r.(*ChatMessage)
		if err := dao.Delete(ctx, msg.ID); err != nil {
			return err
		}
	}
	return nil
}
