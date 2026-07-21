package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

func TestDeepResearchMethodsCRUDAndTest(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-deep-research-config"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	createBody := `{
		"method":"deep_research_market_landscape",
		"description":"Research a market landscape",
		"query_template":"Analyze {{topic}} in {{region}}",
		"input_schema":{"type":"object","required":["topic","region"],"properties":{"topic":{"type":"string"},"region":{"type":"string"}}},
		"research_schema":{"type":"object","required":["overview"],"properties":{"overview":{"type":"string"}}},
		"options":{"max_depth":2,"search_results_per_query":3},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/deep-research-methods", nil)
	listRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}
	methodsAny, _ := listResp["methods"].([]any)
	if len(methodsAny) != 1 {
		t.Fatalf("expected 1 deep research method, got %d", len(methodsAny))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/deep-research-methods/deep_research_market_landscape", nil)
	getRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	if got, _ := getResp["query_template"].(string); !strings.Contains(got, "{{topic}}") {
		t.Fatalf("expected query template in get response, got %q", got)
	}

	updateBody := `{
		"description":"Updated description",
		"query_template":"Analyze {{topic}}",
		"enabled":false
	}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/deep-research-methods/deep_research_market_landscape", strings.NewReader(updateBody))
	updateRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	disabledTestReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_market_landscape/test", strings.NewReader(`{"input":{"topic":"AI","region":"US"}}`))
	disabledTestRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(disabledTestRec, disabledTestReq)
	if disabledTestRec.Code != http.StatusBadRequest {
		t.Fatalf("expected disabled test status %d, got %d body=%s", http.StatusBadRequest, disabledTestRec.Code, disabledTestRec.Body.String())
	}

	enableBody := `{"query_template":"Analyze {{topic}} in {{region}}","enabled":true}`
	enableReq := httptest.NewRequest(http.MethodPut, "/api/deep-research-methods/deep_research_market_landscape", strings.NewReader(enableBody))
	enableRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body=%s", enableRec.Code, enableRec.Body.String())
	}

	originalRunner := deepResearchRunMethodTest
	defer func() { deepResearchRunMethodTest = originalRunner }()

	called := false
	deepResearchRunMethodTest = func(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest) (*deepResearchMethodTestResult, error) {
		called = true
		return &deepResearchMethodTestResult{
			Method: method.Method,
			Query:  "Analyze AI in US",
			Input:  req.Input,
			Result: deepresearch.Result{
				Query: "Analyze AI in US",
				Objectives: []deepresearch.ObjectiveResult{
					{
						Objective: deepresearch.Objective{Key: "overview", Required: true},
						Status:    deepresearch.ObjectiveStatusSatisfied,
					},
				},
				Rounds:     1,
				StartedAt:  time.Now(),
				FinishedAt: time.Now(),
			},
		}, nil
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_market_landscape/test", strings.NewReader(`{"input":{"topic":"AI","region":"US"}}`))
	testRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status = %d, body=%s", testRec.Code, testRec.Body.String())
	}
	if !called {
		t.Fatalf("expected deep research test runner to be called")
	}
	var testResp map[string]any
	if err := json.NewDecoder(testRec.Body).Decode(&testResp); err != nil {
		t.Fatalf("decode test response failed: %v", err)
	}
	if ok, _ := testResp["ok"].(bool); !ok {
		t.Fatalf("expected ok=true in test response")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/deep-research-methods/deep_research_market_landscape", nil)
	deleteRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestDeepResearchMethodTestReturns500OnMissingConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-deep-research-misconfig"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	createBody := `{
		"method":"deep_research_misconfig",
		"description":"Misconfig test",
		"input_schema":{"type":"object","properties":{"topic":{"type":"string"}}},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	originalRunner := deepResearchRunMethodTest
	defer func() { deepResearchRunMethodTest = originalRunner }()
	deepResearchRunMethodTest = func(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest) (*deepResearchMethodTestResult, error) {
		return nil, fmt.Errorf("%w: no Codex profile configured (set DEEP_RESEARCH_CODEX_PLANNER_PROFILE or CODEX_SMART_PROFILE)", deepresearch.ErrMissingConfig)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_misconfig/test", strings.NewReader(`{"input":{"topic":"AI"}}`))
	testRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(testRec, testReq)
	if testRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing-config error, got %d body=%s", testRec.Code, testRec.Body.String())
	}
	if !strings.Contains(testRec.Body.String(), "CODEX_SMART_PROFILE") {
		t.Fatalf("expected specific env-var name in response body, got %s", testRec.Body.String())
	}
}

func TestDeepResearchMethodTestReturns400OnGenericError(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-deep-research-badinput"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	createBody := `{
		"method":"deep_research_badinput",
		"description":"Bad input test",
		"input_schema":{"type":"object","properties":{"topic":{"type":"string"}}},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	originalRunner := deepResearchRunMethodTest
	defer func() { deepResearchRunMethodTest = originalRunner }()
	deepResearchRunMethodTest = func(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest) (*deepResearchMethodTestResult, error) {
		return nil, fmt.Errorf("query is empty after resolving deep research input")
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_badinput/test", strings.NewReader(`{"input":{"topic":"AI"}}`))
	testRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(testRec, testReq)
	if testRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for generic runner error, got %d body=%s", testRec.Code, testRec.Body.String())
	}
}

func TestDeepResearchMethodTestStreamIncludesSourceProgress(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-deep-research-stream"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	createBody := `{
		"method":"deep_research_stream_test",
		"description":"Streaming test",
		"input_schema":{"type":"object","properties":{"topic":{"type":"string"}}},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	originalRunner := deepResearchRunMethodTestProgress
	defer func() { deepResearchRunMethodTestProgress = originalRunner }()

	deepResearchRunMethodTestProgress = func(ctx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest, progress func(deepresearch.ProgressEvent)) (*deepResearchMethodTestResult, error) {
		if progress != nil {
			progress(deepresearch.ProgressEvent{
				Stage:        "source",
				ObjectiveKey: "overview",
				Query:        "stream query",
				URL:          "https://example.com/full-page",
				Title:        "Example Full Page",
				Rank:         0,
			})
		}
		return &deepResearchMethodTestResult{
			Method: method.Method,
			Query:  "stream query",
			Input:  req.Input,
			Result: deepresearch.Result{
				Query: "stream query",
				Objectives: []deepresearch.ObjectiveResult{
					{
						Objective: deepresearch.Objective{Key: "overview", Required: true},
						Status:    deepresearch.ObjectiveStatusSatisfied,
					},
				},
				Rounds:     1,
				StartedAt:  time.Now(),
				FinishedAt: time.Now(),
			},
		}, nil
	}

	streamReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_stream_test/test-stream", strings.NewReader(`{"input":{"topic":"AI"}}`))
	streamRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(streamRec, streamReq)
	if streamRec.Code != http.StatusOK {
		t.Fatalf("stream status = %d, body=%s", streamRec.Code, streamRec.Body.String())
	}
	if contentType := streamRec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/x-ndjson") {
		t.Fatalf("expected ndjson content type, got %q", contentType)
	}

	type streamEvent struct {
		Type  string                        `json:"type"`
		Event *deepresearch.ProgressEvent   `json:"event,omitempty"`
		Test  *deepResearchMethodTestResult `json:"test,omitempty"`
		Error string                        `json:"error,omitempty"`
	}

	foundSource := false
	foundDone := false
	scanner := bufio.NewScanner(strings.NewReader(streamRec.Body.String()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt streamEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("stream line was not valid json: %q err=%v", line, err)
		}
		if evt.Type == "progress" && evt.Event != nil && evt.Event.Stage == "source" {
			foundSource = true
			if evt.Event.URL != "https://example.com/full-page" {
				t.Fatalf("unexpected source URL in stream event: %q", evt.Event.URL)
			}
		}
		if evt.Type == "done" && evt.Test != nil {
			foundDone = true
		}
		if evt.Type == "error" {
			t.Fatalf("unexpected error event: %s", evt.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream response failed: %v", err)
	}
	if !foundSource {
		t.Fatalf("expected source progress event in stream response")
	}
	if !foundDone {
		t.Fatalf("expected done event in stream response")
	}
}

func TestDeepResearchMethodCreateWithoutQueryTemplate(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-deep-research-no-template"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	createBody := `{
		"method":"deep_research_json_first",
		"description":"JSON-first deep research method",
		"input_schema":{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/deep-research-methods/deep_research_json_first", nil)
	getRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	if got, _ := getResp["query_template"].(string); got != "" {
		t.Fatalf("expected empty query_template, got %q", got)
	}
}

func TestDeepResearchQueryFromTemplate(t *testing.T) {
	input := map[string]any{
		"topic": "AI agents",
		"company": map[string]any{
			"name": "GoWild",
		},
	}

	got := deepResearchQueryFromTemplate("Analyze {{topic}} for {{company.name}}", input)
	want := "Analyze AI agents for GoWild"
	if got != want {
		t.Fatalf("query template output = %q, want %q", got, want)
	}

	got = deepResearchQueryFromTemplate("", map[string]any{
		"query": "structured query text",
		"topic": "ignored when query provided",
	})
	if got != "structured query text" {
		t.Fatalf("empty template should use input.query, got %q", got)
	}

	got = deepResearchQueryFromTemplate("", map[string]any{
		"topic":  "AI agents",
		"region": "US",
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("empty template should serialize structured input as JSON, got %q err=%v", got, err)
	}
	if payload["topic"] != "AI agents" || payload["region"] != "US" {
		t.Fatalf("unexpected structured payload serialization: %#v", payload)
	}

	got = deepResearchQueryFromTemplate("", map[string]any{
		"target_statement": "will trump run for president in 2028",
		"region":           "US",
	})
	payload = nil
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("empty template should serialize structured input as JSON, got %q err=%v", got, err)
	}
	if payload["target_statement"] != "will trump run for president in 2028" || payload["region"] != "US" {
		t.Fatalf("unexpected structured payload serialization: %#v", payload)
	}
}

func TestHandleDeepResearchMethodsUnknownAction(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/deep-research-methods/method-x/not-real", nil)
	rec := httptest.NewRecorder()
	h.handleDeepResearchMethods(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeepResearchRouteRecognitionHelpers(t *testing.T) {
	if !isDeepResearchCollectionMethod(http.MethodGet) || !isDeepResearchCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized collection methods")
	}
	if isDeepResearchCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected collection method")
	}

	if !isDeepResearchMethodMethod(http.MethodGet) || !isDeepResearchMethodMethod(http.MethodPut) || !isDeepResearchMethodMethod(http.MethodDelete) {
		t.Fatalf("expected GET/PUT/DELETE to be recognized method routes")
	}
	if isDeepResearchMethodMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected method route")
	}

	if !isDeepResearchMethodAction("test") || !isDeepResearchMethodAction("test-stream") {
		t.Fatalf("expected test and test-stream actions to be recognized")
	}
	if isDeepResearchMethodAction("not-real") {
		t.Fatalf("expected unknown action to be rejected")
	}
}

type stubDeepResearchPlanner struct {
	calls int
}

func (p *stubDeepResearchPlanner) Plan(ctx context.Context, req deepresearch.PlanningRequest) (deepresearch.PlanningResult, error) {
	p.calls++
	return deepresearch.PlanningResult{}, nil
}

type stubDeepResearchChecker struct {
	calls int
}

func (c *stubDeepResearchChecker) Check(ctx context.Context, req deepresearch.CompletenessRequest) (deepresearch.CompletenessResult, error) {
	c.calls++
	return deepresearch.CompletenessResult{Complete: true}, nil
}

type stubDeepResearchSynthesizer struct {
	calls int
}

func (s *stubDeepResearchSynthesizer) Synthesize(ctx context.Context, req deepresearch.SynthesisRequest) (deepresearch.SynthesisResult, error) {
	s.calls++
	return deepresearch.SynthesisResult{
		Output: map[string]any{"summary": "ok"},
	}, nil
}

func TestNewClaudeDeepResearchReasoningClientDisablesBuiltInTools(t *testing.T) {
	client := newClaudeDeepResearchReasoningClient("claude-sonnet", time.Minute, "planner")
	if client.Model != "claude-sonnet" {
		t.Fatalf("model = %q, want %q", client.Model, "claude-sonnet")
	}
	if client.Label != "planner" {
		t.Fatalf("label = %q, want %q", client.Label, "planner")
	}
	if client.Tools == nil || len(client.Tools) != 0 {
		t.Fatalf("expected Tools to be an empty slice, got %#v", client.Tools)
	}
	if client.MCPConfigPath != "" {
		t.Fatalf("expected empty MCPConfigPath, got %q", client.MCPConfigPath)
	}
	if client.StrictMCPConfig {
		t.Fatalf("expected StrictMCPConfig=false")
	}
	if client.AllowedTools != "" {
		t.Fatalf("expected AllowedTools to be empty, got %q", client.AllowedTools)
	}
}

func TestRunDeepResearchMethodTestWithProgress_ClaudeUsesClaudeSearchAndClaudeReasoning(t *testing.T) {
	origGeminiSearcher := buildGeminiDeepResearchSearcher
	origClaudeSearcher := buildClaudeDeepResearchSearcher
	origFetcher := buildDeepResearchFetcher
	origGeminiPlanner := buildGeminiDeepResearchPlanner
	origGeminiChecker := buildGeminiDeepResearchChecker
	origGeminiSynthesizer := buildGeminiDeepResearchSynthesizer
	origClaudePlanner := buildClaudeDeepResearchPlanner
	origClaudeChecker := buildClaudeDeepResearchChecker
	origClaudeSynthesizer := buildClaudeDeepResearchSynthesizer
	defer func() {
		buildGeminiDeepResearchSearcher = origGeminiSearcher
		buildClaudeDeepResearchSearcher = origClaudeSearcher
		buildDeepResearchFetcher = origFetcher
		buildGeminiDeepResearchPlanner = origGeminiPlanner
		buildGeminiDeepResearchChecker = origGeminiChecker
		buildGeminiDeepResearchSynthesizer = origGeminiSynthesizer
		buildClaudeDeepResearchPlanner = origClaudePlanner
		buildClaudeDeepResearchChecker = origClaudeChecker
		buildClaudeDeepResearchSynthesizer = origClaudeSynthesizer
	}()

	searcher := &testDeepResearchSearcher{
		hits: []deepresearch.SearchHit{{
			URL:   "https://example.com/article",
			Title: "Example Article",
		}},
	}
	fetcher := &testDeepResearchFetcher{}
	planner := &stubDeepResearchPlanner{}
	checker := &stubDeepResearchChecker{}
	synthesizer := &stubDeepResearchSynthesizer{}

	buildGeminiDeepResearchSearcher = func() (deepresearch.Searcher, error) {
		t.Fatalf("gemini searcher should not be used for claude deep research")
		return nil, nil
	}
	buildClaudeDeepResearchSearcher = func() (deepresearch.Searcher, error) {
		return searcher, nil
	}
	buildDeepResearchFetcher = func() (deepresearch.Fetcher, error) {
		return fetcher, nil
	}
	buildGeminiDeepResearchPlanner = func() (deepresearch.Planner, error) {
		t.Fatalf("gemini planner should not be used for claude deep research")
		return nil, nil
	}
	buildGeminiDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) {
		t.Fatalf("gemini checker should not be used for claude deep research")
		return nil, nil
	}
	buildGeminiDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) {
		t.Fatalf("gemini synthesizer should not be used for claude deep research")
		return nil, nil
	}

	buildClaudeDeepResearchPlanner = func() deepresearch.Planner {
		return planner
	}
	buildClaudeDeepResearchChecker = func() deepresearch.CompletenessChecker {
		return checker
	}
	buildClaudeDeepResearchSynthesizer = func() deepresearch.Synthesizer {
		return synthesizer
	}

	method := &DeepResearchMethod{
		Method:             "deep_research_claude_runtime",
		Description:        "Claude deep research runtime test",
		QueryTemplate:      "Research {{topic}}",
		ResearchSchemaJSON: `{"type":"object","properties":{"summary":{"type":"string"}}}`,
		OptionsJSON:        `{"llm_backend":"claude","search_results_per_query":1}`,
		Enabled:            true,
	}

	result, err := runDeepResearchMethodTestWithProgress(context.Background(), method, deepResearchMethodTestRequest{
		Input: map[string]any{"topic": "AI agents"},
	}, nil)
	if err != nil {
		t.Fatalf("runDeepResearchMethodTestWithProgress failed: %v", err)
	}
	if searcher.calls == 0 {
		t.Fatalf("expected claude searcher to be called")
	}
	if planner.calls == 0 {
		t.Fatalf("expected claude planner to be called")
	}
	if checker.calls == 0 {
		t.Fatalf("expected claude checker to be called")
	}
	if synthesizer.calls == 0 {
		t.Fatalf("expected claude synthesizer to be called")
	}
	warnings := strings.Join(result.Result.Warnings, " | ")
	if !strings.Contains(warnings, "search_provider: claude_web_search") {
		t.Fatalf("expected claude search provider warning, got %#v", result.Result.Warnings)
	}
	if !strings.Contains(warnings, "fetch_provider: read_webpage_tool_fetcher") {
		t.Fatalf("expected read_webpage fetch provider warning, got %#v", result.Result.Warnings)
	}
}

