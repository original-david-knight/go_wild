package gowild_data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type indexBackend int

const (
	backendSqlite indexBackend = iota
	backendPostgres
)

// ensureIndexMu serializes ensureUniqueIndexWhere across all callers in the
// process. The stale-definition check is a read-then-write sequence; without
// a mutex, two goroutines with different intended definitions could both
// observe "missing" and one would no-op on the other's CREATE IF NOT EXISTS.
// Index setup is a startup-time operation, so contention is effectively nil.
//
// This mutex does NOT protect against two processes sharing the same SQLite
// file racing on the check/create sequence. Geet's design is explicitly
// single-process (one geetd binary + one SQLite file), so that scenario is
// out of scope. Multi-process gowild_data users who need strict guarantees
// must provide their own coordination (e.g. an advisory lock) around schema
// setup.
var ensureIndexMu sync.Mutex

// EnsureUniqueIndex creates a unique index if it does not already exist.
// The model must already be registered via AddTable.
func EnsureUniqueIndex(db Database, model any, indexName string, columns ...string) error {
	return ensureUniqueIndexWhere(db, model, indexName, columns, "")
}

// ensureUniqueIndexWhere creates a (possibly partial) unique index if it
// does not already exist. If whereClause is non-empty, it is appended as a
// SQL `WHERE <clause>` predicate, producing a partial unique index. Both
// SQLite (>= 3.8.0) and PostgreSQL support this form.
//
// The whereClause is caller-provided SQL and must not contain user input.
// It is used verbatim (identifiers in the clause are not quoted for the
// caller). Column names passed via `columns` are quoted.
//
// On SQLite, if an index with the same name already exists with a
// different definition, this function returns an error rather than
// silently leaving the stale index in place — that would break
// load-bearing invariants like geet's one-open-PR-per-branch rule.
// On PostgreSQL, the stale-definition check is skipped because
// `pg_indexes.indexdef` is not comparable to a client-constructed
// CREATE statement; callers who need strict checking on PG should
// DROP INDEX manually as part of migrations.
func ensureUniqueIndexWhere(db Database, model any, indexName string, columns []string, whereClause string) error {
	indexName = strings.TrimSpace(indexName)
	if indexName == "" {
		return fmt.Errorf("index name is required")
	}
	if len(columns) == 0 {
		return fmt.Errorf("at least one column is required")
	}

	ensureIndexMu.Lock()
	defer ensureIndexMu.Unlock()

	exec, tableName, backend, err := sqlExecAndTableForModel(db, model)
	if err != nil {
		return err
	}

	quotedIndex, err := quoteIdentifier(indexName)
	if err != nil {
		return err
	}
	quotedTable, err := quoteIdentifier(tableName)
	if err != nil {
		return err
	}

	quotedColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		quotedColumn, err := quoteIdentifier(strings.TrimSpace(column))
		if err != nil {
			return err
		}
		quotedColumns = append(quotedColumns, quotedColumn)
	}

	whereClause = strings.TrimSpace(whereClause)
	// The stale-definition check compares against a CREATE form WITHOUT
	// `IF NOT EXISTS` (sqlite_master stores the original statement without
	// that clause). The actual execution uses `IF NOT EXISTS` so concurrent
	// callers stay idempotent even if the stale check races with another
	// creator.
	baseDDL := fmt.Sprintf(
		"CREATE UNIQUE INDEX %s ON %s (%s)",
		quotedIndex,
		quotedTable,
		strings.Join(quotedColumns, ", "),
	)
	if whereClause != "" {
		baseDDL += " WHERE " + whereClause
	}
	execDDL := strings.Replace(baseDDL, "CREATE UNIQUE INDEX ", "CREATE UNIQUE INDEX IF NOT EXISTS ", 1)

	// On SQLite, compare stored DDL in sqlite_master against baseDDL and
	// fail loudly if a stale index with the same name holds a different
	// definition. On Postgres, pg_indexes.indexdef is normalized by the
	// server and not reliably comparable — skip the check there.
	if backend == backendSqlite {
		existing, err := lookupIndexDDL(exec, backend, indexName)
		if err != nil {
			return fmt.Errorf("failed to inspect existing index %s: %w", indexName, err)
		}
		if existing != "" && !sqlEquivalent(existing, baseDDL) {
			return fmt.Errorf(
				"index %s already exists with a different definition; refusing to proceed\n"+
					"  existing: %s\n"+
					"  desired:  %s",
				indexName, existing, baseDDL,
			)
		}
	}

	if _, err := exec.ExecContext(context.Background(), execDDL); err != nil {
		return fmt.Errorf("failed to ensure unique index %s on %s: %w", indexName, tableName, err)
	}
	return nil
}

