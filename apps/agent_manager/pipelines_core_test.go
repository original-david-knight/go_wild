package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/agent_net"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestPipelineCloneAndNormalizeDeepCopy(t *testing.T) {
	orig := Pipeline{
		ID:   "  pipeline_1  ",
		Name: " ",
		Steps: []PipelineStep{
			{
				OnMethod:   " seed_method ",
				OnStatus:   " succeeded ",
				FromRole:   " * ",
				ToRole:     " target ",
				NextMethod: " run_next ",
				ParamMap:   map[string]string{"$": "payload"},
				FanOut:     true,
				FanOutKey:  " items ",
			},
		},
	}

	cloned := clonePipeline(orig)
	cloned.Steps[0].ParamMap["$"] = "changed"
	if got := orig.Steps[0].ParamMap["$"]; got != "payload" {
		t.Fatalf("clonePipeline should deep copy ParamMap, got %q", got)
	}

	clonedList := clonePipelines([]Pipeline{orig})
	clonedList[0].Steps[0].ParamMap["$"] = "again"
	if got := orig.Steps[0].ParamMap["$"]; got != "payload" {
		t.Fatalf("clonePipelines should deep copy steps, got %q", got)
	}

	normalized := normalizePipeline(orig)
	if normalized.ID != "pipeline_1" {
		t.Fatalf("normalized ID = %q, want %q", normalized.ID, "pipeline_1")
	}
	if normalized.Name != "pipeline_1" {
		t.Fatalf("normalized Name = %q, want %q", normalized.Name, "pipeline_1")
	}
	step := normalized.Steps[0]
	if step.OnMethod != "seed_method" || step.OnStatus != "succeeded" || step.FromRole != "*" || step.ToRole != "target" || step.NextMethod != "run_next" || step.FanOutKey != "items" {
		t.Fatalf("unexpected normalized step: %+v", step)
	}

	noMap := normalizePipeline(Pipeline{
		ID:   "p",
		Name: "p",
		Steps: []PipelineStep{
			{OnMethod: "m", ToRole: "r", NextMethod: "n"},
		},
	})
	if noMap.Steps[0].ParamMap == nil {
		t.Fatalf("normalizePipeline should initialize nil ParamMap")
	}

	normalizedBuiltin := normalizePipeline(Pipeline{
		ID:   "builtin_alias",
		Name: "builtin_alias",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed",
				NextMethod: "/polymarket_review_positions",
			},
		},
	})
	if got := normalizedBuiltin.Steps[0].NextMethod; got != "builtin_polymarket_snapshot" {
		t.Fatalf("normalizePipeline should canonicalize builtin method alias, got %q", got)
	}

	normalizedFindMarketsBuiltin := normalizePipeline(Pipeline{
		ID:   "builtin_find_markets_name",
		Name: "builtin_find_markets_name",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed",
				NextMethod: "polymarket_find_markets",
			},
		},
	})
	if got := normalizedFindMarketsBuiltin.Steps[0].NextMethod; got != "builtin_polymarket_find_markets" {
		t.Fatalf("normalizePipeline should canonicalize find-markets builtin name, got %q", got)
	}

	normalizedTradeBuiltin := normalizePipeline(Pipeline{
		ID:   "builtin_manage_name",
		Name: "builtin_manage_name",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed",
				NextMethod: "polymarket_manage_position",
			},
		},
	})
	if got := normalizedTradeBuiltin.Steps[0].NextMethod; got != "builtin_polymarket_manage_position" {
		t.Fatalf("normalizePipeline should canonicalize manage-position builtin name, got %q", got)
	}
}