// TestRunDeepResearchMethodTestWithProgress_CodexUsesCodexFactories pins the
// codex branch of runDeepResearchMethodTestWithProgress and proves the
// buildCodex* factory vars are overridable for tests — the same contract the
// Claude/Gemini factories already carry. Without this test the codex
// wire-up would drift silently (no production tests exercise it either,
// since real codex needs a configured CLI profile).
func TestRunDeepResearchMethodTestWithProgress_CodexUsesCodexFactories(t *testing.T) {
	origCodexSearcher := buildCodexDeepResearchSearcher
	origFetcher := buildDeepResearchFetcher
	origCodexPlanner := buildCodexDeepResearchPlanner
	origCodexChecker := buildCodexDeepResearchChecker
	origCodexSynthesizer := buildCodexDeepResearchSynthesizer
	origGeminiSearcher := buildGeminiDeepResearchSearcher
	origClaudeSearcher := buildClaudeDeepResearchSearcher
	origGeminiPlanner := buildGeminiDeepResearchPlanner
	origGeminiChecker := buildGeminiDeepResearchChecker
	origGeminiSynthesizer := buildGeminiDeepResearchSynthesizer
	origClaudePlanner := buildClaudeDeepResearchPlanner
	origClaudeChecker := buildClaudeDeepResearchChecker
	origClaudeSynthesizer := buildClaudeDeepResearchSynthesizer
	defer func() {
		buildCodexDeepResearchSearcher = origCodexSearcher
		buildDeepResearchFetcher = origFetcher
		buildCodexDeepResearchPlanner = origCodexPlanner
		buildCodexDeepResearchChecker = origCodexChecker
		buildCodexDeepResearchSynthesizer = origCodexSynthesizer
		buildGeminiDeepResearchSearcher = origGeminiSearcher
		buildClaudeDeepResearchSearcher = origClaudeSearcher
		buildGeminiDeepResearchPlanner = origGeminiPlanner
		buildGeminiDeepResearchChecker = origGeminiChecker
		buildGeminiDeepResearchSynthesizer = origGeminiSynthesizer
		buildClaudeDeepResearchPlanner = origClaudePlanner
		buildClaudeDeepResearchChecker = origClaudeChecker
		buildClaudeDeepResearchSynthesizer = origClaudeSynthesizer
	}()

	searcher := &testDeepResearchSearcher{
		hits: []deepresearch.SearchHit{{
			URL:   "https://example.com/codex-article",
			Title: "Codex Example Article",
		}},
	}
	fetcher := &testDeepResearchFetcher{}
	planner := &stubDeepResearchPlanner{}
	checker := &stubDeepResearchChecker{}
	synthesizer := &stubDeepResearchSynthesizer{}

	buildGeminiDeepResearchSearcher = func() (deepresearch.Searcher, error) {
		t.Fatalf("gemini searcher should not be used for codex deep research")
		return nil, nil
	}
	buildClaudeDeepResearchSearcher = func() (deepresearch.Searcher, error) {
		t.Fatalf("claude searcher should not be used for codex deep research")
		return nil, nil
	}
	buildCodexDeepResearchSearcher = func() (deepresearch.Searcher, error) {
		return searcher, nil
	}
	buildDeepResearchFetcher = func() (deepresearch.Fetcher, error) {
		return fetcher, nil
	}
	buildGeminiDeepResearchPlanner = func() (deepresearch.Planner, error) {
		t.Fatalf("gemini planner should not be used for codex deep research")
		return nil, nil
	}
	buildGeminiDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) {
		t.Fatalf("gemini checker should not be used for codex deep research")
		return nil, nil
	}
	buildGeminiDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) {
		t.Fatalf("gemini synthesizer should not be used for codex deep research")
		return nil, nil
	}
	buildClaudeDeepResearchPlanner = func() deepresearch.Planner {
		t.Fatalf("claude planner should not be used for codex deep research")
		return nil
	}
	buildClaudeDeepResearchChecker = func() deepresearch.CompletenessChecker {
		t.Fatalf("claude checker should not be used for codex deep research")
		return nil
	}
	buildClaudeDeepResearchSynthesizer = func() deepresearch.Synthesizer {
		t.Fatalf("claude synthesizer should not be used for codex deep research")
		return nil
	}
	buildCodexDeepResearchPlanner = func() (deepresearch.Planner, error) {
		return planner, nil
	}
	buildCodexDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) {
		return checker, nil
	}
	buildCodexDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) {
		return synthesizer, nil
	}

	method := &DeepResearchMethod{
		Method:             "deep_research_codex_runtime",
		Description:        "Codex deep research runtime test",
		QueryTemplate:      "Research {{topic}}",
		ResearchSchemaJSON: `{"type":"object","properties":{"summary":{"type":"string"}}}`,
		OptionsJSON:        `{"llm_backend":"codex","search_results_per_query":1}`,
		Enabled:            true,
	}

	result, err := runDeepResearchMethodTestWithProgress(context.Background(), method, deepResearchMethodTestRequest{
		Input: map[string]any{"topic": "AI agents"},
	}, nil)
	if err != nil {
		t.Fatalf("runDeepResearchMethodTestWithProgress failed: %v", err)
	}
	if searcher.calls == 0 {
		t.Fatalf("expected codex searcher to be called")
	}
	if planner.calls == 0 {
		t.Fatalf("expected codex planner to be called")
	}
	if checker.calls == 0 {
		t.Fatalf("expected codex checker to be called")
	}
	if synthesizer.calls == 0 {
		t.Fatalf("expected codex synthesizer to be called")
	}
	warnings := strings.Join(result.Result.Warnings, " | ")
	if !strings.Contains(warnings, "search_provider: codex_web_search") {
		t.Fatalf("expected codex search provider warning, got %#v", result.Result.Warnings)
	}
	if !strings.Contains(warnings, "fetch_provider: read_webpage_tool_fetcher") {
		t.Fatalf("expected read_webpage fetch provider warning, got %#v", result.Result.Warnings)
	}
}

