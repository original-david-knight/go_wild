package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	data "github.com/original-david-knight/go_wild/agent_data"
)

// executeFanOutStep extracts an array from the result and submits one job per element.
// When multiple agents share the same capability, jobs are distributed round-robin
// across all matching agents. Only the first job per target agent is delivered
// immediately; subsequent jobs stay queued and are delivered one at a time as each
// completes (see deliverNextFanOutJob).
func (pe *PipelineEngine) executeFanOutStep(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any) {
	source := result
	if source == nil {
		source = explicitParams
	}
	arr, ok := fanOutArray(source, step.FanOutKey)
	if !ok {
		log.Printf("Pipeline engine: fan-out key %q not found or not an array in result", step.FanOutKey)
		pe.failPipelineRun(ctx, svc, run.ID, "fan-out key not found or not an array")
		return
	}

	runner := pe.effectiveStepRunner(ctx, step)
	if runner == pipelineStepRunnerBuiltin {
		log.Printf("Pipeline engine: builtin fan-out step %d running %d branch(es) for method %s", stepIdx, len(arr), step.NextMethod)
		// Pre-create all step runs so resolveRunStatus sees them and
		// doesn't mark the pipeline completed while items are still queued.
		type fanOutItem struct {
			params  map[string]any
			stepRun *data.PipelineStepRun
		}
		var items []fanOutItem
		for _, item := range arr {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			params := mapParams(itemMap, step.ParamMap)
			if params == nil {
				params = map[string]any{}
			}
			sr := &data.PipelineStepRun{
				ID:        uuid.New().String(),
				RunID:     run.ID,
				StepIndex: stepIdx,
				Status:    "queued",
			}
			if err := svc.CreateStepRun(ctx, sr); err != nil {
				log.Printf("Pipeline engine: failed to pre-create builtin fan-out step run: %v", err)
			}
			items = append(items, fanOutItem{params: params, stepRun: sr})
		}
		for _, item := range items {
			pe.executeBuiltinStep(ctx, svc, run, step, stepIdx, nil, item.params, item.stepRun)
		}
		pe.resolveRunStatus(ctx, svc, run.ID)
		return
	}

	if runner == pipelineStepRunnerClaudeCode || runner == pipelineStepRunnerCodex {
		log.Printf("Pipeline engine: %s fan-out step %d running %d branch(es) for method %s", runner, stepIdx, len(arr), step.NextMethod)
		// Pre-create all step runs so resolveRunStatus sees them and
		// doesn't mark the pipeline completed while items are still queued.
		type fanOutItem struct {
			params  map[string]any
			stepRun *data.PipelineStepRun
		}
		var items []fanOutItem
		for _, item := range arr {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			params := mapParams(itemMap, step.ParamMap)
			if params == nil {
				params = map[string]any{}
			}
			sr := &data.PipelineStepRun{
				ID:        uuid.New().String(),
				RunID:     run.ID,
				StepIndex: stepIdx,
				Status:    "queued",
			}
			if err := svc.CreateStepRun(ctx, sr); err != nil {
				log.Printf("Pipeline engine: failed to pre-create %s fan-out step run: %v", runner, err)
			}
			items = append(items, fanOutItem{params: params, stepRun: sr})
		}
		// Claude Code and Codex are interchangeable under this fan-out
		// dispatch: both funnel into executePipelineStepShared, which is
		// where every edge case that matters to fan-out lives. Specifically:
		//
		//   - Partial failure: the shared function gates failPipelineRun on
		//     !fanOut, so a single branch's failure marks only its own
		//     step-run as failed; siblings keep running and the pipeline
		//     verdict is deferred to resolveRunStatus below.
		//   - Timeout: both runners wrap their subprocess in
		//     context.WithTimeout(ctx, pipelineRunnerDefaultTimeoutSec)
		//     and emit provider-specific ExecutionError; the shared
		//     function routes both through spec.buildFailure identically.
		//   - Cancellation: both respect ctx in sema.Acquire(ctx) and
		//     exec.CommandContext(execCtx, …); a cancelled ctx propagates
		//     as an invoke error and is marked "failed" by the same code
		//     path as any other runner error.
		//
		// The only documented asymmetry — Codex's spec.deferFanOutActivation
		// — affects status visibility during the queued-for-semaphore
		// window, not the three edge cases above, and is handled inside
		// executePipelineStepShared (see the deferActivation branch there).
		var wg sync.WaitGroup
		executeStep := pe.executeClaudeCodeStep
		if runner == pipelineStepRunnerCodex {
			executeStep = pe.executeCodexStep
		}
		for _, item := range items {
			wg.Add(1)
			go func(item fanOutItem) {
				defer wg.Done()
				executeStep(ctx, svc, run, step, stepIdx, nil, item.params, item.stepRun)
			}(item)
		}
		wg.Wait()
		pe.resolveRunStatus(ctx, svc, run.ID)
		return
	}

	// Resolve all eligible agents for delivery (but jobs are submitted unassigned).
	agentPool := pe.resolveFanOutAgents(ctx, svc, run, step)
	if len(agentPool) == 0 {
		return
	}
	log.Printf("Pipeline engine: fan-out step %d submitting %d pool jobs, %d eligible agent(s): %v", stepIdx, len(arr), len(agentPool), agentPool)

	// Submit all items as pool jobs (no target agent). Jobs sit in a shared
	// pool and get assigned to agents only when delivered/claimed.
	queue := pe.localQueueOrDefault()
	for _, item := range arr {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		params := mapParams(itemMap, step.ParamMap)
		if params == nil {
			params = map[string]any{}
		}
		params = sanitizePipelineStepParams(ctx, svc, step.NextMethod, params)
		jobResult, _, err := queue.Submit(ctx, "pipeline:"+run.ID, "", "", localA2ARequest{
			Method: step.NextMethod,
			Params: params,
		})
		if err != nil {
			log.Printf("Pipeline engine: failed to enqueue pool job for %s/%s: %v", step.ToRole, step.NextMethod, err)
			pe.failPipelineRun(ctx, svc, run.ID, "failed to enqueue pool job")
			return
		}
		jobID := pe.extractJobID(jobResult)
		if jobID == "" {
			if job, ok := jobResult["job"].(map[string]any); ok {
				jobID = pe.extractJobID(job)
			}
		}
		now := time.Now()
		stepRun := &data.PipelineStepRun{
			ID:        uuid.New().String(),
			RunID:     run.ID,
			StepIndex: stepIdx,
			A2AJobID:  jobID,
			Status:    "running",
			StartedAt: now,
		}
		if err := svc.CreateStepRun(ctx, stepRun); err != nil {
			log.Printf("Pipeline engine: failed to record step run: %v", err)
		}
		log.Printf("Pipeline engine: submitted pool job %s for step %d (%s/%s) in run %s", jobID, stepIdx, step.ToRole, step.NextMethod, run.ID)
	}

	// Deliver one job per eligible agent from the pool.
	for _, agent := range agentPool {
		pe.deliverNextFanOutJob(ctx, run.ID, stepIdx, agent)
	}
}