func TestValidatePipelineErrors(t *testing.T) {
	valid := Pipeline{
		ID:   "pipe",
		Name: "Pipe",
		Steps: []PipelineStep{
			{OnMethod: "seed", ToRole: "target", NextMethod: "run"},
		},
	}
	if err := validatePipeline(valid); err != nil {
		t.Fatalf("validatePipeline(valid) unexpected error: %v", err)
	}
	if err := validatePipeline(Pipeline{
		ID:   "builtin_pipe",
		Name: "Builtin Pipe",
		Steps: []PipelineStep{
			{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "builtin_polymarket_snapshot"},
		},
	}); err != nil {
		t.Fatalf("validatePipeline(builtin valid) unexpected error: %v", err)
	}
	if err := validatePipeline(Pipeline{
		ID:   "builtin_pipe_alias",
		Name: "Builtin Pipe Alias",
		Steps: []PipelineStep{
			{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "/polymarket_review_positions"},
		},
	}); err != nil {
		t.Fatalf("validatePipeline(builtin alias valid) unexpected error: %v", err)
	}
	if err := validatePipeline(Pipeline{
		ID:   "builtin_find_markets_name",
		Name: "Builtin Find Markets Name",
		Steps: []PipelineStep{
			{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "polymarket_find_markets"},
		},
	}); err != nil {
		t.Fatalf("validatePipeline(builtin find-markets name valid) unexpected error: %v", err)
	}
	if err := validatePipeline(Pipeline{
		ID:   "builtin_manage_name",
		Name: "Builtin Manage Name",
		Steps: []PipelineStep{
			{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "polymarket_manage_position"},
		},
	}); err != nil {
		t.Fatalf("validatePipeline(builtin manage name valid) unexpected error: %v", err)
	}
	if err := validatePipeline(Pipeline{
		ID:   "builtin_pipe_fanout",
		Name: "Builtin Pipe FanOut",
		Steps: []PipelineStep{
			{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "builtin_polymarket_snapshot", FanOut: true, FanOutKey: "items"},
		},
	}); err != nil {
		t.Fatalf("validatePipeline(builtin fanout valid) unexpected error: %v", err)
	}

	tests := []struct {
		name string
		p    Pipeline
		want string
	}{
		{
			name: "missing id",
			p: Pipeline{
				Name:  "Pipe",
				Steps: valid.Steps,
			},
			want: "pipeline id is required",
		},
		{
			name: "missing name",
			p: Pipeline{
				ID:    "pipe",
				Steps: valid.Steps,
			},
			want: "pipeline name is required",
		},
		{
			name: "missing steps",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
			},
			want: "at least one step",
		},
		{
			name: "missing on_method",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{ToRole: "target", NextMethod: "run"},
				},
			},
			want: "on_method is required",
		},
		{
			name: "missing to_role",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{OnMethod: "seed", NextMethod: "run"},
				},
			},
			want: "to_role is required",
		},
		{
			name: "missing next_method",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{OnMethod: "seed", ToRole: "target"},
				},
			},
			want: "next_method is required",
		},
		{
			name: "missing fan_out_key",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{OnMethod: "seed", ToRole: "target", NextMethod: "run", FanOut: true},
				},
			},
			want: "fan_out_key is required",
		},
		{
			name: "builtin missing method",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "builtin_missing"},
				},
			},
			want: "unknown builtin method",
		},
		{
			name: "removed polymarket execute alias",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "polymarket_execute_trade"},
				},
			},
			want: "unknown builtin method",
		},
		{
			name: "removed polymarket update alias",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "polymarket_update_position"},
				},
			},
			want: "unknown builtin method",
		},
		{
			name: "builtin missing fan_out_key",
			p: Pipeline{
				ID:   "pipe",
				Name: "Pipe",
				Steps: []PipelineStep{
					{Runner: pipelineStepRunnerBuiltin, OnMethod: "seed", NextMethod: "builtin_polymarket_snapshot", FanOut: true},
				},
			},
			want: "fan_out_key is required",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validatePipeline(tc.p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validatePipeline() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestPipelineFromDefinition(t *testing.T) {
	stepsJSON, err := json.Marshal([]PipelineStep{
		{
			OnMethod:   " seed ",
			OnStatus:   " succeeded ",
			FromRole:   " * ",
			ToRole:     " target ",
			NextMethod: " run ",
		},
	})
	if err != nil {
		t.Fatalf("marshal steps failed: %v", err)
	}

	p, err := pipelineFromDefinition(data.PipelineDefinition{
		ID:        " pipe_1 ",
		Name:      " ",
		StepsJSON: string(stepsJSON),
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("pipelineFromDefinition() unexpected error: %v", err)
	}
	if p.ID != "pipe_1" || p.Name != "pipe_1" {
		t.Fatalf("unexpected normalized pipeline: %+v", p)
	}
	if p.Steps[0].OnMethod != "seed" || p.Steps[0].ToRole != "target" || p.Steps[0].NextMethod != "run" {
		t.Fatalf("unexpected normalized step: %+v", p.Steps[0])
	}

	if _, err := pipelineFromDefinition(data.PipelineDefinition{
		ID:        "bad-json",
		Name:      "Bad JSON",
		StepsJSON: "{not-json",
	}); err == nil || !strings.Contains(err.Error(), "invalid steps_json") {
		t.Fatalf("expected invalid steps_json error, got %v", err)
	}

	badStepsJSON, err := json.Marshal([]PipelineStep{{OnMethod: "seed"}})
	if err != nil {
		t.Fatalf("marshal bad steps failed: %v", err)
	}
	if _, err := pipelineFromDefinition(data.PipelineDefinition{
		ID:        "bad-step",
		Name:      "Bad Step",
		StepsJSON: string(badStepsJSON),
	}); err == nil || !strings.Contains(err.Error(), "to_role is required") {
		t.Fatalf("expected validation error for bad step, got %v", err)
	}
}

func TestPipelineStepMatchingAndParamMapping(t *testing.T) {
	engine := &PipelineEngine{}

	if !engine.stepMatches(PipelineStep{
		OnMethod:   "seed",
		OnStatus:   "",
		FromRole:   "",
		ToRole:     "target",
		NextMethod: "next",
	}, "seed", "succeeded", "") {
		t.Fatalf("default OnStatus should match succeeded")
	}

	if engine.stepMatches(PipelineStep{
		OnMethod:   "seed",
		OnStatus:   "failed",
		FromRole:   "",
		ToRole:     "target",
		NextMethod: "next",
	}, "seed", "succeeded", "") {
		t.Fatalf("failed status step should not match succeeded job")
	}

	if !engine.stepMatches(PipelineStep{
		OnMethod:   "seed",
		OnStatus:   "*",
		FromRole:   "origin",
		ToRole:     "target",
		NextMethod: "next",
	}, "seed", "failed", "origin") {
		t.Fatalf("wildcard status with matching role should match")
	}

	if engine.stepMatches(PipelineStep{
		OnMethod:   "seed",
		OnStatus:   "*",
		FromRole:   "origin",
		ToRole:     "target",
		NextMethod: "next",
	}, "seed", "failed", "") {
		t.Fatalf("missing source role should not match role-constrained step")
	}

	result := map[string]any{"order_id": "o-1", "amount": 42}
	mapped := mapParams(result, map[string]string{"order_id": "id", "missing": "skip"})
	if len(mapped) != 1 || mapped["id"] != "o-1" {
		t.Fatalf("unexpected mapped params: %#v", mapped)
	}

	whole := mapParams(result, map[string]string{"$": "payload"})
	payload, _ := whole["payload"].(map[string]any)
	if len(whole) != 1 || payload["amount"] != 42 {
		t.Fatalf("expected full result passthrough, got %#v", whole)
	}
}

func TestPipelineCompletionNormalizationAndExtraction(t *testing.T) {
	engine := &PipelineEngine{}

	job := map[string]any{
		"job_id":       "job-1",
		"request_json": `{"method":"seed_method","params":{"x":1}}`,
		"result_json":  `{"ok":true}`,
	}
	normalized := engine.normalizeCompletionJob(job)
	if normalized["id"] != "job-1" {
		t.Fatalf("normalized id = %v, want %q", normalized["id"], "job-1")
	}

	request, ok := normalized["request"].(map[string]any)
	if !ok || request["method"] != "seed_method" {
		t.Fatalf("expected request map with method, got %#v", normalized["request"])
	}
	result, ok := normalized["result"].(map[string]any)
	if !ok || result["ok"] != true {
		t.Fatalf("expected result map, got %#v", normalized["result"])
	}
	if got := engine.extractJobRequestMethod(job); got != "seed_method" {
		t.Fatalf("extractJobRequestMethod = %q, want %q", got, "seed_method")
	}
	if got := engine.extractJobID(job); got != "job-1" {
		t.Fatalf("extractJobID = %q, want %q", got, "job-1")
	}
	if got := engine.extractJobResult(job); got["ok"] != true {
		t.Fatalf("extractJobResult = %#v, want ok=true", got)
	}

	withRequest := map[string]any{
		"id":      "job-2",
		"request": map[string]any{"method": "direct_method"},
	}
	normalized = engine.normalizeCompletionJob(withRequest)
	if _, ok := normalized["request_json"].(string); !ok {
		t.Fatalf("expected request_json to be generated from request map")
	}

	invalidJSON := map[string]any{
		"id":           "job-3",
		"request_json": "{invalid",
		"result_json":  "{invalid",
	}
	if got := engine.extractJobRequestMethod(invalidJSON); got != "" {
		t.Fatalf("expected empty method for invalid request_json, got %q", got)
	}
	if got := engine.extractJobResult(invalidJSON); got != nil {
		t.Fatalf("expected nil result for invalid result_json, got %#v", got)
	}
	if got := engine.normalizeCompletionJob(nil); got != nil {
		t.Fatalf("normalizeCompletionJob(nil) = %#v, want nil", got)
	}
}

func TestEffectiveCompletionStatusTreatsWrappedResultStatusFailedAsFailed(t *testing.T) {
	engine := &PipelineEngine{}

	status, reason := engine.effectiveCompletionStatus(map[string]any{
		"status": "succeeded",
		"result": map[string]any{
			"result": map[string]any{
				"status": "FAILED",
				"reason": "policy blocked",
			},
		},
	})
	if status != "failed" {
		t.Fatalf("effectiveCompletionStatus status = %q, want failed", status)
	}
	if reason != "policy blocked" {
		t.Fatalf("effectiveCompletionStatus reason = %q, want %q", reason, "policy blocked")
	}

	status, reason = engine.effectiveCompletionStatus(map[string]any{
		"status": "succeeded",
		"result": map[string]any{
			"output": map[string]any{
				"status": "FAILED",
			},
		},
	})
	if status != "failed" {
		t.Fatalf("effectiveCompletionStatus nested output status = %q, want failed", status)
	}
	if reason != "" {
		t.Fatalf("effectiveCompletionStatus nested output reason = %q, want empty", reason)
	}

	status, reason = engine.effectiveCompletionStatus(map[string]any{
		"status": "succeeded",
		"result": map[string]any{
			"status": "FAILED: broker error (500): place order failed",
			"error":  "not enough balance / allowance",
		},
	})
	if status != "failed" {
		t.Fatalf("effectiveCompletionStatus failed-prefix status = %q, want failed", status)
	}
	if reason != "not enough balance / allowance" {
		t.Fatalf("effectiveCompletionStatus failed-prefix reason = %q, want %q", reason, "not enough balance / allowance")
	}
}

func TestPipelineRuntimeClientAndRoleResolution(t *testing.T) {
	t.Setenv("AGENT_NET_URL", "https://agent-net.example")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service}

	agent, err := service.CreateAgent(ctx, "source-agent")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "seed_method", "seed", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	agentSvc := data.NewAgentService(db, agent.ID)
	if err := agentSvc.RegisterCapability(ctx, "source_role", "seed_method"); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	client, err := engine.getA2AClient(ctx)
	if err != nil {
		t.Fatalf("getA2AClient failed: %v", err)
	}
	cached, err := engine.getA2AClient(ctx)
	if err != nil {
		t.Fatalf("second getA2AClient failed: %v", err)
	}
	if client != cached {
		t.Fatalf("expected cached A2A client pointer reuse")
	}

	pubKey, err := engine.getAgentPublicKey(ctx, agent)
	if err != nil {
		t.Fatalf("getAgentPublicKey failed: %v", err)
	}
	if pubKey == "" {
		t.Fatalf("expected non-empty public key")
	}

	role := engine.resolveSourceRole(ctx, map[string]any{
		"from_public_key": pubKey,
	}, "seed_method")
	if role != "source_role" {
		t.Fatalf("resolveSourceRole = %q, want %q", role, "source_role")
	}

	role = engine.resolveSourceRole(ctx, map[string]any{
		"from_role":       "direct_role",
		"from_public_key": "ignored",
	}, "seed_method")
	if role != "direct_role" {
		t.Fatalf("from_role should take precedence, got %q", role)
	}

	if _, err := engine.getA2AClientForPublicKey(ctx, pubKey); err != nil {
		t.Fatalf("getA2AClientForPublicKey failed: %v", err)
	}
	if _, err := engine.callbackClientForPayload(ctx, map[string]any{"to_public_key": pubKey}); err != nil {
		t.Fatalf("callbackClientForPayload(to_public_key) failed: %v", err)
	}
	if _, err := engine.callbackClientForPayload(ctx, map[string]any{}); err != nil {
		t.Fatalf("callbackClientForPayload(fallback) failed: %v", err)
	}
	if _, err := engine.agentIDForPublicKey(ctx, "missing-key"); err == nil {
		t.Fatalf("expected error for unknown public key")
	}

	noSeed := &data.Agent{ID: "no-seed"}
	if _, err := engine.getAgentPublicKey(ctx, noSeed); err == nil || !strings.Contains(err.Error(), "no wallet seed phrase") {
		t.Fatalf("expected missing seed phrase error, got %v", err)
	}
}

func TestPipelineInMemoryRegistryIsolation(t *testing.T) {
	engine := &PipelineEngine{}
	base := Pipeline{
		ID:   "pipe",
		Name: "Pipe",
		Steps: []PipelineStep{
			{OnMethod: "seed", ToRole: "target", NextMethod: "run", ParamMap: map[string]string{"$": "payload"}},
		},
	}

	if created := engine.upsertPipelineInMemory(base); !created {
		t.Fatalf("expected first upsert to create pipeline")
	}

	updated := base
	updated.Name = "Updated"
	if created := engine.upsertPipelineInMemory(updated); created {
		t.Fatalf("expected second upsert to update existing pipeline")
	}

	got, ok := engine.GetPipeline("pipe")
	if !ok {
		t.Fatalf("expected pipeline to exist in memory")
	}
	if got.Name != "Updated" {
		t.Fatalf("GetPipeline name = %q, want %q", got.Name, "Updated")
	}

	got.Steps[0].ParamMap["$"] = "mutated"
	again, _ := engine.GetPipeline("pipe")
	if again.Steps[0].ParamMap["$"] != "payload" {
		t.Fatalf("GetPipeline should return cloned data")
	}

	all := engine.GetPipelines()
	all[0].Name = "Changed Outside"
	check, _ := engine.GetPipeline("pipe")
	if check.Name != "Updated" {
		t.Fatalf("GetPipelines should return cloned list")
	}

	if deleted := engine.deletePipelineInMemory("pipe"); !deleted {
		t.Fatalf("expected deletePipelineInMemory to delete existing pipeline")
	}
	if deleted := engine.deletePipelineInMemory("pipe"); deleted {
		t.Fatalf("expected second delete to return false")
	}
}

func TestPipelineDefinitionUpsertDisableDeleteAndLookup(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service}

	pipeline := Pipeline{
		ID:   " pipeline_one ",
		Name: " ",
		Steps: []PipelineStep{
			{OnMethod: "seed", ToRole: "target", NextMethod: "run"},
		},
	}
	created, err := engine.UpsertPipelineDefinition(ctx, pipeline, true)
	if err != nil {
		t.Fatalf("UpsertPipelineDefinition(create) failed: %v", err)
	}
	if !created {
		t.Fatalf("expected create=true on first upsert")
	}
	if _, ok := engine.GetPipeline("pipeline_one"); !ok {
		t.Fatalf("expected normalized pipeline id to be in memory")
	}

	pipeline.Name = "Updated Name"
	created, err = engine.UpsertPipelineDefinition(ctx, pipeline, true)
	if err != nil {
		t.Fatalf("UpsertPipelineDefinition(update) failed: %v", err)
	}
	if created {
		t.Fatalf("expected create=false on update")
	}

	created, err = engine.UpsertPipelineDefinition(ctx, pipeline, false)
	if err != nil {
		t.Fatalf("UpsertPipelineDefinition(disable) failed: %v", err)
	}
	if created {
		t.Fatalf("expected disable of existing pipeline to report create=false")
	}
	if _, ok := engine.GetPipeline("pipeline_one"); ok {
		t.Fatalf("expected disabled pipeline to be removed from memory")
	}

	def, err := data.NewAgentService(db, "system").GetPipelineDefinition(ctx, "pipeline_one")
	if err != nil {
		t.Fatalf("GetPipelineDefinition failed: %v", err)
	}
	if def.Enabled {
		t.Fatalf("expected persisted definition to be disabled")
	}

	if err := engine.DeletePipelineDefinition(ctx, ""); err == nil || !strings.Contains(err.Error(), "pipeline id is required") {
		t.Fatalf("expected pipeline id required error, got %v", err)
	}
	if err := engine.DeletePipelineDefinition(ctx, "missing"); !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("expected ErrPipelineNotFound, got %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(ctx, pipeline, true); err != nil {
		t.Fatalf("failed to recreate pipeline: %v", err)
	}
	if err := engine.DeletePipelineDefinition(ctx, "pipeline_one"); err != nil {
		t.Fatalf("DeletePipelineDefinition(existing) failed: %v", err)
	}
	if _, err := data.NewAgentService(db, "system").GetPipelineDefinition(ctx, "pipeline_one"); err == nil {
		t.Fatalf("expected deleted definition lookup to fail")
	}
}

func TestPipelineTriggerRecordCompletionAndFanOutFailure(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service}

	if _, _, err := engine.TriggerPipeline(ctx, "missing", nil); !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("TriggerPipeline missing = %v, want ErrPipelineNotFound", err)
	}

	engine.upsertPipelineInMemory(Pipeline{ID: "empty", Name: "Empty"})
	if _, _, err := engine.TriggerPipeline(ctx, "empty", nil); err == nil || !strings.Contains(err.Error(), "no steps") {
		t.Fatalf("expected no steps error, got %v", err)
	}

	engine.upsertPipelineInMemory(Pipeline{
		ID:   "manual_pipeline",
		Name: "Manual Pipeline",
		Steps: []PipelineStep{
			{
				OnMethod:   "seed_method",
				OnStatus:   "succeeded",
				FromRole:   "source_role",
				ToRole:     "missing_role",
				NextMethod: "next_method",
				ParamMap:   map[string]string{"$": "payload"},
			},
		},
	})

	triggerJobID, runID, err := engine.TriggerPipeline(ctx, "manual_pipeline", map[string]any{"x": "y"})
	if err != nil {
		t.Fatalf("TriggerPipeline(manual_pipeline) failed: %v", err)
	}
	if !strings.HasPrefix(triggerJobID, "manual-") || runID == "" {
		t.Fatalf("unexpected trigger response: triggerJobID=%q runID=%q", triggerJobID, runID)
	}
	engine.WaitInFlight()

	run, err := data.NewAgentService(db, "system").GetPipelineRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if run.TriggerJobID != triggerJobID {
		t.Fatalf("run.TriggerJobID = %q, want %q", run.TriggerJobID, triggerJobID)
	}
	if run.Status != "failed" {
		t.Fatalf("expected manual run to fail without target capability, got %q", run.Status)
	}

	engine.upsertPipelineInMemory(Pipeline{
		ID:   "record_pipeline",
		Name: "Record Pipeline",
		Steps: []PipelineStep{
			{
				OnMethod:   "incoming_method",
				OnStatus:   "succeeded",
				FromRole:   "*",
				ToRole:     "missing_role",
				NextMethod: "next_method",
			},
		},
	})

	job := map[string]any{
		"id":     "job-1",
		"status": "succeeded",
		"request": map[string]any{
			"method": "incoming_method",
		},
		"result": map[string]any{"hello": "world"},
	}
	engine.RecordCompletion(job)
	engine.RecordCompletion(job) // duplicate should be ignored
	engine.RecordCompletion(map[string]any{
		"id":     "job-2",
		"status": "queued",
		"request": map[string]any{
			"method": "incoming_method",
		},
	})

	recordRuns, err := db.Table(data.PipelineRun{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"pipeline_id": "record_pipeline"},
	})
	if err != nil {
		t.Fatalf("query record runs failed: %v", err)
	}
	if len(recordRuns) != 1 {
		t.Fatalf("expected 1 run for record_pipeline, got %d", len(recordRuns))
	}

	systemSvc := data.NewAgentService(db, "system")
	fanOutRun := &data.PipelineRun{
		ID:           "fanout-run",
		PipelineID:   "fanout-pipe",
		TriggerJobID: "trigger-1",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, fanOutRun); err != nil {
		t.Fatalf("CreatePipelineRun fanout failed: %v", err)
	}
	engine.executeFanOutStep(ctx, systemSvc, fanOutRun, PipelineStep{
		OnMethod:   "seed",
		ToRole:     "missing_role",
		NextMethod: "next",
		FanOut:     true,
		FanOutKey:  "items",
	}, 0, map[string]any{"items": "not-an-array"}, nil)
	failedRun, err := systemSvc.GetPipelineRun(ctx, fanOutRun.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun fanout failed: %v", err)
	}
	if failedRun.Status != "failed" {
		t.Fatalf("fan-out failure should mark run failed, got %q", failedRun.Status)
	}

	// Builtin fan-out payloads may provide []map[string]any directly.
	// This should be accepted as a valid array shape.
	fanOutRunTyped := &data.PipelineRun{
		ID:           "fanout-run-typed",
		PipelineID:   "fanout-pipe",
		TriggerJobID: "trigger-2",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, fanOutRunTyped); err != nil {
		t.Fatalf("CreatePipelineRun fanout typed failed: %v", err)
	}
	engine.executeFanOutStep(ctx, systemSvc, fanOutRunTyped, PipelineStep{
		OnMethod:   "seed",
		ToAgentID:  "fanout-agent-1",
		NextMethod: "next",
		FanOut:     true,
		FanOutKey:  "items",
		ParamMap:   map[string]string{"$": "payload"},
	}, 0, map[string]any{
		"items": []map[string]any{
			{"id": "a"},
		},
	}, nil)
	typedRunAfter, err := systemSvc.GetPipelineRun(ctx, fanOutRunTyped.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun fanout typed failed: %v", err)
	}
	if typedRunAfter.Status == "failed" {
		t.Fatalf("typed fan-out payload should not fail run")
	}
}

