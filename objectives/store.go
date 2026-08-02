package objectives

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/data"
)

// ObjectiveStore provides persistence operations for the objective tree.
type ObjectiveStore struct {
	db        gowild_data.Database
	companyID string
}

// NewObjectiveStore creates a store backed by the given database.
// If companyID is non-empty, all queries are scoped to that company.
func NewObjectiveStore(db gowild_data.Database, companyID string) *ObjectiveStore {
	return &ObjectiveStore{db: db, companyID: companyID}
}

func (s *ObjectiveStore) enforceCompanyScope(obj *Objective) error {
	if s.companyID == "" || obj == nil {
		return nil
	}
	if obj.CompanyID != s.companyID {
		return fmt.Errorf("not found")
	}
	return nil
}

func (s *ObjectiveStore) addCompanyScope(where map[string]any) map[string]any {
	scoped := make(map[string]any, len(where)+1)
	for k, v := range where {
		scoped[k] = v
	}
	if s.companyID != "" {
		scoped["company_id"] = s.companyID
	}
	return scoped
}

// Create inserts a new objective. ID and timestamps are set automatically.
func (s *ObjectiveStore) Create(ctx context.Context, obj *Objective) error {
	now := time.Now().UTC()
	if obj.ID == "" {
		obj.ID = uuid.New().String()
	}
	if obj.Status == "" {
		obj.Status = StatusPending
	}
	if obj.AutonomyLevel == "" {
		obj.AutonomyLevel = AutonomyFull
	}
	if obj.ScheduleType == "" {
		obj.ScheduleType = ScheduleOneShot
	}
	if s.companyID != "" {
		obj.CompanyID = s.companyID
	}
	if obj.ParentID != "" {
		parent, err := s.Get(ctx, obj.ParentID)
		if err != nil {
			return fmt.Errorf("create objective: parent %s not found: %w", obj.ParentID, err)
		}
		if obj.CompanyID == "" {
			obj.CompanyID = parent.CompanyID
		}
		if obj.CompanyID != parent.CompanyID {
			return fmt.Errorf("create objective: parent %s belongs to a different company", obj.ParentID)
		}
		obj.Depth = parent.Depth + 1
		if len(obj.ToolAllowlist) == 0 && len(parent.ToolAllowlist) > 0 {
			obj.ToolAllowlist = append([]string(nil), parent.ToolAllowlist...)
		}
	}
	obj.CreatedAt = now
	obj.UpdatedAt = now
	return s.db.Table(Objective{}).Insert(ctx, obj)
}

// Get retrieves a single objective by ID.
func (s *ObjectiveStore) Get(ctx context.Context, id string) (*Objective, error) {
	var obj Objective
	if err := s.db.Table(Objective{}).Get(ctx, id, &obj); err != nil {
		return nil, fmt.Errorf("get objective %s: %w", id, err)
	}
	if err := s.enforceCompanyScope(&obj); err != nil {
		return nil, fmt.Errorf("get objective %s: %w", id, err)
	}
	return &obj, nil
}

// Update persists changes to an existing objective.
func (s *ObjectiveStore) Update(ctx context.Context, obj *Objective) error {
	if obj == nil || obj.ID == "" {
		return fmt.Errorf("update objective: id is required")
	}
	current, err := s.Get(ctx, obj.ID)
	if err != nil {
		return fmt.Errorf("update objective %s: %w", obj.ID, err)
	}
	if s.companyID != "" {
		obj.CompanyID = s.companyID
	} else if obj.CompanyID == "" {
		obj.CompanyID = current.CompanyID
	}
	if obj.ParentID != "" {
		parent, err := s.Get(ctx, obj.ParentID)
		if err != nil {
			return fmt.Errorf("update objective %s: parent %s not found: %w", obj.ID, obj.ParentID, err)
		}
		if obj.CompanyID != parent.CompanyID {
			return fmt.Errorf("update objective %s: parent %s belongs to a different company", obj.ID, obj.ParentID)
		}
		obj.Depth = parent.Depth + 1
	}
	obj.UpdatedAt = time.Now().UTC()
	return s.db.Table(Objective{}).Update(ctx, obj)
}

