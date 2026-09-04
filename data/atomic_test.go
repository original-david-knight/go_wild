package gowild_data

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type atomicTestRow struct {
	ID       string    `json:"id"`
	UserID   string    `json:"user_id"`
	Revision int       `json:"revision"`
	Name     string    `json:"name"`
	Enabled  bool      `json:"enabled"`
	At       time.Time `json:"at"`
}

func (atomicTestRow) TableName() string { return "atomic_test_rows" }

func TestAtomicWritesSQLite(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	testAtomicWrites(t, db)
}

func TestAtomicWritesPostgres(t *testing.T) {
	db := skipIfNoPostgres(t)
	t.Cleanup(func() { db.Close() })
	cleanupTable(db, "atomic_test_rows")
	t.Cleanup(func() { cleanupTable(db, "atomic_test_rows") })
	testAtomicWrites(t, db)
}

func testAtomicWrites(t *testing.T, db Database) {
	t.Helper()
	if err := db.AddTable(atomicTestRow{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dao := db.Table(atomicTestRow{})
	atomicDAO := dao.(AtomicTableDAO)
	row := &atomicTestRow{ID: "one", UserID: "alice", Revision: 1, Name: "original", Enabled: true,
		At: time.Date(2026, 9, 4, 12, 0, 0, 123456000, time.UTC)}
	if inserted, err := atomicDAO.InsertIfAbsent(ctx, row); err != nil || !inserted {
		t.Fatalf("insert = %v, %v", inserted, err)
	}
	duplicate := *row
	duplicate.Name = "duplicate"
	if inserted, err := atomicDAO.InsertIfAbsent(ctx, &duplicate); err != nil || inserted {
		t.Fatalf("duplicate insert = %v, %v", inserted, err)
	}
	var read atomicTestRow
	if err := dao.Get(ctx, row.ID, &read); err != nil || read.Name != "original" {
		t.Fatalf("first write lost: %+v, %v", read, err)
	}
	row.Revision = 2
	row.Name = "updated"
	if updated, err := atomicDAO.UpdateIf(ctx, row, map[string]any{"revision": 1, "at": read.At, "enabled": true}); err != nil || !updated {
		t.Fatalf("matched update = %v, %v", updated, err)
	}
	row.Name = "stale"
	if updated, err := atomicDAO.UpdateIf(ctx, row, map[string]any{"revision": 1}); err != nil || updated {
		t.Fatalf("stale update = %v, %v", updated, err)
	}
	if deleted, err := atomicDAO.DeleteIf(ctx, row.ID, map[string]any{"revision": 1}); err != nil || deleted {
		t.Fatalf("stale delete = %v, %v", deleted, err)
	}
	if err := dao.Get(ctx, row.ID, &read); err != nil || read.Name != "updated" || read.Revision != 2 {
		t.Fatalf("stale operation changed row: %+v, %v", read, err)
	}
	if _, err := atomicDAO.DeleteIf(ctx, row.ID, map[string]any{"revision OR 1=1": 2}); err == nil {
		t.Fatal("unknown predicate column was accepted")
	}
	if deleted, err := atomicDAO.DeleteIf(ctx, "missing", nil); err != nil || deleted {
		t.Fatalf("missing delete = %v, %v", deleted, err)
	}
	if deleted, err := atomicDAO.DeleteIf(ctx, row.ID, map[string]any{"revision": 2}); err != nil || !deleted {
		t.Fatalf("matched delete = %v, %v", deleted, err)
	}

	t.Run("one concurrent insert winner", func(t *testing.T) {
		var won atomic.Int32
		concurrentAtomic(t, func(i int) error {
			inserted, err := atomicDAO.InsertIfAbsent(ctx, &atomicTestRow{ID: "claim", Revision: 1, Name: fmt.Sprint(i)})
			if inserted {
				won.Add(1)
			}
			return err
		})
		if won.Load() != 1 {
			t.Fatalf("%d insert winners, want one", won.Load())
		}
	})
	t.Run("one concurrent revision winner", func(t *testing.T) {
		var won atomic.Int32
		concurrentAtomic(t, func(i int) error {
			updated, err := atomicDAO.UpdateIf(ctx, &atomicTestRow{ID: "claim", Revision: 2, Name: fmt.Sprint(i)}, map[string]any{"revision": 1})
			if updated {
				won.Add(1)
			}
			return err
		})
		if won.Load() != 1 {
			t.Fatalf("%d update winners, want one", won.Load())
		}
	})
	t.Run("rollback and user scope", func(t *testing.T) {
		errRollback := errors.New("rollback")
		err := db.RunInTransaction(ctx, func(tx Database) error {
			table := tx.ForUser("alice").Table(atomicTestRow{})
			scoped := table.(AtomicTableDAO)
			row := &atomicTestRow{ID: "scoped", Revision: 1}
			if inserted, err := scoped.InsertIfAbsent(ctx, row); err != nil || !inserted {
				return fmt.Errorf("scoped insert: %v, %v", inserted, err)
			}
			other := tx.ForUser("bob").Table(atomicTestRow{}).(AtomicTableDAO)
			if updated, err := other.UpdateIf(ctx, &atomicTestRow{ID: row.ID, Revision: 2}, map[string]any{"revision": 1, "user_id": "alice"}); err != nil || updated {
				return fmt.Errorf("foreign user update: %v, %v", updated, err)
			}
			if deleted, err := other.DeleteIf(ctx, row.ID, map[string]any{"user_id": "alice"}); err != nil || deleted {
				return fmt.Errorf("foreign user delete: %v, %v", deleted, err)
			}
			row.Revision = 2
			if updated, err := scoped.UpdateIf(ctx, row, map[string]any{"revision": 1}); err != nil || !updated {
				return fmt.Errorf("scoped update: %v, %v", updated, err)
			}
			if deleted, err := scoped.DeleteIf(ctx, row.ID, map[string]any{"revision": 2}); err != nil || !deleted {
				return fmt.Errorf("scoped delete: %v, %v", deleted, err)
			}
			if deleted, err := tx.Table(atomicTestRow{}).(AtomicTableDAO).DeleteIf(ctx, "claim", map[string]any{"revision": 2}); err != nil || !deleted {
				return fmt.Errorf("transactional delete: %v, %v", deleted, err)
			}
			if _, err := scoped.InsertIfAbsent(ctx, &atomicTestRow{ID: "rolled-back"}); err != nil {
				return err
			}
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("transaction = %v", err)
		}
		if err := dao.Get(ctx, "rolled-back", &read); err == nil {
			t.Fatal("insert survived rollback")
		}
		if err := dao.Get(ctx, "claim", &read); err != nil || read.Revision != 2 {
			t.Fatalf("delete survived rollback: %+v, %v", read, err)
		}
	})
	t.Run("value model cannot move ownership", func(t *testing.T) {
		scoped := db.ForUser("alice").Table(atomicTestRow{}).(AtomicTableDAO)
		row := atomicTestRow{ID: "value-model", UserID: "bob", Name: "first", Revision: 1}
		if inserted, err := scoped.InsertIfAbsent(ctx, row); err != nil || !inserted {
			t.Fatalf("value insert = %v, %v", inserted, err)
		}
		var held atomicTestRow
		if err := dao.Get(ctx, row.ID, &held); err != nil || held.UserID != "alice" {
			t.Fatalf("insert stored foreign ownership: %+v, %v", held, err)
		}
		row.Revision = 2
		row.Name = "second"
		if updated, err := scoped.UpdateIf(ctx, row, map[string]any{"revision": 1}); err != nil || !updated {
			t.Fatalf("value update = %v, %v", updated, err)
		}
		if err := dao.Get(ctx, row.ID, &held); err != nil || held.UserID != "alice" || held.Name != "second" {
			t.Fatalf("update moved ownership: %+v, %v", held, err)
		}
	})
}

func concurrentAtomic(t *testing.T, work func(int) error) {
	t.Helper()
	const n = 8
	var wg sync.WaitGroup
	gate := make(chan struct{})
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			errs <- work(i)
		}()
	}
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAtomicHistoricalNullAndConstraintErrors(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.AddTable(atomicTestRow{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Old rows acquire nullable columns when AddTable adds new fields.
	if _, err := db.db.Exec("INSERT INTO atomic_test_rows (id, name) VALUES ('legacy', 'unique-name')"); err != nil {
		t.Fatal(err)
	}
	dao := db.Table(atomicTestRow{}).(AtomicTableDAO)
	emptyScope := db.ForUser("").Table(atomicTestRow{}).(AtomicTableDAO)
	if updated, err := emptyScope.UpdateIf(ctx, &atomicTestRow{ID: "legacy", Name: "stolen"}, nil); err != nil || updated {
		t.Fatalf("empty scope updated an unowned legacy row: %v, %v", updated, err)
	}
	if deleted, err := emptyScope.DeleteIf(ctx, "legacy", nil); err != nil || deleted {
		t.Fatalf("empty scope deleted an unowned legacy row: %v, %v", deleted, err)
	}
	if updated, err := dao.UpdateIf(ctx, &atomicTestRow{ID: "legacy", Revision: 1, Name: "unique-name"}, map[string]any{"revision": 0, "enabled": false, "user_id": nil}); err != nil || !updated {
		t.Fatalf("legacy zero predicate = %v, %v", updated, err)
	}
	if _, err := db.db.Exec("CREATE UNIQUE INDEX atomic_test_name_unique ON atomic_test_rows (name)"); err != nil {
		t.Fatal(err)
	}
	if inserted, err := dao.InsertIfAbsent(ctx, &atomicTestRow{ID: "different-id", Name: "unique-name"}); err == nil || inserted {
		t.Fatalf("secondary constraint treated as same id: %v, %v", inserted, err)
	}
}
