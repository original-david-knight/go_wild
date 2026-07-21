package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

func TestBrokerDeepResearchMethodToolRunsConfiguredMethod(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	agentID := "agent-broker-deep-research-run"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.ModelProvider = data.LLMProviderAnthropic
	agent.SetEnabledTools([]string{"deep_research"})
	if err := agentSvc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_market_landscape",
		"Research market landscape",
		"",
		"Analyze {{topic}} in {{region}}",
		`{"type":"object","required":["topic","region"],"properties":{"topic":{"type":"string"},"region":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":2}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod failed: %v", err)
	}

	originalRunner := deepResearchRunMethodTestProgress
	defer func() { deepResearchRunMethodTestProgress = originalRunner }()

	called := false
	deepResearchRunMethodTestProgress = func(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest, progress func(deepresearch.ProgressEvent)) (*deepResearchMethodTestResult, error) {
		called = true
		if method.Method != "deep_research_market_landscape" {
			t.Fatalf("unexpected method passed to runner: %q", method.Method)
		}
		if got, _ := req.Input["topic"].(string); got != "AI agents" {
			t.Fatalf("expected topic input to reach runner, got %q", got)
		}
		return &deepResearchMethodTestResult{
			Method: method.Method,
			Query:  "Analyze AI agents in US",
			Input:  req.Input,
			Result: deepresearch.Result{
				Query:   "Analyze AI agents in US",
				Summary: "Research query: Analyze AI agents in US",
			},
		}, nil
	}

	payload, _ := json.Marshal(map[string]any{
		"topic":  "AI agents",
		"region": "US",
	})
	resultAny, err := h.callTool(ctx, agentID, agentSvc, "deep_research_market_landscape", payload)
	if err != nil {
		t.Fatalf("callTool deep-research method failed: %v", err)
	}
	if !called {
		t.Fatalf("expected deep research runner to be called")
	}

	result, ok := resultAny.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resultAny)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true in deep-research tool response")
	}
	if got, _ := result["method"].(string); got != "deep_research_market_landscape" {
		t.Fatalf("expected method in response, got %q", got)
	}
	if duration, ok := result["duration_ms"].(int64); !ok || duration < 0 {
		t.Fatalf("expected non-negative duration_ms, got %#v", result["duration_ms"])
	}

	method, err := managerSvc.GetDeepResearchMethod(ctx, "deep_research_market_landscape")
	if err != nil {
		t.Fatalf("GetDeepResearchMethod failed: %v", err)
	}
	if method.LastTestedAt.IsZero() {
		t.Fatalf("expected last_tested_at to be updated after tool run")
	}
}

func TestBrokerDeepResearchMethodToolValidatesInputSchema(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	agentID := "agent-broker-deep-research-schema"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.ModelProvider = data.LLMProviderAnthropic
	agent.SetEnabledTools([]string{"deep_research"})
	if err := agentSvc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_competitor_scan",
		"Scan competitors",
		"",
		"Analyze competitors for {{topic}}",
		`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":1}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod failed: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"region": "US",
	})
	_, err = h.callTool(ctx, agentID, agentSvc, "deep_research_competitor_scan", payload)
	if err == nil {
		t.Fatalf("expected input schema validation error")
	}
	if !strings.Contains(err.Error(), "input payload failed input schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBrokerDeepResearchMethodToolShutdownCancelsInFlight pins the graceful
// shutdown contract for the broker deep-research path. The runner uses
// context.WithoutCancel so the HTTP WriteTimeout can't kill a long codex
// planner mid-run, but that same detachment would also make SIGTERM ignored —
// the manager would either block shutdown until the per-method timeout (up to
// 15 min) or leak work past exit. The fix: when shutdownCtx is set and
// cancelled, the runCtx passed into the runner must cancel promptly.
func TestBrokerDeepResearchMethodToolShutdownCancelsInFlight(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	h.shutdownCtx = shutdownCtx

	ctx := context.Background()
	agentID := "agent-broker-deep-research-shutdown"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.ModelProvider = data.LLMProviderAnthropic
	agent.SetEnabledTools([]string{"deep_research"})
	if err := agentSvc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_shutdown_probe",
		"Shutdown probe",
		"",
		"Analyze {{topic}}",
		`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":1}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod failed: %v", err)
	}

	originalRunner := deepResearchRunMethodTestProgress
	defer func() { deepResearchRunMethodTestProgress = originalRunner }()

	entered := make(chan struct{})
	observed := make(chan error, 1)
	deepResearchRunMethodTestProgress = func(runCtx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest, progress func(deepresearch.ProgressEvent)) (*deepResearchMethodTestResult, error) {
		close(entered)
		<-runCtx.Done()
		observed <- runCtx.Err()
		return nil, runCtx.Err()
	}

	payload, _ := json.Marshal(map[string]any{"topic": "shutdown"})
	done := make(chan error, 1)
	go func() {
		_, err := h.callTool(ctx, agentID, agentSvc, "deep_research_shutdown_probe", payload)
		done <- err
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("runner was not entered within 2s")
	}

	// Request ctx is still live; runner must only unblock once shutdownCtx fires.
	shutdown()

	select {
	case err := <-observed:
		if err == nil {
			t.Fatalf("runner observed ctx.Err() == nil; shutdown must cancel the run ctx")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("shutdownCtx cancellation did not propagate to run ctx within 2s")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("callTool did not return within 2s after shutdown")
	}
}

// TestBrokerDeepResearchMethodToolRequestCancelDoesNotKillRun pins the
// insulation invariant the shutdownCtx plumbing must not break: cancelling
// the caller's (HTTP request) ctx while a deep-research run is in flight
// must NOT propagate into the runner. If it did, the manager's 5-min HTTP
// WriteTimeout would kill multi-minute codex planner/checker runs again.
// The fix composes context.AfterFunc(shutdownCtx, cancel) on top of
// context.WithTimeout(context.WithoutCancel(ctx), ...), so a regression
// that plumbed cancellation from the request ctx would surface here.
func TestBrokerDeepResearchMethodToolRequestCancelDoesNotKillRun(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	// shutdownCtx intentionally unset: we want only the request ctx in play.

	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()

	agentID := "agent-broker-deep-research-reqcancel"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(reqCtx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.ModelProvider = data.LLMProviderAnthropic
	agent.SetEnabledTools([]string{"deep_research"})
	if err := agentSvc.UpdateAgent(reqCtx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		reqCtx,
		"deep_research_reqcancel_probe",
		"Request cancel probe",
		"",
		"Analyze {{topic}}",
		`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":1}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod failed: %v", err)
	}

	originalRunner := deepResearchRunMethodTestProgress
	defer func() { deepResearchRunMethodTestProgress = originalRunner }()

	entered := make(chan struct{})
	release := make(chan struct{})
	// Buffered 2: runner may emit both the "observed Done" probe (on a bug)
	// and the "released without Done" probe (under the fix) without blocking.
	observed := make(chan bool, 2)
	deepResearchRunMethodTestProgress = func(runCtx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest, progress func(deepresearch.ProgressEvent)) (*deepResearchMethodTestResult, error) {
		close(entered)
		select {
		case <-runCtx.Done():
			observed <- true
		case <-release:
			observed <- false
		}
		return &deepResearchMethodTestResult{Method: method.Method}, nil
	}

	payload, _ := json.Marshal(map[string]any{"topic": "insulation"})
	done := make(chan error, 1)
	go func() {
		_, err := h.callTool(reqCtx, agentID, agentSvc, "deep_research_reqcancel_probe", payload)
		done <- err
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("runner was not entered within 2s")
	}

	reqCancel()

	// Give a regressed build time to (incorrectly) propagate cancellation.
	// 200ms is generous: context.AfterFunc / cancel propagation is microseconds.
	select {
	case saw := <-observed:
		if saw {
			t.Fatalf("request ctx cancellation propagated to runCtx — WithoutCancel insulation broken")
		}
		t.Fatalf("runner unparked before release — unexpected")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case saw := <-observed:
		if saw {
			t.Fatalf("runCtx cancelled after release — insulation broken")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runner did not unpark within 2s")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("callTool did not return within 2s")
	}
}

