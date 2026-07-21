package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

var (
	// ErrPipelineNotFound is returned when a pipeline ID is unknown.
	ErrPipelineNotFound = errors.New("pipeline not found")
	// ErrPipelineEngineShutdown is returned when a trigger arrives after
	// Shutdown has been called on the engine. Callers should treat it as a
	// terminal error and stop retrying.
	ErrPipelineEngineShutdown = errors.New("pipeline engine is shut down")
)

const companyScopeNoEligibleAgentsFailureReason = "no eligible agents in company scope"

type pipelineHeartbeatSender interface {
	SendHeartbeat(agentID, message string) error
}

// pipelinePolymarketProvider abstracts Polymarket client creation so that
// PipelineEngine does not depend on the concrete BrokerPolymarketHandler type.
type pipelinePolymarketProvider interface {
	getClientForCompany(ctx context.Context, companyID string) (*polymarket.Client, string, error)
}

// pipelineWalletProvider abstracts wallet helpers so that PipelineEngine does
// not depend on the concrete BrokerWalletHandler type.
type pipelineWalletProvider interface {
	resolvePolygonRPCURL(ctx context.Context, companyID string) string
}

// PipelineEngine watches A2A job completions and chains next steps.
type PipelineEngine struct {
	db           gowild_data.Database
	service      *AgentService
	pipelines    []Pipeline
	pollInterval time.Duration

	mu               sync.Mutex
	pipeMu           sync.RWMutex
	claudeJobsMu     sync.Mutex
	a2aClient        *a2aAgentNetClient
	activeClaudeJobs map[string]struct{}

	localQueue *localA2AQueue

	schedules map[string]time.Time // pipelineID → next scheduled run time

	heartbeatSender     pipelineHeartbeatSender
	builtinTerminal     *BuiltinTerminalHub
	callbackVerifier    *a2aCallbackVerifier
	callbackVerifierErr error
	polymarketHelper    pipelinePolymarketProvider
	walletHelper        pipelineWalletProvider
	claudeCodeRunner    func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error)
	codexRunner         func(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error)

	// Engine lifecycle. runCtx is cancelled by Shutdown; every detached
	// pipeline-step goroutine inherits its cancellation signal so they do
	// not leak past engine shutdown. runWG tracks those goroutines so
	// Shutdown can wait for them to drain.
	//
	// shutdownMu guards shutdownClosed and serialises the Add/Wait boundary
	// required by sync.WaitGroup: Add(1) must never race with the final
	// Wait. tryBeginRun checks shutdownClosed under this mutex before
	// incrementing runWG, and Shutdown flips shutdownClosed under this
	// mutex before calling runCancel, so no new runWG.Add arrives after
	// Shutdown starts.
	//
	// shutdownOnce ensures the single draining goroutine (runWG.Wait +
	// close shutdownDone) is spawned at most once across repeated Shutdown
	// calls — otherwise a stalled runner would leak one waiter per call.
	//
	// Initialization is via runInit so struct-literal test constructions
	// still get a valid lifecycle on first use.
	runInit        sync.Once
	runCtx         context.Context
	runCancel      context.CancelFunc
	runWG          sync.WaitGroup
	shutdownMu     sync.Mutex
	shutdownClosed bool
	shutdownOnce   sync.Once
	shutdownDone   chan struct{}
}

// ensureLifecycle lazily initializes the engine's lifecycle fields. Safe to
// call from any entry point; no-op after first call.
func (pe *PipelineEngine) ensureLifecycle() {
	pe.runInit.Do(func() {
		pe.runCtx, pe.runCancel = context.WithCancel(context.Background())
		pe.shutdownDone = make(chan struct{})
	})
}

// tryBeginRun reserves a slot in runWG for a new pipeline-step goroutine.
// Returns false if the engine has been shut down, in which case the caller
// must reject the work. The caller is responsible for calling runWG.Done
// exactly once when the goroutine exits (typically via defer).
func (pe *PipelineEngine) tryBeginRun() bool {
	pe.ensureLifecycle()
	pe.shutdownMu.Lock()
	defer pe.shutdownMu.Unlock()
	if pe.shutdownClosed {
		return false
	}
	pe.runWG.Add(1)
	return true
}