func fanOutArray(source map[string]any, key string) ([]any, bool) {
	if source == nil {
		return nil, false
	}
	value, ok := source[key]
	if !ok {
		return nil, false
	}
	if arr, ok := value.([]any); ok {
		return arr, true
	}
	// Builtin methods often return []map[string]any directly. Accept it.
	if typed, ok := value.([]map[string]any); ok {
		arr := make([]any, 0, len(typed))
		for _, item := range typed {
			arr = append(arr, item)
		}
		return arr, true
	}
	return nil, false
}

// resolveFanOutAgents returns all agent IDs eligible for a fan-out step.
// For fan-out, we always try capability-based resolution to find ALL agents
// that can handle the work. If the step has an explicit ToAgentID, that agent
// is included in the pool (even if it lacks a formal capability record).
// Falls back to just ToAgentID if no capability matches are found.
func (pe *PipelineEngine) resolveFanOutAgents(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep) []string {
	// Try capability-based resolution first to find all eligible agents.
	var (
		agents []string
		err    error
	)
	if strings.TrimSpace(step.ToRole) != "" && strings.TrimSpace(step.NextMethod) != "" {
		if strings.TrimSpace(run.ScopeMode) == "company" {
			agents, err = svc.FindAllAgentsByCapabilityInCompany(ctx, step.ToRole, step.NextMethod, run.ScopeCompanyID)
		} else {
			agents, err = svc.FindAllAgentsByCapability(ctx, step.ToRole, step.NextMethod)
		}
	}

	// If capability resolution found agents, use them. Ensure the explicit
	// ToAgentID (if any) is included in the pool.
	if err == nil && len(agents) > 0 {
		if explicit := strings.TrimSpace(step.ToAgentID); explicit != "" {
			found := false
			for _, a := range agents {
				if a == explicit {
					found = true
					break
				}
			}
			if !found {
				agents = append([]string{explicit}, agents...)
			}
		}
		return agents
	}

	// Capability resolution failed or returned nothing.
	// Fall back to explicit ToAgentID if set.
	if id := strings.TrimSpace(step.ToAgentID); id != "" {
		return []string{id}
	}

	// No agents found at all — fail the run.
	reason := "no eligible agents for step"
	if strings.TrimSpace(run.ScopeMode) == "company" {
		log.Printf("Pipeline engine: no eligible agents in company scope for %s/%s in company %s: %v", step.ToRole, step.NextMethod, run.ScopeCompanyID, err)
		reason = companyScopeNoEligibleAgentsFailureReason
	} else {
		log.Printf("Pipeline engine: no agents for %s/%s: %v", step.ToRole, step.NextMethod, err)
	}
	pe.failPipelineRun(ctx, svc, run.ID, reason)
	return nil
}