// TestRunDeepResearchMethodTestWithProgress_CodexFactoryErrorsPropagate pins
// that each codex factory's error return surfaces as a run failure instead of
// being swallowed. Codex factories uniquely return (T, error) — the Claude
// factories don't — so error propagation is the behavior worth testing. One
// subtest per factory + the searcher makes it obvious which branch broke when
// a regression flips the wiring.
func TestRunDeepResearchMethodTestWithProgress_CodexFactoryErrorsPropagate(t *testing.T) {
	origCodexSearcher := buildCodexDeepResearchSearcher
	origCodexPlanner := buildCodexDeepResearchPlanner
	origCodexChecker := buildCodexDeepResearchChecker
	origCodexSynthesizer := buildCodexDeepResearchSynthesizer
	origFetcher := buildDeepResearchFetcher
	defer func() {
		buildCodexDeepResearchSearcher = origCodexSearcher
		buildCodexDeepResearchPlanner = origCodexPlanner
		buildCodexDeepResearchChecker = origCodexChecker
		buildCodexDeepResearchSynthesizer = origCodexSynthesizer
		buildDeepResearchFetcher = origFetcher
	}()

	okSearcher := func() (deepresearch.Searcher, error) {
		return &testDeepResearchSearcher{hits: []deepresearch.SearchHit{{URL: "https://ex.com"}}}, nil
	}
	okPlanner := func() (deepresearch.Planner, error) { return &stubDeepResearchPlanner{}, nil }
	okChecker := func() (deepresearch.CompletenessChecker, error) { return &stubDeepResearchChecker{}, nil }
	okSynthesizer := func() (deepresearch.Synthesizer, error) { return &stubDeepResearchSynthesizer{}, nil }
	buildDeepResearchFetcher = func() (deepresearch.Fetcher, error) { return &testDeepResearchFetcher{}, nil }

	cases := []struct {
		name        string
		setup       func()
		wantErrPart string
	}{
		{
			name: "searcher",
			setup: func() {
				buildCodexDeepResearchSearcher = func() (deepresearch.Searcher, error) {
					return nil, errors.New("searcher-boom")
				}
				buildCodexDeepResearchPlanner = okPlanner
				buildCodexDeepResearchChecker = okChecker
				buildCodexDeepResearchSynthesizer = okSynthesizer
			},
			wantErrPart: "searcher-boom",
		},
		{
			name: "planner",
			setup: func() {
				buildCodexDeepResearchSearcher = okSearcher
				buildCodexDeepResearchPlanner = func() (deepresearch.Planner, error) {
					return nil, errors.New("planner-boom")
				}
				buildCodexDeepResearchChecker = okChecker
				buildCodexDeepResearchSynthesizer = okSynthesizer
			},
			wantErrPart: "planner-boom",
		},
		{
			name: "checker",
			setup: func() {
				buildCodexDeepResearchSearcher = okSearcher
				buildCodexDeepResearchPlanner = okPlanner
				buildCodexDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) {
					return nil, errors.New("checker-boom")
				}
				buildCodexDeepResearchSynthesizer = okSynthesizer
			},
			wantErrPart: "checker-boom",
		},
		{
			name: "synthesizer",
			setup: func() {
				buildCodexDeepResearchSearcher = okSearcher
				buildCodexDeepResearchPlanner = okPlanner
				buildCodexDeepResearchChecker = okChecker
				buildCodexDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) {
					return nil, errors.New("synthesizer-boom")
				}
			},
			wantErrPart: "synthesizer-boom",
		},
	}

	method := &DeepResearchMethod{
		Method:        "deep_research_codex_error",
		QueryTemplate: "Research {{topic}}",
		OptionsJSON:   `{"llm_backend":"codex","search_results_per_query":1}`,
		Enabled:       true,
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			_, err := runDeepResearchMethodTestWithProgress(context.Background(), method, deepResearchMethodTestRequest{
				Input: map[string]any{"topic": "AI"},
			}, nil)
			if err == nil {
				t.Fatalf("expected error from %s factory failure", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("error %q does not contain %q — codex %s branch may be swallowing the failure", err.Error(), tc.wantErrPart, tc.name)
			}
		})
	}
}

