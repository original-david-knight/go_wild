package gowild_agentic_loop

import (
	"testing"

	"google.golang.org/genai"
)

func TestSchemaFromStruct_BasicTypes(t *testing.T) {
	type BasicInput struct {
		Name   string  `json:"name"`
		Age    int     `json:"age"`
		Score  float64 `json:"score"`
		Active bool    `json:"active"`
	}

	schema := schemaFromStruct(BasicInput{})

	if schema.Type != genai.TypeObject {
		t.Errorf("expected TypeObject, got %v", schema.Type)
	}

	tests := []struct {
		field    string
		expected genai.Type
	}{
		{"name", genai.TypeString},
		{"age", genai.TypeInteger},
		{"score", genai.TypeNumber},
		{"active", genai.TypeBoolean},
	}

	for _, tt := range tests {
		prop, ok := schema.Properties[tt.field]
		if !ok {
			t.Errorf("missing property %s", tt.field)
			continue
		}
		if prop.Type != tt.expected {
			t.Errorf("property %s: expected %v, got %v", tt.field, tt.expected, prop.Type)
		}
	}
}

func TestSchemaFromStruct_Required(t *testing.T) {
	type RequiredInput struct {
		Required string `json:"required"`
		Optional string `json:"optional,omitempty"`
	}

	schema := schemaFromStruct(RequiredInput{})

	if len(schema.Required) != 1 {
		t.Errorf("expected 1 required field, got %d", len(schema.Required))
	}
	if schema.Required[0] != "required" {
		t.Errorf("expected 'required' in Required, got %v", schema.Required)
	}
}

func TestSchemaFromStruct_Description(t *testing.T) {
	type DescInput struct {
		Field string `json:"field" description:"A test field"`
	}

	schema := schemaFromStruct(DescInput{})

	if schema.Properties["field"].Description != "A test field" {
		t.Errorf("expected description 'A test field', got %s", schema.Properties["field"].Description)
	}
}

func TestSchemaFromStruct_Enum(t *testing.T) {
	type EnumInput struct {
		Status string `json:"status" enum:"pending,active,done"`
	}

	schema := schemaFromStruct(EnumInput{})

	prop := schema.Properties["status"]
	if len(prop.Enum) != 3 {
		t.Errorf("expected 3 enum values, got %d", len(prop.Enum))
	}
	expected := []string{"pending", "active", "done"}
	for i, v := range expected {
		if prop.Enum[i] != v {
			t.Errorf("enum[%d]: expected %s, got %s", i, v, prop.Enum[i])
		}
	}
}

func TestSchemaFromStruct_Pointer(t *testing.T) {
	type PointerInput struct {
		Optional *string `json:"optional"`
	}

	schema := schemaFromStruct(PointerInput{})

	// Pointer fields should not be required by default
	for _, r := range schema.Required {
		if r == "optional" {
			t.Error("pointer field should not be required by default")
		}
	}

	if schema.Properties["optional"].Type != genai.TypeString {
		t.Errorf("expected TypeString for pointer field, got %v", schema.Properties["optional"].Type)
	}
}

func TestSchemaFromStruct_Slice(t *testing.T) {
	type SliceInput struct {
		Tags []string `json:"tags"`
	}

	schema := schemaFromStruct(SliceInput{})

	prop := schema.Properties["tags"]
	if prop.Type != genai.TypeArray {
		t.Errorf("expected TypeArray, got %v", prop.Type)
	}
	if prop.Items.Type != genai.TypeString {
		t.Errorf("expected items TypeString, got %v", prop.Items.Type)
	}
}

func TestSchemaFromStruct_Nested(t *testing.T) {
	type Address struct {
		City    string `json:"city"`
		Country string `json:"country"`
	}
	type NestedInput struct {
		Name    string  `json:"name"`
		Address Address `json:"address"`
	}

	schema := schemaFromStruct(NestedInput{})

	addrProp := schema.Properties["address"]
	if addrProp.Type != genai.TypeObject {
		t.Errorf("expected TypeObject for nested struct, got %v", addrProp.Type)
	}
	if _, ok := addrProp.Properties["city"]; !ok {
		t.Error("missing nested property 'city'")
	}
}