// NewPipelineEngine creates a new pipeline engine.
func NewPipelineEngine(db gowild_data.Database, service *AgentService) *PipelineEngine {
	verifier, verifierErr := loadA2ACallbackVerifierFromEnv()
	engine := &PipelineEngine{
		db:                  db,
		service:             service,
		pipelines:           nil,
		pollInterval:        10 * time.Second,
		localQueue:          newLocalA2AQueue(db),
		activeClaudeJobs:    make(map[string]struct{}),
		schedules:           make(map[string]time.Time),
		builtinTerminal:     newBuiltinTerminalHub(),
		callbackVerifier:    verifier,
		callbackVerifierErr: verifierErr,
		polymarketHelper:    NewBrokerPolymarketHandler(service),
		walletHelper:        NewBrokerWalletHandler(service),
	}
	engine.ensureLifecycle()
	if err := engine.loadStoredPipelineDefinitions(context.Background()); err != nil {
		log.Printf("Pipeline engine: failed loading stored pipeline definitions: %v", err)
	}
	return engine
}

// SetHeartbeatSender configures an optional sender used to nudge interactive
// agents immediately after a new A2A job is queued for them.
func (pe *PipelineEngine) SetHeartbeatSender(sender pipelineHeartbeatSender) {
	pe.heartbeatSender = sender
}

func (pe *PipelineEngine) getPolymarketHelper() pipelinePolymarketProvider {
	if pe == nil {
		return nil
	}
	if pe.polymarketHelper == nil && pe.service != nil {
		pe.polymarketHelper = NewBrokerPolymarketHandler(pe.service)
	}
	return pe.polymarketHelper
}

func (pe *PipelineEngine) getWalletHelper() pipelineWalletProvider {
	if pe == nil {
		return nil
	}
	if pe.walletHelper == nil && pe.service != nil {
		pe.walletHelper = NewBrokerWalletHandler(pe.service)
	}
	return pe.walletHelper
}

func (pe *PipelineEngine) loadStoredPipelineDefinitions(ctx context.Context) error {
	svc := data.NewAgentService(pe.db, "system")
	defs, err := svc.ListPipelineDefinitions(ctx)
	if err != nil {
		return err
	}
	for _, def := range defs {
		if !def.Enabled {
			continue
		}
		pipeline, err := pipelineFromDefinition(def)
		if err != nil {
			log.Printf("Pipeline engine: skipping invalid pipeline definition %q: %v", def.ID, err)
			continue
		}
		pe.upsertPipelineInMemory(pipeline)
	}
	return nil
}

// CleanupStaleRuns fails all pipeline step runs and pipeline runs that were
// left in "running" state from a previous manager process. After a restart the
// in-memory state (goroutines, activeClaudeJobs map) is gone, so these jobs
// can never complete. We mark them failed immediately instead of waiting for
// the orphan-detection grace period.
func (pe *PipelineEngine) CleanupStaleRuns(ctx context.Context) {
	if pe.db == nil {
		return
	}

	svc := data.NewAgentService(pe.db, "system")

	// 1. Fail all "running" step runs.
	dao := pe.db.Table(data.PipelineStepRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"status": "running"},
	})
	if err != nil {
		log.Printf("Pipeline engine: CleanupStaleRuns: failed to query running step runs: %v", err)
		return
	}

	affectedRunIDs := make(map[string]struct{})
	now := time.Now()
	for _, r := range results {
		stepRun := r.(*data.PipelineStepRun)
		stepRun.Status = "failed"
		stepRun.CompletedAt = now
		if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
			log.Printf("Pipeline engine: CleanupStaleRuns: failed to fail step run %s: %v", stepRun.ID, err)
			continue
		}
		pe.markLocalJobFailed(ctx, stepRun.A2AJobID, "interrupted by manager restart")
		affectedRunIDs[stepRun.RunID] = struct{}{}
	}

	if len(affectedRunIDs) > 0 {
		log.Printf("Pipeline engine: CleanupStaleRuns: failed %d stale step run(s) across %d pipeline run(s)", len(results), len(affectedRunIDs))
	}

	// 2. Fail all localA2AJob records still marked "running" — these are
	//    in-process jobs (e.g. claude-code) whose goroutines died with the
	//    previous manager process.
	jobDAO := pe.db.Table(localA2AJob{})
	jobResults, err := jobDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"status": "running"},
	})
	if err != nil {
		log.Printf("Pipeline engine: CleanupStaleRuns: failed to query running jobs: %v", err)
	} else {
		for _, r := range jobResults {
			job := r.(*localA2AJob)
			pe.markLocalJobFailed(ctx, job.ID, "interrupted by manager restart")
		}
		if len(jobResults) > 0 {
			log.Printf("Pipeline engine: CleanupStaleRuns: failed %d stale local A2A job(s)", len(jobResults))
		}
	}

	// 3. Fail all pipeline runs still marked "running" — their step runs are
	//    now all terminal so the run can never advance.
	activeRuns, err := svc.GetActivePipelineRuns(ctx)
	if err != nil {
		log.Printf("Pipeline engine: CleanupStaleRuns: failed to query active pipeline runs: %v", err)
		return
	}
	for i := range activeRuns {
		run := &activeRuns[i]
		run.Status = "failed"
		run.FailureReason = "interrupted by manager restart"
		run.UpdatedAt = now
		if err := svc.UpdatePipelineRun(ctx, run); err != nil {
			log.Printf("Pipeline engine: CleanupStaleRuns: failed to fail pipeline run %s: %v", run.ID, err)
		}
	}
	if len(activeRuns) > 0 {
		log.Printf("Pipeline engine: CleanupStaleRuns: failed %d stale pipeline run(s)", len(activeRuns))
	}
}