// TestBuildCodexDeepResearchFactoriesAreOverridable pins that the three codex
// factory vars exist as package-level var()s (not func decls), so tests can
// swap them. If a refactor ever converts them back to plain functions this
// test will fail to compile, forcing the author to preserve the override seam.
func TestBuildCodexDeepResearchFactoriesAreOverridable(t *testing.T) {
	origPlanner := buildCodexDeepResearchPlanner
	origChecker := buildCodexDeepResearchChecker
	origSynthesizer := buildCodexDeepResearchSynthesizer
	defer func() {
		buildCodexDeepResearchPlanner = origPlanner
		buildCodexDeepResearchChecker = origChecker
		buildCodexDeepResearchSynthesizer = origSynthesizer
	}()

	planner := &stubDeepResearchPlanner{}
	checker := &stubDeepResearchChecker{}
	synthesizer := &stubDeepResearchSynthesizer{}

	buildCodexDeepResearchPlanner = func() (deepresearch.Planner, error) { return planner, nil }
	buildCodexDeepResearchChecker = func() (deepresearch.CompletenessChecker, error) { return checker, nil }
	buildCodexDeepResearchSynthesizer = func() (deepresearch.Synthesizer, error) { return synthesizer, nil }

	gotPlanner, err := buildCodexDeepResearchPlanner()
	if err != nil || gotPlanner != planner {
		t.Fatalf("codex planner override not honored: got=%v err=%v", gotPlanner, err)
	}
	gotChecker, err := buildCodexDeepResearchChecker()
	if err != nil || gotChecker != checker {
		t.Fatalf("codex checker override not honored: got=%v err=%v", gotChecker, err)
	}
	gotSynthesizer, err := buildCodexDeepResearchSynthesizer()
	if err != nil || gotSynthesizer != synthesizer {
		t.Fatalf("codex synthesizer override not honored: got=%v err=%v", gotSynthesizer, err)
	}
}

