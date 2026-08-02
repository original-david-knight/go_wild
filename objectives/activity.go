package objectives

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/data"
)

// ActivityStore provides persistence for the append-only activity event log.
type ActivityStore struct {
	db        gowild_data.Database
	companyID string
}

// NewActivityStore creates an activity store backed by the given database.
// If companyID is non-empty, GetRecentEvents filters to that company's objectives.
func NewActivityStore(db gowild_data.Database, companyID string) *ActivityStore {
	return &ActivityStore{db: db, companyID: companyID}
}

func (s *ActivityStore) ensureObjectiveInScope(ctx context.Context, objectiveID string) error {
	if s.companyID == "" || objectiveID == "" {
		return nil
	}
	var obj Objective
	if err := s.db.Table(Objective{}).Get(ctx, objectiveID, &obj); err != nil {
		return fmt.Errorf("objective %s not found: %w", objectiveID, err)
	}
	if obj.CompanyID != s.companyID {
		return fmt.Errorf("objective %s not found", objectiveID)
	}
	return nil
}

// LogEvent inserts a new activity event.
func (s *ActivityStore) LogEvent(ctx context.Context, event *ActivityEvent) error {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.Severity == "" {
		event.Severity = SeverityInfo
	}
	if err := s.ensureObjectiveInScope(ctx, event.ObjectiveID); err != nil {
		return err
	}
	return s.db.Table(ActivityEvent{}).Insert(ctx, event)
}

// GetEvents returns events for a specific objective, ordered by creation time descending.
func (s *ActivityStore) GetEvents(ctx context.Context, objectiveID string, limit int) ([]*ActivityEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if err := s.ensureObjectiveInScope(ctx, objectiveID); err != nil {
		return nil, err
	}
	results, err := s.db.Table(ActivityEvent{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"objective_id": objectiveID},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get events for %s: %w", objectiveID, err)
	}
	return toEvents(results), nil
}

// GetEventsForTree returns events for an objective and all its descendants.
func (s *ActivityStore) GetEventsForTree(ctx context.Context, store *ObjectiveStore, rootID string, limit int) ([]*ActivityEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	tree, err := store.GetTree(ctx, rootID)
	if err != nil {
		return nil, fmt.Errorf("get events for tree %s: %w", rootID, err)
	}

	ids := make([]any, len(tree))
	for i, obj := range tree {
		ids[i] = obj.ID
	}

	results, err := s.db.Table(ActivityEvent{}).Query(ctx, gowild_data.QueryOpts{
		WhereIn:   map[string][]any{"objective_id": ids},
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get events for tree %s: %w", rootID, err)
	}
	return toEvents(results), nil
}

// GetRecentEvents returns the most recent events across all objectives.
// When the store has a companyID, it filters to that company's objectives.
func (s *ActivityStore) GetRecentEvents(ctx context.Context, limit int) ([]*ActivityEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	// If scoped to a company, get objective IDs first
	if s.companyID != "" {
		objResults, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": s.companyID},
		})
		if err != nil {
			return nil, fmt.Errorf("get company objectives: %w", err)
		}
		if len(objResults) == 0 {
			return []*ActivityEvent{}, nil
		}
		ids := make([]any, len(objResults))
		for i, r := range objResults {
			if obj, ok := r.(*Objective); ok {
				ids[i] = obj.ID
			}
		}
		results, err := s.db.Table(ActivityEvent{}).Query(ctx, gowild_data.QueryOpts{
			WhereIn:   map[string][]any{"objective_id": ids},
			OrderBy:   "created_at",
			OrderDesc: true,
			Limit:     limit,
		})
		if err != nil {
			return nil, fmt.Errorf("get recent events for company: %w", err)
		}
		return toEvents(results), nil
	}

	results, err := s.db.Table(ActivityEvent{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get recent events: %w", err)
	}
	return toEvents(results), nil
}

// Convenience logging methods

func (s *ActivityStore) LogPlanCreated(ctx context.Context, objectiveID, summary string, details map[string]any) error {
	return s.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: objectiveID,
		EventType:   "plan_created",
		Severity:    SeverityInfo,
		Summary:     summary,
		Details:     details,
	})
}

func (s *ActivityStore) LogTaskStarted(ctx context.Context, objectiveID, summary string) error {
	return s.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: objectiveID,
		EventType:   "task_started",
		Severity:    SeverityInfo,
		Summary:     summary,
	})
}

func (s *ActivityStore) LogTaskCompleted(ctx context.Context, objectiveID, summary string, details map[string]any) error {
	return s.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: objectiveID,
		EventType:   "task_completed",
		Severity:    SeverityInfo,
		Summary:     summary,
		Details:     details,
	})
}

func (s *ActivityStore) LogTaskFailed(ctx context.Context, objectiveID, summary string, details map[string]any) error {
	return s.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: objectiveID,
		EventType:   "task_failed",
		Severity:    SeverityError,
		Summary:     summary,
		Details:     details,
	})
}

func toEvents(results []any) []*ActivityEvent {
	events := make([]*ActivityEvent, 0, len(results))
	for _, r := range results {
		if e, ok := r.(*ActivityEvent); ok {
			events = append(events, e)
		}
	}
	return events
}

// DB exposes the underlying database handle, for the same reason
// ObjectiveStore.DB does.
func (a *ActivityStore) DB() gowild_data.Database { return a.db }
