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

// Delete removes an objective and its entire subtree, plus related activity.
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
					ParentID:    parentID,
					Title:       m.Title,
					Description: m.Description,
					Status:      StatusPending,
					Priority:    m.Priority,
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
