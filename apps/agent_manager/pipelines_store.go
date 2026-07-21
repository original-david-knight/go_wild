package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/claudellm"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// GetPipelines returns all configured pipelines.
func (pe *PipelineEngine) GetPipelines() []Pipeline {
	pe.pipeMu.RLock()
	defer pe.pipeMu.RUnlock()
	return clonePipelines(pe.pipelines)
}

// DB returns the pipeline engine's database handle.
func (pe *PipelineEngine) DB() gowild_data.Database {
	if pe == nil {
		return nil
	}
	return pe.db
}

// GetPipeline returns a configured pipeline by ID.
func (pe *PipelineEngine) GetPipeline(pipelineID string) (Pipeline, bool) {
	pe.pipeMu.RLock()
	defer pe.pipeMu.RUnlock()
	for _, pipeline := range pe.pipelines {
		if pipeline.ID == pipelineID {
			return clonePipeline(pipeline), true
		}
	}
	return Pipeline{}, false
}

func (pe *PipelineEngine) upsertPipelineInMemory(pipeline Pipeline) bool {
	pe.pipeMu.Lock()
	defer pe.pipeMu.Unlock()
	for i := range pe.pipelines {
		if pe.pipelines[i].ID == pipeline.ID {
			pe.pipelines[i] = clonePipeline(pipeline)
			return false
		}
	}
	pe.pipelines = append(pe.pipelines, clonePipeline(pipeline))
	return true
}

func (pe *PipelineEngine) deletePipelineInMemory(pipelineID string) bool {
	pe.pipeMu.Lock()
	defer pe.pipeMu.Unlock()
	for i := range pe.pipelines {
		if pe.pipelines[i].ID == pipelineID {
			pe.pipelines = append(pe.pipelines[:i], pe.pipelines[i+1:]...)
			return true
		}
	}
	return false
}

// UpsertPipelineDefinition saves a pipeline definition and updates the in-memory pipeline registry.
func (pe *PipelineEngine) UpsertPipelineDefinition(ctx context.Context, pipeline Pipeline, enabled bool) (bool, error) {
	pipeline = normalizePipeline(pipeline)
	if err := validatePipeline(pipeline); err != nil {
		return false, err
	}

	stepsJSON, err := json.Marshal(pipeline.Steps)
	if err != nil {
		return false, fmt.Errorf("failed to encode steps: %w", err)
	}
	// Validate schedule if provided.
	schedule := strings.TrimSpace(pipeline.Schedule)
	if schedule != "" {
		d, err := time.ParseDuration(schedule)
		if err != nil {
			return false, fmt.Errorf("invalid schedule %q: %w", schedule, err)
		}
		if d <= 0 {
			return false, fmt.Errorf("schedule must be a positive duration, got %q", schedule)
		}
	}

	svc := data.NewAgentService(pe.db, "system")
	if err := svc.UpsertPipelineDefinition(ctx, &data.PipelineDefinition{
		ID:             pipeline.ID,
		Name:           pipeline.Name,
		StepsJSON:      string(stepsJSON),
		ScopeMode:      pipeline.ScopeMode,
		ScopeCompanyID: pipeline.ScopeCompanyID,
		Schedule:       schedule,
		Enabled:        enabled,
	}); err != nil {
		return false, err
	}

	// Reset schedule entry so the next interval starts fresh.
	pe.mu.Lock()
	delete(pe.schedules, pipeline.ID)
	pe.mu.Unlock()

	if !enabled {
		deleted := pe.deletePipelineInMemory(pipeline.ID)
		return !deleted, nil
	}

	created := pe.upsertPipelineInMemory(pipeline)
	return created, nil
}

