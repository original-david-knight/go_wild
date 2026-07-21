package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// RecordSpend records a spend entry for the agent.
func (s *AgentService) RecordSpend(ctx context.Context, category string, amount float64, toolName, detail string) error {
	dao := s.db.Table(SpendEntry{})
	entry := &SpendEntry{
		ID:        newID(),
		AgentID:   s.agentID,
		Category:  category,
		Amount:    amount,
		ToolName:  toolName,
		Detail:    detail,
		CreatedAt: time.Now(),
	}
	return dao.Insert(ctx, entry)
}

// GetTodaySpend returns the total spend for the agent in a category for today (UTC).
func (s *AgentService) GetTodaySpend(ctx context.Context, category string) (float64, error) {
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := startOfDay.Add(24 * time.Hour)

	entries, err := s.GetSpendHistory(ctx, startOfDay, endOfDay)
	if err != nil {
		return 0, err
	}

	var total float64
	for _, e := range entries {
		if e.Category == category {
			total += e.Amount
		}
	}
	return total, nil
}

// GetSpendLimit returns the daily spend limit for the agent in a category.
// Returns 0 if no limit is set.
func (s *AgentService) GetSpendLimit(ctx context.Context, category string) (float64, error) {
	dao := s.db.Table(SpendLimit{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID, "category": category},
		Limit: 1,
	})
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0].(*SpendLimit).DailyLimit, nil
}

// SetSpendLimit sets the daily spend limit for the agent in a category.
func (s *AgentService) SetSpendLimit(ctx context.Context, category string, dailyLimit float64) error {
	dao := s.db.Table(SpendLimit{})

	// Check if limit already exists
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID, "category": category},
		Limit: 1,
	})
	if err != nil {
		return err
	}

	if len(results) > 0 {
		existing := results[0].(*SpendLimit)
		existing.DailyLimit = dailyLimit
		return dao.Update(ctx, existing)
	}

	limit := &SpendLimit{
		ID:         newID(),
		AgentID:    s.agentID,
		Category:   category,
		DailyLimit: dailyLimit,
	}
	return dao.Insert(ctx, limit)
}

// GetSpendHistory returns spend entries for the agent between the given times.
func (s *AgentService) GetSpendHistory(ctx context.Context, from, to time.Time) ([]SpendEntry, error) {
	dao := s.db.Table(SpendEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": s.agentID},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}

	var entries []SpendEntry
	for _, r := range results {
		e := r.(*SpendEntry)
		if !e.CreatedAt.Before(from) && e.CreatedAt.Before(to) {
			entries = append(entries, *e)
		}
	}
	return entries, nil
}