func TestPipelineTriggerNextStepsAndRunQueries(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service}
	systemSvc := data.NewAgentService(db, "system")

	engine.upsertPipelineInMemory(Pipeline{
		ID:   "single_step",
		Name: "Single Step",
		Steps: []PipelineStep{
			{OnMethod: "seed", ToRole: "x", NextMethod: "y"},
		},
	})
	run := &data.PipelineRun{
		ID:           "run-1",
		PipelineID:   "single_step",
		TriggerJobID: "trigger-1",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun run-1 failed: %v", err)
	}

	completedStep := &data.PipelineStepRun{
		ID:          "step-1",
		RunID:       "run-1",
		StepIndex:   0,
		A2AJobID:    "job-1",
		Status:      "succeeded",
		StartedAt:   time.Now().Add(-30 * time.Second),
		CompletedAt: time.Now(),
	}
	if err := systemSvc.CreateStepRun(ctx, completedStep); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	engine.triggerNextSteps(ctx, systemSvc, completedStep, map[string]any{"result": map[string]any{"ok": true}})
	updatedRun, err := systemSvc.GetPipelineRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetPipelineRun run-1 failed: %v", err)
	}
	if updatedRun.Status != "completed" {
		t.Fatalf("expected completed run status, got %q", updatedRun.Status)
	}

	latestRun := &data.PipelineRun{
		ID:           "run-2",
		PipelineID:   "other",
		TriggerJobID: "trigger-2",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, latestRun); err != nil {
		t.Fatalf("CreatePipelineRun run-2 failed: %v", err)
	}
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:        "step-2",
		RunID:     "run-2",
		StepIndex: 1,
		A2AJobID:  "job-2",
		Status:    "running",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateStepRun step-2 failed: %v", err)
	}
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:        "step-3",
		RunID:     "run-2",
		StepIndex: 0,
		A2AJobID:  "job-3",
		Status:    "running",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateStepRun step-3 failed: %v", err)
	}

	runs, err := engine.GetPipelineRuns(ctx, 1)
	if err != nil {
		t.Fatalf("GetPipelineRuns failed: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-2" {
		t.Fatalf("expected newest run only, got %+v", runs)
	}

	_, steps, err := engine.GetPipelineRunDetail(ctx, "run-2")
	if err != nil {
		t.Fatalf("GetPipelineRunDetail failed: %v", err)
	}
	if len(steps) != 2 || steps[0].StepIndex != 0 || steps[1].StepIndex != 1 {
		t.Fatalf("expected step runs ordered by step_index, got %+v", steps)
	}
}

func TestPipelineCheckStepRunCompletionMissingJobFailsRun(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service}
	systemSvc := data.NewAgentService(db, "system")

	run := &data.PipelineRun{
		ID:           "run-missing-job",
		PipelineID:   "pipe-missing-job",
		TriggerJobID: "trigger-missing-job",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	stepRun := &data.PipelineStepRun{
		ID:        "step-missing-job",
		RunID:     run.ID,
		StepIndex: 0,
		A2AJobID:  "missing-job",
		Status:    "running",
		StartedAt: time.Now().Add(-30 * time.Second),
	}
	if err := systemSvc.CreateStepRun(ctx, stepRun); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	engine.checkStepRunCompletion(ctx, stepRun)

	var storedStep data.PipelineStepRun
	if err := db.Table(data.PipelineStepRun{}).Get(ctx, stepRun.ID, &storedStep); err != nil {
		t.Fatalf("Get step run failed: %v", err)
	}
	if storedStep.Status != "failed" {
		t.Fatalf("step run status = %q, want failed", storedStep.Status)
	}
	if storedStep.CompletedAt.IsZero() {
		t.Fatalf("step run completed_at should be set")
	}

	updatedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if updatedRun.Status != "failed" {
		t.Fatalf("run status = %q, want failed", updatedRun.Status)
	}
	if updatedRun.FailureReason != "pipeline step job not found" {
		t.Fatalf("run failure reason = %q, want %q", updatedRun.FailureReason, "pipeline step job not found")
	}
}

func TestPipelineCheckStepRunCompletionRedeliversQueuedJob(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	sender := &stubHeartbeatSender{}
	engine := &PipelineEngine{
		db:              db,
		service:         service,
		localQueue:      newLocalA2AQueue(db),
		heartbeatSender: sender,
	}
	systemSvc := data.NewAgentService(db, "system")

	engine.upsertPipelineInMemory(Pipeline{
		ID:   "pipe-redeliver",
		Name: "Pipe Redeliver",
		Steps: []PipelineStep{
			{
				OnMethod:   "trigger",
				ToAgentID:  "target-agent",
				NextMethod: "do_work",
			},
		},
	})

	run := &data.PipelineRun{
		ID:           "run-redeliver",
		PipelineID:   "pipe-redeliver",
		TriggerJobID: "trigger-redeliver",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	jobResult, _, err := engine.localQueueOrDefault().Submit(ctx, "pipeline:"+run.ID, "target-agent", "", localA2ARequest{
		Method: "do_work",
		Params: map[string]any{"input": "value"},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	jobID, _ := jobResult["job_id"].(string)
	if jobID == "" {
		t.Fatal("expected job_id")
	}

	stepRun := &data.PipelineStepRun{
		ID:        "step-redeliver",
		RunID:     run.ID,
		StepIndex: 0,
		A2AJobID:  jobID,
		Status:    "running",
		StartedAt: time.Now().Add(-30 * time.Second),
	}
	if err := systemSvc.CreateStepRun(ctx, stepRun); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	engine.checkStepRunCompletion(ctx, stepRun)

	if len(sender.calls) != 1 {
		t.Fatalf("expected one heartbeat delivery, got %d", len(sender.calls))
	}
	if sender.calls[0].agentID != "target-agent" {
		t.Fatalf("expected delivery to target-agent, got %q", sender.calls[0].agentID)
	}

	job, err := engine.localQueueOrDefault().GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if status, _ := job["status"].(string); status != localA2AStatusClaimed {
		t.Fatalf("expected claimed after redelivery, got %q", status)
	}
	if toAgent, _ := job["to_public_key"].(string); toAgent != "target-agent" {
		t.Fatalf("expected claimed agent target-agent, got %q", toAgent)
	}
}

func TestPipelineRunLoopStopsOnContextCancel(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{
		db:           db,
		service:      service,
		pollInterval: 5 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		engine.Run(ctx)
		close(done)
	}()

	time.Sleep(15 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("pipeline run loop did not stop after context cancel")
	}
}

func TestPipelineCallbackVerifierAndHandler(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	keyID := gowild_agent_net.EncodePublicKey(pubKey)
	verifier := &a2aCallbackVerifier{
		allowedKeys:  map[string]ed25519.PublicKey{keyID: pubKey},
		maxClockSkew: 5 * time.Minute,
	}

	body := []byte(`{"job_id":"job-1","status":"succeeded","request":{"method":"seed"}}`)
	req := signedCallbackRequest(t, privKey, keyID, "/pipeline/callbacks/a2a?source=test", body)
	if err := verifier.Verify(req, body); err != nil {
		t.Fatalf("verifier.Verify(valid) failed: %v", err)
	}

	badReq := signedCallbackRequest(t, privKey, keyID, "/pipeline/callbacks/a2a", body)
	badReq.Header.Set("X-A2A-Sig", "not-a-signature")
	if err := verifier.Verify(badReq, body); err == nil {
		t.Fatalf("expected invalid signature encoding error")
	}

	skewReq := signedCallbackRequest(t, privKey, keyID, "/pipeline/callbacks/a2a", body)
	oldTS := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	oldSig := gowild_agent_net.SignRequest(privKey, http.MethodPost, "/pipeline/callbacks/a2a", oldTS, body)
	skewReq.Header.Set("X-A2A-Timestamp", oldTS)
	skewReq.Header.Set("X-A2A-Sig", gowild_agent_net.EncodeSignature(oldSig))
	if err := verifier.Verify(skewReq, body); err == nil || !strings.Contains(err.Error(), "skew") {
		t.Fatalf("expected skew validation error, got %v", err)
	}

	engine := &PipelineEngine{callbackVerifier: verifier}
	successReq := signedCallbackRequest(t, privKey, keyID, "/pipeline/callbacks/a2a", body)
	successReq.Header.Set("X-A2A-Job-ID", "job-1")
	successRec := httptest.NewRecorder()
	engine.HandleA2ACallback(successRec, successReq)
	if successRec.Code != http.StatusOK {
		t.Fatalf("HandleA2ACallback success status = %d, body=%s", successRec.Code, successRec.Body.String())
	}

	mismatchReq := signedCallbackRequest(t, privKey, keyID, "/pipeline/callbacks/a2a", body)
	mismatchReq.Header.Set("X-A2A-Job-ID", "different-job")
	mismatchRec := httptest.NewRecorder()
	engine.HandleA2ACallback(mismatchRec, mismatchReq)
	if mismatchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected mismatch status %d, got %d", http.StatusBadRequest, mismatchRec.Code)
	}

	methodReq := httptest.NewRequest(http.MethodGet, "/pipeline/callbacks/a2a", nil)
	methodRec := httptest.NewRecorder()
	engine.HandleA2ACallback(methodRec, methodReq)
	if methodRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected method not allowed, got %d", methodRec.Code)
	}

	errEngine := &PipelineEngine{callbackVerifierErr: errors.New("bad verifier")}
	if err := errEngine.verifyA2ACallbackRequest(successReq, body); err == nil || !strings.Contains(err.Error(), "bad verifier") {
		t.Fatalf("expected verifier setup error, got %v", err)
	}
	if err := (&PipelineEngine{}).verifyA2ACallbackRequest(successReq, body); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected callback verifier not configured error, got %v", err)
	}
}

func TestPipelineCallbackConfigHelpers(t *testing.T) {
	pub1, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey pub1 failed: %v", err)
	}
	pub2, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey pub2 failed: %v", err)
	}
	key1 := gowild_agent_net.EncodePublicKey(pub1)
	key2 := gowild_agent_net.EncodePublicKey(pub2)

	t.Setenv("A2A_CALLBACK_ALLOWED_KEY_IDS", key1+", "+key2+"\n")
	keys, err := loadA2ACallbackAllowedKeysFromEnv()
	if err != nil {
		t.Fatalf("loadA2ACallbackAllowedKeysFromEnv failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 callback keys, got %d", len(keys))
	}

	t.Setenv("A2A_CALLBACK_ALLOWED_KEY_IDS", "not-a-key")
	if _, err := loadA2ACallbackAllowedKeysFromEnv(); err == nil {
		t.Fatalf("expected invalid key parse error")
	}

	t.Setenv("A2A_CALLBACK_MAX_SKEW_SECONDS", "")
	defaultSkew, err := parseA2ACallbackMaxClockSkew()
	if err != nil || defaultSkew != 5*time.Minute {
		t.Fatalf("default skew = %s, err=%v, want 5m nil", defaultSkew, err)
	}

	t.Setenv("A2A_CALLBACK_MAX_SKEW_SECONDS", "15")
	customSkew, err := parseA2ACallbackMaxClockSkew()
	if err != nil || customSkew != 15*time.Second {
		t.Fatalf("custom skew = %s, err=%v, want 15s nil", customSkew, err)
	}

	t.Setenv("A2A_CALLBACK_MAX_SKEW_SECONDS", "bad")
	if _, err := parseA2ACallbackMaxClockSkew(); err == nil {
		t.Fatalf("expected parse error for invalid skew")
	}

	t.Setenv("A2A_CALLBACK_MAX_SKEW_SECONDS", "0")
	if _, err := parseA2ACallbackMaxClockSkew(); err == nil {
		t.Fatalf("expected >0 validation error for skew")
	}

	t.Setenv("PIPELINE_CALLBACK_URL", "https://manager.example/custom/callback")
	t.Setenv("INGRESS_PUBLIC_URL", "https://ignored.example")
	gotURL, err := pipelineA2ACallbackURLWithError()
	if err != nil || gotURL != "https://manager.example/custom/callback" {
		t.Fatalf("pipelineA2ACallbackURLWithError explicit URL = %q, err=%v", gotURL, err)
	}

	t.Setenv("PIPELINE_CALLBACK_URL", "")
	t.Setenv("INGRESS_PUBLIC_URL", "https://ingress.example/base")
	gotURL, err = pipelineA2ACallbackURLWithError()
	if err != nil || gotURL != "https://ingress.example/base/ingress/callbacks/a2a" {
		t.Fatalf("pipelineA2ACallbackURLWithError derived URL = %q, err=%v", gotURL, err)
	}

	t.Setenv("INGRESS_PUBLIC_URL", "https://ingress.example/base?x=1")
	if _, err := pipelineA2ACallbackURLWithError(); err == nil {
		t.Fatalf("expected query string validation error for INGRESS_PUBLIC_URL")
	}

	parsed, err := parseHTTPSURL(" https://example.com/path ")
	if err != nil || parsed.Scheme != "https" || parsed.Host != "example.com" {
		t.Fatalf("parseHTTPSURL valid = %v, err=%v", parsed, err)
	}
	if _, err := parseHTTPSURL("http://example.com"); err == nil {
		t.Fatalf("expected https-only validation error")
	}
	if _, err := parseHTTPSURL("https:///missing-host"); err == nil {
		t.Fatalf("expected missing host validation error")
	}

	req := httptest.NewRequest(http.MethodPost, "https://manager.example/pipeline/callbacks/a2a?x=1", nil)
	if got := callbackSignaturePath(req); got != "/pipeline/callbacks/a2a?x=1" {
		t.Fatalf("callbackSignaturePath = %q, want %q", got, "/pipeline/callbacks/a2a?x=1")
	}
	req.URL.Path = ""
	req.URL.RawQuery = ""
	if got := callbackSignaturePath(req); got != "/" {
		t.Fatalf("callbackSignaturePath empty path = %q, want %q", got, "/")
	}

	t.Setenv("A2A_CALLBACK_ALLOWED_KEY_IDS", key1)
	t.Setenv("A2A_CALLBACK_MAX_SKEW_SECONDS", "20")
	verifier, err := loadA2ACallbackVerifierFromEnv()
	if err != nil || verifier == nil {
		t.Fatalf("loadA2ACallbackVerifierFromEnv = %v, err=%v", verifier, err)
	}
	if verifier.maxClockSkew != 20*time.Second {
		t.Fatalf("verifier.maxClockSkew = %s, want %s", verifier.maxClockSkew, 20*time.Second)
	}

	t.Setenv("PIPELINE_CALLBACK_URL", "https://ingress.example/ingress/callbacks/a2a")
	callbackURL, allowedCount, err := validatePipelineCallbackConfiguration()
	if err != nil {
		t.Fatalf("validatePipelineCallbackConfiguration failed: %v", err)
	}
	if callbackURL == "" || allowedCount != 1 {
		t.Fatalf("unexpected callback config result: callbackURL=%q allowedCount=%d", callbackURL, allowedCount)
	}

	t.Setenv("A2A_CALLBACK_ALLOWED_KEY_IDS", "")
	t.Setenv("PIPELINE_CALLBACK_URL", "")
	t.Setenv("INGRESS_PUBLIC_URL", "")
	callbackURL, allowedCount, err = validatePipelineCallbackConfiguration()
	if err != nil {
		t.Fatalf("validatePipelineCallbackConfiguration empty config failed: %v", err)
	}
	if callbackURL != "" || allowedCount != 0 {
		t.Fatalf("expected empty callback config, got callbackURL=%q allowedCount=%d", callbackURL, allowedCount)
	}
}

