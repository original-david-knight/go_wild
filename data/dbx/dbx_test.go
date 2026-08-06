package dbx

import (
	"context"
	"errors"
	"testing"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// thing is the kit's own model — the tests exercise the generics against a
// real sqlite-backed database, which is what every consumer runs on.
type thing struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (thing) TableName() string { return "dbx_test_things" }

func memDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.AddTable(thing{}); err != nil {
		t.Fatalf("add table: %v", err)
	}
	return db
}

func TestAllTypeAssertsRows(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	for _, row := range []*thing{
		{ID: "a", Name: "first", Kind: "x"},
		{ID: "b", Name: "second", Kind: "y"},
	} {
		if err := db.Table(thing{}).Insert(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := All[thing](ctx, db, gowild_data.QueryOpts{OrderBy: "id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "first" || rows[1].Name != "second" {
		t.Errorf("All = %+v, want both rows typed and ordered", rows)
	}

	got, err := Get[thing](ctx, db, "b")
	if err != nil || got == nil || got.Name != "second" {
		t.Errorf("Get = %+v, %v, want the b row", got, err)
	}
	if absent, err := Get[thing](ctx, db, "zzz"); err != nil || absent != nil {
		t.Errorf("Get(absent) = %+v, %v, want nil and no error", absent, err)
	}
}

func TestUpsertInsertsThenUpdates(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()

	if err := Upsert(ctx, db, "a", &thing{ID: "a", Name: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(ctx, db, "a", &thing{ID: "a", Name: "v2"}); err != nil {
		t.Fatal(err)
	}
	rows, err := All[thing](ctx, db, gowild_data.QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "v2" {
		t.Errorf("rows = %+v, want the one row updated in place", rows)
	}

	// The insert-only shape: a second write under the same id is a no-op.
	inserted, err := InsertNew(ctx, db, "b", &thing{ID: "b", Name: "fresh"})
	if err != nil || !inserted {
		t.Fatalf("InsertNew(fresh) = %t, %v, want an insert", inserted, err)
	}
	inserted, err = InsertNew(ctx, db, "b", &thing{ID: "b", Name: "again"})
	if err != nil || inserted {
		t.Fatalf("InsertNew(existing) = %t, %v, want a no-op", inserted, err)
	}
	if row, _ := Get[thing](ctx, db, "b"); row.Name != "fresh" {
		t.Errorf("row = %+v, want the first write kept", row)
	}
}

func TestReplaceSetDeletesAbsentAndUpsertsPresent(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	for _, row := range []*thing{
		{ID: "keep", Name: "old", Kind: "x"},
		{ID: "drop", Name: "going", Kind: "x"},
		{ID: "other", Name: "unrelated", Kind: "y"},
	} {
		if err := db.Table(thing{}).Insert(ctx, row); err != nil {
			t.Fatal(err)
		}
	}

	err := ReplaceSet(ctx, db,
		gowild_data.QueryOpts{Where: map[string]any{"kind": "x"}},
		[]*thing{
			{ID: "keep", Name: "new", Kind: "x"},
			{ID: "added", Name: "fresh", Kind: "x"},
		},
		func(row *thing) string { return row.ID },
	)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := All[thing](ctx, db, gowild_data.QueryOpts{OrderBy: "id"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.ID] = row.Name
	}
	want := map[string]string{"keep": "new", "added": "fresh", "other": "unrelated"}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v — absent deleted, present upserted, the filter's outside untouched", got, want)
	}
	for id, name := range want {
		if got[id] != name {
			t.Errorf("%s = %q, want %q", id, got[id], name)
		}
	}
}

func TestErrUnavailableWhenDBFuncNil(t *testing.T) {
	ctx := context.Background()
	if _, err := All[thing](ctx, nil, gowild_data.QueryOpts{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("All(nil db) = %v, want ErrUnavailable", err)
	}
	if err := Upsert(ctx, nil, "a", &thing{ID: "a"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Upsert(nil db) = %v, want ErrUnavailable", err)
	}
	if _, err := InsertNew(ctx, nil, "a", &thing{ID: "a"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("InsertNew(nil db) = %v, want ErrUnavailable", err)
	}
	if err := ReplaceSet(ctx, nil, gowild_data.QueryOpts{}, nil, func(*thing) string { return "" }); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ReplaceSet(nil db) = %v, want ErrUnavailable", err)
	}
	var down DBFunc = func() gowild_data.Database { return nil }
	if _, err := All[thing](ctx, down(), gowild_data.QueryOpts{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("All(down DBFunc) = %v, want ErrUnavailable", err)
	}
}
