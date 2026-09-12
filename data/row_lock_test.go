package gowild_data

import (
	"errors"
	"testing"
)

type rowLockFixture struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

func (rowLockFixture) TableName() string { return "row_lock_fixtures" }

func TestRowLockRequiresTransactionAndPreservesValues(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(rowLockFixture{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := db.Table(rowLockFixture{}).Insert(ctx, &rowLockFixture{ID: "one", Body: "preserve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := LockRow(ctx, db, rowLockFixture{}, "one"); err == nil {
		t.Fatal("nontransaction lock accepted")
	}
	abort := errors.New("fixture rollback")
	if err := db.RunInTransaction(ctx, func(tx Database) error {
		if locked, err := LockRow(ctx, tx, rowLockFixture{}, "absent"); err != nil || locked {
			t.Fatalf("absent lock: %v %v", locked, err)
		}
		if locked, err := LockRow(ctx, tx, rowLockFixture{}, "one"); err != nil || !locked {
			t.Fatalf("row lock: %v %v", locked, err)
		}
		var got rowLockFixture
		if err := tx.Table(rowLockFixture{}).Get(ctx, "one", &got); err != nil || got.Body != "preserve" {
			t.Fatalf("lock replaced body: %+v %v", got, err)
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var got rowLockFixture
	if err := db.Table(rowLockFixture{}).Get(ctx, "one", &got); err != nil || got.Body != "preserve" {
		t.Fatalf("rollback changed body: %+v %v", got, err)
	}
}
