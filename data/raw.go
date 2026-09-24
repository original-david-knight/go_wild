package gowild_data

import "fmt"

// Executor runs raw SQL against a database or an open transaction.
type Executor = sqlExecutor

// Backend names the SQL engine behind a Database.
type Backend int

const (
	BackendSqlite Backend = iota
	BackendPostgres
)

// Raw exposes the SQL executor behind db, for queries the table DAOs cannot
// express (full-text search, vector ranking, backend-specific DDL). Inside
// RunInTransaction it returns the transaction, so raw statements commit with
// the DAO writes around them. Callers own dialect differences: placeholders
// are "?" on SQLite and "$n" on PostgreSQL.
func Raw(db Database) (Executor, Backend, error) {
	switch typed := db.(type) {
	case *SqliteDatabase:
		return typed.db, BackendSqlite, nil
	case *SqliteTxDatabase:
		return typed.tx, BackendSqlite, nil
	case *PostgresDatabase:
		return typed.db, BackendPostgres, nil
	case *PostgresTxDatabase:
		return typed.tx, BackendPostgres, nil
	default:
		return nil, 0, fmt.Errorf("unsupported database type %T", db)
	}
}