// Run starts the pipeline engine polling loop.
func (pe *PipelineEngine) Run(ctx context.Context) {
	pe.ensureLifecycle()
	log.Println("Pipeline engine started")
	ticker := time.NewTicker(pe.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("Pipeline engine stopped")
			return
		case <-pe.runCtx.Done():
			log.Println("Pipeline engine stopped")
			return
		case <-ticker.C:
			pe.processCompletions(ctx)
			pe.processSchedules(ctx)
		}
	}
}

// WaitInFlight blocks until every currently-in-flight pipeline-step goroutine
// finishes. Unlike Shutdown, it does not cancel runCtx, so the goroutines are
// allowed to complete their work normally. Intended for tests that need to
// observe post-trigger state deterministically — production code should use
// Shutdown for graceful teardown.
//
// Holds shutdownMu for the duration of Wait so it serialises with tryBeginRun
// on the Add/Wait boundary: new triggers that arrive during the wait block
// on the lock until the current set of in-flight work drains, rather than
// racing runWG.Add(1) against a zero-count runWG.Wait() (which would be
// WaitGroup misuse). Callers must not invoke WaitInFlight from a pipeline
// step goroutine tracked by runWG — that would self-deadlock.
func (pe *PipelineEngine) WaitInFlight() {
	if pe == nil {
		return
	}
	pe.ensureLifecycle()
	pe.shutdownMu.Lock()
	defer pe.shutdownMu.Unlock()
	pe.runWG.Wait()
}