// TestDetachedDeepResearchContextWithoutShutdown pins the nil-shutdown branch:
// the returned ctx must detach from parent cancellation (the whole point of
// context.WithoutCancel) and only cancel when the timeout elapses or the
// returned CancelFunc is invoked. Used by tests and by callers that have no
// lifecycle signal to bind to.
func TestDetachedDeepResearchContextWithoutShutdown(t *testing.T) {
	type parentKey struct{}
	parentCtx, parentCancel := context.WithCancel(context.WithValue(context.Background(), parentKey{}, "trace-id"))

	ctx, cancel := detachedDeepResearchContext(parentCtx, nil, time.Hour)
	defer cancel()

	if got, _ := ctx.Value(parentKey{}).(string); got != "trace-id" {
		t.Fatalf("ctx.Value(parentKey) = %q, want %q — detached ctx must preserve parent values", got, "trace-id")
	}

	parentCancel()
	select {
	case <-ctx.Done():
		t.Fatalf("parent cancellation reached detached ctx — context.WithoutCancel should sever it")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatalf("explicit cancel() did not cancel detached ctx")
	}
}

// TestDetachedDeepResearchContextShutdownFiresCancel pins the core new
// guarantee: when shutdownCtx is cancelled, the detached ctx cancels promptly
// even though parent is still live. This is what lets SIGTERM tear down
// long-running deep-research runs across all three call sites (broker, HTTP
// test, HTTP stream).
func TestDetachedDeepResearchContextShutdownFiresCancel(t *testing.T) {
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	ctx, cancel := detachedDeepResearchContext(context.Background(), shutdownCtx, time.Hour)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("detached ctx cancelled before shutdown fired")
	default:
	}

	shutdown()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("shutdownCtx cancellation did not propagate to detached ctx within 2s")
	}
	if !errors.Is(ctx.Err(), context.Canceled) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("unexpected ctx.Err() = %v", ctx.Err())
	}
}