func TestPipelineFanOutBranchFailureDoesNotKillRun(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service}
	systemSvc := data.NewAgentService(db, "system")

	// Create a 2-step pipeline: step 0 fans out, step 1 runs per branch.
	engine.upsertPipelineInMemory(Pipeline{
		ID:   "fanout_pipeline",
		Name: "Fan-Out Pipeline",
		Steps: []PipelineStep{
			{OnMethod: "trigger", ToRole: "scout", NextMethod: "find_markets", ParamMap: map[string]string{"$": "params"}},
			{OnMethod: "find_markets", ToRole: "trader", NextMethod: "execute", FanOut: true, FanOutKey: "markets", ParamMap: map[string]string{"$": "market"}},
		},
	})

	// Create a running pipeline run at step 1 (post fan-out).
	run := &data.PipelineRun{
		ID:           "fanout-run-1",
		PipelineID:   "fanout_pipeline",
		TriggerJobID: "trigger-1",
		CurrentStep:  1,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	// Create 3 fan-out step runs at step index 1.
	// Branch A: still running
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:        "branch-a",
		RunID:     run.ID,
		StepIndex: 1,
		A2AJobID:  "job-a",
		Status:    "running",
		StartedAt: time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun branch-a failed: %v", err)
	}
	// Branch B: failed (filtered out)
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "branch-b",
		RunID:       run.ID,
		StepIndex:   1,
		A2AJobID:    "job-b",
		Status:      "failed",
		StartedAt:   time.Now().Add(-30 * time.Second),
		CompletedAt: time.Now().Add(-10 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun branch-b failed: %v", err)
	}
	// Branch C: failed (filtered out)
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "branch-c",
		RunID:       run.ID,
		StepIndex:   1,
		A2AJobID:    "job-c",
		Status:      "failed",
		StartedAt:   time.Now().Add(-30 * time.Second),
		CompletedAt: time.Now().Add(-5 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun branch-c failed: %v", err)
	}

	// When branch B fails, resolveRunStatus should see branch A still running → do nothing.
	branchB := &data.PipelineStepRun{
		ID:        "branch-b",
		RunID:     run.ID,
		StepIndex: 1,
		A2AJobID:  "job-b",
		Status:    "failed",
	}
	engine.resolveRunStatus(ctx, systemSvc, branchB.RunID)

	updatedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if updatedRun.Status != "running" {
		t.Fatalf("run should stay running while branch-a is active, got %q", updatedRun.Status)
	}

	// Now mark branch A as succeeded (reached final step).
	branchA := &data.PipelineStepRun{
		ID:          "branch-a",
		RunID:       run.ID,
		StepIndex:   1,
		A2AJobID:    "job-a",
		Status:      "succeeded",
		StartedAt:   time.Now().Add(-30 * time.Second),
		CompletedAt: time.Now(),
	}
	if err := systemSvc.UpdateStepRun(ctx, branchA); err != nil {
		t.Fatalf("UpdateStepRun branch-a failed: %v", err)
	}

	// Resolve again — all done, branch A succeeded at the final step → run completes.
	engine.resolveRunStatus(ctx, systemSvc, run.ID)

	finalRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun final failed: %v", err)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("run should complete when at least one branch succeeds at final step, got %q", finalRun.Status)
	}

	// Test the all-branches-fail case: create a new run where all branches fail.
	allFailRun := &data.PipelineRun{
		ID:           "fanout-run-allfail",
		PipelineID:   "fanout_pipeline",
		TriggerJobID: "trigger-2",
		CurrentStep:  1,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, allFailRun); err != nil {
		t.Fatalf("CreatePipelineRun all-fail failed: %v", err)
	}
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "allfail-1",
		RunID:       allFailRun.ID,
		StepIndex:   1,
		A2AJobID:    "job-af1",
		Status:      "failed",
		StartedAt:   time.Now().Add(-20 * time.Second),
		CompletedAt: time.Now().Add(-10 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun allfail-1 failed: %v", err)
	}
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "allfail-2",
		RunID:       allFailRun.ID,
		StepIndex:   1,
		A2AJobID:    "job-af2",
		Status:      "failed",
		StartedAt:   time.Now().Add(-20 * time.Second),
		CompletedAt: time.Now().Add(-5 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun allfail-2 failed: %v", err)
	}

	engine.resolveRunStatus(ctx, systemSvc, allFailRun.ID)

	allFailResult, err := systemSvc.GetPipelineRun(ctx, allFailRun.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun all-fail failed: %v", err)
	}
	if allFailResult.Status != "failed" {
		t.Fatalf("run should fail when all branches fail, got %q", allFailResult.Status)
	}
	if allFailResult.FailureReason == "" {
		t.Fatalf("expected failure reason to be set")
	}
}