// Shutdown closes the engine to new triggers, cancels its lifecycle context
// so in-flight pipeline-step goroutines can stop, and waits for them to
// finish. Returns ctx.Err() if the deadline carried by ctx fires before the
// goroutines drain; nil otherwise. Safe to call multiple times — the closed
// flag is set once and only a single background waiter goroutine is spawned
// across all calls, so a stalled runner cannot leak one waiter per Shutdown
// retry.
func (pe *PipelineEngine) Shutdown(ctx context.Context) error {
	if pe == nil {
		return nil
	}
	pe.ensureLifecycle()

	pe.shutdownMu.Lock()
	if !pe.shutdownClosed {
		pe.shutdownClosed = true
		pe.runCancel()
	}
	pe.shutdownMu.Unlock()

	// Spawn the WaitGroup drainer exactly once. It survives across repeated
	// Shutdown calls, so a runner that ignores cancellation parks only one
	// waiter regardless of how many times Shutdown is retried.
	pe.shutdownOnce.Do(func() {
		go func() {
			pe.runWG.Wait()
			close(pe.shutdownDone)
		}()
	})

	select {
	case <-pe.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// detachedPipelineContext returns a context that inherits values from valueCtx
// (so trace IDs etc. propagate into step execution) but whose cancellation is
// tied to cancelCtx rather than valueCtx. This lets pipeline steps outlive the
// HTTP request that triggered them while still aborting when the engine is
// shut down. The returned CancelFunc must always be called by the owning
// goroutine (use defer) — it stops the AfterFunc registration and cancels the
// child ctx so neither resource outlives the step.
func detachedPipelineContext(valueCtx, cancelCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(valueCtx))
	stop := context.AfterFunc(cancelCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// processSchedules checks all pipeline definitions with a schedule and triggers
// those that are due, skipping any that already have an active run.
func (pe *PipelineEngine) processSchedules(ctx context.Context) {
	svc := data.NewAgentService(pe.db, "system")
	defs, err := svc.ListPipelineDefinitions(ctx)
	if err != nil {
		log.Printf("Pipeline scheduler: failed to list definitions: %v", err)
		return
	}

	now := time.Now()
	activeDefIDs := make(map[string]bool, len(defs))

	for _, def := range defs {
		activeDefIDs[def.ID] = true

		if !def.Enabled || strings.TrimSpace(def.Schedule) == "" {
			continue
		}

		interval, err := time.ParseDuration(strings.TrimSpace(def.Schedule))
		if err != nil || interval <= 0 {
			continue
		}

		pe.mu.Lock()
		nextRun, exists := pe.schedules[def.ID]
		if !exists {
			// First time seeing this schedule — set next run to now + interval.
			pe.schedules[def.ID] = now.Add(interval)
			pe.mu.Unlock()
			continue
		}
		pe.mu.Unlock()

		if !now.After(nextRun) {
			continue
		}

		// Check for active run before triggering.
		activeRuns, err := svc.GetActivePipelineRuns(ctx)
		if err != nil {
			log.Printf("Pipeline scheduler: failed to check active runs for %s: %v", def.ID, err)
			continue
		}
		hasActive := false
		for _, run := range activeRuns {
			if strings.TrimSpace(run.PipelineID) == def.ID {
				hasActive = true
				break
			}
		}
		if hasActive {
			log.Printf("Pipeline scheduler: skipping %s — already has an active run", def.ID)
			pe.mu.Lock()
			pe.schedules[def.ID] = now.Add(interval)
			pe.mu.Unlock()
			continue
		}

		log.Printf("Pipeline scheduler: triggering scheduled run for %s (interval=%s)", def.ID, interval)
		_, _, err = pe.TriggerPipeline(ctx, def.ID, nil)
		if err != nil {
			log.Printf("Pipeline scheduler: failed to trigger %s: %v", def.ID, err)
		}

		pe.mu.Lock()
		pe.schedules[def.ID] = now.Add(interval)
		pe.mu.Unlock()
	}

	// Clean up map entries for deleted/disabled pipelines.
	pe.mu.Lock()
	for id := range pe.schedules {
		if !activeDefIDs[id] {
			delete(pe.schedules, id)
		}
	}
	pe.mu.Unlock()
}

// RecordCompletion records a completed A2A job and checks if it triggers any pipeline.
// This accepts the broker/agent_net response shape and normalizes fields used by
// existing pipeline matching code.
func (pe *PipelineEngine) RecordCompletion(job map[string]any) {
	normalized := pe.normalizeCompletionJob(job)
	if normalized == nil {
		log.Printf("Pipeline engine: RecordCompletion: normalizeCompletionJob returned nil")
		return
	}

	jobID := pe.extractJobID(normalized)
	rawStatus, _ := normalized["status"].(string)
	status, payloadFailureReason := pe.effectiveCompletionStatus(normalized)
	method := pe.extractJobRequestMethod(normalized)
	log.Printf("Pipeline engine: RecordCompletion called for job %s (status=%s, method=%s)", jobID, status, method)
	if payloadFailureReason != "" && strings.ToLower(strings.TrimSpace(rawStatus)) == "succeeded" {
		log.Printf("Pipeline engine: RecordCompletion treating job %s as failed due to result.status=FAILED (%s)", jobID, payloadFailureReason)
	}

	if status != "succeeded" && status != "failed" {
		log.Printf("Pipeline engine: RecordCompletion: skipping non-terminal status %q", status)
		return
	}

	// Check if this completed job advances an existing pipeline step run.
	pe.checkCompletedJobAgainstStepRuns(context.Background(), normalized)

	// Check if this completed job triggers a new pipeline.
	pe.matchJobToPipelines(context.Background(), normalized)
}

// checkCompletedJobAgainstStepRuns looks up whether a completed job belongs to
// an active pipeline step run and, if so, advances the pipeline immediately
// instead of waiting for the next poll interval.
func (pe *PipelineEngine) checkCompletedJobAgainstStepRuns(ctx context.Context, job map[string]any) {
	jobID := pe.extractJobID(job)
	if jobID == "" || pe.db == nil {
		return
	}

	dao := pe.db.Table(data.PipelineStepRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"a2a_job_id": jobID, "status": "running"},
		Limit: 1,
	})
	if err != nil {
		log.Printf("Pipeline engine: checkCompletedJobAgainstStepRuns query error for job %s: %v", jobID, err)
		return
	}
	if len(results) == 0 {
		log.Printf("Pipeline engine: checkCompletedJobAgainstStepRuns: no running step run found for job %s", jobID)
		return
	}

	stepRun := results[0].(*data.PipelineStepRun)
	log.Printf("Pipeline engine: RecordCompletion matched job %s to step run %s (run %s, step %d)", jobID, stepRun.ID, stepRun.RunID, stepRun.StepIndex)
	pe.checkStepRunCompletion(ctx, stepRun)
}

// processCompletions queries recently completed A2A jobs and triggers matching pipeline steps.
func (pe *PipelineEngine) processCompletions(ctx context.Context) {
	// Query completed pipeline step runs since last check
	dao := pe.db.Table(data.PipelineStepRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"status": "running"},
		OrderBy: "started_at",
	})
	if err != nil {
		log.Printf("Pipeline engine: failed to query running step runs: %v", err)
		return
	}

	// For each running step run, check if the corresponding A2A job has completed.
	// This avoids needing to query the agent_net server directly.
	for _, r := range results {
		stepRun := r.(*data.PipelineStepRun)
		pe.checkStepRunCompletion(ctx, stepRun)
	}

	// Initial pipeline triggers are received via RecordCompletion (broker completion
	// hook and callback endpoint), not by polling a list endpoint.
	pe.checkNewTriggers(ctx)
}

