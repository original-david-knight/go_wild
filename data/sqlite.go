package gowild_data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SqliteDatabase implements Database for SQLite.
type SqliteDatabase struct {
	db     *sql.DB
	tables map[string]*modelMeta
}

// NewSqliteDatabase creates a new SQLite database connection.
// The dsn can be a file path or ":memory:" for an in-memory database.
func NewSqliteDatabase(dsn string) (*SqliteDatabase, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// In-memory SQLite databases are private per connection — a
	// multi-connection pool would give each connection its own empty
	// database, so tables created on one connection would be invisible on
	// others. Pin the pool to a single connection so the whole test or
	// process sees the same schema and data.
	if dsn == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return &SqliteDatabase{
		db:     db,
		tables: make(map[string]*modelMeta),
	}, nil
}

// AddTable registers a model type and creates the table if needed.
// If the table exists, it also adds any missing columns (schema migration).
func (s *SqliteDatabase) AddTable(model any) error {
	meta, err := getModelMeta(model)
	if err != nil {
		return err
	}

	// Create table if it doesn't exist
	createSQL := meta.createTableSQL()
	if _, err := s.db.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create table %s: %w", meta.TableName, err)
	}

	// Check for missing columns and add them (schema migration)
	if err := s.ensureColumns(meta); err != nil {
		return fmt.Errorf("failed to migrate table %s: %w", meta.TableName, err)
	}

	s.tables[meta.Type.Name()] = meta
	return nil
}

// ensureColumns adds any missing columns to an existing table.
func (s *SqliteDatabase) ensureColumns(meta *modelMeta) error {
	// Get existing columns from SQLite
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", meta.TableName))
	if err != nil {
		return err
	}
	defer rows.Close()

	existingCols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return err
		}
		existingCols[name] = true
	}

	// Add missing columns
	for _, field := range meta.Fields {
		if !existingCols[field.ColumnName] {
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
				meta.TableName, field.ColumnName, field.SQLType)
			if _, err := s.db.Exec(alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s: %w", field.ColumnName, err)
			}
		}
	}

	return nil
}

// Table returns a TableDAO for the given model type.
func (s *SqliteDatabase) Table(model any) TableDAO {
	meta := tableMeta(s.tables, model)
	if meta == nil {
		return nil
	}

	return newSqliteTableDAO(s.db, meta)
}

// ForUser returns a user-scoped database view.
func (s *SqliteDatabase) ForUser(userID string) UserDatabase {
	return &SqliteUserDatabase{
		parent: s,
		userID: userID,
	}
}

// RunInTransaction executes a function within a transaction.
func (s *SqliteDatabase) RunInTransaction(ctx context.Context, fn func(tx Database) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	txDB := &SqliteTxDatabase{
		tx:     tx,
		tables: s.tables,
	}

	if err := fn(txDB); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// Ping verifies the connection is still usable.
func (s *SqliteDatabase) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the database connection.
func (s *SqliteDatabase) Close() error {
	return s.db.Close()
}

// SqliteUserDatabase provides user-scoped table access.
type SqliteUserDatabase struct {
	parent *SqliteDatabase
	userID string
}

// Table returns a user-scoped TableDAO.
func (u *SqliteUserDatabase) Table(model any) TableDAO {
	meta := tableMeta(u.parent.tables, model)
	if meta == nil {
		return nil
	}

	return newSqliteUserTableDAO(u.parent.db, meta, u.userID)
}

// SqliteTableDAO implements TableDAO for SQLite.
type SqliteTableDAO struct {
	*sqlTableDAO
}

// SqliteUserTableDAO adds user_id filtering to queries.
type SqliteUserTableDAO struct {
	*sqlUserTableDAO
}

// SqliteTxDatabase wraps a transaction.
type SqliteTxDatabase struct {
	tx     *sql.Tx
	tables map[string]*modelMeta
}

func (t *SqliteTxDatabase) AddTable(model any) error {
	return fmt.Errorf("cannot add tables within a transaction")
}

func (t *SqliteTxDatabase) Table(model any) TableDAO {
	meta := tableMeta(t.tables, model)
	if meta == nil {
		return nil
	}

	return newSqliteTxTableDAO(t.tx, meta)
}

func (t *SqliteTxDatabase) ForUser(userID string) UserDatabase {
	return &SqliteTxUserDatabase{parent: t, userID: userID}
}

func (t *SqliteTxDatabase) RunInTransaction(ctx context.Context, fn func(tx Database) error) error {
	return fn(t) // Already in a transaction
}

// Ping reports the enclosing connection as usable: an open transaction
// implies a live connection.
func (t *SqliteTxDatabase) Ping(ctx context.Context) error {
	return nil
}

func (t *SqliteTxDatabase) Close() error {
	return nil // Transaction handles this
}

// SqliteTxUserDatabase provides user-scoped access within a transaction.
type SqliteTxUserDatabase struct {
	parent *SqliteTxDatabase
	userID string
}

func (u *SqliteTxUserDatabase) Table(model any) TableDAO {
	meta := tableMeta(u.parent.tables, model)
	if meta == nil {
		return nil
	}

	return newSqliteTxUserTableDAO(u.parent.tx, meta, u.userID)
}

// SqliteTxTableDAO implements TableDAO for transactions.
type SqliteTxTableDAO struct {
	*sqlTableDAO
}

// SqliteTxUserTableDAO implements user-scoped TableDAO for transactions.
type SqliteTxUserTableDAO struct {
	*sqlUserTableDAO
}

func newSqliteTableDAO(exec sqlExecutor, meta *modelMeta) *SqliteTableDAO {
	return &SqliteTableDAO{sqlTableDAO: newSQLTableDAO(exec, meta, sqliteDialect{})}
}

func newSqliteUserTableDAO(exec sqlExecutor, meta *modelMeta, userID string) *SqliteUserTableDAO {
	return &SqliteUserTableDAO{sqlUserTableDAO: newSQLUserTableDAO(exec, meta, sqliteDialect{}, userID)}
}

func newSqliteTxTableDAO(tx *sql.Tx, meta *modelMeta) *SqliteTxTableDAO {
	return &SqliteTxTableDAO{sqlTableDAO: newSQLTableDAO(tx, meta, sqliteDialect{})}
}

func newSqliteTxUserTableDAO(tx *sql.Tx, meta *modelMeta, userID string) *SqliteTxUserTableDAO {
	return &SqliteTxUserTableDAO{sqlUserTableDAO: newSQLUserTableDAO(tx, meta, sqliteDialect{}, userID)}
}

type sqliteDialect struct{}

func (sqliteDialect) Placeholder(idx int) string {
	return "?"
}

func (sqliteDialect) PrepareValue(v reflect.Value, field fieldMeta) any {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return sqliteDialect{}.PrepareValue(v.Elem(), field)
	case reflect.Slice, reflect.Map:
		if v.IsNil() {
			return nil
		}
		data, _ := json.Marshal(v.Interface())
		return string(data)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return v.Interface().(time.Time).Format(time.RFC3339)
		}
		data, _ := json.Marshal(v.Interface())
		return string(data)
	case reflect.Bool:
		if v.Bool() {
			return 1
		}
		return 0
	default:
		return v.Interface()
	}
}

