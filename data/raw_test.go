package gowild_data

import (
	"context"
	"testing"
)

type rawModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestRawSharesTransaction(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(rawModel{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = db.RunInTransaction(ctx, func(tx Database) error {
		if err := tx.Table(rawModel{}).Insert(ctx, &rawModel{ID: "a", Name: "x"}); err != nil {
			return err
		}
		exec, backend, err := Raw(tx)
		if err != nil {
			return err
		}
		if backend != BackendSqlite {
			t.Fatalf("backend = %v", backend)
		}
		var n int
		if err := exec.QueryRowContext(ctx, "SELECT count(*) FROM raw_models WHERE name = ?", "x").Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("raw query inside tx saw %d rows, want 1", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