// checkStepRunCompletion checks if a running step's A2A job has completed.
func (pe *PipelineEngine) checkStepRunCompletion(ctx context.Context, stepRun *data.PipelineStepRun) {
	queue := pe.localQueueOrDefault()
	jobResult, err := queue.GetJob(ctx, stepRun.A2AJobID)
	if err != nil {
		svc := data.NewAgentService(pe.db, "system")
		if errors.Is(err, ErrLocalA2AJobNotFound) {
			now := time.Now()
			stepRun.Status = "failed"
			stepRun.CompletedAt = now
			if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
				log.Printf("Pipeline engine: failed to mark missing-job step run %s as failed: %v", stepRun.ID, err)
				return
			}
			pe.failPipelineRun(ctx, svc, stepRun.RunID, "pipeline step job not found")
			return
		}
		log.Printf("Pipeline engine: failed to get job %s: %v", stepRun.A2AJobID, err)
		return
	}

	rawStatus, _ := jobResult["status"].(string)
	rawStatus = strings.ToLower(strings.TrimSpace(rawStatus))
	if rawStatus == localA2AStatusQueued {
		pe.redeliverQueuedStepRun(ctx, stepRun, jobResult)
		return
	}
	if rawStatus == "running" && pe.failOrphanedClaudeCodeStepRun(ctx, stepRun, jobResult) {
		return
	}

	effectiveStatus, payloadFailureReason := pe.effectiveCompletionStatus(jobResult)
	if effectiveStatus != "succeeded" && effectiveStatus != "failed" {
		return // Still in progress
	}

	if payloadFailureReason != "" {
		log.Printf("Pipeline engine: step run %s treating job %s as failed due to result.status=FAILED (%s)", stepRun.ID, stepRun.A2AJobID, payloadFailureReason)
	} else {
		rawStatus, _ := jobResult["status"].(string)
		rawStatus = strings.ToLower(strings.TrimSpace(rawStatus))
		if rawStatus == "succeeded" && effectiveStatus == "failed" {
			log.Printf("Pipeline engine: step run %s treating job %s as failed due to result.status=FAILED", stepRun.ID, stepRun.A2AJobID)
		}
	}

	// Update step run
	now := time.Now()
	stepRun.Status = effectiveStatus
	stepRun.CompletedAt = now

	svc := data.NewAgentService(pe.db, "system")
	if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
		log.Printf("Pipeline engine: failed to update step run %s: %v", stepRun.ID, err)
		return
	}

	// Deliver queued fan-out siblings to any free eligible agent (not just the completing one).
	pe.dispatchFanOutJobs(ctx, stepRun.RunID, stepRun.StepIndex)

	// If the step succeeded, trigger next steps
	if effectiveStatus == "succeeded" {
		pe.triggerNextSteps(ctx, svc, stepRun, jobResult)
	} else {
		// Branch failed — resolve run status (other fan-out branches may still be active)
		pe.resolveRunStatus(ctx, svc, stepRun.RunID)
	}
}

