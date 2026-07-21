package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestNewPipelineEngineLoadsStoredPipelineDefinitions(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	stepsJSON, err := json.Marshal([]PipelineStep{
		{
			OnMethod:   "seed_method",
			OnStatus:   "succeeded",
			FromRole:   "*",
			ToRole:     "tester",
			NextMethod: "run_test",
			ParamMap:   map[string]string{"$": "payload"},
		},
	})
	if err != nil {
		t.Fatalf("marshal steps failed: %v", err)
	}

	dataSvc := data.NewAgentService(db, "system")
	if err := dataSvc.UpsertPipelineDefinition(ctx, &data.PipelineDefinition{
		ID:        "stored_pipeline",
		Name:      "Stored Pipeline",
		StepsJSON: string(stepsJSON),
		Enabled:   true,
	}); err != nil {
		t.Fatalf("upsert pipeline definition failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	_, ok := engine.GetPipeline("stored_pipeline")
	if !ok {
		t.Fatalf("expected stored pipeline to be loaded into pipeline engine")
	}
}

func TestNewPipelineEngineSkipsDisabledDefinitions(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)

	stepsJSON, err := json.Marshal([]PipelineStep{
		{
			OnMethod:   "seed_method",
			OnStatus:   "succeeded",
			FromRole:   "*",
			ToRole:     "tester",
			NextMethod: "run_test",
		},
	})
	if err != nil {
		t.Fatalf("marshal steps failed: %v", err)
	}

	dataSvc := data.NewAgentService(db, "system")
	if err := dataSvc.UpsertPipelineDefinition(ctx, &data.PipelineDefinition{
		ID:        "disabled_pipeline",
		Name:      "Disabled Pipeline",
		StepsJSON: string(stepsJSON),
		Enabled:   false,
	}); err != nil {
		t.Fatalf("upsert pipeline definition failed: %v", err)
	}

	engine := NewPipelineEngine(db, svc)
	if _, ok := engine.GetPipeline("disabled_pipeline"); ok {
		t.Fatalf("expected disabled pipeline definition to be skipped")
	}
}
