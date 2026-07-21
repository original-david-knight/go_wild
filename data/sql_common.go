package gowild_data

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type sqlDialect interface {
	Placeholder(idx int) string
	PrepareValue(v reflect.Value, field fieldMeta) any
	MakeScanDest(field fieldMeta) any
	ApplyScannedValue(fv reflect.Value, field fieldMeta, scanned any) error
}

type sqlTableDAO struct {
	exec    sqlExecutor
	meta    *modelMeta
	dialect sqlDialect
}

func newSQLTableDAO(exec sqlExecutor, meta *modelMeta, dialect sqlDialect) *sqlTableDAO {
	return &sqlTableDAO{exec: exec, meta: meta, dialect: dialect}
}

func (d *sqlTableDAO) Insert(ctx context.Context, model any) error {
	columns, values := d.prepareInsert(model)
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = d.dialect.Placeholder(i + 1)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		d.meta.TableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err := d.exec.ExecContext(ctx, query, values...)
	return err
}

func (d *sqlTableDAO) Update(ctx context.Context, model any) error {
	columns, values := d.prepareInsert(model)

	var sets []string
	var updateValues []any
	var idValue any
	paramIdx := 1

	for i, col := range columns {
		if d.meta.Fields[i].IsID {
			idValue = values[i]
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = %s", col, d.dialect.Placeholder(paramIdx)))
		updateValues = append(updateValues, values[i])
		paramIdx++
	}
	updateValues = append(updateValues, idValue)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = %s",
		d.meta.TableName,
		strings.Join(sets, ", "),
		d.getIDColumn(),
		d.dialect.Placeholder(paramIdx),
	)

	_, err := d.exec.ExecContext(ctx, query, updateValues...)
	return err
}

func (d *sqlTableDAO) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = %s", d.meta.TableName, d.getIDColumn(), d.dialect.Placeholder(1))
	_, err := d.exec.ExecContext(ctx, query, id)
	return err
}

func (d *sqlTableDAO) Get(ctx context.Context, id string, dest any) error {
	columns := d.getColumns()
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = %s",
		strings.Join(columns, ", "),
		d.meta.TableName,
		d.getIDColumn(),
		d.dialect.Placeholder(1),
	)

	row := d.exec.QueryRowContext(ctx, query, id)
	return d.scanRow(row, dest)
}

func (d *sqlTableDAO) GetAll(ctx context.Context) ([]any, error) {
	return d.Query(ctx, QueryOpts{})
}

func (d *sqlTableDAO) Query(ctx context.Context, opts QueryOpts) ([]any, error) {
	columns := d.getColumns()
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), d.meta.TableName)

	var args []any
	var conditions []string
	paramIdx := 1
	for col, val := range opts.Where {
		conditions = append(conditions, fmt.Sprintf("%s = %s", col, d.dialect.Placeholder(paramIdx)))
		args = append(args, val)
		paramIdx++
	}
	for col, vals := range opts.WhereIn {
		if len(vals) == 0 {
			continue
		}
		placeholders := make([]string, len(vals))
		for i, v := range vals {
			placeholders[i] = d.dialect.Placeholder(paramIdx)
			args = append(args, v)
			paramIdx++
		}
		conditions = append(conditions, fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ", ")))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.OrderBy != "" {
		query += " ORDER BY " + opts.OrderBy
		if opts.OrderDesc {
			query += " DESC"
		}
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", opts.Offset)
	}

	rows, err := d.exec.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []any
	for rows.Next() {
		dest := reflect.New(d.meta.Type).Interface()
		if err := d.scanRows(rows, dest); err != nil {
			return nil, err
		}
		results = append(results, dest)
	}

	return results, rows.Err()
}

func (d *sqlTableDAO) prepareInsert(model any) ([]string, []any) {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	var columns []string
	var values []any

	for _, field := range d.meta.Fields {
		columns = append(columns, field.ColumnName)
		fv := v.FieldByName(field.Name)
		values = append(values, d.dialect.PrepareValue(fv, field))
	}

	return columns, values
}