func (pe *PipelineEngine) markClaudeJobActive(jobID string) {
	if pe == nil {
		return
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}
	pe.claudeJobsMu.Lock()
	defer pe.claudeJobsMu.Unlock()
	if pe.activeClaudeJobs == nil {
		pe.activeClaudeJobs = make(map[string]struct{})
	}
	pe.activeClaudeJobs[jobID] = struct{}{}
}

func (pe *PipelineEngine) clearClaudeJobActive(jobID string) {
	if pe == nil {
		return
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}
	pe.claudeJobsMu.Lock()
	defer pe.claudeJobsMu.Unlock()
	delete(pe.activeClaudeJobs, jobID)
}

func (pe *PipelineEngine) isClaudeJobActive(jobID string) bool {
	if pe == nil {
		return false
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return false
	}
	pe.claudeJobsMu.Lock()
	defer pe.claudeJobsMu.Unlock()
	_, ok := pe.activeClaudeJobs[jobID]
	return ok
}

func (pe *PipelineEngine) failOrphanedClaudeCodeStepRun(ctx context.Context, stepRun *data.PipelineStepRun, jobResult map[string]any) bool {
	if pe == nil || pe.db == nil || stepRun == nil {
		return false
	}
	if pe.isClaudeJobActive(stepRun.A2AJobID) {
		return false
	}

	// Don't orphan steps that started recently — the in-memory active set may
	// lag the DB (e.g. the executing goroutine cleared activeClaudeJobs via
	// defer but the DB update hasn't committed yet, or the heartbeat raced the
	// executing goroutine between markClaudeJobActive and the DB write).
	// Only treat a step as truly orphaned after it has been "running" longer
	// than the maximum allowed execution time plus a generous buffer.
	orphanGrace := time.Duration(pipelineRunnerMaxTimeoutSec+600) * time.Second
	if !stepRun.StartedAt.IsZero() && time.Since(stepRun.StartedAt) < orphanGrace {
		return false
	}

	svc := data.NewAgentService(pe.db, "system")
	run, err := svc.GetPipelineRun(ctx, stepRun.RunID)
	if err != nil || run == nil {
		return false
	}
	pipeline, ok := pe.GetPipeline(run.PipelineID)
	if !ok || stepRun.StepIndex < 0 || stepRun.StepIndex >= len(pipeline.Steps) {
		return false
	}
	step := pipeline.Steps[stepRun.StepIndex]
	runner := pe.effectiveStepRunner(ctx, step)
	if runner != pipelineStepRunnerClaudeCode && runner != pipelineStepRunnerCodex {
		return false
	}

	reason := fmt.Sprintf("%s step %s was interrupted before completion; persisted runner state cannot be resumed after manager restart", runner, strings.TrimSpace(step.NextMethod))
	pe.markLocalJobFailed(ctx, stepRun.A2AJobID, reason)

	now := time.Now()
	stepRun.Status = "failed"
	stepRun.CompletedAt = now
	if err := svc.UpdateStepRun(ctx, stepRun); err != nil {
		log.Printf("Pipeline engine: failed to mark orphaned claude step run %s as failed: %v", stepRun.ID, err)
		return true
	}

	log.Printf("Pipeline engine: marked orphaned claude-code job %s for step run %s as failed after restart", stepRun.A2AJobID, stepRun.ID)
	if jobReason := pipelineResultFailureReason(jobResult); strings.TrimSpace(jobReason) != "" {
		reason = strings.TrimSpace(jobReason)
	}
	pe.failPipelineRun(ctx, svc, stepRun.RunID, reason)
	return true
}