// Delete removes an objective and its entire subtree, plus related escalations and activity.
func (s *ObjectiveStore) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	tree, err := s.GetTree(ctx, id)
	if err != nil {
		// Fall back to single delete
		return s.db.Table(Objective{}).Delete(ctx, id)
	}

	for _, obj := range tree {
		// Delete related escalations
		escs, _ := s.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"objective_id": obj.ID},
		})
		for _, e := range escs {
			if esc, ok := e.(*Escalation); ok {
				s.db.Table(Escalation{}).Delete(ctx, esc.ID)
			}
		}

		// Delete related activity events
		events, _ := s.db.Table(ActivityEvent{}).Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"objective_id": obj.ID},
		})
		for _, e := range events {
			if ev, ok := e.(*ActivityEvent); ok {
				s.db.Table(ActivityEvent{}).Delete(ctx, ev.ID)
			}
		}

		// Delete the objective itself
		s.db.Table(Objective{}).Delete(ctx, obj.ID)
	}
	return nil
}

// GetChildren returns the direct children of the given objective.
func (s *ObjectiveStore) GetChildren(ctx context.Context, parentID string) ([]*Objective, error) {
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where:   s.addCompanyScope(map[string]any{"parent_id": parentID}),
		OrderBy: "priority",
	})
	if err != nil {
		return nil, fmt.Errorf("get children of %s: %w", parentID, err)
	}
	return toObjectives(results), nil
}

// GetRoots returns all root objectives (those with no parent).
func (s *ObjectiveStore) GetRoots(ctx context.Context) ([]*Objective, error) {
	where := s.addCompanyScope(map[string]any{"parent_id": ""})
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where:   where,
		OrderBy: "priority",
	})
	if err != nil {
		return nil, fmt.Errorf("get roots: %w", err)
	}
	return toObjectives(results), nil
}

// GetByStatus returns all objectives with the given status.
func (s *ObjectiveStore) GetByStatus(ctx context.Context, status ObjectiveStatus) ([]*Objective, error) {
	where := s.addCompanyScope(map[string]any{"status": string(status)})
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where: where,
	})
	if err != nil {
		return nil, fmt.Errorf("get by status %s: %w", status, err)
	}
	return toObjectives(results), nil
}

// GetTree returns the full subtree rooted at the given objective (BFS).
// The root itself is included as the first element.
func (s *ObjectiveStore) GetTree(ctx context.Context, rootID string) ([]*Objective, error) {
	root, err := s.Get(ctx, rootID)
	if err != nil {
		return nil, err
	}

	var tree []*Objective
	queue := []*Objective{root}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		tree = append(tree, node)

		children, err := s.GetChildren(ctx, node.ID)
		if err != nil {
			return nil, err
		}
		queue = append(queue, children...)
	}

	return tree, nil
}

// GetLeafTasks returns all objectives that have no children and are
// in a pending or active status — i.e., executable work items.
func (s *ObjectiveStore) GetLeafTasks(ctx context.Context, rootID string) ([]*Objective, error) {
	tree, err := s.GetTree(ctx, rootID)
	if err != nil {
		return nil, err
	}

	// Build set of IDs that are parents
	parents := make(map[string]bool)
	for _, obj := range tree {
		if obj.ParentID != "" {
			parents[obj.ParentID] = true
		}
	}

	var leaves []*Objective
	for _, obj := range tree {
		if parents[obj.ID] {
			continue
		}
		if obj.Status == StatusPending || obj.Status == StatusActive {
			leaves = append(leaves, obj)
		}
	}
	return leaves, nil
}

