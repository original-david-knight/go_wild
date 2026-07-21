package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	data "github.com/original-david-knight/go_wild/agent_data"
)

// startPipelineFromJob creates a pipeline run and triggers the next step.
func (pe *PipelineEngine) startPipelineFromJob(ctx context.Context, pipeline Pipeline, stepIdx int, job map[string]any) string {
	jobID := pe.extractJobID(job)
	result := pe.extractJobResult(job)

	now := time.Now()
	svc := data.NewAgentService(pe.db, "system")

	run := &data.PipelineRun{
		ID:             uuid.New().String(),
		PipelineID:     pipeline.ID,
		TriggerJobID:   jobID,
		ScopeMode:      pipeline.ScopeMode,
		ScopeCompanyID: pipeline.ScopeCompanyID,
		CurrentStep:    stepIdx,
		Status:         "running",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := svc.CreatePipelineRun(ctx, run); err != nil {
		log.Printf("Pipeline engine: failed to create pipeline run: %v", err)
		return ""
	}

	step := pipeline.Steps[stepIdx]
	pe.executeStep(ctx, svc, run, step, stepIdx, result, nil)
	return run.ID
}

// triggerNextSteps finds and executes the next pipeline step after a completion.
func (pe *PipelineEngine) triggerNextSteps(ctx context.Context, svc *data.AgentService, completedStepRun *data.PipelineStepRun, jobResult map[string]any) {
	// Find the pipeline run
	run, err := svc.GetPipelineRun(ctx, completedStepRun.RunID)
	if err != nil {
		log.Printf("Pipeline engine: failed to get pipeline run %s: %v", completedStepRun.RunID, err)
		return
	}

	// Find the pipeline definition
	pipeline, ok := pe.GetPipeline(run.PipelineID)
	if !ok {
		log.Printf("Pipeline engine: unknown pipeline %s", run.PipelineID)
		return
	}

	nextIdx := completedStepRun.StepIndex + 1

	// Extract the completed job's decoded result object.
	result := pe.extractJobResult(jobResult)

	// Check if there are more steps
	if nextIdx >= len(pipeline.Steps) {
		// This branch reached the end — resolve run status (other branches may still be active)
		pe.resolveRunStatus(ctx, svc, run.ID)
		return
	}

	nextStep := pipeline.Steps[nextIdx]

	// Update run progress
	run.CurrentStep = nextIdx
	run.UpdatedAt = time.Now()
	svc.UpdatePipelineRun(ctx, run)

	pe.executeStep(ctx, svc, run, nextStep, nextIdx, result, nil)
}

// executeStep resolves the target agent, maps params, and submits the A2A job.
func (pe *PipelineEngine) executeStep(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any) {
	if step.FanOut && step.FanOutKey != "" {
		pe.executeFanOutStep(ctx, svc, run, step, stepIdx, result, explicitParams)
		return
	}

	switch pe.effectiveStepRunner(ctx, step) {
	case pipelineStepRunnerBuiltin:
		pe.executeBuiltinStep(ctx, svc, run, step, stepIdx, result, explicitParams)
	case pipelineStepRunnerClaudeCode:
		pe.executeClaudeCodeStep(ctx, svc, run, step, stepIdx, result, explicitParams)
	case pipelineStepRunnerCodex:
		pe.executeCodexStep(ctx, svc, run, step, stepIdx, result, explicitParams)
	default:
		pe.submitStepJob(ctx, svc, run, step, stepIdx, result, explicitParams)
	}
}

// effectiveStepRunner returns the normalized runner for a pipeline step.
// Steps with no explicit runner are treated as the A2A default ("agent").
// Selecting the Claude Code or Codex CLI runners requires setting Runner
// explicitly — we intentionally do not infer them from the agent's
// ModelProvider, because the provider identifies the LLM backend (e.g.
// OpenAI or Anthropic), not whether the pipeline step should shell out to
// a specific CLI tool (Codex is one specific OpenAI CLI; Claude Code is
// one specific Anthropic CLI).
func (pe *PipelineEngine) effectiveStepRunner(ctx context.Context, step PipelineStep) string {
	return normalizePipelineStepRunner(step.Runner)
}

func (pe *PipelineEngine) executeBuiltinStep(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any, existingStepRun ...*data.PipelineStepRun) bool {
	params := explicitParams
	if params == nil {
		params = mapParams(result, step.ParamMap)
	}
	if params == nil {
		params = map[string]any{}
	}
	// Always apply literal injections (=key) from the param map, even when
	// explicit params were provided (e.g. from pipeline trigger).
	for k, v := range step.ParamMap {
		if strings.HasPrefix(k, "=") {
			params[strings.TrimPrefix(k, "=")] = v
		}
	}
	params = sanitizePipelineStepParams(ctx, svc, step.NextMethod, params)

	jobID := "builtin-" + uuid.New().String()
	now := time.Now()

	// Use pre-created step run if provided (fan-out), otherwise create a new one.
	// Fan-out branches must not fail the entire pipeline run on individual failure.
	fanOut := len(existingStepRun) > 0 && existingStepRun[0] != nil
	var stepRun *data.PipelineStepRun
	stepRunPreCreated := false
	if fanOut {
		stepRun = existingStepRun[0]
		stepRun.A2AJobID = jobID
		stepRun.Status = "running"
		stepRun.StartedAt = now
		if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
			log.Printf("Pipeline engine: failed to update pre-created step run to running: %v", err)
		}
		stepRunPreCreated = true
	} else {
		stepRun = &data.PipelineStepRun{
			ID:        uuid.New().String(),
			RunID:     run.ID,
			StepIndex: stepIdx,
			A2AJobID:  jobID,
			Status:    "running",
			StartedAt: now,
		}
	}

	pe.publishBuiltinTerminalRequest(run, step, stepIdx, params)
	methodResult, err := executeBuiltinPipelineMethod(ctx, pe, run, step, params)
	duration := time.Since(now)
	if err != nil {
		pe.publishBuiltinTerminalError(run, step, stepIdx, duration, err)
		stepRun.Status = "failed"
		stepRun.CompletedAt = time.Now()
		if stepRunPreCreated {
			if updateErr := svc.UpdateStepRun(ctx, stepRun); updateErr != nil {
				log.Printf("Pipeline engine: failed to update builtin step failure: %v", updateErr)
			}
		} else if createErr := svc.CreateStepRun(ctx, stepRun); createErr != nil {
			log.Printf("Pipeline engine: failed to record builtin step failure: %v", createErr)
		}
		if !fanOut {
			pe.failPipelineRun(ctx, svc, run.ID, fmt.Sprintf("builtin step %s failed: %v", step.NextMethod, err))
		}
		return false
	}
	if methodResult == nil {
		methodResult = map[string]any{}
	}

	stepRun.CompletedAt = time.Now()

	jobResult := map[string]any{
		"id":     jobID,
		"status": "succeeded",
		"request": map[string]any{
			"method": step.NextMethod,
			"params": params,
		},
		"result": methodResult,
	}
	effectiveStatus, payloadFailureReason := pe.effectiveCompletionStatus(jobResult)
	stepRun.Status = effectiveStatus
	if stepRun.Status == "" {
		stepRun.Status = "failed"
	}
	pe.publishBuiltinTerminalResult(run, step, stepIdx, duration, stepRun.Status, methodResult)
	if stepRunPreCreated {
		if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
			log.Printf("Pipeline engine: failed to update builtin step run: %v", err)
		}
	} else if err := svc.CreateStepRun(ctx, stepRun); err != nil {
		log.Printf("Pipeline engine: failed to record builtin step run: %v", err)
	}

	setCompletionMarketProperties(ctx, pe.db, step.NextMethod, effectiveStatus, extractCompletionConditionID(jobResult), run.ScopeCompanyID)

	if effectiveStatus != "succeeded" {
		if payloadFailureReason == "" {
			payloadFailureReason = fmt.Sprintf("builtin step %s returned status %s", step.NextMethod, effectiveStatus)
		}
		if !fanOut {
			pe.failPipelineRun(ctx, svc, run.ID, payloadFailureReason)
		}
		return false
	}

	pe.triggerNextSteps(ctx, svc, stepRun, jobResult)
	return true
}

