package data

import (
	"context"
	"strings"
	"testing"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestPipelineDefinitionServiceCRUD(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	svc := NewAgentService(db, "system")

	if err := svc.UpsertPipelineDefinition(ctx, nil); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected nil definition validation error, got %v", err)
	}
	if err := svc.UpsertPipelineDefinition(ctx, &PipelineDefinition{
		Name:      "Missing ID",
		StepsJSON: `[]`,
	}); err == nil || !strings.Contains(err.Error(), "pipeline id is required") {
		t.Fatalf("expected missing id validation error, got %v", err)
	}
	if err := svc.UpsertPipelineDefinition(ctx, &PipelineDefinition{
		ID:        "missing-name",
		StepsJSON: `[]`,
	}); err == nil || !strings.Contains(err.Error(), "pipeline name is required") {
		t.Fatalf("expected missing name validation error, got %v", err)
	}
	if err := svc.UpsertPipelineDefinition(ctx, &PipelineDefinition{
		ID:   "missing-steps",
		Name: "Missing Steps",
	}); err == nil || !strings.Contains(err.Error(), "steps_json is required") {
		t.Fatalf("expected missing steps validation error, got %v", err)
	}

	initial := &PipelineDefinition{
		ID:        "pipe-1",
		Name:      "Pipeline One",
		StepsJSON: `[{"on_method":"seed","to_role":"target","next_method":"run"}]`,
		Enabled:   true,
	}
	if err := svc.UpsertPipelineDefinition(ctx, initial); err != nil {
		t.Fatalf("UpsertPipelineDefinition insert failed: %v", err)
	}

	got, err := svc.GetPipelineDefinition(ctx, "pipe-1")
	if err != nil {
		t.Fatalf("GetPipelineDefinition failed: %v", err)
	}
	if got.Name != "Pipeline One" || !got.Enabled {
		t.Fatalf("unexpected inserted definition: %+v", got)
	}

	list, err := svc.ListPipelineDefinitions(ctx)
	if err != nil {
		t.Fatalf("ListPipelineDefinitions failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != "pipe-1" {
		t.Fatalf("unexpected definitions list: %+v", list)
	}

	updated := &PipelineDefinition{
		ID:        "pipe-1",
		Name:      "Pipeline One Updated",
		StepsJSON: `[{"on_method":"seed","to_role":"target","next_method":"run2"}]`,
		Enabled:   false,
	}
	if err := svc.UpsertPipelineDefinition(ctx, updated); err != nil {
		t.Fatalf("UpsertPipelineDefinition update failed: %v", err)
	}

	got, err = svc.GetPipelineDefinition(ctx, "pipe-1")
	if err != nil {
		t.Fatalf("GetPipelineDefinition after update failed: %v", err)
	}
	if got.Name != "Pipeline One Updated" || got.Enabled {
		t.Fatalf("unexpected updated definition: %+v", got)
	}
	if !strings.Contains(got.StepsJSON, "run2") {
		t.Fatalf("expected updated steps_json, got %s", got.StepsJSON)
	}

	if err := svc.DeletePipelineDefinition(ctx, "pipe-1"); err != nil {
		t.Fatalf("DeletePipelineDefinition failed: %v", err)
	}
	if _, err := svc.GetPipelineDefinition(ctx, "pipe-1"); err == nil {
		t.Fatalf("expected get deleted pipeline definition to fail")
	}
}

func TestPipelineRunAndStepRunServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	svc := NewAgentService(db, "system")

	oldRun := &PipelineRun{
		ID:           "run-old",
		PipelineID:   "pipe-a",
		TriggerJobID: "job-old",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Hour),
		UpdatedAt:    time.Now().Add(-time.Hour),
	}
	newRun := &PipelineRun{
		ID:           "run-new",
		PipelineID:   "pipe-b",
		TriggerJobID: "job-new",
		CurrentStep:  1,
		Status:       "failed",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := svc.CreatePipelineRun(ctx, oldRun); err != nil {
		t.Fatalf("CreatePipelineRun old failed: %v", err)
	}
	if err := svc.CreatePipelineRun(ctx, newRun); err != nil {
		t.Fatalf("CreatePipelineRun new failed: %v", err)
	}

	gotRun, err := svc.GetPipelineRun(ctx, "run-old")
	if err != nil {
		t.Fatalf("GetPipelineRun run-old failed: %v", err)
	}
	if gotRun.Status != "running" {
		t.Fatalf("unexpected run status: %q", gotRun.Status)
	}

	active, err := svc.GetActivePipelineRuns(ctx)
	if err != nil {
		t.Fatalf("GetActivePipelineRuns failed: %v", err)
	}
	if len(active) != 1 || active[0].ID != "run-old" {
		t.Fatalf("unexpected active runs: %+v", active)
	}

	gotRun.Status = "completed"
	gotRun.UpdatedAt = time.Now()
	if err := svc.UpdatePipelineRun(ctx, gotRun); err != nil {
		t.Fatalf("UpdatePipelineRun failed: %v", err)
	}
	active, err = svc.GetActivePipelineRuns(ctx)
	if err != nil {
		t.Fatalf("GetActivePipelineRuns after update failed: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active runs after completion, got %+v", active)
	}

	if _, err := svc.GetPipelineRun(ctx, "missing-run"); err == nil {
		t.Fatalf("expected missing pipeline run lookup to fail")
	}

	step := &PipelineStepRun{
		ID:        "step-1",
		RunID:     "run-new",
		StepIndex: 0,
		A2AJobID:  "a2a-1",
		Status:    "running",
		StartedAt: time.Now(),
	}
	if err := svc.CreateStepRun(ctx, step); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}
	step.Status = "succeeded"
	step.CompletedAt = time.Now()
	if err := svc.UpdateStepRun(ctx, step); err != nil {
		t.Fatalf("UpdateStepRun failed: %v", err)
	}

	var stored PipelineStepRun
	if err := db.Table(PipelineStepRun{}).Get(ctx, "step-1", &stored); err != nil {
		t.Fatalf("step run lookup failed: %v", err)
	}
	if stored.Status != "succeeded" || stored.CompletedAt.IsZero() {
		t.Fatalf("unexpected stored step run: %+v", stored)
	}

	// Basic query sanity check for pipeline runs table.
	results, err := db.Table(PipelineRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"pipeline_id": "pipe-b"},
	})
	if err != nil {
		t.Fatalf("pipeline run query failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 queried run, got %d", len(results))
	}
}
