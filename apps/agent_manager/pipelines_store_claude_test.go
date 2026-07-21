package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestGetPipelineRunDetailEnrichedIncludesClaudeArtifacts(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_detail_test",
		Name: "Claude Detail Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  "agent-claude",
				NextMethod: "analyze_market",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	systemSvc := data.NewAgentService(db, "system")
	run := &data.PipelineRun{
		ID:           "run-claude-detail",
		PipelineID:   "claude_detail_test",
		TriggerJobID: "manual-test",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	engine.recordPipelineJob(
		ctx,
		"claude-job-detail",
		"agent-claude",
		localA2ARequest{
			Method: "analyze_market",
			Params: map[string]any{
				"payload": map[string]any{"condition_id": "cond-1"},
			},
		},
		"succeeded",
		map[string]any{
			"status": "succeeded",
			"result": map[string]any{
				"condition_id": "cond-1",
				"question":     "Will it happen?",
			},
			"event_log":  "ASSISTANT tool_use: polymarket_check_policy\n{\"condition_id\":\"cond-1\"}",
			"raw_output": "{\"status\":\"succeeded\",\"result\":{\"condition_id\":\"cond-1\"}}",
		},
		nil,
		now,
	)

	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "claude-step-detail",
		RunID:       run.ID,
		StepIndex:   0,
		A2AJobID:    "claude-job-detail",
		Status:      "succeeded",
		StartedAt:   now,
		CompletedAt: now,
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	detail, err := engine.GetPipelineRunDetailEnriched(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRunDetailEnriched failed: %v", err)
	}

	steps, ok := detail["steps"].([]enrichedStepRun)
	if !ok {
		t.Fatalf("steps type = %T", detail["steps"])
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}

	step := steps[0]
	if step.Runner != pipelineStepRunnerClaudeCode {
		t.Fatalf("runner = %q, want %q", step.Runner, pipelineStepRunnerClaudeCode)
	}
	if step.AgentID != "agent-claude" {
		t.Fatalf("agent_id = %q", step.AgentID)
	}
	request, ok := step.Request.(map[string]any)
	if !ok {
		t.Fatalf("request type = %T", step.Request)
	}
	params, ok := request["params"].(map[string]any)
	if !ok {
		t.Fatalf("request.params type = %T", request["params"])
	}
	payload, ok := params["payload"].(map[string]any)
	if !ok || payload["condition_id"] != "cond-1" {
		t.Fatalf("request payload = %#v", params["payload"])
	}
	result, ok := step.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", step.Result)
	}
	if result["condition_id"] != "cond-1" {
		t.Fatalf("result.condition_id = %#v", result["condition_id"])
	}
	if step.RawOutput == "" {
		t.Fatalf("expected raw output to be present")
	}
	if step.ClaudeLog == "" {
		t.Fatalf("expected claude log to be present")
	}
}

