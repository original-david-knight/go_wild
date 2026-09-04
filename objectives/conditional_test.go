package objectives

import (
	"context"
	"errors"
	"testing"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestConditionalUpdatePreservesValidationAndRevisionCondition(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	store := NewObjectiveStore(db, "company-a")
	root := &Objective{Title: "Root", Revision: 1}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatal(err)
	}
	stale := *root
	root.Title = "Saved"
	root.Revision = 2
	updated, err := store.UpdateIf(ctx, root, map[string]any{"revision": 1})
	if err != nil || !updated {
		t.Fatalf("matching update = %v, %v", updated, err)
	}
	stale.Title = "Stale"
	updated, err = store.UpdateIf(ctx, &stale, map[string]any{"revision": 1})
	if err != nil || updated {
		t.Fatalf("stale update = %v, %v", updated, err)
	}
	invalid := *root
	invalid.Target = 10
	if _, err := store.UpdateIf(ctx, &invalid, map[string]any{"revision": 2}); !errors.Is(err, ErrObjectiveMeasures) {
		t.Fatalf("measuring root error = %v", err)
	}
	if _, err := NewObjectiveStore(db, "company-b").UpdateIf(ctx, root, map[string]any{"revision": 2}); err == nil {
		t.Fatal("conditional update escaped company scope")
	}
	got, err := store.Get(ctx, root.ID)
	if err != nil || got.Title != "Saved" || got.Revision != 2 || got.Target != 0 {
		t.Fatalf("refused updates changed root: %+v, %v", got, err)
	}
}

func TestConditionalDeleteChecksRootBeforeRemovingTheSubtree(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	store := NewObjectiveStore(db, "company-a")
	root := &Objective{Title: "Root", Revision: 2}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatal(err)
	}
	child := &Objective{Title: "Child", Revision: 1}
	if err := store.CreateKeyResult(ctx, root.ID, child); err != nil {
		t.Fatal(err)
	}
	activity := &ActivityEvent{ID: "event-1", ObjectiveID: child.ID}
	if err := db.Table(ActivityEvent{}).Insert(ctx, activity); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.DeleteIf(ctx, root.ID, map[string]any{"revision": 1})
	if err != nil || deleted {
		t.Fatalf("stale delete = %v, %v", deleted, err)
	}
	if _, err := NewObjectiveStore(db, "company-b").DeleteIf(ctx, root.ID, map[string]any{"revision": 2}); err == nil {
		t.Fatal("conditional delete escaped company scope")
	}
	if tree, err := store.GetTree(ctx, root.ID); err != nil || len(tree) != 2 {
		t.Fatalf("refused deletes changed tree: %+v, %v", tree, err)
	}
	var held ActivityEvent
	if err := db.Table(ActivityEvent{}).Get(ctx, activity.ID, &held); err != nil {
		t.Fatalf("refused deletes removed activity: %v", err)
	}
	deleted, err = store.DeleteIf(ctx, root.ID, map[string]any{"revision": 2})
	if err != nil || !deleted {
		t.Fatalf("matching delete = %v, %v", deleted, err)
	}
	for _, model := range []any{Objective{}, ActivityEvent{}} {
		rows, err := db.Table(model).GetAll(ctx)
		if err != nil || len(rows) != 0 {
			t.Fatalf("matching delete left %T rows: %+v, %v", model, rows, err)
		}
	}
}

type failedActivityDB struct {
	gowild_data.Database
	err error
}

func (d *failedActivityDB) Table(model any) gowild_data.TableDAO {
	dao := d.Database.Table(model)
	if _, ok := model.(ActivityEvent); ok {
		return &failedActivityDAO{TableDAO: dao, err: d.err}
	}
	return dao
}

func (d *failedActivityDB) RunInTransaction(ctx context.Context, fn func(gowild_data.Database) error) error {
	return d.Database.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		return fn(&failedActivityDB{Database: tx, err: d.err})
	})
}

type failedActivityDAO struct {
	gowild_data.TableDAO
	err error
}

func (d *failedActivityDAO) Delete(context.Context, string) error { return d.err }

func TestDeleteRollsBackWhenActivityDeletionFails(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	store := NewObjectiveStore(db, "")
	root := &Objective{Title: "Root"}
	if err := store.CreateObjective(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := db.Table(ActivityEvent{}).Insert(ctx, &ActivityEvent{ID: "event-1", ObjectiveID: root.ID}); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("activity delete failed")
	err := NewObjectiveStore(&failedActivityDB{Database: db, err: injected}, "").Delete(ctx, root.ID)
	if !errors.Is(err, injected) {
		t.Fatalf("delete error = %v, want %v", err, injected)
	}
	if _, err := store.Get(ctx, root.ID); err != nil {
		t.Fatalf("failed delete removed root: %v", err)
	}
	var event ActivityEvent
	if err := db.Table(ActivityEvent{}).Get(ctx, "event-1", &event); err != nil {
		t.Fatalf("failed delete removed activity: %v", err)
	}
}
