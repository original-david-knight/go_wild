package objectives

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/original-david-knight/go_wild/data"
)

// The store refuses anything that is not an OKR tree, and each refusal is its
// own sentinel so a consumer can name the reason — an HTTP layer answering 400,
// say — with errors.Is rather than by re-walking the tree itself.
var (
	// ErrObjectiveParented rejects an Objective with a parent. Objectives are
	// roots: a parented one is a Key Result that has not said so.
	ErrObjectiveParented = errors.New("an objective is a root and cannot have a parent")
	// ErrParentNotObjective rejects a key result whose named parent is not
	// there at all.
	ErrParentNotObjective = errors.New("a key result's parent must name an existing objective")
	// ErrThirdLevel rejects a key result under a key result. Two levels
	// exactly: a key result that needs sub-structure is an objective that has
	// not admitted it yet.
	ErrThirdLevel = errors.New("a key result cannot hold key results: the tree is two levels")
	// ErrObjectiveMeasures rejects progress fields on an Objective. Measuring
	// happens at exactly one level, and it is not this one.
	ErrObjectiveMeasures = errors.New("an objective does not measure: target, current and unit belong on its key results")
)

// measures reports whether a node carries a measurable progress claim. Any one
// of the three is enough: a unit with no numbers still states that something is
// being counted.
func measures(obj *Objective) bool {
	return obj.Target != 0 || obj.Current != 0 || obj.Unit != ""
}

// ObjectiveStore provides persistence operations for the two-level OKR tree:
// Objectives are its roots, Key Results are their children, and the structure
// is enforced here rather than left to caller convention.
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

// CreateObjective inserts a root Objective — a container that states a
// direction and measures nothing itself. A parent or any progress field is a
// contradiction and is refused.
func (s *ObjectiveStore) CreateObjective(ctx context.Context, obj *Objective) error {
	if obj == nil {
		return fmt.Errorf("create objective: an objective is required")
	}
	if obj.ParentID != "" {
		return fmt.Errorf("create objective %q: %w", obj.Title, ErrObjectiveParented)
	}
	if measures(obj) {
		return fmt.Errorf("create objective %q: %w", obj.Title, ErrObjectiveMeasures)
	}
	obj.Depth = 0
	return s.create(ctx, obj)
}

// CreateKeyResult inserts a Key Result under the named Objective. The caller
// names the parent; the store sets ParentID and Depth, so tree position stops
// being the caller's problem — and stops being something the caller can get
// wrong.
func (s *ObjectiveStore) CreateKeyResult(ctx context.Context, objectiveID string, kr *Objective) error {
	if kr == nil {
		return fmt.Errorf("create key result: a key result is required")
	}
	parent, err := s.Get(ctx, objectiveID)
	if err != nil {
		return fmt.Errorf("create key result %q: %w", kr.Title, ErrParentNotObjective)
	}
	if parent.ParentID != "" {
		return fmt.Errorf("create key result %q under %s: %w", kr.Title, objectiveID, ErrThirdLevel)
	}
	kr.ParentID = parent.ID
	kr.Depth = parent.Depth + 1
	return s.create(ctx, kr)
}

// create inserts a node. ID and timestamps are set automatically. It is the
// shared tail of both public creates and of ApplyMutations, and it enforces
// nothing: the structure was decided by the caller above it.
func (s *ObjectiveStore) create(ctx context.Context, obj *Objective) error {
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

// Update persists changes to an existing node, Objective or Key Result, and
// holds the shape rules the creates hold. Enforcing them here too is the whole
// point: without it an Objective could be edited into measuring, or a Key
// Result re-parented under another one, and the tree would be two levels only
// until someone typed a PATCH.
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
	if obj.ParentID == "" {
		if measures(obj) {
			return fmt.Errorf("update objective %s: %w", obj.ID, ErrObjectiveMeasures)
		}
		obj.Depth = 0
	} else {
		parent, err := s.Get(ctx, obj.ParentID)
		if err != nil {
			return fmt.Errorf("update objective %s: %w", obj.ID, ErrParentNotObjective)
		}
		if parent.ParentID != "" {
			return fmt.Errorf("update objective %s under %s: %w", obj.ID, obj.ParentID, ErrThirdLevel)
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

// GetKeyResults returns the Key Results of the named Objective, and nothing
// from any other one.
func (s *ObjectiveStore) GetKeyResults(ctx context.Context, objectiveID string) ([]*Objective, error) {
	return s.children(ctx, objectiveID)
}

// children returns the direct children of a node. It stays unexported: the
// public surface speaks Objectives and Key Results, and generic tree
// traversal is this package's own business.
func (s *ObjectiveStore) children(ctx context.Context, parentID string) ([]*Objective, error) {
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where:   s.addCompanyScope(map[string]any{"parent_id": parentID}),
		OrderBy: "priority",
	})
	if err != nil {
		return nil, fmt.Errorf("get children of %s: %w", parentID, err)
	}
	return toObjectives(results), nil
}

// GetObjectives returns every Objective — the tree's roots.
func (s *ObjectiveStore) GetObjectives(ctx context.Context) ([]*Objective, error) {
	where := s.addCompanyScope(map[string]any{"parent_id": ""})
	results, err := s.db.Table(Objective{}).Query(ctx, gowild_data.QueryOpts{
		Where:   where,
		OrderBy: "priority",
	})
	if err != nil {
		return nil, fmt.Errorf("get objectives: %w", err)
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

		children, err := s.children(ctx, node.ID)
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
				if err := txStore.create(ctx, obj); err != nil {
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