// mapParams transforms a completed job's result into params for the next job.
// Keys starting with "=" inject literal values: {"=stale_only": "true"} sets
// params["stale_only"] = "true".
func mapParams(result map[string]any, paramMap map[string]string) map[string]any {
	if key, ok := paramMap["$"]; ok && key != "" {
		// Pass entire result under the given key, then overlay literals.
		mapped := map[string]any{key: result}
		for k, v := range paramMap {
			if strings.HasPrefix(k, "=") {
				mapped[strings.TrimPrefix(k, "=")] = v
			}
		}
		return mapped
	}
	mapped := make(map[string]any)
	for resultKey, paramName := range paramMap {
		if strings.HasPrefix(resultKey, "=") {
			// Literal value injection.
			mapped[strings.TrimPrefix(resultKey, "=")] = paramName
			continue
		}
		if v, ok := result[resultKey]; ok {
			mapped[paramName] = v
		}
	}
	return mapped
}

// failPipelineRun marks a pipeline run as failed.
func (pe *PipelineEngine) failPipelineRun(ctx context.Context, svc *data.AgentService, runID, reason string) {
	run, err := svc.GetPipelineRun(ctx, runID)
	if err != nil {
		return
	}
	run.Status = "failed"
	run.FailureReason = strings.TrimSpace(reason)
	run.UpdatedAt = time.Now()
	svc.UpdatePipelineRun(ctx, run)
	if run.FailureReason != "" {
		log.Printf("Pipeline engine: pipeline run %s failed: %s", runID, run.FailureReason)
		return
	}
	log.Printf("Pipeline engine: pipeline run %s failed", runID)
}

