package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Soul operations

// GetSoul retrieves the agent's soul.
func (s *AgentService) GetSoul(ctx context.Context) (*Soul, error) {
	dao := s.db.Table(Soul{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*Soul), nil
}

// SaveSoul saves the agent's soul.
func (s *AgentService) SaveSoul(ctx context.Context, content string) error {
	dao := s.db.Table(Soul{})
	now := time.Now()

	existing, err := s.GetSoul(ctx)
	if err != nil {
		return err
	}

	if existing != nil {
		existing.Content = content
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	soul := &Soul{
		ID:        newID(),
		AgentID:   s.agentID,
		Content:   content,
		UpdatedAt: now,
	}
	return dao.Insert(ctx, soul)
}