// TestDetachedDeepResearchContextTimeoutFires pins the third cancellation
// path: when neither parent nor shutdown fires, the timeout governs — which
// is the whole reason callers use this helper (the method's own per-run
// budget, up to 15 min, not the surrounding HTTP WriteTimeout).
func TestDetachedDeepResearchContextTimeoutFires(t *testing.T) {
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	ctx, cancel := detachedDeepResearchContext(context.Background(), shutdownCtx, 20*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout did not fire within 2s (budget was 20ms)")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want context.DeadlineExceeded — timeout branch must surface as DeadlineExceeded so callers can distinguish it from shutdown/cancel", ctx.Err())
	}
}

// TestDetachedDeepResearchContextParentCancelIsSevered pins the insulation
// invariant: cancelling the parent ctx (e.g. HTTP request ended) must not
// leak cancellation into the run ctx. Without context.WithoutCancel the
// manager's 5-minute HTTP WriteTimeout would kill multi-minute codex runs.
func TestDetachedDeepResearchContextParentCancelIsSevered(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	ctx, cancel := detachedDeepResearchContext(parentCtx, shutdownCtx, time.Hour)
	defer cancel()

	parentCancel()
	select {
	case <-ctx.Done():
		t.Fatalf("parent cancel reached detached ctx despite context.WithoutCancel")
	case <-time.After(100 * time.Millisecond):
	}
}

