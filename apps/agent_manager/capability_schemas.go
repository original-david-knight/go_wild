package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

type capabilitySchemaKind int

const (
	capabilitySchemaInput capabilitySchemaKind = iota
	capabilitySchemaOutput
)

var capabilitySchemaCache sync.Map // map[string]*jsonschema.Schema

func normalizeCapabilitySchema(raw json.RawMessage, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return "", nil
	}

	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return "", fmt.Errorf("%s must be valid JSON: %w", fieldName, err)
	}
	if _, ok := parsed.(map[string]any); !ok {
		return "", fmt.Errorf("%s must be a JSON object", fieldName)
	}

	normalized, err := json.Marshal(parsed)
	if err != nil {
		return "", fmt.Errorf("%s failed to serialize: %w", fieldName, err)
	}
	normalizedText := string(normalized)
	if _, err := compileCapabilitySchema(normalizedText); err != nil {
		return "", fmt.Errorf("%s is not a valid JSON schema: %w", fieldName, err)
	}
	return normalizedText, nil
}

func compileCapabilitySchema(schemaJSON string) (*jsonschema.Schema, error) {
	schemaJSON = strings.TrimSpace(schemaJSON)
	if schemaJSON == "" {
		return nil, nil
	}
	if cached, ok := capabilitySchemaCache.Load(schemaJSON); ok {
		if compiled, ok := cached.(*jsonschema.Schema); ok && compiled != nil {
			return compiled, nil
		}
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("capability_schema.json", strings.NewReader(schemaJSON)); err != nil {
		return nil, err
	}
	compiled, err := compiler.Compile("capability_schema.json")
	if err != nil {
		return nil, err
	}
	capabilitySchemaCache.Store(schemaJSON, compiled)
	return compiled, nil
}

func parseCapabilitySchema(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func validatePayloadAgainstCapabilitySchema(schemaJSON string, payload any) error {
	compiled, err := compileCapabilitySchema(schemaJSON)
	if err != nil {
		return err
	}
	if compiled == nil {
		return nil
	}
	if err := compiled.Validate(payload); err != nil {
		return err
	}
	return nil
}

func schemaJSONForMethod(ctx context.Context, db gowild_data.Database, method string, kind capabilitySchemaKind) (string, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return "", fmt.Errorf("method is required")
	}

	m, err := data.NewAgentService(db, "system").GetA2AMethod(ctx, method)
	if err != nil {
		return "", fmt.Errorf("unknown method %q", method)
	}

	switch kind {
	case capabilitySchemaInput:
		return strings.TrimSpace(m.InputSchemaJSON), nil
	case capabilitySchemaOutput:
		return strings.TrimSpace(m.OutputSchemaJSON), nil
	default:
		return "", fmt.Errorf("unknown schema kind")
	}
}

func validatePayloadForMethod(ctx context.Context, db gowild_data.Database, method string, kind capabilitySchemaKind, payload any) error {
	schemaJSON, err := schemaJSONForMethod(ctx, db, method, kind)
	if err != nil {
		return err
	}
	if strings.TrimSpace(schemaJSON) == "" {
		return nil
	}

	kindLabel := "input"
	if kind == capabilitySchemaOutput {
		kindLabel = "output"
	}
	if err := validatePayloadAgainstCapabilitySchema(schemaJSON, payload); err != nil {
		return fmt.Errorf("%s payload failed %s schema for method %q: %w", kindLabel, kindLabel, method, err)
	}
	return nil
}
