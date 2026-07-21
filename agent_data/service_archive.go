package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Archive operations

// AddArchiveEntry adds a new archive entry.
func (s *AgentService) AddArchiveEntry(ctx context.Context, summary, tags, content string) error {
	dao := s.db.Table(ArchiveEntry{})
	entry := &ArchiveEntry{
		ID:        newID(),
		AgentID:   s.agentID,
		Summary:   summary,
		Tags:      tags,
		Content:   content,
		CreatedAt: time.Now(),
	}
	return dao.Insert(ctx, entry)
}

// GetArchiveEntries retrieves archive entries, newest first.
func (s *AgentService) GetArchiveEntries(ctx context.Context, limit int) ([]*ArchiveEntry, error) {
	dao := s.db.Table(ArchiveEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}

	entries := make([]*ArchiveEntry, len(results))
	for i, r := range results {
		entries[i] = r.(*ArchiveEntry)
	}
	return entries, nil
}

// SearchArchive searches archive entries by query.
func (s *AgentService) SearchArchive(ctx context.Context, query string, limit int) ([]*ArchiveEntry, error) {
	// Get all entries and filter in memory (database doesn't support LIKE)
	entries, err := s.GetArchiveEntries(ctx, 0) // 0 = no limit
	if err != nil {
		return nil, err
	}

	var matched []*ArchiveEntry
	queryLower := toLower(query)
	for _, entry := range entries {
		if contains(toLower(entry.Summary), queryLower) ||
			contains(toLower(entry.Tags), queryLower) ||
			contains(toLower(entry.Content), queryLower) {
			matched = append(matched, entry)
			if limit > 0 && len(matched) >= limit {
				break
			}
		}
	}
	return matched, nil
}