func TestResolveRunStatusQueuedPreventsCompletion(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service, localQueue: newLocalA2AQueue(db)}
	systemSvc := data.NewAgentService(db, "system")

	// 2-step pipeline (step 0 -> step 1)
	engine.upsertPipelineInMemory(Pipeline{
		ID:   "queued_test_pipeline",
		Name: "queued test",
		Steps: []PipelineStep{
			{NextMethod: "step_zero"},
			{NextMethod: "step_one"},
		},
	})

	run := &data.PipelineRun{
		ID:          "queued-run-1",
		PipelineID:  "queued_test_pipeline",
		CurrentStep: 0,
		Status:      "running",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	// Simulate fan-out: one branch already completed both steps, but
	// another branch is still queued at step 0.
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID: "qr-step0-done", RunID: run.ID, StepIndex: 0,
		A2AJobID: "j1", Status: "succeeded",
		StartedAt: time.Now().Add(-30 * time.Second), CompletedAt: time.Now().Add(-20 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID: "qr-step1-done", RunID: run.ID, StepIndex: 1,
		A2AJobID: "j2", Status: "succeeded",
		StartedAt: time.Now().Add(-20 * time.Second), CompletedAt: time.Now().Add(-10 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}
	// Second branch still queued.
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID: "qr-step0-queued", RunID: run.ID, StepIndex: 0,
		Status: "queued",
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	// resolveRunStatus should NOT mark the run completed because of the queued step run.
	engine.resolveRunStatus(ctx, systemSvc, run.ID)

	updatedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun failed: %v", err)
	}
	if updatedRun.Status != "running" {
		t.Fatalf("run should stay running while queued step runs exist, got %q", updatedRun.Status)
	}

	// Now mark the queued step run as succeeded (simulate completion).
	queuedSR := &data.PipelineStepRun{
		ID: "qr-step0-queued", RunID: run.ID, StepIndex: 0,
		A2AJobID: "j3", Status: "succeeded",
		StartedAt: time.Now().Add(-5 * time.Second), CompletedAt: time.Now(),
	}
	if err := systemSvc.UpdateStepRun(ctx, queuedSR); err != nil {
		t.Fatalf("UpdateStepRun failed: %v", err)
	}
	// Also need its step 1 to complete.
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID: "qr-step1-done-2", RunID: run.ID, StepIndex: 1,
		A2AJobID: "j4", Status: "succeeded",
		StartedAt: time.Now().Add(-3 * time.Second), CompletedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	// Now all step runs are terminal — should complete.
	engine.resolveRunStatus(ctx, systemSvc, run.ID)

	finalRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun final failed: %v", err)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("run should complete when all step runs are terminal, got %q", finalRun.Status)
	}
}