// submitStepJob resolves the target agent, submits the job, and immediately delivers it.
func (pe *PipelineEngine) submitStepJob(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any) {
	pe.submitStepJobInternal(ctx, svc, run, step, stepIdx, result, explicitParams, "", true)
}

// submitStepJobInternal resolves the target agent and submits a single A2A job for a pipeline step.
// When overrideAgentID is non-empty, it is used directly instead of resolving via capabilities.
func (pe *PipelineEngine) submitStepJobInternal(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any, overrideAgentID string, deliver bool) string {
	// Resolve target agent via capabilities (or use override)
	var (
		targetAgentID string
		err           error
	)
	if strings.TrimSpace(overrideAgentID) != "" {
		targetAgentID = strings.TrimSpace(overrideAgentID)
	} else if strings.TrimSpace(step.ToAgentID) != "" {
		targetAgentID = strings.TrimSpace(step.ToAgentID)
	} else if strings.TrimSpace(run.ScopeMode) == "company" {
		targetAgentID, err = svc.FindAgentByCapabilityInCompany(ctx, step.ToRole, step.NextMethod, run.ScopeCompanyID)
	} else {
		targetAgentID, err = svc.FindAgentByCapability(ctx, step.ToRole, step.NextMethod)
	}
	if err != nil {
		reason := "no eligible agents for step"
		if strings.TrimSpace(run.ScopeMode) == "company" {
			log.Printf("Pipeline engine: no eligible agents in company scope for %s/%s in company %s: %v", step.ToRole, step.NextMethod, run.ScopeCompanyID, err)
			reason = companyScopeNoEligibleAgentsFailureReason
		} else {
			log.Printf("Pipeline engine: no agent for %s/%s: %v", step.ToRole, step.NextMethod, err)
		}
		pe.failPipelineRun(ctx, svc, run.ID, reason)
		return ""
	}

	if strings.TrimSpace(overrideAgentID) == "" && strings.TrimSpace(run.ScopeMode) == "company" {
		member, err := pe.service.GetCompanyMemberForAgent(ctx, targetAgentID)
		if err != nil || member == nil || strings.TrimSpace(member.CompanyID) != strings.TrimSpace(run.ScopeCompanyID) {
			pe.failPipelineRun(ctx, svc, run.ID, companyScopeNoEligibleAgentsFailureReason)
			return ""
		}
	}

	// Map params
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

	jobResult, _, err := pe.localQueueOrDefault().Submit(ctx, "pipeline:"+run.ID, targetAgentID, "", localA2ARequest{
		Method: step.NextMethod,
		Params: params,
	})
	if err != nil {
		log.Printf("Pipeline engine: failed to enqueue job for %s/%s: %v", step.ToRole, step.NextMethod, err)
		pe.failPipelineRun(ctx, svc, run.ID, "failed to enqueue local job")
		return ""
	}

	jobID := pe.extractJobID(jobResult)
	if jobID == "" {
		// Try nested
		if job, ok := jobResult["job"].(map[string]any); ok {
			jobID = pe.extractJobID(job)
		}
	}

	// Record the step run
	now := time.Now()
	stepRun := &data.PipelineStepRun{
		ID:        uuid.New().String(),
		RunID:     run.ID,
		StepIndex: stepIdx,
		A2AJobID:  jobID,
		Status:    "running",
		StartedAt: now,
	}
	if err := svc.CreateStepRun(ctx, stepRun); err != nil {
		log.Printf("Pipeline engine: failed to record step run: %v", err)
	}

	log.Printf("Pipeline engine: submitted job %s for step %d (%s/%s) -> %s in run %s", jobID, stepIdx, step.ToRole, step.NextMethod, targetAgentID, run.ID)
	if deliver {
		pe.deliverPipelineStepJob(ctx, targetAgentID, jobID, step.NextMethod)
	}
	return targetAgentID
}

