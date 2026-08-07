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
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}

	keyResults := []*Objective{
		{Title: "Beta users", Status: StatusCompleted},
		{Title: "Store listing", Status: StatusFailed},
		{Title: "Crash-free rate", Status: StatusActive},
		{Title: "Pricing page", Status: StatusPending},
	}
	for _, kr := range keyResults {
		if err := store.CreateKeyResult(ctx, root.ID, kr); err != nil {
			t.Fatalf("create key result %q: %v", kr.Title, err)
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
		t.Fatalf("expected 4 key results, got %d", rollup.ChildCount)
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
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}
	child := &Objective{Title: "Child", Status: StatusCompleted}
	if err := store.CreateKeyResult(ctx, root.ID, child); err != nil {
		t.Fatalf("create key result: %v", err)
	}

	rollups, err := GetTreeStatus(ctx, store, activity, root.ID)
	if err != nil {
		t.Fatalf("get tree status: %v", err)
	}
	if len(rollups) != 2 {
		t.Fatalf("expected a rollup per node, got %d", len(rollups))
	}
	// BFS order: the objective first, then its key result.
	if rollups[0].Objective.ID != root.ID || rollups[1].Objective.ID != child.ID {
		t.Fatalf("expected objective then key result, got %s then %s",
			rollups[0].Objective.ID, rollups[1].Objective.ID)
	}
	if rollups[0].CompletedCount != 1 {
		t.Fatalf("expected the objective to count its completed key result, got %d", rollups[0].CompletedCount)
	}
	if rollups[1].ChildCount != 0 {
		t.Fatalf("expected the key result to hold nothing, got %d", rollups[1].ChildCount)
	}
}