// TestDeepResearchMethodTestHandlerShutdownCancelsInFlight pins that the
// non-streaming /test HTTP endpoint honors h.shutdownCtx. Regressions that
// forget to plumb shutdownCtx into detachedDeepResearchContext would keep
// in-flight work running until the per-method timeout (up to 15 min).
func TestDeepResearchMethodTestHandlerShutdownCancelsInFlight(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	h.shutdownCtx = shutdownCtx

	createBody := `{
		"method":"deep_research_test_shutdown",
		"description":"Shutdown test",
		"input_schema":{"type":"object","properties":{"topic":{"type":"string"}}},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	originalRunner := deepResearchRunMethodTest
	defer func() { deepResearchRunMethodTest = originalRunner }()

	entered := make(chan struct{})
	observed := make(chan error, 1)
	deepResearchRunMethodTest = func(runCtx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest) (*deepResearchMethodTestResult, error) {
		close(entered)
		<-runCtx.Done()
		err := runCtx.Err()
		observed <- err
		return nil, err
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_test_shutdown/test", strings.NewReader(`{"input":{"topic":"AI"}}`))
	testRec := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		h.handleDeepResearchMethods(testRec, testReq)
		close(handlerDone)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("runner was not entered within 2s")
	}

	shutdown()

	select {
	case err := <-observed:
		if err == nil {
			t.Fatalf("runner observed ctx.Err() == nil; shutdown must cancel the detached run ctx")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("shutdownCtx cancellation did not propagate to test handler run ctx within 2s")
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("test handler did not return within 2s after shutdown")
	}
}

// TestDeepResearchMethodStreamHandlerShutdownCancelsInFlight pins the same
// graceful-shutdown contract for the /test-stream endpoint. This path
// launches the runner in a goroutine and streams progress events, so the
// shutdown signal must still reach the detached run ctx.
func TestDeepResearchMethodStreamHandlerShutdownCancelsInFlight(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	h.shutdownCtx = shutdownCtx

	createBody := `{
		"method":"deep_research_stream_shutdown",
		"description":"Stream shutdown test",
		"input_schema":{"type":"object","properties":{"topic":{"type":"string"}}},
		"enabled":true
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleDeepResearchMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}

	originalRunner := deepResearchRunMethodTestProgress
	defer func() { deepResearchRunMethodTestProgress = originalRunner }()

	entered := make(chan struct{})
	observed := make(chan error, 1)
	deepResearchRunMethodTestProgress = func(runCtx context.Context, method *DeepResearchMethod, req deepResearchMethodTestRequest, progress func(deepresearch.ProgressEvent)) (*deepResearchMethodTestResult, error) {
		close(entered)
		<-runCtx.Done()
		err := runCtx.Err()
		observed <- err
		return nil, err
	}

	streamReq := httptest.NewRequest(http.MethodPost, "/api/deep-research-methods/deep_research_stream_shutdown/test-stream", strings.NewReader(`{"input":{"topic":"AI"}}`))
	streamRec := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		h.handleDeepResearchMethods(streamRec, streamReq)
		close(handlerDone)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("stream runner was not entered within 2s")
	}

	shutdown()

	select {
	case err := <-observed:
		if err == nil {
			t.Fatalf("stream runner observed ctx.Err() == nil; shutdown must cancel the detached run ctx")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("shutdownCtx cancellation did not propagate to stream handler run ctx within 2s")
	}
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("stream handler did not return within 2s after shutdown")
	}
}