// DeletePipelineDefinition deletes a custom pipeline definition and removes it from memory.
func (pe *PipelineEngine) DeletePipelineDefinition(ctx context.Context, pipelineID string) error {
	pipelineID = strings.TrimSpace(pipelineID)
	if pipelineID == "" {
		return fmt.Errorf("pipeline id is required")
	}

	svc := data.NewAgentService(pe.db, "system")
	if _, err := svc.GetPipelineDefinition(ctx, pipelineID); err != nil {
		return ErrPipelineNotFound
	}
	if err := svc.DeletePipelineDefinition(ctx, pipelineID); err != nil {
		return err
	}
	pe.deletePipelineInMemory(pipelineID)
	return nil
}

// TriggerPipeline starts a pipeline run manually, treating payload as params for
// the first step's method.
func (pe *PipelineEngine) TriggerPipeline(ctx context.Context, pipelineID string, payload map[string]any) (triggerJobID string, runID string, err error) {
	pipeline, ok := pe.GetPipeline(pipelineID)
	if !ok {
		return "", "", ErrPipelineNotFound
	}
	if len(pipeline.Steps) == 0 {
		return "", "", fmt.Errorf("pipeline %q has no steps", pipelineID)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	// Reserve a slot in runWG before creating any persistent state. Done
	// first so a trigger that arrives after Shutdown fails fast without
	// leaving an orphan "running" PipelineRun row behind — and so the
	// runWG.Add never races a concurrent runWG.Wait from Shutdown.
	if !pe.tryBeginRun() {
		return "", "", ErrPipelineEngineShutdown
	}
	// From here on, any early return must release the reserved slot.
	slotReleased := false
	releaseSlot := func() {
		if !slotReleased {
			slotReleased = true
			pe.runWG.Done()
		}
	}
	defer releaseSlot()

	triggerJobID = "manual-" + uuid.New().String()

	now := time.Now()
	svc := data.NewAgentService(pe.db, "system")
	run := &data.PipelineRun{
		ID:             uuid.New().String(),
		PipelineID:     pipeline.ID,
		TriggerJobID:   triggerJobID,
		ScopeMode:      pipeline.ScopeMode,
		ScopeCompanyID: pipeline.ScopeCompanyID,
		CurrentStep:    0,
		Status:         "running",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := svc.CreatePipelineRun(ctx, run); err != nil {
		return triggerJobID, "", fmt.Errorf("failed to create pipeline run: %w", err)
	}

	// Detach from the caller's cancellation so the pipeline survives after the
	// HTTP trigger response is sent and the client disconnects. This applies to
	// every runner reachable from executeStep:
	//   - builtin LLM-backed methods: a single call can take 30-60s (market
	//     research, polymarket position eval).
	//   - claude-code / codex: multi-turn agent loops, seconds to minutes.
	//   - external A2A (submitStepJob): the inline work is fast — agent
	//     resolve, queue insert, step-run record, optional heartbeat — but
	//     request cancellation mid-enqueue would leave the run stuck at
	//     "running" with no queue job to drive completion. Detachment
	//     protects trigger-side bookkeeping here, not remote execution time.
	// detachedPipelineContext keeps values from ctx (trace IDs, etc.) but
	// rewires cancellation to the engine's runCtx. Combined with runWG the
	// engine's Shutdown can signal and then wait for every in-flight step
	// goroutine, so they don't leak when the manager stops. Same pattern as
	// broker_tools_deep_research_methods.go and handlers_deep_research_methods.go.
	firstStep := pipeline.Steps[0]
	stepCtx, stepCancel := detachedPipelineContext(ctx, pe.runCtx)
	slotReleased = true // handed to the goroutine
	go func() {
		defer pe.runWG.Done()
		defer stepCancel()
		pe.executeStep(stepCtx, svc, run, firstStep, 0, nil, payload)
	}()
	return triggerJobID, run.ID, nil
}

// SubmitInitialRequest sends an initial A2A request to an agent selected by role/method capability.
// This is intended to kick off workflows whose first pipeline step listens for the request's completion.
func (pe *PipelineEngine) SubmitInitialRequest(ctx context.Context, toRole, method string, params map[string]any) (jobID string, targetAgentID string, err error) {
	toRole = strings.TrimSpace(toRole)
	method = strings.TrimSpace(method)
	if toRole == "" {
		return "", "", fmt.Errorf("to_role is required")
	}
	if method == "" {
		return "", "", fmt.Errorf("method is required")
	}
	if params == nil {
		params = map[string]any{}
	}

	svc := data.NewAgentService(pe.db, "system")
	targetAgentID, err = svc.FindAgentByCapability(ctx, toRole, method)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve capability %s/%s: %w", toRole, method, err)
	}

	if err := validatePayloadForMethod(ctx, pe.db, method, capabilitySchemaInput, params); err != nil {
		return "", targetAgentID, err
	}

	jobResult, _, err := pe.localQueueOrDefault().Submit(ctx, "pipeline_initial_request", targetAgentID, "", localA2ARequest{
		Method: method,
		Params: params,
	})
	if err != nil {
		return "", targetAgentID, fmt.Errorf("failed to submit initial request: %w", err)
	}

	jobID = pe.extractJobID(jobResult)
	if jobID == "" {
		if job, ok := jobResult["job"].(map[string]any); ok {
			jobID = pe.extractJobID(job)
		}
	}
	if jobID == "" {
		return "", targetAgentID, fmt.Errorf("failed to submit initial request: agent_net response missing job_id")
	}

	pe.deliverPipelineStepJob(ctx, targetAgentID, jobID, method)
	return jobID, targetAgentID, nil
}

// GetPipelineRuns returns recent pipeline runs.
func (pe *PipelineEngine) GetPipelineRuns(ctx context.Context, limit int) ([]data.PipelineRun, error) {
	dao := pe.db.Table(data.PipelineRun{})
	if limit <= 0 {
		limit = 50
	}
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	runs := make([]data.PipelineRun, len(results))
	for i, r := range results {
		runs[i] = *r.(*data.PipelineRun)
	}
	return runs, nil
}

// GetPipelineRunDetail returns a pipeline run with its step runs.
func (pe *PipelineEngine) GetPipelineRunDetail(ctx context.Context, runID string) (*data.PipelineRun, []data.PipelineStepRun, error) {
	svc := data.NewAgentService(pe.db, "system")
	run, err := svc.GetPipelineRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}

	dao := pe.db.Table(data.PipelineStepRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"run_id": runID},
		OrderBy: "step_index",
	})
	if err != nil {
		return run, nil, err
	}

	steps := make([]data.PipelineStepRun, len(results))
	for i, r := range results {
		steps[i] = *r.(*data.PipelineStepRun)
	}
	return run, steps, nil
}