// deliverNextFanOutJob finds the next queued unassigned pool job for a fan-out
// step and delivers it to the given agent. Called after a fan-out sibling
// completes so jobs are processed one at a time per agent.
func (pe *PipelineEngine) deliverNextFanOutJob(ctx context.Context, runID string, stepIdx int, agentID string) {
	if pe.db == nil {
		return
	}

	svc := data.NewAgentService(pe.db, "system")
	stepRuns, err := svc.ListStepRunsForRun(ctx, runID)
	if err != nil {
		return
	}

	queue := pe.localQueueOrDefault()
	for _, sr := range stepRuns {
		if sr.StepIndex != stepIdx || sr.Status != "running" || sr.A2AJobID == "" {
			continue
		}
		// Check if the underlying job is still queued (not yet claimed/delivered).
		jobResult, err := queue.GetJob(ctx, sr.A2AJobID)
		if err != nil {
			continue
		}
		status, _ := jobResult["status"].(string)
		if status != localA2AStatusQueued {
			continue
		}
		// Accept unassigned pool jobs (empty to_agent_id) or jobs already
		// assigned to this agent. Skip jobs assigned to other agents.
		toAgent, _ := jobResult["to_public_key"].(string)
		if strings.TrimSpace(toAgent) != "" && strings.TrimSpace(toAgent) != strings.TrimSpace(agentID) {
			continue
		}
		method := ""
		if req, ok := jobResult["request"].(map[string]any); ok {
			method, _ = req["method"].(string)
		}
		pe.deliverPipelineStepJob(ctx, agentID, sr.A2AJobID, method)
		return // deliver only one
	}
}