func TestBrokerDeepResearchMethodToolMarksTestTimestampOnRun(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	agentID := "agent-broker-deep-research-timestamp"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.ModelProvider = data.LLMProviderAnthropic
	agent.SetEnabledTools([]string{"deep_research"})
	if err := agentSvc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_mark_time",
		"Mark time",
		"",
		"Analyze {{topic}}",
		`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":1}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod failed: %v", err)
	}

	originalRunner := deepResearchRunMethodTestProgress
	defer func() { deepResearchRunMethodTestProgress = originalRunner }()
	deepResearchRunMethodTestProgress = func(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest, progress func(deepresearch.ProgressEvent)) (*deepResearchMethodTestResult, error) {
		return &deepResearchMethodTestResult{
			Method: method.Method,
			Query:  "Analyze test",
			Input:  req.Input,
			Result: deepresearch.Result{
				Query:      "Analyze test",
				StartedAt:  time.Now(),
				FinishedAt: time.Now(),
			},
		}, nil
	}

	payload, _ := json.Marshal(map[string]any{"topic": "AI"})
	if _, err := h.callTool(ctx, agentID, agentSvc, "deep_research_mark_time", payload); err != nil {
		t.Fatalf("callTool deep-research method failed: %v", err)
	}
	method, err := managerSvc.GetDeepResearchMethod(ctx, "deep_research_mark_time")
	if err != nil {
		t.Fatalf("GetDeepResearchMethod failed: %v", err)
	}
	if method.LastTestedAt.IsZero() {
		t.Fatalf("expected LastTestedAt to be set")
	}
}
