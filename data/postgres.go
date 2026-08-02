package gowild_data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresDatabase implements Database for PostgreSQL.
type PostgresDatabase struct {
	db     *sql.DB
	tables map[string]*modelMeta
}

// NewPostgresDatabase creates a new PostgreSQL database connection.
// The connectionString should be in the format:
// "postgres://user:password@host:port/database?sslmode=disable"
func NewPostgresDatabase(connectionString string) (*PostgresDatabase, error) {
	db, err := sql.Open("pgx", connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresDatabase{
		db:     db,
		tables: make(map[string]*modelMeta),
	}, nil
}

// AddTable registers a model type and creates the table if needed.
// If the table exists, it also adds any missing columns (schema migration).
func (p *PostgresDatabase) AddTable(model any) error {
	meta, err := getModelMeta(model)
	if err != nil {
		return err
	}

	// Create table if it doesn't exist using PostgreSQL syntax
	createSQL := meta.createTableSQLPostgres()
	if _, err := p.db.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create table %s: %w", meta.TableName, err)
	}

	// Check for missing columns and add them (schema migration)
	if err := p.ensureColumns(meta); err != nil {
		return fmt.Errorf("failed to migrate table %s: %w", meta.TableName, err)
	}

	p.tables[meta.Type.Name()] = meta
	return nil
}