func (pe *PipelineEngine) markLocalJobFailed(ctx context.Context, jobID, reason string) {
	if pe == nil || pe.db == nil {
		return
	}
	jobID = strings.TrimSpace(jobID)
	reason = strings.TrimSpace(reason)
	if jobID == "" || reason == "" {
		return
	}

	table := pe.db.Table(localA2AJob{})
	var job localA2AJob
	if err := table.Get(ctx, jobID, &job); err != nil {
		log.Printf("Pipeline engine: failed to load local job %s for failure update: %v", jobID, err)
		return
	}

	now := time.Now().UTC()
	job.Status = localA2AStatusFailed
	job.UpdatedAt = now
	job.CompletedAt = &now
	job.ErrorJSON = mustJSON(map[string]any{"message": reason})
	job.ResultJSON = mustJSON(map[string]any{
		"status": "failed",
		"reason": reason,
	})
	if err := table.Update(ctx, &job); err != nil {
		log.Printf("Pipeline engine: failed to update local job %s to failed: %v", jobID, err)
	}
}

func (pe *PipelineEngine) redeliverQueuedStepRun(ctx context.Context, stepRun *data.PipelineStepRun, jobResult map[string]any) {
	if pe.db == nil || pe.heartbeatSender == nil || stepRun == nil {
		return
	}

	svc := data.NewAgentService(pe.db, "system")
	run, err := svc.GetPipelineRun(ctx, stepRun.RunID)
	if err != nil || run == nil {
		return
	}

	pipeline, ok := pe.GetPipeline(run.PipelineID)
	if !ok || stepRun.StepIndex < 0 || stepRun.StepIndex >= len(pipeline.Steps) {
		return
	}

	step := pipeline.Steps[stepRun.StepIndex]
	runner := pe.effectiveStepRunner(ctx, step)
	if runner == pipelineStepRunnerBuiltin || runner == pipelineStepRunnerClaudeCode || runner == pipelineStepRunnerCodex {
		return
	}

	method := pe.extractJobRequestMethod(jobResult)
	if strings.TrimSpace(method) == "" {
		method = strings.TrimSpace(step.NextMethod)
	}
	if strings.TrimSpace(method) == "" {
		return
	}

	candidates := pe.redeliveryCandidates(ctx, svc, run, step)
	for _, agentID := range candidates {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		pe.deliverPipelineStepJob(ctx, agentID, stepRun.A2AJobID, method)
		refreshed, err := pe.localQueueOrDefault().GetJob(ctx, stepRun.A2AJobID)
		if err != nil {
			return
		}
		status, _ := refreshed["status"].(string)
		if strings.EqualFold(strings.TrimSpace(status), localA2AStatusClaimed) ||
			strings.EqualFold(strings.TrimSpace(status), localA2AStatusSucceeded) ||
			strings.EqualFold(strings.TrimSpace(status), localA2AStatusFailed) {
			return
		}
	}
}

func (pe *PipelineEngine) redeliveryCandidates(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep) []string {
	if svc == nil || run == nil {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, 4)
	add := func(agentID string) {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			return
		}
		if _, ok := seen[agentID]; ok {
			return
		}
		seen[agentID] = struct{}{}
		out = append(out, agentID)
	}

	add(step.ToAgentID)

	var agents []string
	var err error
	if strings.TrimSpace(step.ToRole) != "" && strings.TrimSpace(step.NextMethod) != "" {
		if strings.TrimSpace(run.ScopeMode) == "company" {
			agents, err = svc.FindAllAgentsByCapabilityInCompany(ctx, step.ToRole, step.NextMethod, run.ScopeCompanyID)
		} else {
			agents, err = svc.FindAllAgentsByCapability(ctx, step.ToRole, step.NextMethod)
		}
		if err == nil {
			for _, agentID := range agents {
				add(agentID)
			}
		}
	}

	return out
}

func pipelineResultStatusFailed(result map[string]any) (bool, string) {
	if len(result) == 0 {
		return false, ""
	}
	return pipelineResultStatusFailedInValue(result, 0)
}

func pipelineResultStatusFailedInValue(value any, depth int) (bool, string) {
	if depth > 3 {
		return false, ""
	}
	switch v := value.(type) {
	case map[string]any:
		return pipelineResultStatusFailedInMap(v, depth)
	case string:
		trimmed := strings.TrimSpace(v)
		if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
			return false, ""
		}
		var nested map[string]any
		if err := json.Unmarshal([]byte(trimmed), &nested); err != nil {
			return false, ""
		}
		return pipelineResultStatusFailedInMap(nested, depth+1)
	default:
		return false, ""
	}
}

