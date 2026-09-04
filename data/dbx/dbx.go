// Package dbx is a small typed kit over the data module's Database and
// TableDAO: the query-then-assert loop, the query-by-id upsert, the
// delete-absent/upsert-present reconcile, and the two facts every consumer
// would otherwise declare for itself — the DBFunc handle seam and the one
// "database unavailable" error, so an unavailable database is reported one
// way everywhere.
package dbx

import (
	"context"
	"errors"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// DBFunc yields the live database handle, or nil while it is down.
type DBFunc func() gowild_data.Database

// ErrUnavailable reports the database being down — the one declaration of
// the fact, so every caller's unavailable response is produced one way.
var ErrUnavailable = errors.New("database unavailable")

// ErrAtomicUnsupported refuses a conditional write through a table that does
// not implement it. A read-then-write fallback would silently lose atomicity.
var ErrAtomicUnsupported = errors.New("table does not support atomic writes")

// Table resolves a model's DAO, or nil with the database down.
func Table[T any](db gowild_data.Database) gowild_data.TableDAO {
	if db == nil {
		return nil
	}
	var zero T
	return db.Table(zero)
}

// All queries a model's table and type-asserts each row — the
// query-then-assert loop every consumer would otherwise write by hand.
func All[T any](ctx context.Context, db gowild_data.Database, opts gowild_data.QueryOpts) ([]*T, error) {
	dao := Table[T](db)
	if dao == nil {
		return nil, ErrUnavailable
	}
	rows, err := dao.Query(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := make([]*T, 0, len(rows))
	for _, raw := range rows {
		if row, ok := raw.(*T); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// Get reads one row by id, or nil when absent.
func Get[T any](ctx context.Context, db gowild_data.Database, id string) (*T, error) {
	rows, err := All[T](ctx, db, gowild_data.QueryOpts{
		Where: map[string]any{"id": id}, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// Upsert writes row under id: an existing row is updated in place, a fresh
// one inserted — the query-by-id then Update-or-Insert body in one place.
func Upsert[T any](ctx context.Context, db gowild_data.Database, id string, row *T) error {
	dao := Table[T](db)
	if dao == nil {
		return ErrUnavailable
	}
	existing, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"id": id}, Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return dao.Update(ctx, row)
	}
	return dao.Insert(ctx, row)
}

// InsertNew atomically inserts row only when its primary key is absent. id
// must be row's primary key; it remains in the signature for existing callers.
func InsertNew[T any](ctx context.Context, db gowild_data.Database, id string, row *T) (bool, error) {
	dao, err := atomicTable[T](db)
	if err != nil {
		return false, err
	}
	return dao.InsertIfAbsent(ctx, row)
}

// UpdateIf writes row only when its stored columns match expected.
func UpdateIf[T any](ctx context.Context, db gowild_data.Database, row *T, expected map[string]any) (bool, error) {
	dao, err := atomicTable[T](db)
	if err != nil {
		return false, err
	}
	return dao.UpdateIf(ctx, row, expected)
}

// DeleteIf deletes id only when its stored columns match expected.
func DeleteIf[T any](ctx context.Context, db gowild_data.Database, id string, expected map[string]any) (bool, error) {
	dao, err := atomicTable[T](db)
	if err != nil {
		return false, err
	}
	return dao.DeleteIf(ctx, id, expected)
}

func atomicTable[T any](db gowild_data.Database) (gowild_data.AtomicTableDAO, error) {
	dao := Table[T](db)
	if dao == nil {
		return nil, ErrUnavailable
	}
	atomic, ok := dao.(gowild_data.AtomicTableDAO)
	if !ok {
		return nil, ErrAtomicUnsupported
	}
	return atomic, nil
}

// ReplaceSet reconciles the rows matching filter to exactly want: present
// rows are upserted, rows the filter matches that want does not name are
// deleted. idOf names a row's id — the reconcile every sync against an
// external source of truth would otherwise write by hand.
func ReplaceSet[T any](ctx context.Context, db gowild_data.Database, filter gowild_data.QueryOpts, want []*T, idOf func(*T) string) error {
	dao := Table[T](db)
	if dao == nil {
		return ErrUnavailable
	}
	existing, err := All[T](ctx, db, filter)
	if err != nil {
		return err
	}
	wanted := make(map[string]bool, len(want))
	for _, row := range want {
		wanted[idOf(row)] = true
	}
	for _, row := range existing {
		if !wanted[idOf(row)] {
			if err := dao.Delete(ctx, idOf(row)); err != nil {
				return err
			}
		}
	}
	for _, row := range want {
		if err := Upsert(ctx, db, idOf(row), row); err != nil {
			return err
		}
	}
	return nil
}