func TestRecordCompletionAdvancesStepRun(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service, localQueue: newLocalA2AQueue(db)}
	systemSvc := data.NewAgentService(db, "system")

	// 1-step pipeline so step 0 completion resolves the run.
	engine.upsertPipelineInMemory(Pipeline{
		ID:   "record_test",
		Name: "Record Test",
		Steps: []PipelineStep{
			{OnMethod: "trigger", ToRole: "x", NextMethod: "do_work"},
		},
	})

	// Create a pipeline run and step run.
	run := &data.PipelineRun{
		ID:           "record-run",
		PipelineID:   "record_test",
		TriggerJobID: "trigger-job",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun: %v", err)
	}

	// Submit a real A2A job so CompleteJob works.
	queue := engine.localQueueOrDefault()
	jobResult, _, err := queue.Submit(ctx, "pipeline:record-run", "target-agent", "", localA2ARequest{
		Method: "do_work",
		Params: map[string]any{"input": "data"},
	})
	if err != nil {
		t.Fatalf("Submit job: %v", err)
	}
	jobID, _ := jobResult["job_id"].(string)
	if jobID == "" {
		t.Fatal("expected job_id")
	}

	// Create the step run linked to this job.
	stepRun := &data.PipelineStepRun{
		ID:        "record-step",
		RunID:     run.ID,
		StepIndex: 0,
		A2AJobID:  jobID,
		Status:    "running",
		StartedAt: time.Now().Add(-10 * time.Second),
	}
	if err := systemSvc.CreateStepRun(ctx, stepRun); err != nil {
		t.Fatalf("CreateStepRun: %v", err)
	}

	// Claim the job (simulating deliverPipelineStepJob).
	_, err = queue.ClaimJob(ctx, "target-agent", jobID, 600)
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}

	// Complete the job (simulating agent calling job_result via broker).
	completedJob, err := queue.CompleteJob(ctx, "target-agent", jobID, "succeeded", map[string]any{"markets": []any{"m1", "m2"}}, nil)
	if err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	// This is what the broker does after CompleteJob:
	engine.RecordCompletion(completedJob)

	// Verify the step run was updated.
	var storedStep data.PipelineStepRun
	if err := db.Table(data.PipelineStepRun{}).Get(ctx, stepRun.ID, &storedStep); err != nil {
		t.Fatalf("Get step run: %v", err)
	}
	if storedStep.Status != "succeeded" {
		t.Fatalf("step run status = %q, want succeeded", storedStep.Status)
	}

	// Verify the pipeline run resolved.
	updatedRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun: %v", err)
	}
	if updatedRun.Status != "completed" {
		t.Fatalf("run status = %q, want completed", updatedRun.Status)
	}
}

func TestRecordCompletionPayloadFailedStopsOnlyThatFanoutBranch(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	engine := &PipelineEngine{db: db, service: service, localQueue: newLocalA2AQueue(db)}
	systemSvc := data.NewAgentService(db, "system")

	engine.upsertPipelineInMemory(Pipeline{
		ID:   "payload_failed_fanout",
		Name: "Payload Failed Fanout",
		Steps: []PipelineStep{
			{OnMethod: "trigger", ToAgentID: "collector-agent", NextMethod: "collect"},
			{OnMethod: "collect", ToAgentID: "worker-agent", NextMethod: "trade", FanOut: true, FanOutKey: "markets"},
			{OnMethod: "trade", ToAgentID: "settler-agent", NextMethod: "settle"},
		},
	})

	run := &data.PipelineRun{
		ID:           "payload-failed-run",
		PipelineID:   "payload_failed_fanout",
		TriggerJobID: "trigger-job",
		CurrentStep:  1,
		Status:       "running",
		CreatedAt:    time.Now().Add(-time.Minute),
		UpdatedAt:    time.Now().Add(-time.Minute),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun: %v", err)
	}

	queue := engine.localQueueOrDefault()
	jobA, _, err := queue.Submit(ctx, "pipeline:"+run.ID, "worker-a", "", localA2ARequest{
		Method: "trade",
		Params: map[string]any{"market": "A"},
	})
	if err != nil {
		t.Fatalf("Submit branch A: %v", err)
	}
	jobAID, _ := jobA["job_id"].(string)

	jobB, _, err := queue.Submit(ctx, "pipeline:"+run.ID, "worker-b", "", localA2ARequest{
		Method: "trade",
		Params: map[string]any{"market": "B"},
	})
	if err != nil {
		t.Fatalf("Submit branch B: %v", err)
	}
	jobBID, _ := jobB["job_id"].(string)

	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:        "payload-branch-a",
		RunID:     run.ID,
		StepIndex: 1,
		A2AJobID:  jobAID,
		Status:    "running",
		StartedAt: time.Now().Add(-20 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun branch A: %v", err)
	}
	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:        "payload-branch-b",
		RunID:     run.ID,
		StepIndex: 1,
		A2AJobID:  jobBID,
		Status:    "running",
		StartedAt: time.Now().Add(-20 * time.Second),
	}); err != nil {
		t.Fatalf("CreateStepRun branch B: %v", err)
	}

	if _, err := queue.ClaimJob(ctx, "worker-a", jobAID, 600); err != nil {
		t.Fatalf("ClaimJob branch A: %v", err)
	}
	if _, err := queue.ClaimJob(ctx, "worker-b", jobBID, 600); err != nil {
		t.Fatalf("ClaimJob branch B: %v", err)
	}

	completedA, err := queue.CompleteJob(ctx, "worker-a", jobAID, "succeeded", map[string]any{
		"result": map[string]any{
			"status": "FAILED",
			"reason": "policy blocked",
		},
	}, nil)
	if err != nil {
		t.Fatalf("CompleteJob branch A: %v", err)
	}
	engine.RecordCompletion(completedA)

	var branchA data.PipelineStepRun
	if err := db.Table(data.PipelineStepRun{}).Get(ctx, "payload-branch-a", &branchA); err != nil {
		t.Fatalf("Get branch A step run: %v", err)
	}
	if branchA.Status != "failed" {
		t.Fatalf("branch A status = %q, want failed", branchA.Status)
	}

	runAfterA, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun after branch A: %v", err)
	}
	if runAfterA.Status != "running" {
		t.Fatalf("run should remain running while sibling branch is active, got %q", runAfterA.Status)
	}

	stepRunsAfterA, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListStepRunsForRun after branch A: %v", err)
	}
	if len(stepRunsAfterA) != 2 {
		t.Fatalf("expected no downstream step for failed branch yet, got %d step runs", len(stepRunsAfterA))
	}

	completedB, err := queue.CompleteJob(ctx, "worker-b", jobBID, "succeeded", map[string]any{
		"status": "SUCCEEDED",
		"market": "B",
	}, nil)
	if err != nil {
		t.Fatalf("CompleteJob branch B: %v", err)
	}
	engine.RecordCompletion(completedB)

	var branchB data.PipelineStepRun
	if err := db.Table(data.PipelineStepRun{}).Get(ctx, "payload-branch-b", &branchB); err != nil {
		t.Fatalf("Get branch B step run: %v", err)
	}
	if branchB.Status != "succeeded" {
		t.Fatalf("branch B status = %q, want succeeded", branchB.Status)
	}

	stepRunsAfterB, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListStepRunsForRun after branch B: %v", err)
	}
	if len(stepRunsAfterB) != 3 {
		t.Fatalf("expected one downstream step for successful branch, got %d step runs", len(stepRunsAfterB))
	}

	var downstream *data.PipelineStepRun
	for i := range stepRunsAfterB {
		if stepRunsAfterB[i].StepIndex == 2 {
			downstream = &stepRunsAfterB[i]
			break
		}
	}
	if downstream == nil {
		t.Fatalf("expected downstream step run for successful branch")
	}

	if _, err := queue.ClaimJob(ctx, "settler-agent", downstream.A2AJobID, 600); err != nil {
		t.Fatalf("ClaimJob downstream: %v", err)
	}
	completedDownstream, err := queue.CompleteJob(ctx, "settler-agent", downstream.A2AJobID, "succeeded", map[string]any{
		"status": "SUCCEEDED",
		"ok":     true,
	}, nil)
	if err != nil {
		t.Fatalf("CompleteJob downstream: %v", err)
	}
	engine.RecordCompletion(completedDownstream)

	finalRun, err := systemSvc.GetPipelineRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRun final: %v", err)
	}
	if finalRun.Status != "completed" {
		t.Fatalf("final run status = %q, want completed", finalRun.Status)
	}
}