func TestSchemaFromStruct_SkipUnexported(t *testing.T) {
	type UnexportedInput struct {
		Public  string `json:"public"`
		private string
	}

	schema := schemaFromStruct(UnexportedInput{})

	if len(schema.Properties) != 1 {
		t.Errorf("expected 1 property (only Public), got %d", len(schema.Properties))
	}
	if _, ok := schema.Properties["public"]; !ok {
		t.Error("missing public property")
	}
}

func TestSchemaFromStruct_SkipDash(t *testing.T) {
	type DashInput struct {
		Included string `json:"included"`
		Skipped  string `json:"-"`
	}

	schema := schemaFromStruct(DashInput{})

	if len(schema.Properties) != 1 {
		t.Errorf("expected 1 property (Skipped should be excluded), got %d", len(schema.Properties))
	}
}

func TestMapToStruct_Basic(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	m := map[string]any{
		"name":  "test",
		"count": float64(42), // JSON numbers come as float64
	}

	var result TestStruct
	err := mapToStruct(m, &result)
	if err != nil {
		t.Fatalf("mapToStruct failed: %v", err)
	}

	if result.Name != "test" {
		t.Errorf("expected name 'test', got %s", result.Name)
	}
	if result.Count != 42 {
		t.Errorf("expected count 42, got %d", result.Count)
	}
}

func TestMapToStruct_Nested(t *testing.T) {
	type Inner struct {
		Value string `json:"value"`
	}
	type Outer struct {
		Inner Inner `json:"inner"`
	}

	m := map[string]any{
		"inner": map[string]any{
			"value": "nested",
		},
	}

	var result Outer
	err := mapToStruct(m, &result)
	if err != nil {
		t.Fatalf("mapToStruct failed: %v", err)
	}

	if result.Inner.Value != "nested" {
		t.Errorf("expected inner value 'nested', got %s", result.Inner.Value)
	}
}

func TestMapToStruct_Slice(t *testing.T) {
	type SliceStruct struct {
		Items []string `json:"items"`
	}

	m := map[string]any{
		"items": []any{"a", "b", "c"},
	}

	var result SliceStruct
	err := mapToStruct(m, &result)
	if err != nil {
		t.Fatalf("mapToStruct failed: %v", err)
	}

	if len(result.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(result.Items))
	}
}

func TestMapToStruct_MissingFields(t *testing.T) {
	type TestStruct struct {
		Name    string `json:"name"`
		Missing string `json:"missing"`
	}

	m := map[string]any{
		"name": "test",
	}

	var result TestStruct
	err := mapToStruct(m, &result)
	if err != nil {
		t.Fatalf("mapToStruct failed: %v", err)
	}

	if result.Name != "test" {
		t.Errorf("expected name 'test', got %s", result.Name)
	}
	if result.Missing != "" {
		t.Errorf("expected missing to be empty, got %s", result.Missing)
	}
}

func TestMapToStruct_MapField(t *testing.T) {
	// This tests the scenario where input_schema map[string]string
	// receives a map[string]any from the Gemini API
	type SkillInput struct {
		Name        string            `json:"name"`
		InputSchema map[string]string `json:"input_schema"`
	}

	m := map[string]any{
		"name": "test_skill",
		"input_schema": map[string]any{
			"symbol": "string",
			"limit":  "int",
		},
	}

	var result SkillInput
	err := mapToStruct(m, &result)
	if err != nil {
		t.Fatalf("mapToStruct failed: %v", err)
	}

	if result.Name != "test_skill" {
		t.Errorf("expected name 'test_skill', got %s", result.Name)
	}
	if result.InputSchema == nil {
		t.Fatal("expected input_schema to be populated, got nil")
	}
	if len(result.InputSchema) != 2 {
		t.Errorf("expected input_schema to have 2 entries, got %d", len(result.InputSchema))
	}
	if result.InputSchema["symbol"] != "string" {
		t.Errorf("expected input_schema[symbol]='string', got %s", result.InputSchema["symbol"])
	}
	if result.InputSchema["limit"] != "int" {
		t.Errorf("expected input_schema[limit]='int', got %s", result.InputSchema["limit"])
	}
}