func pipelineResultStatusFailedInMap(result map[string]any, depth int) (bool, string) {
	for key, raw := range result {
		if !strings.EqualFold(strings.TrimSpace(key), "status") {
			continue
		}
		status, ok := raw.(string)
		if !ok {
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(status))
		if normalized == "failed" || strings.HasPrefix(normalized, "failed:") {
			return true, pipelineResultFailureReason(result)
		}
	}

	// Accept common wrapper keys so payloads like {"result":{"status":"FAILED"}}
	// also halt the branch.
	for _, wrapperKey := range []string{"result", "output", "payload", "response", "data"} {
		child, ok := result[wrapperKey]
		if !ok {
			continue
		}
		if failed, reason := pipelineResultStatusFailedInValue(child, depth+1); failed {
			if reason != "" {
				return true, reason
			}
			return true, pipelineResultFailureReason(result)
		}
	}
	return false, ""
}

func pipelineResultFailureReason(result map[string]any) string {
	for _, key := range []string{"reason", "message"} {
		if msg, ok := result[key].(string); ok {
			if msg = strings.TrimSpace(msg); msg != "" {
				return msg
			}
		}
	}

	if rawErr, ok := result["error"]; ok {
		switch e := rawErr.(type) {
		case string:
			if msg := strings.TrimSpace(e); msg != "" {
				return msg
			}
		case map[string]any:
			if msg, ok := e["message"].(string); ok {
				if msg = strings.TrimSpace(msg); msg != "" {
					return msg
				}
			}
		}
	}

	return ""
}

// checkNewTriggers is retained for poll-loop structure consistency.
// New triggers are ingest-only via RecordCompletion.
func (pe *PipelineEngine) checkNewTriggers(ctx context.Context) {
	_ = ctx
}

func (pe *PipelineEngine) effectiveCompletionStatus(job map[string]any) (string, string) {
	status, _ := job["status"].(string)
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "succeeded" && status != "failed" {
		return status, ""
	}
	if status == "failed" {
		return status, ""
	}

	result := pe.extractJobResult(job)
	if failed, reason := pipelineResultStatusFailed(result); failed {
		return "failed", reason
	}
	return status, ""
}

// matchJobToPipelines checks if a completed job matches any pipeline's first step trigger.
func (pe *PipelineEngine) matchJobToPipelines(ctx context.Context, job map[string]any) {
	method := pe.extractJobRequestMethod(job)
	if method == "" {
		return
	}
	jobID := pe.extractJobID(job)
	if jobID == "" {
		return
	}
	status, _ := pe.effectiveCompletionStatus(job)
	fromRole := pe.resolveSourceRole(ctx, job, method)

	pipelines := pe.GetPipelines()
	for _, pipeline := range pipelines {
		if len(pipeline.Steps) == 0 {
			continue
		}
		step := pipeline.Steps[0]
		if !pe.stepMatches(step, method, status, fromRole) {
			continue
		}

		// Check if we already have a pipeline run for this job
		if pe.hasExistingRun(ctx, pipeline.ID, jobID) {
			continue
		}

		log.Printf("Pipeline engine: job %s triggered pipeline %q step %d", jobID, pipeline.Name, 0)
		pe.startPipelineFromJob(ctx, pipeline, 0, job)
	}
}

// stepMatches checks if a pipeline step's triggers match the given job attributes.
func (pe *PipelineEngine) stepMatches(step PipelineStep, method, status, fromRole string) bool {
	if step.OnMethod != method {
		return false
	}

	expectedStatus := step.OnStatus
	if expectedStatus == "" {
		expectedStatus = "succeeded"
	}
	if expectedStatus != "*" && expectedStatus != status {
		return false
	}

	if step.FromRole != "*" && step.FromRole != "" {
		// If a role is required, missing role context does not match.
		if fromRole == "" {
			return false
		}
		if step.FromRole != fromRole {
			return false
		}
	}

	return true
}

// hasExistingRun checks if a pipeline run already exists for this pipeline+trigger job.
func (pe *PipelineEngine) hasExistingRun(ctx context.Context, pipelineID, triggerJobID string) bool {
	dao := pe.db.Table(data.PipelineRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"pipeline_id": pipelineID, "trigger_job_id": triggerJobID},
		Limit: 1,
	})
	if err != nil {
		return false
	}
	return len(results) > 0
}
