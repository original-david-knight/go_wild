package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// History Snapshot operations

// GetHistorySnapshot retrieves the agent's latest history snapshot.
func (s *AgentService) GetHistorySnapshot(ctx context.Context) (*HistorySnapshot, error) {
	dao := s.db.Table(HistorySnapshot{})
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
	return results[0].(*HistorySnapshot), nil
}

// SaveHistorySnapshot saves or updates the agent's history snapshot.
func (s *AgentService) SaveHistorySnapshot(ctx context.Context, payload string) error {
	dao := s.db.Table(HistorySnapshot{})
	now := time.Now()

	existing, err := s.GetHistorySnapshot(ctx)
	if err != nil {
		return err
	}

	if existing != nil {
		existing.Payload = payload
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	snap := &HistorySnapshot{
		ID:        newID(),
		AgentID:   s.agentID,
		Payload:   payload,
		UpdatedAt: now,
	}
	return dao.Insert(ctx, snap)
}

// DeleteHistorySnapshot deletes any stored history snapshot for the agent.
func (s *AgentService) DeleteHistorySnapshot(ctx context.Context) error {
	dao := s.db.Table(HistorySnapshot{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		snap := r.(*HistorySnapshot)
		if err := dao.Delete(ctx, snap.ID); err != nil {
			return err
		}
	}
	return nil
}
