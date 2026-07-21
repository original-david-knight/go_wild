package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Memory operations

// GetMemory retrieves the agent's short-term memory.
func (s *AgentService) GetMemory(ctx context.Context) (*MemoryEntry, error) {
	dao := s.db.Table(MemoryEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID},
		OrderBy:   "updated_at",
		OrderDesc: true,
		Limit:     1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*MemoryEntry), nil
}

// SaveMemory saves the agent's short-term memory.
func (s *AgentService) SaveMemory(ctx context.Context, content string) error {
	dao := s.db.Table(MemoryEntry{})
	now := time.Now()

	// Check if memory exists
	existing, err := s.GetMemory(ctx)
	if err != nil {
		return err
	}

	if existing != nil {
		// Update existing
		existing.Content = content
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	// Create new
	entry := &MemoryEntry{
		ID:        newID(),
		AgentID:   s.agentID,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return dao.Insert(ctx, entry)
}