func TestGetPipelineRunDetailEnrichedRedactsLegacyPolymarketResearchPositionInput(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_detail_redaction_test",
		Name: "Claude Detail Redaction Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  "agent-claude",
				NextMethod: "polymarket_research_position",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "polymarket_research_position", "legacy research method", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	run := &data.PipelineRun{
		ID:           "run-claude-detail-redaction",
		PipelineID:   "claude_detail_redaction_test",
		TriggerJobID: "manual-test",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	engine.recordPipelineJob(
		ctx,
		"claude-job-detail-redaction",
		"agent-claude",
		localA2ARequest{
			Method: "polymarket_research_position",
			Params: map[string]any{
				"payload": map[string]any{
					"condition_id":       "cond-redact",
					"aum":                6067.23,
					"current_position":   306.87,
					"max_allowed":        303.36,
					"remaining_capacity": 0.0,
					"position": map[string]any{
						"curPrice":     0.855,
						"currentValue": 262.37385,
						"avgPrice":     0.89,
						"initialValue": 273.1143,
						"size":         306.87,
						"totalBought":  306.87,
					},
				},
			},
		},
		"failed",
		nil,
		map[string]any{"message": "claude-code failed (exit 1): claude exited with code 1"},
		now,
	)

	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "claude-step-detail-redaction",
		RunID:       run.ID,
		StepIndex:   0,
		A2AJobID:    "claude-job-detail-redaction",
		Status:      "failed",
		StartedAt:   now,
		CompletedAt: now,
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	detail, err := engine.GetPipelineRunDetailEnriched(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRunDetailEnriched failed: %v", err)
	}

	steps, ok := detail["steps"].([]enrichedStepRun)
	if !ok || len(steps) != 1 {
		t.Fatalf("steps = %#v", detail["steps"])
	}
	request, ok := steps[0].Request.(map[string]any)
	if !ok {
		t.Fatalf("request type = %T", steps[0].Request)
	}
	params, ok := request["params"].(map[string]any)
	if !ok {
		t.Fatalf("request.params type = %T", request["params"])
	}
	payload, ok := params["payload"].(map[string]any)
	if !ok {
		t.Fatalf("request.payload type = %T", params["payload"])
	}
	if _, exists := payload["aum"]; exists {
		t.Fatalf("payload.aum should be redacted: %#v", payload["aum"])
	}
	if _, exists := payload["current_position"]; exists {
		t.Fatalf("payload.current_position should be redacted: %#v", payload["current_position"])
	}
	if _, exists := payload["max_allowed"]; exists {
		t.Fatalf("payload.max_allowed should be redacted: %#v", payload["max_allowed"])
	}
	if _, exists := payload["remaining_capacity"]; exists {
		t.Fatalf("payload.remaining_capacity should be redacted: %#v", payload["remaining_capacity"])
	}
	position, ok := payload["position"].(map[string]any)
	if !ok {
		t.Fatalf("payload.position type = %T", payload["position"])
	}
	if _, exists := position["curPrice"]; exists {
		t.Fatalf("position.curPrice should be redacted: %#v", position["curPrice"])
	}
	if _, exists := position["currentValue"]; exists {
		t.Fatalf("position.currentValue should be redacted: %#v", position["currentValue"])
	}
	if _, exists := position["avgPrice"]; exists {
		t.Fatalf("position.avgPrice should be redacted: %#v", position["avgPrice"])
	}
	if _, exists := position["initialValue"]; exists {
		t.Fatalf("position.initialValue should be redacted: %#v", position["initialValue"])
	}
	if _, exists := position["size"]; exists {
		t.Fatalf("position.size should be redacted: %#v", position["size"])
	}
	if _, exists := position["totalBought"]; exists {
		t.Fatalf("position.totalBought should be redacted: %#v", position["totalBought"])
	}
}

func TestGetPipelineRunDetailEnrichedIncludesClaudeFailureArtifacts(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "claude_detail_failure_artifacts_test",
		Name: "Claude Detail Failure Artifacts Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  "agent-claude",
				NextMethod: "analyze_market",
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	now := time.Now().UTC()
	systemSvc := data.NewAgentService(db, "system")
	run := &data.PipelineRun{
		ID:           "run-claude-detail-failure-artifacts",
		PipelineID:   "claude_detail_failure_artifacts_test",
		TriggerJobID: "manual-test",
		CurrentStep:  0,
		Status:       "failed",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	streamOutput := "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"polymarket_get_market\",\"input\":{\"condition_id\":\"cond-debug\"}}]}}"
	engine.recordPipelineJob(
		ctx,
		"claude-job-detail-failure-artifacts",
		"agent-claude",
		localA2ARequest{
			Method: "analyze_market",
			Params: map[string]any{
				"payload": map[string]any{"condition_id": "cond-debug"},
			},
		},
		"failed",
		map[string]any{
			"status":         "failed",
			"failure_reason": "claude-code failed (exit 1): permission denied",
			"raw_output":     streamOutput,
			"stderr":         "permission denied",
			"event_log":      "ASSISTANT tool_use: polymarket_get_market\n{\n  \"condition_id\": \"cond-debug\"\n}",
		},
		map[string]any{
			"message":   "claude-code failed (exit 1): permission denied",
			"stderr":    "permission denied",
			"stdout":    streamOutput,
			"exit_code": 1,
		},
		now,
	)

	if err := systemSvc.CreateStepRun(ctx, &data.PipelineStepRun{
		ID:          "claude-step-detail-failure-artifacts",
		RunID:       run.ID,
		StepIndex:   0,
		A2AJobID:    "claude-job-detail-failure-artifacts",
		Status:      "failed",
		StartedAt:   now,
		CompletedAt: now,
	}); err != nil {
		t.Fatalf("CreateStepRun failed: %v", err)
	}

	detail, err := engine.GetPipelineRunDetailEnriched(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetPipelineRunDetailEnriched failed: %v", err)
	}

	steps, ok := detail["steps"].([]enrichedStepRun)
	if !ok || len(steps) != 1 {
		t.Fatalf("steps = %#v", detail["steps"])
	}
	step := steps[0]
	if step.ClaudeStderr != "permission denied" {
		t.Fatalf("claude_stderr = %q", step.ClaudeStderr)
	}
	if step.RawOutput != streamOutput {
		t.Fatalf("raw_output = %q", step.RawOutput)
	}
	if !strings.Contains(step.ClaudeLog, "polymarket_get_market") {
		t.Fatalf("claude_log = %q", step.ClaudeLog)
	}
	errPayload, ok := step.Error.(map[string]any)
	if !ok {
		t.Fatalf("error type = %T", step.Error)
	}
	if strings.TrimSpace(fmt.Sprint(errPayload["stderr"])) != "permission denied" {
		t.Fatalf("error.stderr = %#v", errPayload["stderr"])
	}
}
