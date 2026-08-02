package objectives

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

func setupTestDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("add tables: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestObjectiveStore_CRUD(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// Create
	obj := &Objective{
		Title:       "Test Mission",
		Description: "A test mission",
		Priority:    1,
	}
	if err := store.Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}
	if obj.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if obj.Status != StatusPending {
		t.Fatalf("expected status pending, got %s", obj.Status)
	}

	// Get
	got, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Test Mission" {
		t.Fatalf("expected title 'Test Mission', got %q", got.Title)
	}

	// Update
	got.Title = "Updated Mission"
	got.Status = StatusActive
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := store.Get(ctx, obj.ID)
	if got2.Title != "Updated Mission" {
		t.Fatalf("expected updated title, got %q", got2.Title)
	}
	if got2.Status != StatusActive {
		t.Fatalf("expected active status, got %s", got2.Status)
	}

	// Delete
	if err := store.Delete(ctx, obj.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = store.Get(ctx, obj.ID)
	if err == nil {
		t.Fatal("expected error getting deleted objective")
	}
}

func TestObjectiveStore_TreeOps(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// Create a tree: root -> child1, child2 -> grandchild
	root := &Objective{Title: "Root", Priority: 1}
	store.Create(ctx, root)

	child1 := &Objective{Title: "Child 1", ParentID: root.ID, Priority: 1, Depth: 1}
	store.Create(ctx, child1)

	child2 := &Objective{Title: "Child 2", ParentID: root.ID, Priority: 2, Depth: 1}
	store.Create(ctx, child2)

	grandchild := &Objective{Title: "Grandchild", ParentID: child1.ID, Priority: 1, Depth: 2}
	store.Create(ctx, grandchild)

	// GetRoots
	roots, err := store.GetRoots(ctx)
	if err != nil {
		t.Fatalf("get roots: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if roots[0].Title != "Root" {
		t.Fatalf("expected root title 'Root', got %q", roots[0].Title)
	}

	// GetChildren
	children, err := store.GetChildren(ctx, root.ID)
	if err != nil {
		t.Fatalf("get children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}

	// GetTree (BFS)
	tree, err := store.GetTree(ctx, root.ID)
	if err != nil {
		t.Fatalf("get tree: %v", err)
	}
	if len(tree) != 4 {
		t.Fatalf("expected 4 nodes in tree, got %d", len(tree))
	}
	// BFS order: root, child1, child2, grandchild
	if tree[0].Title != "Root" {
		t.Fatalf("expected first node to be Root, got %q", tree[0].Title)
	}

	// GetLeafTasks
	leaves, err := store.GetLeafTasks(ctx, root.ID)
	if err != nil {
		t.Fatalf("get leaves: %v", err)
	}
	// child2 and grandchild are leaves (no children, pending status)
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaf tasks, got %d", len(leaves))
	}
}

func TestObjectiveStore_GetByStatus(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	store.Create(ctx, &Objective{Title: "Pending 1"})
	store.Create(ctx, &Objective{Title: "Pending 2"})

	active := &Objective{Title: "Active 1"}
	store.Create(ctx, active)
	active.Status = StatusActive
	store.Update(ctx, active)

	pending, err := store.GetByStatus(ctx, StatusPending)
	if err != nil {
		t.Fatalf("get by status: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	actives, err := store.GetByStatus(ctx, StatusActive)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if len(actives) != 1 {
		t.Fatalf("expected 1 active, got %d", len(actives))
	}
}

func TestObjectiveStore_ApplyMutations(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// Create a root first
	root := &Objective{Title: "Root"}
	store.Create(ctx, root)

	// Apply add mutations
	mutations := []TreeMutation{
		{
			Action:      MutationAdd,
			ParentID:    root.ID,
			Title:       "Child A",
			Description: "First child",
			Priority:    1,
		},
		{
			Action:      MutationAdd,
			ParentID:    root.ID,
			Title:       "Child B",
			Description: "Second child",
			Priority:    2,
		},
	}

	if err := store.ApplyMutations(ctx, mutations); err != nil {
		t.Fatalf("apply mutations: %v", err)
	}

	children, err := store.GetChildren(ctx, root.ID)
	if err != nil {
		t.Fatalf("get children: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	if children[0].Depth != 1 {
		t.Fatalf("expected depth 1, got %d", children[0].Depth)
	}

	// Apply update mutation
	updateMutations := []TreeMutation{
		{
			Action:      MutationUpdate,
			ObjectiveID: children[0].ID,
			Title:       "Updated Child A",
			Status:      StatusCompleted,
		},
	}

	if err := store.ApplyMutations(ctx, updateMutations); err != nil {
		t.Fatalf("apply update: %v", err)
	}

	updated, _ := store.Get(ctx, children[0].ID)
	if updated.Title != "Updated Child A" {
		t.Fatalf("expected updated title, got %q", updated.Title)
	}
	if updated.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", updated.Status)
	}

	// Apply remove mutation
	removeMutations := []TreeMutation{
		{
			Action:      MutationRemove,
			ObjectiveID: children[1].ID,
		},
	}

	if err := store.ApplyMutations(ctx, removeMutations); err != nil {
		t.Fatalf("apply remove: %v", err)
	}

	remaining, _ := store.GetChildren(ctx, root.ID)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 child after remove, got %d", len(remaining))
	}
}

func TestObjectiveStore_CreateInheritsToolAllowlist(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	root := &Objective{
		Title:         "Root",
		ToolAllowlist: []string{"read_webpage", "http_request"},
	}
	if err := store.Create(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}

	child := &Objective{
		Title:    "Child",
		ParentID: root.ID,
	}
	if err := store.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	got, err := store.Get(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if !reflect.DeepEqual(got.ToolAllowlist, root.ToolAllowlist) {
		t.Fatalf("expected inherited allowlist %v, got %v", root.ToolAllowlist, got.ToolAllowlist)
	}
}

func TestActivityStore(t *testing.T) {
	db := setupTestDB(t)
	activity := NewActivityStore(db, "")
	ctx := context.Background()

	objID := "test-obj-123"

	// Log some events
	activity.LogPlanCreated(ctx, objID, "Created plan", map[string]any{"tasks": 3})
	activity.LogTaskStarted(ctx, objID, "Starting task 1")
	activity.LogTaskCompleted(ctx, objID, "Task 1 done", nil)
	activity.LogTaskFailed(ctx, objID, "Task 2 failed", map[string]any{"error": "timeout"})

	// Get events for objective
	events, err := activity.GetEvents(ctx, objID, 10)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// Verify all event types are present
	types := make(map[string]bool)
	for _, e := range events {
		types[e.EventType] = true
	}
	for _, expected := range []string{"plan_created", "task_started", "task_completed", "task_failed"} {
		if !types[expected] {
			t.Errorf("missing event type: %s", expected)
		}
	}

	// Get recent across all objectives
	recent, err := activity.GetRecentEvents(ctx, 2)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent, got %d", len(recent))
	}
}

func TestObjectiveStore_CompanyScopeIsolation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	storeA := NewObjectiveStore(db, "company-a")
	storeB := NewObjectiveStore(db, "company-b")
	globalStore := NewObjectiveStore(db, "")

	rootA := &Objective{Title: "Mission A"}
	if err := storeA.Create(ctx, rootA); err != nil {
		t.Fatalf("create rootA: %v", err)
	}
	if rootA.CompanyID != "company-a" {
		t.Fatalf("expected company-a, got %q", rootA.CompanyID)
	}

	rootB := &Objective{Title: "Mission B"}
	if err := storeB.Create(ctx, rootB); err != nil {
		t.Fatalf("create rootB: %v", err)
	}
	if rootB.CompanyID != "company-b" {
		t.Fatalf("expected company-b, got %q", rootB.CompanyID)
	}

	if _, err := storeA.Get(ctx, rootB.ID); err == nil {
		t.Fatal("expected scoped get to reject cross-company objective")
	}

	rootB.Title = "Should Not Update From A"
	if err := storeA.Update(ctx, rootB); err == nil {
		t.Fatal("expected scoped update to reject cross-company objective")
	}

	if err := storeA.Delete(ctx, rootB.ID); err == nil {
		t.Fatal("expected scoped delete to reject cross-company objective")
	}

	if _, err := globalStore.Get(ctx, rootB.ID); err != nil {
		t.Fatalf("expected rootB to still exist after rejected delete, got: %v", err)
	}
}

func TestObjectiveStore_ResolveEscalationHonorsCompanyScope(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	storeA := NewObjectiveStore(db, "company-a")
	storeB := NewObjectiveStore(db, "company-b")

	objB := &Objective{Title: "Mission B"}
	if err := storeB.Create(ctx, objB); err != nil {
		t.Fatalf("create objB: %v", err)
	}

	esc := &Escalation{
		ID:          "esc-company-b",
		ObjectiveID: objB.ID,
		Question:    "Need a decision",
		Context:     "context",
		Severity:    SeverityWarning,
		Status:      EscalationPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := db.Table(Escalation{}).Insert(ctx, esc); err != nil {
		t.Fatalf("insert escalation: %v", err)
	}

	if _, err := storeA.ResolveEscalation(ctx, esc.ID, "answer"); err == nil {
		t.Fatal("expected cross-company escalation resolution to be rejected")
	}

	resolved, err := storeB.ResolveEscalation(ctx, esc.ID, "answer")
	if err != nil {
		t.Fatalf("resolve escalation in owning company: %v", err)
	}
	if resolved.Status != EscalationResolved {
		t.Fatalf("expected escalation resolved, got %s", resolved.Status)
	}
}

func TestActivityStore_CompanyScopeIsolation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	storeA := NewObjectiveStore(db, "company-a")
	storeB := NewObjectiveStore(db, "company-b")

	objA := &Objective{Title: "Mission A"}
	if err := storeA.Create(ctx, objA); err != nil {
		t.Fatalf("create objA: %v", err)
	}
	objB := &Objective{Title: "Mission B"}
	if err := storeB.Create(ctx, objB); err != nil {
		t.Fatalf("create objB: %v", err)
	}

	activityA := NewActivityStore(db, "company-a")
	activityB := NewActivityStore(db, "company-b")

	if err := activityA.LogTaskStarted(ctx, objA.ID, "start a"); err != nil {
		t.Fatalf("log activity for own company objective: %v", err)
	}
	if err := activityA.LogTaskStarted(ctx, objB.ID, "start b from a"); err == nil {
		t.Fatal("expected cross-company activity write to be rejected")
	}

	if _, err := activityA.GetEvents(ctx, objB.ID, 10); err == nil {
		t.Fatal("expected cross-company activity read to be rejected")
	}

	if err := activityB.LogTaskStarted(ctx, objB.ID, "start b"); err != nil {
		t.Fatalf("log activity for company-b objective: %v", err)
	}
	if _, err := activityB.GetEvents(ctx, objB.ID, 10); err != nil {
		t.Fatalf("expected company-b read to succeed: %v", err)
	}
}
