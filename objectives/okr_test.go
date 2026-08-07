package objectives

import (
	"context"
	"errors"
	"testing"
)

// The OKR shape is the store's rule, not the caller's convention. Each refusal
// below asserts on its exported sentinel rather than on message text, because
// that is the contract a consumer codes against when it turns a refusal into
// its own answer.

func TestCreateKeyResultUnderAnObjective(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	shipIt := &Objective{Title: "Ship app #2"}
	if err := store.CreateObjective(ctx, shipIt); err != nil {
		t.Fatalf("create objective: %v", err)
	}
	if shipIt.ParentID != "" || shipIt.Depth != 0 {
		t.Fatalf("objective = parent %q depth %d, want a root at depth 0", shipIt.ParentID, shipIt.Depth)
	}

	betaUsers := &Objective{Title: "10 beta users", Target: 10, Unit: "users"}
	if err := store.CreateKeyResult(ctx, shipIt.ID, betaUsers); err != nil {
		t.Fatalf("create key result: %v", err)
	}

	// The store set the tree position; the caller named a parent and nothing
	// else about where the row sits.
	stored, err := store.Get(ctx, betaUsers.ID)
	if err != nil {
		t.Fatalf("get key result: %v", err)
	}
	if stored.ParentID != shipIt.ID {
		t.Fatalf("key result parent = %q, want %q", stored.ParentID, shipIt.ID)
	}
	if stored.Depth != 1 {
		t.Fatalf("key result depth = %d, want 1", stored.Depth)
	}

	// A second objective with a key result of its own: neither read leaks into
	// the other.
	other := &Objective{Title: "Sleep 7 hours"}
	if err := store.CreateObjective(ctx, other); err != nil {
		t.Fatalf("create second objective: %v", err)
	}
	otherKR := &Objective{Title: "Lights out by 23:00", Target: 30, Unit: "days"}
	if err := store.CreateKeyResult(ctx, other.ID, otherKR); err != nil {
		t.Fatalf("create second key result: %v", err)
	}

	roots, err := store.GetObjectives(ctx)
	if err != nil {
		t.Fatalf("get objectives: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("GetObjectives returned %d rows, want the 2 objectives and neither key result", len(roots))
	}
	for _, r := range roots {
		if r.ParentID != "" {
			t.Fatalf("GetObjectives returned %q, which has a parent", r.Title)
		}
	}

	krs, err := store.GetKeyResults(ctx, shipIt.ID)
	if err != nil {
		t.Fatalf("get key results: %v", err)
	}
	if len(krs) != 1 || krs[0].ID != betaUsers.ID {
		t.Fatalf("GetKeyResults(%s) = %d rows, want exactly its own key result", shipIt.ID, len(krs))
	}
}

func TestCreateKeyResultRefusesAThirdLevel(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	root := &Objective{Title: "Ship app #2"}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatalf("create objective: %v", err)
	}
	kr := &Objective{Title: "10 beta users", Target: 10}
	if err := store.CreateKeyResult(ctx, root.ID, kr); err != nil {
		t.Fatalf("create key result: %v", err)
	}

	grandchild := &Objective{Title: "Email the first five"}
	err := store.CreateKeyResult(ctx, kr.ID, grandchild)
	if !errors.Is(err, ErrThirdLevel) {
		t.Fatalf("err = %v, want ErrThirdLevel", err)
	}

	// Refused means nothing was written, not written and then rejected.
	under, err := store.GetKeyResults(ctx, kr.ID)
	if err != nil {
		t.Fatalf("get key results of a key result: %v", err)
	}
	if len(under) != 0 {
		t.Fatalf("%d rows hang under a key result, want 0", len(under))
	}
	if grandchild.ID != "" {
		if _, err := store.Get(ctx, grandchild.ID); err == nil {
			t.Fatal("the refused row is in the store")
		}
	}
}

