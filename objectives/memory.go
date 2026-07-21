package objectives

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/data"
)

// MemoryStore provides persistence for knowledge, decisions, and learnings.
type MemoryStore struct {
	db gowild_data.Database
}

// NewMemoryStore creates a memory store backed by the given database.
func NewMemoryStore(db gowild_data.Database) *MemoryStore {
	return &MemoryStore{db: db}
}

// addKnowledge inserts a new knowledge entry with auto-set ID and timestamps.
func (m *MemoryStore) addKnowledge(ctx context.Context, entry *KnowledgeEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.DiscoveredAt.IsZero() {
		entry.DiscoveredAt = time.Now().UTC()
	}
	return m.db.Table(KnowledgeEntry{}).Insert(ctx, entry)
}

// getRelevantKnowledge returns knowledge entries matching any of the given tags,
// excluding entries that have expired. Since gowild_data doesn't support
// array-contains queries, we fetch all and filter in Go.
func (m *MemoryStore) getRelevantKnowledge(ctx context.Context, tags []string, limit int) ([]*KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	results, err := m.db.Table(KnowledgeEntry{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "discovered_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("query knowledge: %w", err)
	}

	now := time.Now().UTC()
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}

	var filtered []*KnowledgeEntry
	for _, r := range results {
		entry, ok := r.(*KnowledgeEntry)
		if !ok {
			continue
		}
		// Skip expired entries
		if !entry.ExpiresAt.IsZero() && entry.ExpiresAt.Before(now) {
			continue
		}
		// Filter by tags if provided
		if len(tags) > 0 {
			if !hasMatchingTag(entry.Tags, tagSet) {
				continue
			}
		}
		filtered = append(filtered, entry)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// expireStaleKnowledge deletes entries whose expires_at is in the past.
// Returns the number of entries removed.
func (m *MemoryStore) expireStaleKnowledge(ctx context.Context) (int, error) {
	results, err := m.db.Table(KnowledgeEntry{}).Query(ctx, gowild_data.QueryOpts{})
	if err != nil {
		return 0, fmt.Errorf("query knowledge for expiry: %w", err)
	}

	now := time.Now().UTC()
	count := 0
	for _, r := range results {
		entry, ok := r.(*KnowledgeEntry)
		if !ok {
			continue
		}
		if !entry.ExpiresAt.IsZero() && entry.ExpiresAt.Before(now) {
			if err := m.db.Table(KnowledgeEntry{}).Delete(ctx, entry.ID); err != nil {
				return count, fmt.Errorf("delete expired entry %s: %w", entry.ID, err)
			}
			count++
		}
	}
	return count, nil
}

// addDecision inserts a new decision entry with auto-set ID and timestamp.
func (m *MemoryStore) addDecision(ctx context.Context, entry *DecisionEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	return m.db.Table(DecisionEntry{}).Insert(ctx, entry)
}

// getRecentDecisions returns the most recent decisions for an objective.
func (m *MemoryStore) getRecentDecisions(ctx context.Context, objectiveID string, limit int) ([]*DecisionEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	results, err := m.db.Table(DecisionEntry{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"objective_id": objectiveID},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query decisions for %s: %w", objectiveID, err)
	}

	entries := make([]*DecisionEntry, 0, len(results))
	for _, r := range results {
		if e, ok := r.(*DecisionEntry); ok {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// addLearning inserts a new learning entry with auto-set ID and timestamp.
func (m *MemoryStore) addLearning(ctx context.Context, entry *LearningEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	return m.db.Table(LearningEntry{}).Insert(ctx, entry)
}

// getApplicableLearnings returns learnings whose applicable_to tags overlap
// with the given tags. Filters in Go since gowild_data doesn't support
// array-contains queries.
func (m *MemoryStore) getApplicableLearnings(ctx context.Context, tags []string, limit int) ([]*LearningEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	results, err := m.db.Table(LearningEntry{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("query learnings: %w", err)
	}

	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}

	var filtered []*LearningEntry
	for _, r := range results {
		entry, ok := r.(*LearningEntry)
		if !ok {
			continue
		}
		if len(tags) > 0 {
			if !hasMatchingTag(entry.ApplicableTo, tagSet) {
				continue
			}
		}
		filtered = append(filtered, entry)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// FormatMemoryContext builds a text summary of relevant memory for planner prompts.
func (m *MemoryStore) FormatMemoryContext(ctx context.Context, objectiveID string) (string, error) {
	var sb strings.Builder

	// Recent decisions for this objective
	decisions, err := m.getRecentDecisions(ctx, objectiveID, 5)
	if err != nil {
		return "", fmt.Errorf("get decisions for memory context: %w", err)
	}
	if len(decisions) > 0 {
		sb.WriteString("### Recent Decisions\n")
		for _, d := range decisions {
			fmt.Fprintf(&sb, "- **%s**: %s", d.Decision, d.Reasoning)
			if d.Outcome != "" {
				fmt.Fprintf(&sb, " → Outcome: %s", d.Outcome)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Relevant knowledge (no tag filter — get general knowledge)
	knowledge, err := m.getRelevantKnowledge(ctx, nil, 10)
	if err != nil {
		return "", fmt.Errorf("get knowledge for memory context: %w", err)
	}
	if len(knowledge) > 0 {
		sb.WriteString("### Known Facts\n")
		for _, k := range knowledge {
			fmt.Fprintf(&sb, "- %s (confidence: %.0f%%", k.Fact, k.Confidence*100)
			if len(k.Tags) > 0 {
				fmt.Fprintf(&sb, ", tags: %s", strings.Join(k.Tags, ", "))
			}
			sb.WriteString(")\n")
		}
		sb.WriteString("\n")
	}

	// Applicable learnings (no tag filter — get general learnings)
	learnings, err := m.getApplicableLearnings(ctx, nil, 5)
	if err != nil {
		return "", fmt.Errorf("get learnings for memory context: %w", err)
	}
	if len(learnings) > 0 {
		sb.WriteString("### Learned Patterns\n")
		for _, l := range learnings {
			fmt.Fprintf(&sb, "- %s (confidence: %.0f%%)\n", l.Learning, l.Confidence*100)
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// hasMatchingTag checks if any entry tags match the target tag set.
func hasMatchingTag(entryTags []string, targetSet map[string]bool) bool {
	for _, t := range entryTags {
		if targetSet[strings.ToLower(t)] {
			return true
		}
	}
	return false
}
