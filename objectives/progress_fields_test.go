package objectives

import (
	"context"
	"testing"
)

// Target/Current/Unit are the whole reason the life dashboard consumes this
// module, and they are plain scalars that a careless model or column change
// would silently drop. They get their own round-trip test rather than riding
// along on TestObjectiveStore_CRUD.
//
// They live on Key Results now: measurement happens at exactly one level, and
// the Objective above is a container. Each case below hangs its measured node
// under a container of its own for that reason.

// measuredKR creates a container Objective and one Key Result under it,
// returning the key result the case then round-trips.
func measuredKR(t *testing.T, ctx context.Context, store *ObjectiveStore, kr *Objective) *Objective {
	t.Helper()
	parent := &Objective{Title: kr.Title + " — container"}
	if err := store.CreateObjective(ctx, parent); err != nil {
		t.Fatalf("create container: %v", err)
	}
	if err := store.CreateKeyResult(ctx, parent.ID, kr); err != nil {
		t.Fatalf("create key result: %v", err)
	}
	return kr
}

func TestKeyResultStore_ProgressFieldsRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	obj := measuredKR(t, ctx, store, &Objective{
		Title:   "Weight 198 to 180",
		Target:  180,
		Current: 197.4,
		Unit:    "lb",
	})

	got, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Target != 180 {
		t.Fatalf("expected target 180, got %v", got.Target)
	}
	if got.Current != 197.4 {
		t.Fatalf("expected current 197.4, got %v", got.Current)
	}
	if got.Unit != "lb" {
		t.Fatalf("expected unit %q, got %q", "lb", got.Unit)
	}

	// A fresh reading moves Current only; Target and Unit must survive Update.
	got.Current = 196.2
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}

	got2, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.Current != 196.2 {
		t.Fatalf("expected current 196.2 after update, got %v", got2.Current)
	}
	if got2.Target != 180 {
		t.Fatalf("expected target 180 after update, got %v", got2.Target)
	}
	if got2.Unit != "lb" {
		t.Fatalf("expected unit %q after update, got %q", "lb", got2.Unit)
	}
}

func TestKeyResultStore_ProgressFieldsZeroValues(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// A key result backed by tasks rather than a number leaves the progress
	// fields zero. Reading them back as anything other than 0/0/"" would let a
	// consumer render a fabricated number.
	obj := measuredKR(t, ctx, store, &Objective{Title: "Ship app #2 to the store"})

	got, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Target != 0 || got.Current != 0 || got.Unit != "" {
		t.Fatalf("expected zero progress fields, got target=%v current=%v unit=%q", got.Target, got.Current, got.Unit)
	}

	// Zeroing a previously-set field must persist the zero rather than being
	// treated as "unset" and leaving the old value in place.
	got.Target = 30
	got.Current = 24
	got.Unit = "days"
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("update to non-zero: %v", err)
	}
	set, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get after non-zero update: %v", err)
	}
	if set.Target != 30 || set.Current != 24 || set.Unit != "days" {
		t.Fatalf("expected 24/30 days, got target=%v current=%v unit=%q", set.Target, set.Current, set.Unit)
	}

	set.Target = 0
	set.Current = 0
	set.Unit = ""
	if err := store.Update(ctx, set); err != nil {
		t.Fatalf("update back to zero: %v", err)
	}
	cleared, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get after zeroing: %v", err)
	}
	if cleared.Target != 0 || cleared.Current != 0 || cleared.Unit != "" {
		t.Fatalf("expected cleared progress fields, got target=%v current=%v unit=%q", cleared.Target, cleared.Current, cleared.Unit)
	}
}

func TestKeyResultStore_ProgressFieldsNegativeDelta(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// Progress is not always upward and not always positive: a savings gap
	// starts below zero and closes toward a target. Both the negative stored
	// value and the downward step have to survive the REAL column.
	obj := measuredKR(t, ctx, store, &Objective{
		Title:   "Close the cash gap",
		Target:  0,
		Current: -1250.75,
		Unit:    "usd",
	})

	got, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Current != -1250.75 {
		t.Fatalf("expected current -1250.75, got %v", got.Current)
	}

	got.Current = -1875.5 // a step in the wrong direction
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, err := store.Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if after.Current != -1875.5 {
		t.Fatalf("expected current -1875.5 after update, got %v", after.Current)
	}
	if delta := after.Current - got.Target; delta >= 0 {
		t.Fatalf("expected negative delta from target, got %v", delta)
	}
}
