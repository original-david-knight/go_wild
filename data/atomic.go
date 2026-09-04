package gowild_data

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

func (d *sqlTableDAO) InsertIfAbsent(ctx context.Context, model any) (bool, error) {
	columns, values := d.prepareInsert(model)
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = d.dialect.Placeholder(i + 1)
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO NOTHING",
		d.meta.TableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "), d.getIDColumn())
	return wroteRow(d.exec.ExecContext(ctx, query, values...))
}

func (d *sqlTableDAO) UpdateIf(ctx context.Context, model any, expected map[string]any) (bool, error) {
	columns, values := d.prepareInsert(model)
	var sets []string
	var args []any
	var id any
	for i, col := range columns {
		if d.meta.Fields[i].IsID {
			id = values[i]
			continue
		}
		args = append(args, values[i])
		sets = append(sets, fmt.Sprintf("%s = %s", col, d.dialect.Placeholder(len(args))))
	}
	// A key-only model can still take a row lock and test a precondition.
	if len(sets) == 0 {
		sets = append(sets, d.getIDColumn()+" = "+d.getIDColumn())
	}
	args = append(args, id)
	where := d.getIDColumn() + " = " + d.dialect.Placeholder(len(args))
	conditions, args, err := d.expectedConditions(expected, args)
	if err != nil {
		return false, err
	}
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s%s", d.meta.TableName, strings.Join(sets, ", "), where, conditions)
	return wroteRow(d.exec.ExecContext(ctx, query, args...))
}

func (d *sqlTableDAO) DeleteIf(ctx context.Context, id string, expected map[string]any) (bool, error) {
	conditions, args, err := d.expectedConditions(expected, []any{id})
	if err != nil {
		return false, err
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE %s = %s%s", d.meta.TableName, d.getIDColumn(), d.dialect.Placeholder(1), conditions)
	return wroteRow(d.exec.ExecContext(ctx, query, args...))
}

// expectedConditions validates names against the model and encodes values
// with the write codec. In particular, SQLite stores timestamps as text and
// booleans as integers, so raw driver arguments would not compare correctly.
func (d *sqlTableDAO) expectedConditions(expected map[string]any, args []any) (string, []any, error) {
	var conditions strings.Builder
	for _, col := range slices.Sorted(maps.Keys(expected)) {
		var field *fieldMeta
		for i := range d.meta.Fields {
			if d.meta.Fields[i].ColumnName == col {
				field = &d.meta.Fields[i]
				break
			}
		}
		if field == nil {
			return "", nil, fmt.Errorf("unknown expected column %q on %s", col, d.meta.TableName)
		}
		raw := expected[col]
		exact, strict := raw.(exactMatch)
		if strict {
			raw = exact.value
		}
		value := reflect.ValueOf(raw)
		encoded := d.dialect.PrepareValue(value, *field)
		if encoded == nil {
			conditions.WriteString(" AND " + col + " IS NULL")
			continue
		}
		args = append(args, encoded)
		comparison := col + " = " + d.dialect.Placeholder(len(args))
		if !strict && value.IsZero() && field.Type.Kind() != reflect.Ptr {
			comparison = "(" + comparison + " OR " + col + " IS NULL)"
		}
		conditions.WriteString(" AND " + comparison)
	}
	return conditions.String(), args, nil
}

func wroteRow(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (d *sqlUserTableDAO) InsertIfAbsent(ctx context.Context, model any) (bool, error) {
	scoped, err := d.scopedModel(model)
	if err != nil {
		return false, err
	}
	return d.base.InsertIfAbsent(ctx, scoped)
}

func (d *sqlUserTableDAO) UpdateIf(ctx context.Context, model any, expected map[string]any) (bool, error) {
	scoped, err := d.scopedModel(model)
	if err != nil {
		return false, err
	}
	return d.base.UpdateIf(ctx, scoped, d.scopedExpected(expected))
}

func (d *sqlUserTableDAO) DeleteIf(ctx context.Context, id string, expected map[string]any) (bool, error) {
	return d.base.DeleteIf(ctx, id, d.scopedExpected(expected))
}

func (d *sqlUserTableDAO) scopedExpected(expected map[string]any) map[string]any {
	scoped := make(map[string]any, len(expected)+1)
	maps.Copy(scoped, expected)
	scoped["user_id"] = exactMatch{d.userID}
	return scoped
}

// Tenant scope matches the corresponding reads exactly: an empty user scope
// does not own rows whose user_id is SQL NULL. Legacy-zero tolerance applies
// to the caller's optimistic conditions, never the injected access boundary.
type exactMatch struct{ value any }

// Both pointer and value models are accepted by the base DAO. Copy before
// enforcing ownership so a non-settable value cannot preserve a foreign
// user_id, and neither form changes the caller's model as a side effect.
func (d *sqlUserTableDAO) scopedModel(model any) (any, error) {
	v := reflect.ValueOf(model)
	if v.IsValid() && v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("user-scoped atomic write requires a struct or non-nil struct pointer")
	}
	copy := reflect.New(v.Type())
	copy.Elem().Set(v)
	user := copy.Elem().FieldByName("UserID")
	if !user.IsValid() || user.Kind() != reflect.String || !user.CanSet() {
		return nil, fmt.Errorf("user-scoped atomic write requires a UserID string field")
	}
	user.SetString(d.userID)
	return copy.Interface(), nil
}
