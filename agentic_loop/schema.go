package gowild_agentic_loop

import (
	"reflect"
	"strings"

	"google.golang.org/genai"
)

// schemaFromStruct generates a Gemini Schema from a Go struct type.
// It uses struct tags to customize the schema:
//   - `json:"name"` - field name in JSON
//   - `description:"..."` - field description
//   - `enum:"a,b,c"` - allowed values
//   - `required:"true"` - mark field as required (default: required unless omitempty)
func schemaFromStruct(v any) *genai.Schema {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return schemaFromType(t)
}

func schemaFromType(t reflect.Type) *genai.Schema {
	switch t.Kind() {
	case reflect.Struct:
		return schemaFromStructType(t)
	case reflect.String:
		return &genai.Schema{Type: genai.TypeString}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &genai.Schema{Type: genai.TypeInteger}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &genai.Schema{Type: genai.TypeInteger}
	case reflect.Float32, reflect.Float64:
		return &genai.Schema{Type: genai.TypeNumber}
	case reflect.Bool:
		return &genai.Schema{Type: genai.TypeBoolean}
	case reflect.Slice, reflect.Array:
		return &genai.Schema{
			Type:  genai.TypeArray,
			Items: schemaFromType(t.Elem()),
		}
	case reflect.Map:
		return &genai.Schema{Type: genai.TypeObject}
	case reflect.Ptr:
		return schemaFromType(t.Elem())
	default:
		return &genai.Schema{Type: genai.TypeString}
	}
}

func schemaFromStructType(t reflect.Type) *genai.Schema {
	properties := make(map[string]*genai.Schema)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get JSON field name
		jsonTag := field.Tag.Get("json")
		fieldName := field.Name
		omitempty := false

		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" && parts[0] != "-" {
				fieldName = parts[0]
			}
			if parts[0] == "-" {
				continue // Skip this field
			}
			for _, part := range parts[1:] {
				if part == "omitempty" {
					omitempty = true
				}
			}
		}

		// Generate schema for field
		fieldSchema := schemaFromType(field.Type)

		// Add description from tag
		if desc := field.Tag.Get("description"); desc != "" {
			fieldSchema.Description = desc
		}

		// Handle enum tag
		if enumTag := field.Tag.Get("enum"); enumTag != "" {
			fieldSchema.Enum = strings.Split(enumTag, ",")
		}

		// Handle required tag or infer from omitempty
		isRequired := !omitempty
		if reqTag := field.Tag.Get("required"); reqTag != "" {
			isRequired = reqTag == "true"
		}

		// Optional fields (pointers without required tag)
		if field.Type.Kind() == reflect.Ptr && field.Tag.Get("required") == "" {
			isRequired = false
		}

		if isRequired {
			required = append(required, fieldName)
		}

		properties[fieldName] = fieldSchema
	}

	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: properties,
		Required:   required,
	}
}

// mapToStruct populates a struct from a map using reflection.
// This is used to convert tool input from map[string]any to a typed struct.
func mapToStruct(m map[string]any, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr {
		panic("mapToStruct requires a pointer to struct")
	}
	val = val.Elem()
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		if !fieldVal.CanSet() {
			continue
		}

		// Get JSON field name
		jsonTag := field.Tag.Get("json")
		fieldName := field.Name
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" && parts[0] != "-" {
				fieldName = parts[0]
			}
		}

		// Get value from map
		mapVal, ok := m[fieldName]
		if !ok {
			// Try lowercase version
			mapVal, ok = m[strings.ToLower(fieldName)]
			if !ok {
				continue
			}
		}

		if mapVal == nil {
			continue
		}

		setFieldValue(fieldVal, mapVal)
	}

	return nil
}

func setFieldValue(fieldVal reflect.Value, mapVal any) {
	mapValReflect := reflect.ValueOf(mapVal)

	// Handle pointer fields
	if fieldVal.Kind() == reflect.Ptr {
		if mapVal == nil {
			return
		}
		if fieldVal.IsNil() {
			fieldVal.Set(reflect.New(fieldVal.Type().Elem()))
		}
		setFieldValue(fieldVal.Elem(), mapVal)
		return
	}

	// Handle type conversions
	switch fieldVal.Kind() {
	case reflect.String:
		if s, ok := mapVal.(string); ok {
			fieldVal.SetString(s)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := mapVal.(type) {
		case int:
			fieldVal.SetInt(int64(v))
		case int64:
			fieldVal.SetInt(v)
		case float64:
			fieldVal.SetInt(int64(v))
		}
	case reflect.Float32, reflect.Float64:
		switch v := mapVal.(type) {
		case float64:
			fieldVal.SetFloat(v)
		case int:
			fieldVal.SetFloat(float64(v))
		}
	case reflect.Bool:
		if b, ok := mapVal.(bool); ok {
			fieldVal.SetBool(b)
		}
	case reflect.Slice:
		if mapValReflect.Kind() == reflect.Slice {
			newSlice := reflect.MakeSlice(fieldVal.Type(), mapValReflect.Len(), mapValReflect.Len())
			for i := 0; i < mapValReflect.Len(); i++ {
				setFieldValue(newSlice.Index(i), mapValReflect.Index(i).Interface())
			}
			fieldVal.Set(newSlice)
		}
	case reflect.Struct:
		if m, ok := mapVal.(map[string]any); ok {
			ptr := reflect.New(fieldVal.Type())
			mapToStruct(m, ptr.Interface())
			fieldVal.Set(ptr.Elem())
		}
	case reflect.Map:
		// Handle map types (e.g., map[string]string from map[string]any)
		if mapValReflect.Kind() == reflect.Map {
			newMap := reflect.MakeMap(fieldVal.Type())
			keyType := fieldVal.Type().Key()
			valType := fieldVal.Type().Elem()
			iter := mapValReflect.MapRange()
			for iter.Next() {
				k := iter.Key()
				v := iter.Value()
				// Convert key if needed
				var newKey reflect.Value
				if k.Type().AssignableTo(keyType) {
					newKey = k
				} else if k.Kind() == reflect.Interface {
					newKey = reflect.ValueOf(k.Interface()).Convert(keyType)
				} else {
					continue
				}
				// Convert value if needed
				var newVal reflect.Value
				if v.Type().AssignableTo(valType) {
					newVal = v
				} else if v.Kind() == reflect.Interface {
					vi := v.Interface()
					viVal := reflect.ValueOf(vi)
					if viVal.Type().ConvertibleTo(valType) {
						newVal = viVal.Convert(valType)
					} else {
						continue
					}
				} else if v.Type().ConvertibleTo(valType) {
					newVal = v.Convert(valType)
				} else {
					continue
				}
				newMap.SetMapIndex(newKey, newVal)
			}
			fieldVal.Set(newMap)
		}
	default:
		if mapValReflect.Type().AssignableTo(fieldVal.Type()) {
			fieldVal.Set(mapValReflect)
		}
	}
}
