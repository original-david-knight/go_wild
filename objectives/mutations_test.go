package objectives

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"
)

func titlesOf(objs []*Objective) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.Title)
	}
	sort.Strings(out)
	return out
}

func TestApplyMutationsResolvesParentsByTitle(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	launch := &Objective{Title: "Launch"}
	if err := store.CreateObjective(ctx, launch); err != nil {
		t.Fatal(err)
	}

	err := store.ApplyMutations(ctx, []TreeMutation{
		// "Launch" is an existing objective, found by its title.
		{Action: MutationAdd, ParentID: "Launch", Title: "Ship beta", Priority: 1},
		{Action: MutationAdd, Title: "Grow"},
		// "Grow" was created earlier in this batch.
		{Action: MutationAdd, ParentID: "Grow", Title: "Reach 100 users", Description: "first cohort"},
	})
	if err != nil {
		t.Fatal(err)
	}

	krs, err := store.GetKeyResults(ctx, launch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(krs) != 1 || krs[0].Title != "Ship beta" || krs[0].Depth != 1 || krs[0].Priority != 1 {
		t.Fatalf("Launch key results = %+v, want Ship beta at depth 1", krs)
	}

	roots, err := store.GetObjectives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(titlesOf(roots), ","); got != "Grow,Launch" {
		t.Fatalf("roots = %s, want Grow,Launch", got)
	}
	var grow *Objective
	for _, r := range roots {
		if r.Title == "Grow" {
			grow = r
		}
	}
	growKRs, err := store.GetKeyResults(ctx, grow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(growKRs) != 1 || growKRs[0].Title != "Reach 100 users" || growKRs[0].Description != "first cohort" || growKRs[0].Status != StatusPending {
		t.Fatalf("Grow key results = %+v", growKRs)
	}
}

func TestApplyMutationsMovesAndUpdates(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	a := &Objective{Title: "A"}
	b := &Objective{Title: "B"}
	store.CreateObjective(ctx, a)
	store.CreateObjective(ctx, b)
	kr := &Objective{Title: "KR"}
	if err := store.CreateKeyResult(ctx, a.ID, kr); err != nil {
		t.Fatal(err)
	}

	err := store.ApplyMutations(ctx, []TreeMutation{
		{Action: MutationMove, ObjectiveID: kr.ID, ParentID: b.ID},
		{Action: MutationUpdate, ObjectiveID: kr.ID, Description: "moved", Priority: 5, Status: StatusActive},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, kr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID != b.ID || got.Depth != 1 || got.Description != "moved" || got.Priority != 5 || got.Status != StatusActive || got.Title != "KR" {
		t.Fatalf("after move and update: %+v", got)
	}
	if !got.CompletedAt.IsZero() {
		t.Fatalf("a non-completed status must not stamp CompletedAt, got %v", got.CompletedAt)
	}

	// Moving to no parent promotes the node to a root Objective.
	if err := store.ApplyMutations(ctx, []TreeMutation{{Action: MutationMove, ObjectiveID: kr.ID}}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, kr.ID)
	if got.ParentID != "" || got.Depth != 0 {
		t.Fatalf("after move to root: parent %q depth %d", got.ParentID, got.Depth)
	}

	before := time.Now().UTC()
	if err := store.ApplyMutations(ctx, []TreeMutation{{Action: MutationUpdate, ObjectiveID: kr.ID, Status: StatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, kr.ID)
	if got.Status != StatusCompleted || got.CompletedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("completing should stamp CompletedAt, got status %s at %v", got.Status, got.CompletedAt)
	}
}

// TestApplyMutationsRollsBackOnError checks each failing mutation reports
// its cause and that nothing earlier in the batch survives.
func TestApplyMutationsRollsBackOnError(t *testing.T) {
	missing := "00000000-0000-0000-0000-000000000000"
	cases := []struct {
		name    string
		bad     func(root, kr string) TreeMutation
		wantErr string
	}{
		{"unknown action", func(string, string) TreeMutation { return TreeMutation{Action: "explode"} }, "unknown mutation action: explode"},
		{"add under a missing title", func(string, string) TreeMutation {
			return TreeMutation{Action: MutationAdd, ParentID: "No Such Title", Title: "x"}
		}, "add mutation: parent No Such Title not found"},
		{"add under a missing id", func(string, string) TreeMutation {
			return TreeMutation{Action: MutationAdd, ParentID: missing, Title: "x"}
		}, "add mutation: parent " + missing + " not found"},
		{"update a missing node", func(string, string) TreeMutation {
			return TreeMutation{Action: MutationUpdate, ObjectiveID: missing, Title: "x"}
		}, "update mutation: get " + missing},
		{"move a missing node", func(string, string) TreeMutation {
			return TreeMutation{Action: MutationMove, ObjectiveID: missing, ParentID: missing}
		}, "move mutation: get " + missing},
		{"move under a missing parent", func(_, kr string) TreeMutation {
			return TreeMutation{Action: MutationMove, ObjectiveID: kr, ParentID: missing}
		}, "move mutation: new parent " + missing + " not found"},
		{"move into a third level", func(_, kr string) TreeMutation {
			return TreeMutation{Action: MutationMove, ObjectiveID: kr, ParentID: kr}
		}, "the tree is two levels"},
		{"remove a missing node", func(string, string) TreeMutation {
			return TreeMutation{Action: MutationRemove, ObjectiveID: missing}
		}, "remove mutation " + missing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			store := NewObjectiveStore(db, "")
			ctx := context.Background()
			root := &Objective{Title: "Root"}
			store.CreateObjective(ctx, root)
			kr := &Objective{Title: "KR"}
			if err := store.CreateKeyResult(ctx, root.ID, kr); err != nil {
				t.Fatal(err)
			}

			err := store.ApplyMutations(ctx, []TreeMutation{
				{Action: MutationAdd, Title: "Doomed"},
				tc.bad(root.ID, kr.ID),
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ApplyMutations error = %v, want it to contain %q", err, tc.wantErr)
			}
			roots, _ := store.GetObjectives(ctx)
			if got := strings.Join(titlesOf(roots), ","); got != "Root" {
				t.Fatalf("roots after a failed batch = %s, want only Root", got)
			}
		})
	}
}

func TestGetEventsForTreeCoversDescendantsOnly(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	activity := NewActivityStore(db, "")
	ctx := context.Background()

	root := &Objective{Title: "Root"}
	other := &Objective{Title: "Other"}
	store.CreateObjective(ctx, root)
	store.CreateObjective(ctx, other)
	kr := &Objective{Title: "KR"}
	store.CreateKeyResult(ctx, root.ID, kr)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, e := range []struct{ obj, summary string }{
		{root.ID, "root-1"}, {kr.ID, "kr-1"}, {other.ID, "other-1"}, {kr.ID, "kr-2"},
	} {
		if err := activity.LogEvent(ctx, &ActivityEvent{ObjectiveID: e.obj, EventType: "note", Summary: e.summary, CreatedAt: base.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}

	events, err := activity.GetEventsForTree(ctx, store, root.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		got = append(got, e.Summary)
	}
	if strings.Join(got, ",") != "kr-2,kr-1,root-1" {
		t.Fatalf("tree events = %v, want newest first and without other-1", got)
	}

	limited, err := activity.GetEventsForTree(ctx, store, root.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].Summary != "kr-2" {
		t.Fatalf("limit 1 = %+v", limited)
	}

	if _, err := activity.GetEventsForTree(ctx, store, "missing", 10); err == nil || !strings.HasPrefix(err.Error(), "get events for tree missing:") {
		t.Fatalf("missing root = %v", err)
	}
}

func TestGetRecentEventsScopedToCompany(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	empty := NewActivityStore(db, "company-empty")
	if got, err := empty.GetRecentEvents(ctx, 0); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("company with no objectives = %v, %v; want an empty non-nil list", got, err)
	}

	storeA := NewObjectiveStore(db, "company-a")
	storeB := NewObjectiveStore(db, "company-b")
	objA := &Objective{Title: "A"}
	objB := &Objective{Title: "B"}
	storeA.CreateObjective(ctx, objA)
	storeB.CreateObjective(ctx, objB)

	actA := NewActivityStore(db, "company-a")
	actB := NewActivityStore(db, "company-b")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	actA.LogEvent(ctx, &ActivityEvent{ObjectiveID: objA.ID, Summary: "a-old", CreatedAt: base})
	actB.LogEvent(ctx, &ActivityEvent{ObjectiveID: objB.ID, Summary: "b", CreatedAt: base.Add(time.Minute)})
	actA.LogEvent(ctx, &ActivityEvent{ObjectiveID: objA.ID, Summary: "a-new", CreatedAt: base.Add(2 * time.Minute)})

	got, err := actA.GetRecentEvents(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var summaries []string
	for _, e := range got {
		summaries = append(summaries, e.Summary)
		if e.Severity != SeverityInfo {
			t.Errorf("event %s severity = %q, want the info default", e.Summary, e.Severity)
		}
	}
	if strings.Join(summaries, ",") != "a-new,a-old" {
		t.Fatalf("company-a recent = %v", summaries)
	}

	all, err := NewActivityStore(db, "").GetRecentEvents(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Summary != "a-new" || all[2].Summary != "a-old" {
		t.Fatalf("unscoped recent = %+v", all)
	}

	// GetEvents defaults its limit too, and an unscoped store reads any id.
	if evs, err := NewActivityStore(db, "").GetEvents(ctx, objB.ID, 0); err != nil || len(evs) != 1 || evs[0].Summary != "b" {
		t.Fatalf("GetEvents(objB) = %+v, %v", evs, err)
	}
}