func (d *sqlTableDAO) getColumns() []string {
	columns := make([]string, len(d.meta.Fields))
	for i, f := range d.meta.Fields {
		columns[i] = f.ColumnName
	}
	return columns
}

func (d *sqlTableDAO) getIDColumn() string {
	for _, f := range d.meta.Fields {
		if f.IsID {
			return f.ColumnName
		}
	}
	return "id"
}

func (d *sqlTableDAO) scanRow(row *sql.Row, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}
	v = v.Elem()

	scanDest := make([]any, len(d.meta.Fields))
	for i, field := range d.meta.Fields {
		scanDest[i] = d.dialect.MakeScanDest(field)
	}

	if err := row.Scan(scanDest...); err != nil {
		return err
	}

	return d.applyScanned(v, scanDest)
}

func (d *sqlTableDAO) scanRows(rows *sql.Rows, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}
	v = v.Elem()

	scanDest := make([]any, len(d.meta.Fields))
	for i, field := range d.meta.Fields {
		scanDest[i] = d.dialect.MakeScanDest(field)
	}

	if err := rows.Scan(scanDest...); err != nil {
		return err
	}

	return d.applyScanned(v, scanDest)
}

func (d *sqlTableDAO) applyScanned(v reflect.Value, scanDest []any) error {
	for i, field := range d.meta.Fields {
		fv := v.FieldByName(field.Name)
		if !fv.CanSet() {
			continue
		}

		if err := d.dialect.ApplyScannedValue(fv, field, scanDest[i]); err != nil {
			return err
		}
	}
	return nil
}

type sqlUserTableDAO struct {
	base   *sqlTableDAO
	userID string
}

func newSQLUserTableDAO(exec sqlExecutor, meta *modelMeta, dialect sqlDialect, userID string) *sqlUserTableDAO {
	return &sqlUserTableDAO{
		base:   newSQLTableDAO(exec, meta, dialect),
		userID: userID,
	}
}

func (d *sqlUserTableDAO) Insert(ctx context.Context, model any) error {
	setUserID(model, d.userID)
	return d.base.Insert(ctx, model)
}

func (d *sqlUserTableDAO) Update(ctx context.Context, model any) error {
	return d.base.Update(ctx, model)
}

func (d *sqlUserTableDAO) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = %s AND user_id = %s",
		d.base.meta.TableName, d.base.getIDColumn(), d.base.dialect.Placeholder(1), d.base.dialect.Placeholder(2))
	_, err := d.base.exec.ExecContext(ctx, query, id, d.userID)
	return err
}

func (d *sqlUserTableDAO) Get(ctx context.Context, id string, dest any) error {
	columns := d.base.getColumns()
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = %s AND user_id = %s",
		strings.Join(columns, ", "),
		d.base.meta.TableName,
		d.base.getIDColumn(),
		d.base.dialect.Placeholder(1),
		d.base.dialect.Placeholder(2),
	)

	row := d.base.exec.QueryRowContext(ctx, query, id, d.userID)
	return d.base.scanRow(row, dest)
}

func (d *sqlUserTableDAO) GetAll(ctx context.Context) ([]any, error) {
	return d.Query(ctx, QueryOpts{})
}

func (d *sqlUserTableDAO) Query(ctx context.Context, opts QueryOpts) ([]any, error) {
	if opts.Where == nil {
		opts.Where = make(map[string]any)
	}
	opts.Where["user_id"] = d.userID

	return d.base.Query(ctx, opts)
}

func setUserID(model any, userID string) {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if userField := v.FieldByName("UserID"); userField.IsValid() && userField.CanSet() {
		userField.SetString(userID)
	}
}

func tableMeta(tables map[string]*modelMeta, model any) *modelMeta {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return tables[t.Name()]
}