// lookupIndexDDL returns the stored CREATE statement for an index, or ""
// if it does not exist.
func lookupIndexDDL(exec sqlExecutor, backend indexBackend, indexName string) (string, error) {
	var (
		query string
		arg   any = indexName
	)
	switch backend {
	case backendSqlite:
		query = "SELECT sql FROM sqlite_master WHERE type='index' AND name = ?"
	case backendPostgres:
		query = "SELECT indexdef FROM pg_indexes WHERE indexname = $1"
	default:
		return "", nil
	}
	var ddl sql.NullString
	err := exec.QueryRowContext(context.Background(), query, arg).Scan(&ddl)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if !ddl.Valid {
		return "", nil
	}
	return ddl.String, nil
}

// sqlEquivalent compares two CREATE INDEX statements ignoring whitespace
// and the optional `IF NOT EXISTS` marker, while preserving single-quoted
// string literals byte-for-byte (partial indices with `WHERE status = 'open'`
// must not be treated as equivalent to `'OPEN'`). Identifiers inside double
// quotes are also preserved, since SQLite treats them case-sensitively when
// quoted. It is a best-effort match; on a false positive, DROP INDEX and
// re-run.
func sqlEquivalent(a, b string) bool {
	return normalizeIndexDDL(a) == normalizeIndexDDL(b)
}

func normalizeIndexDDL(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\'' || c == '"' {
			// Copy the string literal verbatim, including escaped quote-quote.
			quote := c
			out.WriteByte(quote)
			i++
			for i < len(s) {
				if s[i] == quote {
					if i+1 < len(s) && s[i+1] == quote {
						out.WriteByte(quote)
						out.WriteByte(quote)
						i += 2
						continue
					}
					out.WriteByte(quote)
					i++
					break
				}
				out.WriteByte(s[i])
				i++
			}
			continue
		}
		// Outside a literal: lowercase and collapse whitespace.
		if c >= 'A' && c <= 'Z' {
			out.WriteByte(c + ('a' - 'A'))
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			out.WriteByte(' ')
			i++
			for i < len(s) {
				d := s[i]
				if d == ' ' || d == '\t' || d == '\n' || d == '\r' {
					i++
					continue
				}
				break
			}
			continue
		}
		out.WriteByte(c)
		i++
	}
	norm := strings.TrimSpace(out.String())
	// Strip an IF NOT EXISTS marker only at the start of the statement,
	// where CREATE UNIQUE INDEX places it. A ReplaceAll would also touch
	// any preserved string literal that happens to spell the phrase.
	const ifNotExists = "create unique index if not exists "
	const noIfNotExists = "create unique index "
	if strings.HasPrefix(norm, ifNotExists) {
		norm = noIfNotExists + norm[len(ifNotExists):]
	}
	return norm
}

func sqlExecAndTableForModel(db Database, model any) (sqlExecutor, string, indexBackend, error) {
	switch typed := db.(type) {
	case *SqliteDatabase:
		meta := tableMeta(typed.tables, model)
		if meta == nil {
			return nil, "", 0, fmt.Errorf("model %T is not registered", model)
		}
		return typed.db, meta.TableName, backendSqlite, nil
	case *SqliteTxDatabase:
		meta := tableMeta(typed.tables, model)
		if meta == nil {
			return nil, "", 0, fmt.Errorf("model %T is not registered", model)
		}
		return typed.tx, meta.TableName, backendSqlite, nil
	case *PostgresDatabase:
		meta := tableMeta(typed.tables, model)
		if meta == nil {
			return nil, "", 0, fmt.Errorf("model %T is not registered", model)
		}
		return typed.db, meta.TableName, backendPostgres, nil
	case *PostgresTxDatabase:
		meta := tableMeta(typed.tables, model)
		if meta == nil {
			return nil, "", 0, fmt.Errorf("model %T is not registered", model)
		}
		return typed.tx, meta.TableName, backendPostgres, nil
	default:
		return nil, "", 0, fmt.Errorf("unsupported database type %T", db)
	}
}

func quoteIdentifier(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("identifier cannot be empty")
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}