func TestCreateKeyResultRefusesAParentThatIsNotThere(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	err := store.CreateKeyResult(ctx, "no-such-objective", &Objective{Title: "10 beta users"})
	if !errors.Is(err, ErrParentNotObjective) {
		t.Fatalf("err = %v, want ErrParentNotObjective", err)
	}
}

func TestCreateObjectiveRefusesAParent(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	root := &Objective{Title: "Ship app #2"}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatalf("create objective: %v", err)
	}

	err := store.CreateObjective(ctx, &Objective{Title: "A parented objective", ParentID: root.ID})
	if !errors.Is(err, ErrObjectiveParented) {
		t.Fatalf("err = %v, want ErrObjectiveParented", err)
	}
	krs, err := store.GetKeyResults(ctx, root.ID)
	if err != nil {
		t.Fatalf("get key results: %v", err)
	}
	if len(krs) != 0 {
		t.Fatalf("%d rows landed under the objective, want 0", len(krs))
	}
}

func TestObjectivesNeverMeasure(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// On create: each of the three fields alone is enough to be a measurement.
	for _, c := range []struct {
		name string
		obj  *Objective
	}{
		{"target", &Objective{Title: "Weight", Target: 180}},
		{"current", &Objective{Title: "Weight", Current: 197.4}},
		{"unit", &Objective{Title: "Weight", Unit: "lb"}},
	} {
		t.Run("create with a "+c.name, func(t *testing.T) {
			if err := store.CreateObjective(ctx, c.obj); !errors.Is(err, ErrObjectiveMeasures) {
				t.Fatalf("err = %v, want ErrObjectiveMeasures", err)
			}
		})
	}

	// On update: an objective cannot be edited into measuring either.
	root := &Objective{Title: "Ship app #2"}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatalf("create objective: %v", err)
	}
	root.Target = 10
	root.Unit = "users"
	if err := store.Update(ctx, root); !errors.Is(err, ErrObjectiveMeasures) {
		t.Fatalf("update err = %v, want ErrObjectiveMeasures", err)
	}
	stored, err := store.Get(ctx, root.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Target != 0 || stored.Unit != "" {
		t.Fatalf("the refused update was written: target=%v unit=%q", stored.Target, stored.Unit)
	}

	// The same fields on a key result are the point of a key result.
	kr := &Objective{Title: "10 beta users", Target: 10, Current: 3, Unit: "users"}
	if err := store.CreateKeyResult(ctx, root.ID, kr); err != nil {
		t.Fatalf("create measured key result: %v", err)
	}
	kr.Current = 6
	if err := store.Update(ctx, kr); err != nil {
		t.Fatalf("update measured key result: %v", err)
	}
	if got, _ := store.Get(ctx, kr.ID); got.Current != 6 {
		t.Fatalf("key result current = %v, want 6", got.Current)
	}
}

func TestUpdateRefusesReparentingIntoAThirdLevel(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	root := &Objective{Title: "Ship app #2"}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatalf("create objective: %v", err)
	}
	first := &Objective{Title: "10 beta users", Target: 10}
	if err := store.CreateKeyResult(ctx, root.ID, first); err != nil {
		t.Fatalf("create first key result: %v", err)
	}
	second := &Objective{Title: "Crash-free 99%", Target: 99}
	if err := store.CreateKeyResult(ctx, root.ID, second); err != nil {
		t.Fatalf("create second key result: %v", err)
	}

	second.ParentID = first.ID
	if err := store.Update(ctx, second); !errors.Is(err, ErrThirdLevel) {
		t.Fatalf("err = %v, want ErrThirdLevel", err)
	}
	stored, err := store.Get(ctx, second.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.ParentID != root.ID || stored.Depth != 1 {
		t.Fatalf("the refused move was written: parent=%q depth=%d", stored.ParentID, stored.Depth)
	}

	// A parent that names nothing is the other half of the same rule.
	second.ParentID = "no-such-objective"
	if err := store.Update(ctx, second); !errors.Is(err, ErrParentNotObjective) {
		t.Fatalf("err = %v, want ErrParentNotObjective", err)
	}
}