// TestTriggerPipelinePropagatesValuesButNotCancellation pins the semantics of
// the context.WithoutCancel(ctx) call inside TriggerPipeline (pipelines_store.go).
// The detached goroutine running executeStep must:
//   - inherit VALUES from the caller's ctx (trace IDs, request-scoped metadata)
//   - NOT inherit the caller's cancellation (client hangup, HTTP request end)
//
// This matters because the change replaced context.Background() with
// context.WithoutCancel(ctx). Background would have failed the "values"
// assertion; forgetting to detach would fail the "no-cancellation" assertion.
// Verified via the claudeCodeRunner injection seam — the runner is called
// inside the goroutine and captures the ctx that was actually threaded through.
func TestTriggerPipelinePropagatesValuesButNotCancellation(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "trigger-ctx-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "trigger_ctx_propagation",
		Name: "Trigger Ctx Propagation",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	type ctxKey string
	const propagationKey ctxKey = "trigger-ctx-propagation-key"

	type observation struct {
		value any
		err   error
	}
	// The runner blocks on runnerEntered being received, then on
	// releaseRunner being sent, before sampling ctx. This closes the race
	// codex-review flagged: sampling ctx.Err() immediately on runner entry
	// would false-pass whenever the goroutine reaches the runner before the
	// test's cancel() fires — so we force cancel() to happen first.
	runnerEntered := make(chan struct{}, 1)
	releaseRunner := make(chan struct{})
	observed := make(chan observation, 1)
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		select {
		case runnerEntered <- struct{}{}:
		default:
		}
		<-releaseRunner
		observed <- observation{value: ctx.Value(propagationKey), err: ctx.Err()}
		return `{"status":"succeeded","result":{"ok":true}}`, nil
	}

	parentCtx, cancel := context.WithCancel(bgCtx)
	parentCtx = context.WithValue(parentCtx, propagationKey, "propagated-value")

	_, _, err = engine.TriggerPipeline(parentCtx, "trigger_ctx_propagation", map[string]any{"condition_id": "cond-1"})
	if err != nil {
		t.Fatalf("TriggerPipeline failed: %v", err)
	}

	// Wait until the runner is definitely in flight before cancelling, so
	// we can be sure the goroutine has received its ctx and is holding it.
	select {
	case <-runnerEntered:
	case <-time.After(5 * time.Second):
		close(releaseRunner)
		cancel()
		t.Fatalf("claudeCodeRunner was not entered within 5s — the goroutine may have died before reaching the runner, or the claude-code dispatch path changed")
	}

	// Cancel the caller's ctx while the runner is parked. A detached
	// goroutine using context.WithoutCancel must ignore this cancellation;
	// passing the raw ctx through would cause the sampled ctx.Err() below
	// to be context.Canceled.
	cancel()

	// Release the runner only AFTER cancel has fired, then observe. This is
	// the load-bearing ordering: the ctx sample must happen strictly after
	// the parent cancellation, not before.
	close(releaseRunner)

	select {
	case got := <-observed:
		if got.value != "propagated-value" {
			t.Fatalf("ctx.Value(propagationKey) inside goroutine = %v, want %q — context.WithoutCancel must preserve values from parent ctx (context.Background would drop them)", got.value, "propagated-value")
		}
		if got.err != nil {
			t.Fatalf("ctx.Err() inside goroutine = %v, want nil — caller cancellation (fired before this sample) must not reach the detached step-execution goroutine", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("observation never arrived after releasing the runner")
	}
}

// TestShutdownCancelsInFlightStepGoroutine verifies that Shutdown signals
// cancellation to a detached pipeline step goroutine (preventing leaks past
// engine shutdown) and then waits for it to exit before returning.
//
// Before the fix, TriggerPipeline launched the step goroutine with
// context.WithoutCancel(ctx) and no engine-level tracking — the goroutine
// would outlive Shutdown with no way to signal it. This test pins that
// guarantee by parking the claude-code runner on a ctx.Done() select so the
// goroutine is still alive when Shutdown is called, then asserting that
// Shutdown returns before a 2s deadline (bounded by the 1s test timeout
// we pass in) and that the runner observed ctx.Err() != nil.
func TestShutdownCancelsInFlightStepGoroutine(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "shutdown-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "shutdown_pipeline",
		Name: "Shutdown Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	runnerEntered := make(chan struct{}, 1)
	runnerCtxErr := make(chan error, 1)
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		select {
		case runnerEntered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		runnerCtxErr <- ctx.Err()
		return "", ctx.Err()
	}

	if _, _, err := engine.TriggerPipeline(bgCtx, "shutdown_pipeline", map[string]any{"condition_id": "cond-1"}); err != nil {
		t.Fatalf("TriggerPipeline: %v", err)
	}

	select {
	case <-runnerEntered:
	case <-time.After(5 * time.Second):
		t.Fatalf("claudeCodeRunner was not entered within 5s — shutdown test cannot proceed")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- engine.Shutdown(shutdownCtx)
	}()

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown returned error: %v (did the goroutine drain?)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Shutdown did not return within 3s — in-flight pipeline goroutine leaked")
	}

	select {
	case ctxErr := <-runnerCtxErr:
		if ctxErr == nil {
			t.Fatalf("runner observed ctx.Err() == nil; Shutdown must cancel the step ctx")
		}
	case <-time.After(time.Second):
		t.Fatalf("runner never reported its ctx.Err() after Shutdown")
	}
}

// TestShutdownReturnsDeadlineErrorWhenStepHangs verifies that Shutdown
// surfaces ctx.Err() when a step goroutine does not honour cancellation in
// time. Without this guarantee, a buggy runner could stall shutdown forever
// — the 30s timeout in main.go relies on Shutdown observing its deadline.
func TestShutdownReturnsDeadlineErrorWhenStepHangs(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "shutdown-stall-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "shutdown_stall_pipeline",
		Name: "Shutdown Stall Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	runnerEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	// Runner ignores ctx.Done() and only exits when the test releases it —
	// simulating a step that blocks past the shutdown deadline.
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		select {
		case runnerEntered <- struct{}{}:
		default:
		}
		<-release
		return "", context.Canceled
	}
	defer close(release)

	if _, _, err := engine.TriggerPipeline(bgCtx, "shutdown_stall_pipeline", map[string]any{"condition_id": "x"}); err != nil {
		t.Fatalf("TriggerPipeline: %v", err)
	}

	select {
	case <-runnerEntered:
	case <-time.After(5 * time.Second):
		t.Fatalf("runner never entered")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = engine.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context.DeadlineExceeded — stalled step must surface the deadline", err)
	}
}

// TestShutdownNilEngineIsSafe guards the nil-receiver check in Shutdown.
// The broker handlers may hold a nil pipelineEngine reference in stripped-
// down test setups; Shutdown should treat that as a no-op rather than panic.
func TestShutdownNilEngineIsSafe(t *testing.T) {
	var engine *PipelineEngine
	if err := engine.Shutdown(context.Background()); err != nil {
		t.Fatalf("nil-receiver Shutdown = %v, want nil", err)
	}
}

// TestShutdownBeforeAnyTrigger verifies that Shutdown works on a live
// non-nil engine that has never had a trigger. Pins the lazy-init path
// (ensureLifecycle must produce a valid runCtx / shutdownDone) and
// catches regressions where the drainer goroutine blocks forever
// because runWG was never touched.
func TestShutdownBeforeAnyTrigger(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown on fresh engine = %v, want nil", err)
	}
}

// TestShutdownRejectsNewTriggers verifies the admission-control fix for
// the WaitGroup race that codex flagged: once Shutdown is called, new
// triggers must be rejected with ErrPipelineEngineShutdown rather than
// racing runWG.Add against the final Wait (which would panic) or leaving
// an orphan "running" PipelineRun row that never completes.
func TestShutdownRejectsNewTriggers(t *testing.T) {
	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "shutdown-reject-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "shutdown_reject_pipeline",
		Name: "Shutdown Reject Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := engine.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	_, _, err = engine.TriggerPipeline(bgCtx, "shutdown_reject_pipeline", map[string]any{"condition_id": "late"})
	if !errors.Is(err, ErrPipelineEngineShutdown) {
		t.Fatalf("TriggerPipeline after Shutdown = %v, want ErrPipelineEngineShutdown", err)
	}

	// Confirm no orphan PipelineRun row was created for the rejected trigger.
	runs, err := engine.GetPipelineRuns(bgCtx, 50)
	if err != nil {
		t.Fatalf("GetPipelineRuns: %v", err)
	}
	for _, r := range runs {
		if r.PipelineID == "shutdown_reject_pipeline" {
			t.Fatalf("rejected trigger still created PipelineRun row: %+v", r)
		}
	}
}