// ApplyMutations applies a set of tree mutations inside a transaction.
// Parent references can be UUIDs or title strings — titles are resolved to IDs
// from previously-created mutations or existing objectives in the tree.
func (s *ObjectiveStore) ApplyMutations(ctx context.Context, mutations []TreeMutation) error {
	return s.db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		txStore := &ObjectiveStore{db: tx, companyID: s.companyID}

		// Title→ID map for resolving parent references by title.
		// Populated as new objectives are created.
		titleToID := make(map[string]string)

		for _, m := range mutations {
			switch m.Action {
			case MutationAdd:
				parentID := m.ParentID

				// Resolve title-based parent references
				if parentID != "" {
					if resolved, ok := titleToID[parentID]; ok {
						parentID = resolved
					} else if !isUUID(parentID) {
						// Try to find existing objective by title
						if found, err := txStore.findByTitle(ctx, parentID); err == nil {
							parentID = found.ID
						}
					}
				}

				obj := &Objective{
					ParentID:      parentID,
					Title:         m.Title,
					Description:   m.Description,
					Status:        StatusPending,
					Priority:      m.Priority,
					ScheduleType:  m.ScheduleType,
					ScheduleCron:  m.ScheduleCron,
					ToolAllowlist: m.ToolAllowlist,
				}
				// Calculate depth and inherit CompanyID from parent
				if parentID != "" {
					parent, err := txStore.Get(ctx, parentID)
					if err != nil {
						return fmt.Errorf("add mutation: parent %s not found: %w", parentID, err)
					}
					obj.Depth = parent.Depth + 1
					if obj.CompanyID == "" {
						obj.CompanyID = parent.CompanyID
					}
				}
				if err := txStore.Create(ctx, obj); err != nil {
					return fmt.Errorf("add mutation: %w", err)
				}
				// Store the generated ID back into the mutation for reference
				m.ObjectiveID = obj.ID
				// Register title→ID so later mutations can reference this as a parent
				if m.Title != "" {
					titleToID[m.Title] = obj.ID
				}

			case MutationRemove:
				if err := txStore.Delete(ctx, m.ObjectiveID); err != nil {
					return fmt.Errorf("remove mutation %s: %w", m.ObjectiveID, err)
				}

			case MutationUpdate:
				obj, err := txStore.Get(ctx, m.ObjectiveID)
				if err != nil {
					return fmt.Errorf("update mutation: get %s: %w", m.ObjectiveID, err)
				}
				if m.Title != "" {
					obj.Title = m.Title
				}
				if m.Description != "" {
					obj.Description = m.Description
				}
				if m.Status != "" {
					obj.Status = m.Status
					if m.Status == StatusCompleted {
						obj.CompletedAt = time.Now().UTC()
					}
				}
				if m.Priority != 0 {
					obj.Priority = m.Priority
				}
				if m.ScheduleType != "" {
					obj.ScheduleType = m.ScheduleType
				}
				if m.ScheduleCron != "" {
					obj.ScheduleCron = m.ScheduleCron
				}
				if m.ToolAllowlist != nil {
					obj.ToolAllowlist = m.ToolAllowlist
				}
				if err := txStore.Update(ctx, obj); err != nil {
					return fmt.Errorf("update mutation %s: %w", m.ObjectiveID, err)
				}

			case MutationMove:
				obj, err := txStore.Get(ctx, m.ObjectiveID)
				if err != nil {
					return fmt.Errorf("move mutation: get %s: %w", m.ObjectiveID, err)
				}
				obj.ParentID = m.ParentID
				if m.ParentID != "" {
					parent, err := txStore.Get(ctx, m.ParentID)
					if err != nil {
						return fmt.Errorf("move mutation: new parent %s not found: %w", m.ParentID, err)
					}
					obj.Depth = parent.Depth + 1
				} else {
					obj.Depth = 0
				}
				if err := txStore.Update(ctx, obj); err != nil {
					return fmt.Errorf("move mutation %s: %w", m.ObjectiveID, err)
				}

			default:
				return fmt.Errorf("unknown mutation action: %s", m.Action)
			}
		}
		return nil
	})
}