// enrichedStepRun extends PipelineStepRun with data from the A2A job and pipeline definition.
type enrichedStepRun struct {
	data.PipelineStepRun
	Runner       string `json:"runner,omitempty"`
	Method       string `json:"method"`
	AgentID      string `json:"agent_id"`
	ToRole       string `json:"to_role"`
	Request      any    `json:"request,omitempty"`
	Result       any    `json:"result,omitempty"`
	ClaudeLog    string `json:"claude_log,omitempty"`
	RawOutput    string `json:"raw_output,omitempty"`
	ClaudeStderr string `json:"claude_stderr,omitempty"`
	Error        any    `json:"error,omitempty"`
}

// GetPipelineRunDetailEnriched returns a pipeline run with step runs enriched with job/method data.
func (pe *PipelineEngine) GetPipelineRunDetailEnriched(ctx context.Context, runID string) (map[string]any, error) {
	run, steps, err := pe.GetPipelineRunDetail(ctx, runID)
	if err != nil {
		return nil, err
	}
	systemSvc := data.NewAgentService(pe.db, "system")

	// Look up the pipeline definition for step metadata.
	var pipelineSteps []PipelineStep
	if pipeline, ok := pe.GetPipeline(run.PipelineID); ok {
		pipelineSteps = pipeline.Steps
	}

	jobDAO := pe.db.Table(localA2AJob{})
	enriched := make([]enrichedStepRun, len(steps))

	for i, sr := range steps {
		e := enrichedStepRun{PipelineStepRun: sr}

		// Add method/role from pipeline definition.
		if sr.StepIndex >= 0 && sr.StepIndex < len(pipelineSteps) {
			ps := pipelineSteps[sr.StepIndex]
			e.Runner = normalizePipelineStepRunner(ps.Runner)
			e.Method = ps.NextMethod
			e.ToRole = ps.ToRole
			e.AgentID = strings.TrimSpace(ps.ToAgentID)
		}

		// Look up A2A job for agent and result.
		if sr.A2AJobID != "" {
			jobs, jErr := jobDAO.Query(ctx, gowild_data.QueryOpts{
				Where: map[string]any{"id": sr.A2AJobID},
				Limit: 1,
			})
			if jErr == nil && len(jobs) > 0 {
				job := jobs[0].(*localA2AJob)
				if strings.TrimSpace(job.ToAgentID) != "" {
					e.AgentID = job.ToAgentID
				}
				// Reflect actual job status: if the step run says "running"
				// but the job is still "queued" (not yet delivered/claimed),
				// show "queued" so the UI distinguishes waiting vs active.
				if e.Status == "running" && job.Status == localA2AStatusQueued {
					e.Status = "queued"
				}
				if e.Method == "" {
					var req map[string]any
					if json.Unmarshal([]byte(job.RequestJSON), &req) == nil {
						if m, _ := req["method"].(string); m != "" {
							e.Method = m
						}
						e.Request = sanitizePipelineMethodRequest(ctx, systemSvc, e.Method, req)
					}
				}
				if e.Request == nil && job.RequestJSON != "" {
					var req map[string]any
					if json.Unmarshal([]byte(job.RequestJSON), &req) == nil {
						e.Request = sanitizePipelineMethodRequest(ctx, systemSvc, e.Method, req)
					}
				}
				if job.ResultJSON != "" {
					var result any
					if json.Unmarshal([]byte(job.ResultJSON), &result) == nil {
						e.Result = unwrapPipelineStepResult(result)
						e.ClaudeLog = extractPipelineStepClaudeLog(result)
						e.RawOutput = extractPipelineStepRawOutput(result)
						e.ClaudeStderr = extractPipelineStepClaudeStderr(result)
					}
				}
				if job.ErrorJSON != "" {
					var errPayload any
					if json.Unmarshal([]byte(job.ErrorJSON), &errPayload) == nil {
						e.Error = errPayload
						if e.RawOutput == "" {
							e.RawOutput = extractPipelineStepClaudeStdout(errPayload)
						}
						if e.ClaudeStderr == "" {
							e.ClaudeStderr = extractPipelineStepClaudeStderr(errPayload)
						}
					}
				}
			}
		}

		enriched[i] = e
	}

	return map[string]any{
		"run":   run,
		"steps": enriched,
	}, nil
}

func unwrapPipelineStepResult(result any) any {
	if resultMap, ok := result.(map[string]any); ok {
		if raw, exists := resultMap["result"]; exists {
			return raw
		}
	}
	return result
}

func extractPipelineStepRawOutput(result any) string {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	raw, _ := resultMap["raw_output"].(string)
	if strings.TrimSpace(raw) == "" {
		raw, _ = resultMap["stdout"].(string)
	}
	return strings.TrimSpace(raw)
}

func extractPipelineStepClaudeLog(result any) string {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	if eventLog, _ := resultMap["event_log"].(string); strings.TrimSpace(eventLog) != "" {
		return strings.TrimSpace(eventLog)
	}
	return strings.TrimSpace(claudellm.FormatEventLog(extractPipelineStepRawOutput(result)))
}

func extractPipelineStepClaudeStdout(result any) string {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	raw, _ := resultMap["stdout"].(string)
	return strings.TrimSpace(raw)
}

func extractPipelineStepClaudeStderr(result any) string {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	raw, _ := resultMap["stderr"].(string)
	return strings.TrimSpace(raw)
}
