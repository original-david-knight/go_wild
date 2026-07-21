package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// History Summary operations

// GetHistorySummary retrieves the agent's latest history summary.
func (s *AgentService) GetHistorySummary(ctx context.Context) (*HistorySummary, error) {
	dao := s.db.Table(HistorySummary{})
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
	return results[0].(*HistorySummary), nil
}

// SaveHistorySummary saves or updates the agent's history summary.
func (s *AgentService) SaveHistorySummary(ctx context.Context, content string) error {
	dao := s.db.Table(HistorySummary{})
	now := time.Now()

	existing, err := s.GetHistorySummary(ctx)
	if err != nil {
		return err
	}

	if existing != nil {
		existing.Content = content
		existing.CreatedAt = now
		return dao.Update(ctx, existing)
	}

	summary := &HistorySummary{
		ID:        newID(),
		AgentID:   s.agentID,
		Content:   content,
		CreatedAt: now,
	}
	return dao.Insert(ctx, summary)
}

// DeleteHistorySummary deletes any stored history summary for the agent.
func (s *AgentService) DeleteHistorySummary(ctx context.Context) error {
	dao := s.db.Table(HistorySummary{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		summary := r.(*HistorySummary)
		if err := dao.Delete(ctx, summary.ID); err != nil {
			return err
		}
	}
	return nil
}