// ensureColumns adds any missing columns to an existing table.
func (p *PostgresDatabase) ensureColumns(meta *modelMeta) error {
	// Get existing columns from PostgreSQL information_schema
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_name = $1 AND table_schema = 'public'
	`
	rows, err := p.db.Query(query, meta.TableName)
	if err != nil {
		return err
	}
	defer rows.Close()

	existingCols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		existingCols[name] = true
	}

	// Add missing columns
	for _, field := range meta.Fields {
		if !existingCols[field.ColumnName] {
			pgType := goTypeToPostgresType(field.Type)
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
				meta.TableName, field.ColumnName, pgType)
			if _, err := p.db.Exec(alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s: %w", field.ColumnName, err)
			}
		}
	}

	return nil
}

// Table returns a TableDAO for the given model type.
func (p *PostgresDatabase) Table(model any) TableDAO {
	meta := tableMeta(p.tables, model)
	if meta == nil {
		return nil
	}

	return newPostgresTableDAO(p.db, meta)
}

// ForUser returns a user-scoped database view.
func (p *PostgresDatabase) ForUser(userID string) UserDatabase {
	return &PostgresUserDatabase{
		parent: p,
		userID: userID,
	}
}

// RunInTransaction executes a function within a transaction.
func (p *PostgresDatabase) RunInTransaction(ctx context.Context, fn func(tx Database) error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	txDB := &PostgresTxDatabase{
		tx:     tx,
		tables: p.tables,
	}

	if err := fn(txDB); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// Ping verifies the connection is still usable.
func (p *PostgresDatabase) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// Close closes the database connection.
func (p *PostgresDatabase) Close() error {
	return p.db.Close()
}

// PostgresUserDatabase provides user-scoped table access.
type PostgresUserDatabase struct {
	parent *PostgresDatabase
	userID string
}

// Table returns a user-scoped TableDAO.
func (u *PostgresUserDatabase) Table(model any) TableDAO {
	meta := tableMeta(u.parent.tables, model)
	if meta == nil {
		return nil
	}

	return newPostgresUserTableDAO(u.parent.db, meta, u.userID)
}

// PostgresTableDAO implements TableDAO for PostgreSQL.
type PostgresTableDAO struct {
	*sqlTableDAO
}

// PostgresUserTableDAO adds user_id filtering to queries.
type PostgresUserTableDAO struct {
	*sqlUserTableDAO
}

// PostgresTxDatabase wraps a transaction.
type PostgresTxDatabase struct {
	tx     *sql.Tx
	tables map[string]*modelMeta
}

func (t *PostgresTxDatabase) AddTable(model any) error {
	return fmt.Errorf("cannot add tables within a transaction")
}

func (t *PostgresTxDatabase) Table(model any) TableDAO {
	meta := tableMeta(t.tables, model)
	if meta == nil {
		return nil
	}

	return newPostgresTxTableDAO(t.tx, meta)
}

func (t *PostgresTxDatabase) ForUser(userID string) UserDatabase {
	return &PostgresTxUserDatabase{parent: t, userID: userID}
}

func (t *PostgresTxDatabase) RunInTransaction(ctx context.Context, fn func(tx Database) error) error {
	return fn(t) // Already in a transaction
}

// Ping reports the enclosing connection as usable: an open transaction
// implies a live connection.
func (t *PostgresTxDatabase) Ping(ctx context.Context) error {
	return nil
}

func (t *PostgresTxDatabase) Close() error {
	return nil // Transaction handles this
}

// PostgresTxUserDatabase provides user-scoped access within a transaction.
type PostgresTxUserDatabase struct {
	parent *PostgresTxDatabase
	userID string
}

func (u *PostgresTxUserDatabase) Table(model any) TableDAO {
	meta := tableMeta(u.parent.tables, model)
	if meta == nil {
		return nil
	}

	return newPostgresTxUserTableDAO(u.parent.tx, meta, u.userID)
}

// PostgresTxTableDAO implements TableDAO for transactions.
type PostgresTxTableDAO struct {
	*sqlTableDAO
}

// PostgresTxUserTableDAO implements user-scoped TableDAO for transactions.
type PostgresTxUserTableDAO struct {
	*sqlUserTableDAO
}

func newPostgresTableDAO(exec sqlExecutor, meta *modelMeta) *PostgresTableDAO {
	return &PostgresTableDAO{sqlTableDAO: newSQLTableDAO(exec, meta, postgresDialect{})}
}

func newPostgresUserTableDAO(exec sqlExecutor, meta *modelMeta, userID string) *PostgresUserTableDAO {
	return &PostgresUserTableDAO{sqlUserTableDAO: newSQLUserTableDAO(exec, meta, postgresDialect{}, userID)}
}

func newPostgresTxTableDAO(tx *sql.Tx, meta *modelMeta) *PostgresTxTableDAO {
	return &PostgresTxTableDAO{sqlTableDAO: newSQLTableDAO(tx, meta, postgresDialect{})}
}

func newPostgresTxUserTableDAO(tx *sql.Tx, meta *modelMeta, userID string) *PostgresTxUserTableDAO {
	return &PostgresTxUserTableDAO{sqlUserTableDAO: newSQLUserTableDAO(tx, meta, postgresDialect{}, userID)}
}

type postgresDialect struct{}

func (postgresDialect) Placeholder(idx int) string {
	return fmt.Sprintf("$%d", idx)
}

func (postgresDialect) PrepareValue(v reflect.Value, field fieldMeta) any {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return postgresDialect{}.PrepareValue(v.Elem(), field)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			// []byte - return as-is for BYTEA
			return v.Interface()
		}
		if v.IsNil() {
			return nil
		}
		// JSON array
		data, _ := json.Marshal(v.Interface())
		return data
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		data, _ := json.Marshal(v.Interface())
		return data
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			// Return time.Time directly for TIMESTAMPTZ
			return v.Interface()
		}
		data, _ := json.Marshal(v.Interface())
		return data
	case reflect.Bool:
		// PostgreSQL supports native booleans
		return v.Bool()
	default:
		return v.Interface()
	}
}

func (postgresDialect) MakeScanDest(field fieldMeta) any {
	t := field.Type
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return new(sql.NullString)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return new(sql.NullInt64)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return new(sql.NullInt64)
	case reflect.Float32, reflect.Float64:
		return new(sql.NullFloat64)
	case reflect.Bool:
		return new(sql.NullBool)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			// []byte - BYTEA
			return new([]byte)
		}
		// JSONB array
		return new([]byte)
	case reflect.Map, reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return new(sql.NullTime)
		}
		// JSONB object
		return new([]byte)
	default:
		return new(sql.NullString)
	}
}

func (d postgresDialect) ApplyScannedValue(fv reflect.Value, field fieldMeta, scanned any) error {
	return d.setFieldFromScan(fv, field, scanned)
}

func (postgresDialect) setFieldFromScan(fv reflect.Value, field fieldMeta, scanned any) error {
	t := field.Type
	isPtr := t.Kind() == reflect.Ptr
	if isPtr {
		t = t.Elem()
	}

	switch dest := scanned.(type) {
	case *sql.NullString:
		if !dest.Valid {
			return nil
		}
		if isPtr {
			s := dest.String
			fv.Set(reflect.ValueOf(&s))
		} else {
			fv.SetString(dest.String)
		}

	case *sql.NullInt64:
		if !dest.Valid {
			return nil
		}
		if isPtr {
			i := dest.Int64
			fv.Set(reflect.ValueOf(&i))
		} else {
			fv.SetInt(dest.Int64)
		}

	case *sql.NullFloat64:
		if !dest.Valid {
			return nil
		}
		if isPtr {
			f := dest.Float64
			fv.Set(reflect.ValueOf(&f))
		} else {
			fv.SetFloat(dest.Float64)
		}

	case *sql.NullBool:
		if !dest.Valid {
			return nil
		}
		if isPtr {
			b := dest.Bool
			fv.Set(reflect.ValueOf(&b))
		} else {
			fv.SetBool(dest.Bool)
		}

	case *sql.NullTime:
		if !dest.Valid {
			return nil
		}
		if isPtr {
			tm := dest.Time
			fv.Set(reflect.ValueOf(&tm))
		} else {
			fv.Set(reflect.ValueOf(dest.Time))
		}

	case *[]byte:
		if dest == nil || *dest == nil {
			return nil
		}
		data := *dest

		// Handle []byte (BYTEA) directly
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			if isPtr {
				fv.Set(reflect.ValueOf(&data))
			} else {
				fv.SetBytes(data)
			}
			return nil
		}

		// Handle JSONB types
		if len(data) == 0 {
			return nil
		}

		switch t.Kind() {
		case reflect.Slice, reflect.Map:
			newVal := reflect.New(t)
			if err := json.Unmarshal(data, newVal.Interface()); err != nil {
				return err
			}
			if isPtr {
				fv.Set(newVal)
			} else {
				fv.Set(newVal.Elem())
			}
		case reflect.Struct:
			if t == reflect.TypeOf(time.Time{}) {
				// Shouldn't reach here - time uses NullTime
				var tm time.Time
				if err := json.Unmarshal(data, &tm); err != nil {
					return err
				}
				if isPtr {
					fv.Set(reflect.ValueOf(&tm))
				} else {
					fv.Set(reflect.ValueOf(tm))
				}
			} else {
				newVal := reflect.New(t)
				if err := json.Unmarshal(data, newVal.Interface()); err != nil {
					return err
				}
				if isPtr {
					fv.Set(newVal)
				} else {
					fv.Set(newVal.Elem())
				}
			}
		}
	}

	return nil
}
