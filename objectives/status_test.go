package objectives

import (
	"context"
	"testing"
)

// StatusRollup used to be covered only through the embedded API server's
// /status endpoint, which left with api.go. It is a kept surface, so it gets
// its own store-level test rather than dying with the HTTP layer that happened
// to exercise it.

func TestGetObjectiveStatus(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	activity := NewActivityStore(db, "")
	ctx := context.Background()

	root := &Objective{Title: "Ship app #2"}
	if err := store.Create(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}

	children := []*Objective{
		{Title: "Beta users", ParentID: root.ID, Status: StatusCompleted},
		{Title: "Store listing", ParentID: root.ID, Status: StatusFailed},
		{Title: "Crash-free rate", ParentID: root.ID, Status: StatusActive},
		{Title: "Pricing page", ParentID: root.ID, Status: StatusPending},
	}
	for _, c := range children {
		if err := store.Create(ctx, c); err != nil {
			t.Fatalf("create child %q: %v", c.Title, err)
		}
	}

	if err := activity.LogTaskStarted(ctx, root.ID, "kicked off"); err != nil {
		t.Fatalf("log activity: %v", err)
	}

	rollup, err := GetObjectiveStatus(ctx, store, activity, root.ID)
	if err != nil {
		t.Fatalf("get objective status: %v", err)
	}
	if rollup.Objective.ID != root.ID {
		t.Fatalf("expected rollup for %s, got %s", root.ID, rollup.Objective.ID)
	}
	if rollup.ChildCount != 4 {
		t.Fatalf("expected 4 children, got %d", rollup.ChildCount)
	}
	if rollup.CompletedCount != 1 || rollup.FailedCount != 1 || rollup.ActiveCount != 1 {
		t.Fatalf("expected 1/1/1 completed/failed/active, got %d/%d/%d",
			rollup.CompletedCount, rollup.FailedCount, rollup.ActiveCount)
	}
	if rollup.LastActivity == nil || rollup.LastActivity.Summary != "kicked off" {
		t.Fatalf("expected the logged event as last activity, got %+v", rollup.LastActivity)
	}
}

func TestGetTreeStatus(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	activity := NewActivityStore(db, "")
	ctx := context.Background()

	root := &Objective{Title: "Root"}
	if err := store.Create(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}
	child := &Objective{Title: "Child", ParentID: root.ID, Status: StatusCompleted}
	if err := store.Create(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	rollups, err := GetTreeStatus(ctx, store, activity, root.ID)
	if err != nil {
		t.Fatalf("get tree status: %v", err)
	}
	if len(rollups) != 2 {
		t.Fatalf("expected a rollup per node, got %d", len(rollups))
	}
	// BFS order: the root first, then its child.
	if rollups[0].Objective.ID != root.ID || rollups[1].Objective.ID != child.ID {
		t.Fatalf("expected root then child, got %s then %s",
			rollups[0].Objective.ID, rollups[1].Objective.ID)
	}
	if rollups[0].CompletedCount != 1 {
		t.Fatalf("expected the root to count its completed child, got %d", rollups[0].CompletedCount)
	}
	if rollups[1].ChildCount != 0 {
		t.Fatalf("expected the leaf to have no children, got %d", rollups[1].ChildCount)
	}
}
