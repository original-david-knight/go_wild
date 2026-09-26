package gowild_data

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

type address struct {
	City string `json:"city"`
	Zip  string `json:"zip"`
}

// typedRow carries every field shape the SQLite dialect encodes differently:
// pointers to scalars and structs, nested structs, maps and pointer-to-slice.
type typedRow struct {
	ID       string            `json:"id"`
	UserID   string            `json:"user_id"`
	Home     address           `json:"home"`
	Labels   map[string]string `json:"labels"`
	SeenAt   *time.Time        `json:"seen_at"`
	Score    *float64          `json:"score"`
	Count    *int64            `json:"count"`
	Flag     *bool             `json:"flag"`
	Nick     *string           `json:"nick"`
	Work     *address          `json:"work"`
	Aliases  *[]string         `json:"aliases"`
	Weight   float64           `json:"weight"`
	Archived bool              `json:"archived"`
}

func (typedRow) TableName() string { return "typed_rows" }

func newTypedDB(t *testing.T) *SqliteDatabase {
	t.Helper()
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.AddTable(typedRow{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSqliteRoundTripsPointerAndNestedFields(t *testing.T) {
	db := newTypedDB(t)
	ctx := context.Background()

	seen := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	score, count, flag, nick := 2.5, int64(42), true, "al"
	aliases := []string{"a", "b"}
	in := typedRow{
		ID:      "r1",
		Home:    address{City: "Oslo", Zip: "0150"},
		Labels:  map[string]string{"k": "v"},
		SeenAt:  &seen,
		Score:   &score,
		Count:   &count,
		Flag:    &flag,
		Nick:    &nick,
		Work:    &address{City: "Bergen"},
		Aliases: &aliases,
		Weight:  71.5,
	}
	if err := db.Table(typedRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}

	var got typedRow
	if err := db.Table(typedRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}

func TestSqliteLeavesNilPointersNil(t *testing.T) {
	db := newTypedDB(t)
	ctx := context.Background()

	falseFlag := false
	if err := db.Table(typedRow{}).Insert(ctx, &typedRow{ID: "r2", Flag: &falseFlag}); err != nil {
		t.Fatal(err)
	}
	var got typedRow
	if err := db.Table(typedRow{}).Get(ctx, "r2", &got); err != nil {
		t.Fatal(err)
	}
	if got.SeenAt != nil || got.Score != nil || got.Count != nil || got.Nick != nil || got.Work != nil || got.Aliases != nil || got.Labels != nil {
		t.Fatalf("NULL columns should leave pointers, maps and slices nil, got %+v", got)
	}
	if got.Flag == nil || *got.Flag != false {
		t.Fatalf("a stored false *bool should read back as false, got %v", got.Flag)
	}
}

// TestSqliteRejectsCorruptStoredValues writes values the dialect cannot
// decode straight into the table and checks Get reports the column's error
// instead of returning a half-filled row.
func TestSqliteRejectsCorruptStoredValues(t *testing.T) {
	cases := []struct {
		name, column, value, wantErr string
	}{
		{"bad time pointer", "seen_at", "yesterday", "cannot parse"},
		{"bad bool pointer", "flag", "maybe", "invalid bool value: maybe"},
		{"bad float pointer", "score", "lots", "invalid character"},
		{"bad int pointer", "count", "many", "invalid character"},
		{"bad struct pointer", "work", "{", "unexpected end of JSON input"},
		{"bad slice pointer", "aliases", "[1,", "unexpected end of JSON input"},
		{"bad nested struct", "home", "{", "unexpected end of JSON input"},
		{"bad map", "labels", "[", "unexpected end of JSON input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTypedDB(t)
			ctx := context.Background()
			if _, err := db.db.Exec("INSERT INTO typed_rows (id, "+tc.column+") VALUES ('bad', ?)", tc.value); err != nil {
				t.Fatal(err)
			}
			var got typedRow
			err := db.Table(typedRow{}).Get(ctx, "bad", &got)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Get error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestSqliteRejectsCorruptTimeValue(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(TestUser{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO test_users (id, created_at) VALUES ('u', 'not-a-time')"); err != nil {
		t.Fatal(err)
	}
	var got TestUser
	err = db.Table(TestUser{}).Get(context.Background(), "u", &got)
	if err == nil || !strings.Contains(err.Error(), `parsing time "not-a-time"`) {
		t.Fatalf("Get error = %v, want a time parse error", err)
	}
}

func TestSqliteUserScopedUpdate(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(TestUser{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	users := db.ForUser("alice").Table(TestUser{})
	if err := users.Insert(ctx, &TestUser{ID: "u1", Name: "Before"}); err != nil {
		t.Fatal(err)
	}
	if err := users.Update(ctx, &TestUser{ID: "u1", UserID: "alice", Name: "After", Age: 7}); err != nil {
		t.Fatal(err)
	}
	var got TestUser
	if err := users.Get(ctx, "u1", &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "After" || got.Age != 7 || got.UserID != "alice" {
		t.Fatalf("after Update got %+v", got)
	}
}

func TestSqliteTransactionHandleSemantics(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(TestUser{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("Ping on an open database: %v", err)
	}

	err = db.RunInTransaction(ctx, func(tx Database) error {
		if err := tx.Ping(ctx); err != nil {
			return err
		}
		// A nested RunInTransaction joins the outer transaction: a write
		// in it is visible to the outer handle before commit.
		if err := tx.RunInTransaction(ctx, func(inner Database) error {
			if inner != tx {
				t.Errorf("nested transaction handle = %p, want the outer %p", inner, tx)
			}
			return inner.Table(TestUser{}).Insert(ctx, &TestUser{ID: "n1", Name: "nested"})
		}); err != nil {
			return err
		}
		var got TestUser
		if err := tx.Table(TestUser{}).Get(ctx, "n1", &got); err != nil {
			return err
		}
		if got.Name != "nested" {
			t.Errorf("nested write read back as %q", got.Name)
		}
		// Close on a transaction handle is a no-op; the commit still lands.
		return tx.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	var got TestUser
	if err := db.Table(TestUser{}).Get(ctx, "n1", &got); err != nil {
		t.Fatalf("committed nested write missing: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(ctx); err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("Ping after Close = %v, want database is closed", err)
	}
	if err := db.RunInTransaction(ctx, func(Database) error { return nil }); err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("RunInTransaction after Close = %v, want database is closed", err)
	}
}

// TestSqliteTablesExistOnlyForRegisteredModels checks that a handle, a
// transaction and their user-scoped views hand out a DAO for a registered
// model and nil for one never added.
func TestSqliteTablesExistOnlyForRegisteredModels(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(TestUser{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = db.RunInTransaction(ctx, func(tx Database) error {
		if dao := tx.Table(typedRow{}); dao != nil {
			t.Errorf("tx.Table for an unregistered model = %T, want nil", dao)
		}
		if dao := tx.ForUser("u").Table(typedRow{}); dao != nil {
			t.Errorf("tx.ForUser.Table for an unregistered model = %T, want nil", dao)
		}
		return tx.ForUser("u").Table(TestUser{}).Insert(ctx, &TestUser{ID: "t1", Name: "in tx"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if dao := db.ForUser("u").Table(typedRow{}); dao != nil {
		t.Errorf("ForUser.Table for an unregistered model = %T, want nil", dao)
	}
	var got TestUser
	if err := db.ForUser("u").Table(TestUser{}).Get(ctx, "t1", &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "in tx" || got.UserID != "u" {
		t.Fatalf("row written through the tx's user table = %+v, want Name in tx, UserID u", got)
	}
}

func TestNewSqliteDatabaseReportsUnopenableFile(t *testing.T) {
	_, err := NewSqliteDatabase(t.TempDir() + "/missing-dir/db.sqlite")
	if err == nil || !strings.Contains(err.Error(), "failed to enable foreign keys") {
		t.Fatalf("NewSqliteDatabase on an unwritable path = %v, want a foreign-keys pragma error", err)
	}
}

func TestSqliteAddTableRejectsNonStruct(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(42); err == nil || err.Error() != "model must be a struct, got int" {
		t.Fatalf("AddTable(42) = %v", err)
	}
}
