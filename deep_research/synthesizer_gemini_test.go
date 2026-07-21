package deepresearch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestDeepResearchSynthesisResponseSchemaConvertsNestedSchema(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"summary"},
		"properties": map[string]any{
			"summary": map[string]any{"type": "string"},
			"score":   map[string]any{"type": "number"},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"company": map[string]any{
				"type":     "object",
				"required": []any{"name"},
				"properties": map[string]any{
					"name":    map[string]any{"type": "string"},
					"founded": map[string]any{"type": "integer"},
				},
			},
		},
	}

	respSchema := deepResearchSynthesisResponseSchema(schema)
	if respSchema == nil {
		t.Fatalf("expected response schema")
	}
	if respSchema.Type != genai.TypeObject {
		t.Fatalf("response schema type = %s, want OBJECT", respSchema.Type)
	}
	outputSchema := respSchema.Properties["output"]
	if outputSchema == nil {
		t.Fatalf("expected output schema in response schema")
	}
	if outputSchema.Type != genai.TypeObject {
		t.Fatalf("output schema type = %s, want OBJECT", outputSchema.Type)
	}
	if len(outputSchema.Required) != 1 || outputSchema.Required[0] != "summary" {
		t.Fatalf("unexpected output required fields: %#v", outputSchema.Required)
	}
	if got := outputSchema.Properties["tags"]; got == nil || got.Type != genai.TypeArray {
		t.Fatalf("expected tags to be ARRAY, got %#v", got)
	}
	if got := outputSchema.Properties["tags"].Items; got == nil || got.Type != genai.TypeString {
		t.Fatalf("expected tags.items to be STRING, got %#v", got)
	}
	if got := outputSchema.Properties["company"]; got == nil || got.Type != genai.TypeObject {
		t.Fatalf("expected company to be OBJECT, got %#v", got)
	}
	if got := outputSchema.Properties["company"].Properties["founded"]; got == nil || got.Type != genai.TypeInteger {
		t.Fatalf("expected company.founded to be INTEGER, got %#v", got)
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaHandlesNullableAnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "null"},
			map[string]any{"type": "string"},
		},
	}

	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected converted schema")
	}
	if out.Type != genai.TypeString {
		t.Fatalf("converted type = %s, want STRING", out.Type)
	}
	if out.Nullable == nil || !*out.Nullable {
		t.Fatalf("expected nullable=true for anyOf null|string")
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaBasicTypes(t *testing.T) {
	cases := []struct {
		typeName string
		want     genai.Type
	}{
		{"string", genai.TypeString},
		{"number", genai.TypeNumber},
		{"integer", genai.TypeInteger},
		{"boolean", genai.TypeBoolean},
	}
	for _, tc := range cases {
		t.Run(tc.typeName, func(t *testing.T) {
			out := deepResearchJSONSchemaToGenAISchema(map[string]any{"type": tc.typeName}, 0)
			if out == nil {
				t.Fatalf("expected schema for type %s", tc.typeName)
			}
			if out.Type != tc.want {
				t.Fatalf("type %s: got %s, want %s", tc.typeName, out.Type, tc.want)
			}
		})
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaStringConstraints(t *testing.T) {
	schema := map[string]any{
		"type":      "string",
		"minLength": float64(3),
		"maxLength": float64(100),
		"pattern":   "^[a-z]+$",
		"format":    "email",
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeString {
		t.Fatalf("type = %s, want STRING", out.Type)
	}
	if out.MinLength == nil || *out.MinLength != 3 {
		t.Fatalf("minLength = %v, want 3", out.MinLength)
	}
	if out.MaxLength == nil || *out.MaxLength != 100 {
		t.Fatalf("maxLength = %v, want 100", out.MaxLength)
	}
	if out.Pattern != "^[a-z]+$" {
		t.Fatalf("pattern = %q, want %q", out.Pattern, "^[a-z]+$")
	}
	if out.Format != "email" {
		t.Fatalf("format = %q, want %q", out.Format, "email")
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaNumericConstraints(t *testing.T) {
	schema := map[string]any{
		"type":    "number",
		"minimum": float64(0.5),
		"maximum": float64(99.9),
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeNumber {
		t.Fatalf("type = %s, want NUMBER", out.Type)
	}
	if out.Minimum == nil || *out.Minimum != 0.5 {
		t.Fatalf("minimum = %v, want 0.5", out.Minimum)
	}
	if out.Maximum == nil || *out.Maximum != 99.9 {
		t.Fatalf("maximum = %v, want 99.9", out.Maximum)
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaArrayOfObjects(t *testing.T) {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "integer"},
			},
			"required": []any{"name"},
		},
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeArray {
		t.Fatalf("type = %s, want ARRAY", out.Type)
	}
	if out.Items == nil {
		t.Fatalf("expected items schema")
	}
	if out.Items.Type != genai.TypeObject {
		t.Fatalf("items type = %s, want OBJECT", out.Items.Type)
	}
	if out.Items.Properties["name"] == nil || out.Items.Properties["name"].Type != genai.TypeString {
		t.Fatalf("expected items.name to be STRING")
	}
	if out.Items.Properties["age"] == nil || out.Items.Properties["age"].Type != genai.TypeInteger {
		t.Fatalf("expected items.age to be INTEGER")
	}
	if len(out.Items.Required) != 1 || out.Items.Required[0] != "name" {
		t.Fatalf("expected items.required = [name], got %v", out.Items.Required)
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaEnumValues(t *testing.T) {
	schema := map[string]any{
		"type": "string",
		"enum": []any{"a", "b", "c"},
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeString {
		t.Fatalf("type = %s, want STRING", out.Type)
	}
	if len(out.Enum) != 3 {
		t.Fatalf("enum length = %d, want 3", len(out.Enum))
	}
	for i, want := range []string{"a", "b", "c"} {
		if out.Enum[i] != want {
			t.Fatalf("enum[%d] = %q, want %q", i, out.Enum[i], want)
		}
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaTypeArrayWithNull(t *testing.T) {
	schema := map[string]any{
		"type": []any{"string", "null"},
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeString {
		t.Fatalf("type = %s, want STRING", out.Type)
	}
	if out.Nullable == nil || !*out.Nullable {
		t.Fatalf("expected nullable=true for type [string, null]")
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaDepthLimit(t *testing.T) {
	// Build a schema nested 14 levels deep
	inner := map[string]any{"type": "string"}
	for i := 0; i < 14; i++ {
		inner = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"child": inner,
			},
		}
	}
	out := deepResearchJSONSchemaToGenAISchema(inner, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	// Walk down to depth 13 — function called with depth=13 > 12 returns TypeObject (fallback)
	current := out
	for d := 0; d < 13; d++ {
		if current.Type != genai.TypeObject {
			t.Fatalf("depth %d: type = %s, want OBJECT", d, current.Type)
		}
		child, ok := current.Properties["child"]
		if !ok || child == nil {
			t.Fatalf("depth %d: missing child property", d)
		}
		current = child
	}
	// At depth 13 the function was called with depth=13 > 12, so returns TypeObject with no properties
	if current.Type != genai.TypeObject {
		t.Fatalf("depth-limited node: type = %s, want OBJECT", current.Type)
	}
	if len(current.Properties) != 0 {
		t.Fatalf("depth-limited node should have no properties, got %d", len(current.Properties))
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaEmptySchema(t *testing.T) {
	out := deepResearchJSONSchemaToGenAISchema(map[string]any{}, 0)
	if out == nil {
		t.Fatalf("expected schema for empty map")
	}
	if out.Type != genai.TypeObject {
		t.Fatalf("type = %s, want OBJECT for empty schema", out.Type)
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaNoTypeInfersFromProperties(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeObject {
		t.Fatalf("type = %s, want OBJECT (inferred from properties)", out.Type)
	}
	if out.Properties["name"] == nil || out.Properties["name"].Type != genai.TypeString {
		t.Fatalf("expected name property to be STRING")
	}
}

func TestDeepResearchJSONSchemaToGenAISchemaNoTypeInfersFromItems(t *testing.T) {
	schema := map[string]any{
		"items": map[string]any{"type": "integer"},
	}
	out := deepResearchJSONSchemaToGenAISchema(schema, 0)
	if out == nil {
		t.Fatalf("expected schema")
	}
	if out.Type != genai.TypeArray {
		t.Fatalf("type = %s, want ARRAY (inferred from items)", out.Type)
	}
	if out.Items == nil || out.Items.Type != genai.TypeInteger {
		t.Fatalf("expected items to be INTEGER")
	}
}

func TestDeepResearchNormalizeJSONSchemaNodeAnyOfMerge(t *testing.T) {
	node := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string", "minLength": float64(1)},
			map[string]any{"type": "null"},
		},
	}
	normalized, nullable := deepResearchNormalizeJSONSchemaNode(node)
	if !nullable {
		t.Fatalf("expected nullable=true for anyOf with null")
	}
	if normalized["type"] != "string" {
		t.Fatalf("type = %v, want string", normalized["type"])
	}
	if normalized["minLength"] != float64(1) {
		t.Fatalf("minLength = %v, want 1", normalized["minLength"])
	}
}

func TestDeepResearchSynthesisResponseSchemaWrapsOutput(t *testing.T) {
	userSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string"},
		},
	}
	resp := deepResearchSynthesisResponseSchema(userSchema)
	if resp == nil {
		t.Fatalf("expected response schema")
	}
	if resp.Type != genai.TypeObject {
		t.Fatalf("wrapper type = %s, want OBJECT", resp.Type)
	}
	outputProp := resp.Properties["output"]
	if outputProp == nil {
		t.Fatalf("expected output property")
	}
	if outputProp.Type != genai.TypeObject {
		t.Fatalf("output type = %s, want OBJECT", outputProp.Type)
	}
	if outputProp.Properties["title"] == nil || outputProp.Properties["title"].Type != genai.TypeString {
		t.Fatalf("expected output.title to be STRING")
	}
	summaryProp := resp.Properties["summary"]
	if summaryProp == nil {
		t.Fatalf("expected summary property")
	}
	if summaryProp.Type != genai.TypeString {
		t.Fatalf("summary type = %s, want STRING", summaryProp.Type)
	}
	if len(resp.Required) != 1 || resp.Required[0] != "output" {
		t.Fatalf("expected required = [output], got %v", resp.Required)
	}
}

func TestGeminiDeepResearchSynthesizerSynthesizeWrappedOutput(t *testing.T) {
	var gotCfg *genai.GenerateContentConfig
	synth := &geminiDeepResearchSynthesizer{
		model: "synth-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			gotCfg = cfg
			return deepResearchJSONCandidateResponse(`{
				"output": {"probability": 0.42},
				"summary": "Evidence is mixed but leans negative."
			}`), nil
		},
	}

	result, err := synth.Synthesize(context.Background(), SynthesisRequest{
		Query: "test query",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"probability": map[string]any{"type": "number"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}
	if gotCfg == nil || gotCfg.ResponseSchema == nil || gotCfg.ResponseMIMEType != "application/json" {
		t.Fatalf("unexpected generate config: %#v", gotCfg)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected output map, got %T", result.Output)
	}
	if output["probability"] != float64(0.42) {
		t.Fatalf("unexpected output: %#v", output)
	}
	if !strings.Contains(result.Summary, "Evidence is mixed") {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
}

func TestGeminiDeepResearchSynthesizerSynthesizeFallbackCall(t *testing.T) {
	callCount := 0
	synth := &geminiDeepResearchSynthesizer{
		model: "synth-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			callCount++
			if callCount == 1 {
				if cfg == nil || cfg.ResponseSchema == nil {
					t.Fatalf("expected first call with response schema")
				}
				return nil, errors.New("schema not supported")
			}
			if cfg == nil || cfg.ResponseSchema != nil {
				t.Fatalf("expected second call without response schema")
			}
			return deepResearchJSONCandidateResponse(`{"probability": 0.9}`), nil
		},
	}

	result, err := synth.Synthesize(context.Background(), SynthesisRequest{
		Query: "test query",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"probability": map[string]any{"type": "number"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
	output, ok := result.Output.(map[string]any)
	if !ok || output["probability"] != float64(0.9) {
		t.Fatalf("unexpected output: %#v", result.Output)
	}
}

func TestGeminiDeepResearchSynthesizerSynthesizeInvalidJSON(t *testing.T) {
	synth := &geminiDeepResearchSynthesizer{
		model: "synth-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return deepResearchJSONCandidateResponse(`not-json`), nil
		},
	}

	_, err := synth.Synthesize(context.Background(), SynthesisRequest{
		Query: "test query",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"probability": map[string]any{"type": "number"},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if !strings.Contains(err.Error(), "synthesizer returned invalid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeminiDeepResearchSynthesizerSynthesizeEmptySchemaNoCall(t *testing.T) {
	called := false
	synth := &geminiDeepResearchSynthesizer{
		model: "synth-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			called = true
			return nil, errors.New("should not be called")
		},
	}

	result, err := synth.Synthesize(context.Background(), SynthesisRequest{})
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}
	if called {
		t.Fatalf("expected generateContent not to be called")
	}
	if result.Output != nil || result.Summary != "" {
		t.Fatalf("expected empty result, got %#v", result)
	}
}