// TestShutdownIdempotentUnderStalledRunner stresses the single-waiter
// guarantee flagged by codex: repeated Shutdown calls must not spawn one
// runWG-waiter goroutine per call. We park a runner on a channel so
// runWG never drains, call Shutdown three times with short deadlines
// (each returning context.DeadlineExceeded), then release the runner and
// verify a final Shutdown with a generous deadline returns nil — proving
// the original drainer goroutine (not a new one per call) observed the
// final runWG.Done().
func TestShutdownIdempotentUnderStalledRunner(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "shutdown-idempotent-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "shutdown_idempotent_pipeline",
		Name: "Shutdown Idempotent Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	runnerEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		select {
		case runnerEntered <- struct{}{}:
		default:
		}
		<-release
		return "", nil
	}

	if _, _, err := engine.TriggerPipeline(bgCtx, "shutdown_idempotent_pipeline", map[string]any{"condition_id": "x"}); err != nil {
		t.Fatalf("TriggerPipeline: %v", err)
	}
	select {
	case <-runnerEntered:
	case <-time.After(5 * time.Second):
		t.Fatalf("runner never entered")
	}

	for i := 0; i < 3; i++ {
		shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := engine.Shutdown(shortCtx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown #%d = %v, want context.DeadlineExceeded while runner is parked", i+1, err)
		}
	}

	close(release)

	finalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Shutdown(finalCtx); err != nil {
		t.Fatalf("final Shutdown after release = %v, want nil (single drainer must have observed the final Done)", err)
	}
}

// TestShutdownConcurrentTriggerSafety races many TriggerPipeline calls
// against Shutdown to exercise the admission-control path under -race.
// Every accepted trigger must successfully complete its goroutine (no
// WaitGroup panic); rejected triggers must return ErrPipelineEngineShutdown.
// Together these guarantees mean the engine never admits work it cannot
// wait for, and never panics from a late runWG.Add.
func TestShutdownConcurrentTriggerSafety(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "shutdown-race-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "shutdown_race_pipeline",
		Name: "Shutdown Race Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	// Runner exits immediately so accepted triggers drain fast.
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		return `{"status":"succeeded","result":{"ok":true}}`, nil
	}

	const triggerCount = 64
	var wg sync.WaitGroup
	var acceptedCount, rejectedCount, otherErrCount int64
	start := make(chan struct{})
	for i := 0; i < triggerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := engine.TriggerPipeline(bgCtx, "shutdown_race_pipeline", map[string]any{"i": "x"})
			switch {
			case err == nil:
				atomic.AddInt64(&acceptedCount, 1)
			case errors.Is(err, ErrPipelineEngineShutdown):
				atomic.AddInt64(&rejectedCount, 1)
			default:
				atomic.AddInt64(&otherErrCount, 1)
			}
		}()
	}

	// Release the trigger storm and call Shutdown concurrently.
	close(start)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown did not drain accepted triggers within deadline: %v (accepted=%d rejected=%d other=%d)",
			err, atomic.LoadInt64(&acceptedCount), atomic.LoadInt64(&rejectedCount), atomic.LoadInt64(&otherErrCount))
	}
	wg.Wait()

	if atomic.LoadInt64(&otherErrCount) != 0 {
		t.Fatalf("unexpected non-shutdown errors from TriggerPipeline: %d", atomic.LoadInt64(&otherErrCount))
	}
	if atomic.LoadInt64(&acceptedCount)+atomic.LoadInt64(&rejectedCount) != triggerCount {
		t.Fatalf("accepted+rejected = %d, want %d",
			atomic.LoadInt64(&acceptedCount)+atomic.LoadInt64(&rejectedCount), triggerCount)
	}
	t.Logf("accepted=%d rejected=%d (total=%d)",
		atomic.LoadInt64(&acceptedCount), atomic.LoadInt64(&rejectedCount), triggerCount)
}

// TestWaitInFlightConcurrentTriggerSafety pins the Add/Wait serialization
// that WaitInFlight needs to avoid "sync: WaitGroup misuse: Add called
// concurrently with Wait". Fires a storm of triggers that race WaitInFlight
// calls on an initially-idle engine — the exact window where a naive
// runWG.Wait() would race tryBeginRun's runWG.Add(1). All accepted
// triggers must complete, no panic, no stray errors. Runs under -race to
// catch the WaitGroup violation if the lock is ever removed.
func TestWaitInFlightConcurrentTriggerSafety(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "waitinflight-race-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "waitinflight_race_pipeline",
		Name: "WaitInFlight Race Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		return `{"status":"succeeded","result":{"ok":true}}`, nil
	}

	const triggerCount = 64
	const waiterCount = 16
	var wg sync.WaitGroup
	start := make(chan struct{})

	var triggerErrs int64
	for i := 0; i < triggerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, _, err := engine.TriggerPipeline(bgCtx, "waitinflight_race_pipeline", map[string]any{"i": "x"}); err != nil {
				atomic.AddInt64(&triggerErrs, 1)
			}
		}()
	}
	for i := 0; i < waiterCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			engine.WaitInFlight()
		}()
	}

	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&triggerErrs); got != 0 {
		t.Fatalf("TriggerPipeline errors under concurrent WaitInFlight: %d", got)
	}

	// After all waiters returned, the final drain must still succeed and
	// the engine must still accept new triggers (WaitInFlight must not
	// set shutdownClosed).
	engine.WaitInFlight()
	if _, _, err := engine.TriggerPipeline(bgCtx, "waitinflight_race_pipeline", map[string]any{"i": "tail"}); err != nil {
		t.Fatalf("post-wait TriggerPipeline failed; WaitInFlight must not close the engine: %v", err)
	}
	engine.WaitInFlight()
}

// TestWaitInFlightNilEngineIsSafe guards the nil-receiver check in
// WaitInFlight. Mirrors TestShutdownNilEngineIsSafe — callers that hold a
// possibly-nil engine reference should be able to invoke the wait as a no-op
// rather than panic.
func TestWaitInFlightNilEngineIsSafe(t *testing.T) {
	var engine *PipelineEngine
	engine.WaitInFlight()
}

// TestWaitInFlightBeforeAnyTrigger verifies WaitInFlight is a no-op on a
// fresh engine that has never accepted a trigger. Pins the lazy-init path
// (ensureLifecycle must produce a valid runCtx) and guards against a
// regression where runWG.Wait blocks forever on a never-Add'd WaitGroup
// (it doesn't — but the test locks the contract so future refactors can't
// break it by, say, pre-Adding a sentinel).
func TestWaitInFlightBeforeAnyTrigger(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	done := make(chan struct{})
	go func() {
		engine.WaitInFlight()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("WaitInFlight on fresh engine did not return within 2s")
	}
}

// TestWaitInFlightWaitsForRunnerWithoutCancelling pins the core contract
// of WaitInFlight: it blocks until in-flight trigger goroutines finish,
// but — unlike Shutdown — does NOT cancel runCtx, so the runner observes
// a live ctx throughout. Tests that assert post-trigger state rely on
// both halves of this contract (block until done, but let the goroutine
// do its work normally).
func TestWaitInFlightWaitsForRunnerWithoutCancelling(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	bgCtx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	targetAgent, err := svc.CreateAgent(bgCtx, "waitinflight-target")
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := engine.UpsertPipelineDefinition(bgCtx, Pipeline{
		ID:   "waitinflight_pipeline",
		Name: "WaitInFlight Pipeline",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: "check_policy",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition: %v", err)
	}

	runnerEntered := make(chan struct{}, 1)
	releaseRunner := make(chan struct{})
	runnerCtxErrAtExit := make(chan error, 1)
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		select {
		case runnerEntered <- struct{}{}:
		default:
		}
		<-releaseRunner
		runnerCtxErrAtExit <- ctx.Err()
		return `{"status":"succeeded","result":{"ok":true}}`, nil
	}

	if _, _, err := engine.TriggerPipeline(bgCtx, "waitinflight_pipeline", map[string]any{"condition_id": "cond-1"}); err != nil {
		t.Fatalf("TriggerPipeline: %v", err)
	}

	select {
	case <-runnerEntered:
	case <-time.After(5 * time.Second):
		t.Fatalf("runner did not enter within 5s — test cannot proceed")
	}

	// WaitInFlight must not return until the runner releases. Kick it off in
	// a goroutine and assert it's still blocked while the runner parks.
	waitReturned := make(chan struct{})
	go func() {
		engine.WaitInFlight()
		close(waitReturned)
	}()

	select {
	case <-waitReturned:
		t.Fatalf("WaitInFlight returned before the runner was released")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseRunner)

	select {
	case <-waitReturned:
	case <-time.After(2 * time.Second):
		t.Fatalf("WaitInFlight did not return within 2s after runner release")
	}

	// Runner must have seen a live ctx throughout — WaitInFlight must not
	// cancel runCtx (that's Shutdown's job).
	select {
	case ctxErr := <-runnerCtxErrAtExit:
		if ctxErr != nil {
			t.Fatalf("runner observed ctx.Err() = %v at exit; WaitInFlight must NOT cancel runCtx", ctxErr)
		}
	case <-time.After(time.Second):
		t.Fatalf("runner never reported its ctx.Err()")
	}

	// Further triggers must still be accepted — the engine was not shut down.
	if _, _, err := engine.TriggerPipeline(bgCtx, "waitinflight_pipeline", map[string]any{"condition_id": "cond-2"}); err != nil {
		t.Fatalf("TriggerPipeline after WaitInFlight failed: %v; the engine must remain open", err)
	}
	// Drain the second trigger so the test cleanup doesn't race an ongoing
	// runner parked on releaseRunner (already closed).
	engine.WaitInFlight()
}

func signedCallbackRequest(t *testing.T, privateKey ed25519.PrivateKey, keyID, pathWithQuery string, body []byte) *http.Request {
	t.Helper()
	timestamp := time.Now().UTC().Format(time.RFC3339)
	sig := gowild_agent_net.SignRequest(privateKey, http.MethodPost, pathWithQuery, timestamp, body)
	req := httptest.NewRequest(http.MethodPost, "https://manager.example"+pathWithQuery, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-A2A-Key-ID", keyID)
	req.Header.Set("X-A2A-Timestamp", timestamp)
	req.Header.Set("X-A2A-Sig", gowild_agent_net.EncodeSignature(sig))
	return req
}