func (sqliteDialect) MakeScanDest(field fieldMeta) any {
	switch field.Type.Kind() {
	case reflect.String:
		return new(sql.NullString)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return new(sql.NullInt64)
	case reflect.Float32, reflect.Float64:
		return new(sql.NullFloat64)
	case reflect.Bool:
		return new(sql.NullBool)
	default:
		return new(sql.NullString)
	}
}

func (d sqliteDialect) ApplyScannedValue(fv reflect.Value, field fieldMeta, scanned any) error {
	switch dest := scanned.(type) {
	case *sql.NullString:
		if dest.Valid {
			return d.setFieldValue(fv, field, dest.String)
		}
	case *sql.NullInt64:
		if dest.Valid {
			fv.SetInt(dest.Int64)
		}
	case *sql.NullFloat64:
		if dest.Valid {
			fv.SetFloat(dest.Float64)
		}
	case *sql.NullBool:
		if dest.Valid {
			fv.SetBool(dest.Bool)
		}
	}
	return nil
}

func (sqliteDialect) setFieldValue(fv reflect.Value, field fieldMeta, strVal string) error {
	switch field.Type.Kind() {
	case reflect.String:
		fv.SetString(strVal)
	case reflect.Slice, reflect.Map:
		if strVal != "" {
			newVal := reflect.New(field.Type)
			if err := json.Unmarshal([]byte(strVal), newVal.Interface()); err != nil {
				return err
			}
			fv.Set(newVal.Elem())
		}
	case reflect.Struct:
		if field.Type == reflect.TypeOf(time.Time{}) {
			t, err := time.Parse(time.RFC3339, strVal)
			if err != nil {
				return err
			}
			fv.Set(reflect.ValueOf(t))
		} else if strVal != "" {
			newVal := reflect.New(field.Type)
			if err := json.Unmarshal([]byte(strVal), newVal.Interface()); err != nil {
				return err
			}
			fv.Set(newVal.Elem())
		}
	case reflect.Ptr:
		// Handle pointer types (e.g., *time.Time, *float64, *bool)
		if strVal == "" {
			// Leave nil for empty values
			return nil
		}
		elemType := field.Type.Elem()
		switch elemType.Kind() {
		case reflect.Struct:
			if elemType == reflect.TypeOf(time.Time{}) {
				t, err := time.Parse(time.RFC3339, strVal)
				if err != nil {
					return err
				}
				fv.Set(reflect.ValueOf(&t))
			} else {
				newVal := reflect.New(elemType)
				if err := json.Unmarshal([]byte(strVal), newVal.Interface()); err != nil {
					return err
				}
				fv.Set(newVal)
			}
		case reflect.Float64:
			var f float64
			if err := json.Unmarshal([]byte(strVal), &f); err != nil {
				return err
			}
			fv.Set(reflect.ValueOf(&f))
		case reflect.Int64, reflect.Int:
			var i int64
			if err := json.Unmarshal([]byte(strVal), &i); err != nil {
				return err
			}
			fv.Set(reflect.ValueOf(&i))
		case reflect.Bool:
			// SQLite stores bools as 0/1, handle both "0"/"1" and "true"/"false"
			var b bool
			if strVal == "1" || strVal == "true" {
				b = true
			} else if strVal == "0" || strVal == "false" {
				b = false
			} else {
				return fmt.Errorf("invalid bool value: %s", strVal)
			}
			fv.Set(reflect.ValueOf(&b))
		case reflect.String:
			fv.Set(reflect.ValueOf(&strVal))
		default:
			// Fallback: try JSON unmarshal
			newVal := reflect.New(elemType)
			if err := json.Unmarshal([]byte(strVal), newVal.Interface()); err != nil {
				return err
			}
			fv.Set(newVal)
		}
	default:
		fv.SetString(strVal)
	}
	return nil
}
