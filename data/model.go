package gowild_data

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// tableNamer can be implemented to customize the table name.
// If not implemented, the table name is derived from the struct name.
type tableNamer interface {
	TableName() string
}

// modelMeta contains metadata about a model extracted via reflection.
type modelMeta struct {
	Type      reflect.Type
	TableName string
	Fields    []fieldMeta
	IDField   string
}

// fieldMeta contains metadata about a single field.
type fieldMeta struct {
	Name       string       // Go field name
	ColumnName string       // Database column name (from json tag or snake_case)
	Type       reflect.Type // Go type
	SQLType    string       // SQL type for this field
	IsID       bool         // Is this the ID field?
	OmitEmpty  bool         // Has omitempty tag?
}

// getModelMeta extracts metadata from a model type using reflection.
func getModelMeta(model any) (*modelMeta, error) {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model must be a struct, got %s", t.Kind())
	}

	meta := &modelMeta{
		Type:   t,
		Fields: make([]fieldMeta, 0, t.NumField()),
	}

	// Determine table name
	if namer, ok := model.(tableNamer); ok {
		meta.TableName = namer.TableName()
	} else {
		meta.TableName = toSnakeCase(t.Name()) + "s" // Pluralize
	}

	// Extract field metadata
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fm := fieldMeta{
			Name: field.Name,
			Type: field.Type,
		}

		// Get column name from json tag or convert to snake_case
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			fm.ColumnName = parts[0]
			fm.OmitEmpty = len(parts) > 1 && parts[1] == "omitempty"
		} else {
			fm.ColumnName = toSnakeCase(field.Name)
		}

		// Check if this is the ID field
		if strings.ToLower(field.Name) == "id" || field.Tag.Get("db") == "id" {
			fm.IsID = true
			meta.IDField = field.Name
		}

		// Determine SQL type
		fm.SQLType = goTypeToSQLType(field.Type)

		meta.Fields = append(meta.Fields, fm)
	}

	// Default to "ID" if no id field found
	if meta.IDField == "" {
		for i, f := range meta.Fields {
			if f.Name == "ID" {
				meta.Fields[i].IsID = true
				meta.IDField = "ID"
				break
			}
		}
	}

	return meta, nil
}

// goTypeToSQLType maps Go types to SQLite types.
func goTypeToSQLType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "TEXT"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "INTEGER"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "INTEGER"
	case reflect.Float32, reflect.Float64:
		return "REAL"
	case reflect.Bool:
		return "INTEGER" // SQLite stores booleans as 0/1
	case reflect.Slice, reflect.Map:
		return "TEXT" // JSON serialized
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return "TEXT" // ISO8601 format
		}
		return "TEXT" // JSON serialized
	case reflect.Ptr:
		return goTypeToSQLType(t.Elem())
	default:
		return "TEXT"
	}
}

// toSnakeCase converts PascalCase/camelCase to snake_case.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// createTableSQL generates a CREATE TABLE statement for the model (SQLite).
func (m *modelMeta) createTableSQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(m.TableName)
	sb.WriteString(" (\n")

	for i, field := range m.Fields {
		sb.WriteString("    ")
		sb.WriteString(field.ColumnName)
		sb.WriteString(" ")
		sb.WriteString(field.SQLType)

		if field.IsID {
			sb.WriteString(" PRIMARY KEY")
		}

		if i < len(m.Fields)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(")")
	return sb.String()
}

// goTypeToPostgresType maps Go types to PostgreSQL types.
func goTypeToPostgresType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "TEXT"
	case reflect.Int, reflect.Int32:
		return "INTEGER"
	case reflect.Int8, reflect.Int16:
		return "SMALLINT"
	case reflect.Int64:
		return "BIGINT"
	case reflect.Uint, reflect.Uint32:
		return "INTEGER"
	case reflect.Uint8, reflect.Uint16:
		return "SMALLINT"
	case reflect.Uint64:
		return "BIGINT"
	case reflect.Float32:
		return "REAL"
	case reflect.Float64:
		return "DOUBLE PRECISION"
	case reflect.Bool:
		return "BOOLEAN"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "BYTEA" // []byte
		}
		return "JSONB" // JSON arrays
	case reflect.Map:
		return "JSONB"
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return "TIMESTAMPTZ"
		}
		return "JSONB" // JSON objects
	case reflect.Ptr:
		return goTypeToPostgresType(t.Elem())
	default:
		return "TEXT"
	}
}

// createTableSQLPostgres generates a CREATE TABLE statement for PostgreSQL.
func (m *modelMeta) createTableSQLPostgres() string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(m.TableName)
	sb.WriteString(" (\n")

	for i, field := range m.Fields {
		sb.WriteString("    ")
		sb.WriteString(field.ColumnName)
		sb.WriteString(" ")
		sb.WriteString(goTypeToPostgresType(field.Type))

		if field.IsID {
			sb.WriteString(" PRIMARY KEY")
		}

		if i < len(m.Fields)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(")")
	return sb.String()
}