// dispatchFanOutJobs resolves all eligible agents for a fan-out step and
// delivers one queued pool job per free agent.
func (pe *PipelineEngine) dispatchFanOutJobs(ctx context.Context, runID string, stepIdx int) {
	if pe.db == nil {
		return
	}
	svc := data.NewAgentService(pe.db, "system")
	run, err := svc.GetPipelineRun(ctx, runID)
	if err != nil || run == nil {
		return
	}
	pipeline, ok := pe.GetPipeline(run.PipelineID)
	if !ok || stepIdx >= len(pipeline.Steps) {
		return
	}
	step := pipeline.Steps[stepIdx]
	agents := pe.resolveFanOutAgents(ctx, svc, run, step)
	for _, agent := range agents {
		pe.deliverNextFanOutJob(ctx, runID, stepIdx, agent)
	}
}

// deliverPipelineStepJob claims a queued pipeline job and delivers a structured
// heartbeat containing the method instructions, input params, schemas, and
// completion rules — the same format used by company method tool calls.
func (pe *PipelineEngine) deliverPipelineStepJob(ctx context.Context, targetAgentID, jobID, method string) {
	sender := pe.heartbeatSender
	if sender == nil {
		return
	}

	queue := pe.localQueueOrDefault()

	claimedJob, err := queue.ClaimJob(ctx, strings.TrimSpace(targetAgentID), strings.TrimSpace(jobID), localA2AMaxClaimLeaseSeconds)
	if err != nil {
		log.Printf("Pipeline engine: failed to claim job %s for %s: %v", jobID, targetAgentID, err)
		return
	}

	dataSvc := data.NewAgentService(pe.db, "system")
	methodDef, _ := dataSvc.GetA2AMethod(ctx, strings.TrimSpace(method))

	spec := companyMethodToolSpec{Method: strings.TrimSpace(method)}
	message := buildClaimedCompanyMethodHeartbeat("pipeline", claimedJob, spec, methodDef)

	if err := sender.SendHeartbeat(targetAgentID, message); err != nil {
		log.Printf("Pipeline engine: failed to deliver claimed job %s to %s: %v", jobID, targetAgentID, err)
		if requeueErr := queue.RequeueClaimedJob(ctx, strings.TrimSpace(targetAgentID), strings.TrimSpace(jobID)); requeueErr != nil {
			log.Printf("Pipeline engine: failed to requeue job %s for %s: %v", jobID, targetAgentID, requeueErr)
		}
		return
	}
	log.Printf("Pipeline engine: delivered claimed job %s to %s (method: %s)", jobID, targetAgentID, method)
}

func (pe *PipelineEngine) localQueueOrDefault() *localA2AQueue {
	if pe.localQueue != nil {
		return pe.localQueue
	}
	return newLocalA2AQueue(pe.db)
}

// getA2AClient returns or creates a cached A2A agent net client.
func (pe *PipelineEngine) getA2AClient(ctx context.Context) (*a2aAgentNetClient, error) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	if pe.a2aClient != nil {
		return pe.a2aClient, nil
	}

	// Use the first available agent's service for key derivation
	agents, err := pe.service.ListAgents(ctx)
	if err != nil || len(agents) == 0 {
		log.Printf("Pipeline engine: no agents available for A2A client")
		return nil, fmt.Errorf("no agents available")
	}

	svc := data.NewAgentService(pe.db, agents[0].ID)
	client, err := newA2AAgentNetClient(ctx, svc)
	if err != nil {
		log.Printf("Pipeline engine: failed to create A2A client: %v", err)
		return nil, err
	}
	pe.a2aClient = client
	return client, nil
}

// getAgentPublicKey derives the public key for an agent from their wallet seed phrase.
func (pe *PipelineEngine) getAgentPublicKey(ctx context.Context, agent *data.Agent) (string, error) {
	if agent.WalletSeedPhrase == "" {
		return "", fmt.Errorf("agent %s has no wallet seed phrase", agent.ID)
	}
	svc := data.NewAgentService(pe.db, agent.ID)
	client, err := newA2AAgentNetClient(ctx, svc)
	if err != nil {
		return "", err
	}
	return client.agentID, nil
}