// GetEscalations returns pending escalations for a given objective.
// Deduplicates by question text — only the first pending escalation per unique question is returned.
// Pending escalations whose question was already resolved are auto-resolved and skipped.
func (s *ObjectiveStore) GetEscalations(ctx context.Context, objectiveID string) []*Escalation {
	if s.companyID != "" {
		if _, err := s.Get(ctx, objectiveID); err != nil {
			return []*Escalation{}
		}
	}

	// Get resolved questions to detect stale duplicates
	resolved, _ := s.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"objective_id": objectiveID, "status": string(EscalationResolved)},
	})
	resolvedQuestions := make(map[string]string) // question -> resolution
	for _, r := range resolved {
		if e, ok := r.(*Escalation); ok {
			resolvedQuestions[e.Question] = e.Resolution
		}
	}

	results, err := s.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"objective_id": objectiveID, "status": string(EscalationPending)},
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	escs := make([]*Escalation, 0, len(results))
	for _, r := range results {
		e, ok := r.(*Escalation)
		if !ok {
			continue
		}
		// Auto-resolve if this question was already answered
		if res, answered := resolvedQuestions[e.Question]; answered {
			e.Status = EscalationResolved
			e.Resolution = res
			e.ResolvedAt = time.Now().UTC()
			s.db.Table(Escalation{}).Update(ctx, e)
			continue
		}
		// Deduplicate by question text
		if seen[e.Question] {
			continue
		}
		seen[e.Question] = true
		escs = append(escs, e)
	}
	return escs
}

// ResolveEscalation marks an escalation as resolved and unblocks the objective if all are answered.
// Also resolves any duplicate pending escalations with the same question text.
func (s *ObjectiveStore) ResolveEscalation(ctx context.Context, escID, resolution string) (*Escalation, error) {
	var esc Escalation
	if err := s.db.Table(Escalation{}).Get(ctx, escID, &esc); err != nil {
		return nil, fmt.Errorf("escalation not found: %s", escID)
	}
	if s.companyID != "" {
		if _, err := s.Get(ctx, esc.ObjectiveID); err != nil {
			return nil, fmt.Errorf("escalation not found: %s", escID)
		}
	}

	now := time.Now().UTC()
	esc.Status = EscalationResolved
	esc.Resolution = resolution
	esc.ResolvedAt = now

	if err := s.db.Table(Escalation{}).Update(ctx, &esc); err != nil {
		return nil, err
	}

	// Auto-resolve duplicate pending escalations with the same question
	dupes, _ := s.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"objective_id": esc.ObjectiveID, "status": string(EscalationPending)},
	})
	for _, r := range dupes {
		if dupe, ok := r.(*Escalation); ok && dupe.Question == esc.Question && dupe.ID != esc.ID {
			dupe.Status = EscalationResolved
			dupe.Resolution = resolution
			dupe.ResolvedAt = now
			s.db.Table(Escalation{}).Update(ctx, dupe)
		}
	}

	// If all escalations for this objective are resolved, unblock it
	remaining, _ := s.db.Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"objective_id": esc.ObjectiveID, "status": string(EscalationPending)},
		Limit: 1,
	})
	if len(remaining) == 0 {
		obj, err := s.Get(ctx, esc.ObjectiveID)
		if err == nil && obj.Status == StatusBlocked {
			obj.Status = StatusPending
			obj.CooldownUntil = time.Time{}
			s.Update(ctx, obj)
		}
	}

	return &esc, nil
}

func toObjectives(results []any) []*Objective {
	objs := make([]*Objective, 0, len(results))
	for _, r := range results {
		if obj, ok := r.(*Objective); ok {
			objs = append(objs, obj)
		}
	}
	return objs
}

// findByTitle returns the first objective matching the given title.
func (s *ObjectiveStore) findByTitle(ctx context.Context, title string) (*Objective, error) {
	where := s.addCompanyScope(map[string]any{"title": title})
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where: where,
		Limit: 1,
	})
	if err != nil || len(results) == 0 {
		return nil, fmt.Errorf("objective with title %q not found", title)
	}
	return results[0].(*Objective), nil
}

// isUUID returns true if the string looks like a UUID.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// GetByScheduleType returns all objectives with the given schedule type.
func (s *ObjectiveStore) GetByScheduleType(ctx context.Context, schedType ScheduleType) ([]*Objective, error) {
	where := map[string]any{"schedule_type": string(schedType)}
	if s.companyID != "" {
		where["company_id"] = s.companyID
	}
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where: where,
	})
	if err != nil {
		return nil, fmt.Errorf("get by schedule type %s: %w", schedType, err)
	}
	return toObjectives(results), nil
}

// DB exposes the underlying database handle. The scheduler in
// objectives_planner queries escalations directly; before the module split
// that was a private field access inside the package.
func (s *ObjectiveStore) DB() gowild_data.Database { return s.db }
