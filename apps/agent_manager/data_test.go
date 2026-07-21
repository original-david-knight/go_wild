package main

import (
	"context"
	"strings"
	"testing"
	"time"

	agentdata "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestGetSetSetting(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	// Get non-existent setting
	val, err := GetSetting(ctx, db, "nonexistent")
	if err == nil && val != "" {
		t.Errorf("expected empty value for nonexistent key, got %q", val)
	}

	// Set setting
	if err := SetSetting(ctx, db, "test_key", "test_value"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	// Get setting
	val, err = GetSetting(ctx, db, "test_key")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val != "test_value" {
		t.Errorf("expected test_value, got %q", val)
	}

	// Update setting (upsert)
	if err := SetSetting(ctx, db, "test_key", "updated_value"); err != nil {
		t.Fatalf("SetSetting update failed: %v", err)
	}

	val, _ = GetSetting(ctx, db, "test_key")
	if val != "updated_value" {
		t.Errorf("expected updated_value, got %q", val)
	}
}

func TestRegisterTables(t *testing.T) {
	db := setupManagerTestDB(t)

	// Verify key tables are registered
	if db.Table(Setting{}) == nil {
		t.Error("Setting table not registered")
	}
}

func TestEnsureSchemaBackfillsA2AMethodsFromCapabilities(t *testing.T) {
	ctx := context.Background()

	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := RegisterTables(db); err != nil {
		t.Fatalf("RegisterTables failed: %v", err)
	}

	svc := NewAgentService(db)
	agent, err := svc.CreateAgent(ctx, "legacy-agent")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	inputSchema := `{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}},"additionalProperties":false}`
	outputSchema := `{"type":"object","properties":{"ok":{"type":"boolean"}}}`
	if err := db.Table(agentdata.AgentCapability{}).Insert(ctx, &agentdata.AgentCapability{
		ID:               "cap-legacy-1",
		AgentID:          agent.ID,
		Role:             "legacy-role",
		Method:           "legacy_method",
		Description:      "Legacy capability method",
		InputSchemaJSON:  inputSchema,
		OutputSchemaJSON: outputSchema,
		RegisteredAt:     time.Now(),
	}); err != nil {
		t.Fatalf("failed inserting legacy capability: %v", err)
	}

	systemSvc := agentdata.NewAgentService(db, "system")
	if _, err := systemSvc.GetA2AMethod(ctx, "legacy_method"); err == nil {
		t.Fatalf("expected legacy_method to be absent before EnsureSchema backfill")
	}

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema failed: %v", err)
	}

	method, err := systemSvc.GetA2AMethod(ctx, "legacy_method")
	if err != nil {
		t.Fatalf("expected backfilled method to exist: %v", err)
	}
	if method.Description != "Legacy capability method" {
		t.Fatalf("method description = %q, want %q", method.Description, "Legacy capability method")
	}
	if method.InputSchemaJSON != inputSchema {
		t.Fatalf("unexpected backfilled input schema: %q", method.InputSchemaJSON)
	}
	if method.OutputSchemaJSON != outputSchema {
		t.Fatalf("unexpected backfilled output schema: %q", method.OutputSchemaJSON)
	}

	if err := validatePayloadForMethod(ctx, db, "legacy_method", capabilitySchemaInput, map[string]any{
		"order_id": "ord-1",
	}); err != nil {
		t.Fatalf("validatePayloadForMethod should succeed for valid payload: %v", err)
	}
	if err := validatePayloadForMethod(ctx, db, "legacy_method", capabilitySchemaInput, map[string]any{
		"order_id": 42,
	}); err == nil {
		t.Fatalf("expected schema validation error for invalid payload type")
	}

	err = validatePayloadForMethod(ctx, db, "totally_unknown_method", capabilitySchemaInput, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), `unknown method "totally_unknown_method"`) {
		t.Fatalf("expected strict unknown method error, got %v", err)
	}
}
