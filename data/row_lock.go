package gowild_data

import (
	"context"
	"errors"
	"fmt"
)

// LockRow takes a database row lock until the supplied transaction finishes.
// A no-op primary-key update works on both PostgreSQL and SQLite without
// replacing any concurrently updated fields. Call it before reading a record
// whose related rows must remain stable through a compound operation.
// Missing rows return false; nontransaction databases fail closed.
func LockRow(ctx context.Context, tx Database, model any, id string) (bool, error) {
	switch tx.(type) {
	case *SqliteTxDatabase, *PostgresTxDatabase:
	default:
		return false, errors.New("row locking requires a SQL transaction")
	}
	var dao *sqlTableDAO
	switch table := tx.Table(model).(type) {
	case *SqliteTxTableDAO:
		dao = table.sqlTableDAO
	case *PostgresTxTableDAO:
		dao = table.sqlTableDAO
	}
	if dao == nil {
		return false, errors.New("row locking requires a registered SQL table")
	}
	column := dao.getIDColumn()
	query := fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s", dao.meta.TableName, column, column, column, dao.dialect.Placeholder(1))
	return wroteRow(dao.exec.ExecContext(ctx, query, id))
}