// resolveRunStatus checks whether a pipeline run should be marked completed or failed
// based on the current state of all its step runs. This supports fan-out pipelines where
// individual branches may fail without killing the entire run.
func (pe *PipelineEngine) resolveRunStatus(ctx context.Context, svc *data.AgentService, runID string) {
	stepRuns, err := svc.ListStepRunsForRun(ctx, runID)
	if err != nil {
		log.Printf("Pipeline engine: failed to list step runs for run %s: %v", runID, err)
		return
	}

	// If any step runs are still active, the run stays running.
	for _, sr := range stepRuns {
		if sr.Status == "running" || sr.Status == "pending" || sr.Status == "queued" {
			return
		}
	}

	// All step runs are done. Determine final status.
	run, err := svc.GetPipelineRun(ctx, runID)
	if err != nil {
		log.Printf("Pipeline engine: failed to get pipeline run %s: %v", runID, err)
		return
	}

	pipeline, ok := pe.GetPipeline(run.PipelineID)
	if !ok {
		log.Printf("Pipeline engine: unknown pipeline %s for run %s", run.PipelineID, runID)
		return
	}

	finalStepIdx := len(pipeline.Steps) - 1
	anyFinalSucceeded := false
	for _, sr := range stepRuns {
		if sr.StepIndex == finalStepIdx && sr.Status == "succeeded" {
			anyFinalSucceeded = true
			break
		}
	}

	if anyFinalSucceeded {
		run.Status = "completed"
	} else {
		run.Status = "failed"
		run.FailureReason = "all branches failed or were filtered"
	}
	run.UpdatedAt = time.Now()
	svc.UpdatePipelineRun(ctx, run)
	log.Printf("Pipeline engine: pipeline run %s resolved to %s", run.ID, run.Status)
}
