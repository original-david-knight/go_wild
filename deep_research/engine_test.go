package deepresearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockSearcher struct {
	mu      sync.Mutex
	calls   []SearchRequest
	results func(SearchRequest) ([]SearchHit, error)
}

func (m *mockSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	if m.results == nil {
		return nil, nil
	}
	return m.results(req)
}

func (m *mockSearcher) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockFetcher struct {
	mu      sync.Mutex
	calls   []string
	results func(string) (FetchedDocument, error)
}

func (m *mockFetcher) Fetch(ctx context.Context, url string) (FetchedDocument, error) {
	m.mu.Lock()
	m.calls = append(m.calls, url)
	m.mu.Unlock()
	if m.results == nil {
		return FetchedDocument{}, nil
	}
	return m.results(url)
}

func (m *mockFetcher) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockPlanner struct {
	mu      sync.Mutex
	calls   []PlanningRequest
	results func(PlanningRequest) (PlanningResult, error)
}

func (m *mockPlanner) Plan(ctx context.Context, req PlanningRequest) (PlanningResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	if m.results == nil {
		return PlanningResult{}, nil
	}
	return m.results(req)
}

func (m *mockPlanner) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockCompletenessChecker struct {
	mu      sync.Mutex
	calls   []CompletenessRequest
	results func(CompletenessRequest) (CompletenessResult, error)
}

func (m *mockCompletenessChecker) Check(ctx context.Context, req CompletenessRequest) (CompletenessResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	if m.results == nil {
		return CompletenessResult{}, nil
	}
	return m.results(req)
}

func (m *mockCompletenessChecker) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockSynthesizer struct {
	mu      sync.Mutex
	calls   []SynthesisRequest
	results func(SynthesisRequest) (SynthesisResult, error)
}

func (m *mockSynthesizer) Synthesize(ctx context.Context, req SynthesisRequest) (SynthesisResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	if m.results == nil {
		return SynthesisResult{}, nil
	}
	return m.results(req)
}

func (m *mockSynthesizer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestEngineRunSatisfiesObjectives(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{
					URL:     fmt.Sprintf("https://example.com/%s", req.ObjectiveKey),
					Title:   "Result for " + req.ObjectiveKey,
					Snippet: "Evidence about " + req.ObjectiveKey,
				},
			}, nil
		},
	}
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			return FetchedDocument{URL: url, Title: "Fetched", Content: "Long evidence body"}, nil
		},
	}

	engine := newEngine(searcher, fetcher)
	fixedNow := time.Date(2026, 2, 14, 10, 0, 0, 0, time.UTC)
	engine.nowFn = func() time.Time { return fixedNow }

	result, err := engine.Run(context.Background(), Request{
		Query: "quantum computing market",
		Objectives: []Objective{
			{Key: "market_size", Required: true},
			{Key: "top_competitors", Required: true},
		},
		Options: Options{
			MaxDepth:                2,
			SearchResultsPerQuery:   2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 1 {
		t.Fatalf("expected 1 round, got %d", result.Rounds)
	}
	if len(result.Objectives) != 2 {
		t.Fatalf("expected 2 objective results, got %d", len(result.Objectives))
	}
	for _, objective := range result.Objectives {
		if objective.Status != ObjectiveStatusSatisfied {
			t.Fatalf("objective %s status=%s, want satisfied", objective.Objective.Key, objective.Status)
		}
		if objective.EvidenceCount != 1 {
			t.Fatalf("objective %s evidence=%d, want 1", objective.Objective.Key, objective.EvidenceCount)
		}
	}
	if searcher.callCount() != 2 {
		t.Fatalf("expected 2 search calls, got %d", searcher.callCount())
	}
	if fetcher.callCount() != 2 {
		t.Fatalf("expected 2 fetch calls, got %d", fetcher.callCount())
	}
}

func TestEngineRunUsesPlannerQueries(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			if !strings.Contains(req.Query, "planned-query") {
				t.Fatalf("expected planned query, got %q", req.Query)
			}
			return []SearchHit{{
				URL:     "https://example.com/planned",
				Title:   "Planned Evidence",
				Snippet: "Evidence via planner query",
			}}, nil
		},
	}
	planner := &mockPlanner{
		results: func(req PlanningRequest) (PlanningResult, error) {
			if req.Depth != 0 {
				t.Fatalf("expected depth 0 planning call, got %d", req.Depth)
			}
			if len(req.MissingObjectives) != 1 || req.MissingObjectives[0].Key != "market_size" {
				t.Fatalf("unexpected missing objectives: %#v", req.MissingObjectives)
			}
			return PlanningResult{
				Queries: []PlannedQuery{
					{ObjectiveKey: "market_size", Query: "planned-query market size"},
				},
			}, nil
		},
	}

	engine := newEngineWithPlanner(searcher, nil, planner)

	result, err := engine.Run(context.Background(), Request{
		Query: "market outlook",
		Objectives: []Objective{
			{Key: "market_size", Required: true},
		},
		Options: Options{
			MaxDepth:                1,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if planner.callCount() != 1 {
		t.Fatalf("expected 1 planner call, got %d", planner.callCount())
	}
	if searcher.callCount() != 1 {
		t.Fatalf("expected 1 search call, got %d", searcher.callCount())
	}
	if result.Objectives[0].Status != ObjectiveStatusSatisfied {
		t.Fatalf("expected satisfied objective, got %s", result.Objectives[0].Status)
	}
}

func TestEngineRunUsesCompletenessCheckerAndSynthesizer(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			switch req.ObjectiveKey {
			case "field_a":
				return []SearchHit{{
					URL:     "https://example.com/a",
					Title:   "A",
					Snippet: "A snippet",
				}}, nil
			case "field_b":
				return []SearchHit{{
					URL:     "https://example.com/b",
					Title:   "B",
					Snippet: "B snippet",
				}}, nil
			default:
				return nil, nil
			}
		},
	}
	checker := &mockCompletenessChecker{
		results: func(req CompletenessRequest) (CompletenessResult, error) {
			if req.Round == 1 {
				return CompletenessResult{
					Complete: false,
					MissingObjectives: []MissingObjective{
						{ObjectiveKey: "field_b", Question: "need field_b evidence"},
					},
					Reasoning: "field_b missing",
				}, nil
			}
			return CompletenessResult{Complete: true}, nil
		},
	}
	synth := &mockSynthesizer{
		results: func(req SynthesisRequest) (SynthesisResult, error) {
			return SynthesisResult{
				Output: map[string]any{
					"field_a": "value_a",
					"field_b": "value_b",
				},
				Summary: "Synthesized schema output",
			}, nil
		},
	}

	engine := NewEngineWithReasoning(searcher, nil, nil, checker, synth)
	result, err := engine.Run(context.Background(), Request{
		Query: "checker/synth test",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"field_a": map[string]any{"type": "string"},
				"field_b": map[string]any{"type": "string"},
			},
			"required": []any{"field_a", "field_b"},
		},
		Options: Options{
			MaxDepth:                2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if checker.callCount() == 0 {
		t.Fatalf("expected completeness checker to be called")
	}
	if synth.callCount() != 1 {
		t.Fatalf("expected synthesizer to be called once, got %d", synth.callCount())
	}
	if !result.SchemaSatisfied {
		t.Fatalf("expected schema satisfied")
	}
	if len(result.MissingObjectiveKeys) != 0 {
		t.Fatalf("expected no missing objective keys, got %#v", result.MissingObjectiveKeys)
	}
	if got := strings.TrimSpace(result.Summary); got != "Synthesized schema output" {
		t.Fatalf("unexpected synthesized summary: %q", got)
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized output map, got %T", result.Output)
	}
	if output["field_a"] != "value_a" || output["field_b"] != "value_b" {
		t.Fatalf("unexpected synthesized output: %#v", output)
	}
	if searcher.callCount() < 2 {
		t.Fatalf("expected at least 2 search calls, got %d", searcher.callCount())
	}
}

func TestEngineRunRecursesWhenObjectiveMissing(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			if req.ObjectiveKey == "hard_gap" && req.Depth == 0 {
				return nil, nil
			}
			return []SearchHit{{
				URL:     fmt.Sprintf("https://example.com/%s/%d", req.ObjectiveKey, req.Depth),
				Title:   "Evidence",
				Snippet: "Details",
			}}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "supply chain disruption",
		Objectives: []Objective{
			{Key: "hard_gap", Required: true},
		},
		Options: Options{
			MaxDepth:                3,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 2 {
		t.Fatalf("expected 2 rounds, got %d", result.Rounds)
	}
	if len(result.Objectives) != 1 {
		t.Fatalf("expected 1 objective result, got %d", len(result.Objectives))
	}
	if result.Objectives[0].Status != ObjectiveStatusSatisfied {
		t.Fatalf("expected objective satisfied, got %s", result.Objectives[0].Status)
	}

	foundDepth1 := false
	searcher.mu.Lock()
	for _, call := range searcher.calls {
		if call.ObjectiveKey == "hard_gap" && call.Depth == 1 {
			foundDepth1 = true
			if !strings.Contains(call.Query, "detailed") {
				t.Fatalf("expected depth>0 query to include detailed marker, got %q", call.Query)
			}
		}
	}
	searcher.mu.Unlock()
	if !foundDepth1 {
		t.Fatalf("expected depth=1 retry call for hard_gap")
	}
}

func TestEngineRunDedupesURLs(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://dup.example.com", Title: "Primary", Snippet: "A"},
				{URL: "https://dup.example.com", Title: "Secondary", Snippet: "B"},
			}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "duplicate test",
		Objectives: []Objective{
			{Key: "dup_obj", Required: true},
		},
		Options: Options{
			MinEvidencePerObjective: 1,
			SearchResultsPerQuery:   2,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	rows := result.Findings["dup_obj"]
	if len(rows) != 1 {
		t.Fatalf("expected deduped findings length 1, got %d", len(rows))
	}
	if len(result.Sources) != 1 {
		t.Fatalf("expected deduped sources length 1, got %d", len(result.Sources))
	}
}

func TestEngineRunEmitsProgressSourceEvents(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/a", Title: "Source A", Snippet: "A"},
				{URL: "https://example.com/b", Title: "Source B", Snippet: "B"},
			}, nil
		},
	}
	engine := newEngine(searcher, nil)

	var mu sync.Mutex
	events := make([]ProgressEvent, 0, 16)
	progress := func(event ProgressEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	_, err := engine.Run(context.Background(), Request{
		Query: "progress test",
		Objectives: []Objective{
			{Key: "test_obj", Required: true},
		},
		Options: Options{
			MinEvidencePerObjective: 1,
			SearchResultsPerQuery:   2,
		},
		Progress: progress,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatalf("expected progress events")
	}

	foundSourceA := false
	foundSourceB := false
	foundSearchStart := false
	foundRunComplete := false
	for _, event := range events {
		switch event.Stage {
		case "search_start":
			foundSearchStart = true
		case "source":
			if event.URL == "https://example.com/a" {
				foundSourceA = true
			}
			if event.URL == "https://example.com/b" {
				foundSourceB = true
			}
		case "run_complete":
			foundRunComplete = true
		}
	}
	if !foundSearchStart {
		t.Fatalf("expected search_start progress event")
	}
	if !foundSourceA || !foundSourceB {
		t.Fatalf("expected source events for both URLs, got %#v", events)
	}
	if !foundRunComplete {
		t.Fatalf("expected run_complete progress event")
	}
}

func TestEngineRunWithFetcherAttemptsAllHits(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/full", Title: "Full Source", Snippet: "snippet a"},
				{URL: "https://example.com/empty", Title: "Empty Source", Snippet: "snippet b"},
				{URL: "https://example.com/error", Title: "Error Source", Snippet: "snippet c"},
			}, nil
		},
	}
	var fetchedURLs []string
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			fetchedURLs = append(fetchedURLs, url)
			switch url {
			case "https://example.com/full":
				return FetchedDocument{URL: url, Title: "Full Source", Content: "Long full-page content"}, nil
			case "https://example.com/empty":
				return FetchedDocument{URL: url, Title: "Empty Source", Content: ""}, nil
			default:
				return FetchedDocument{}, fmt.Errorf("fetch failed for %s", url)
			}
		},
	}
	engine := newEngine(searcher, fetcher)

	result, err := engine.Run(context.Background(), Request{
		Query: "fetch all test",
		Objectives: []Objective{
			{Key: "objective_a", Required: true},
		},
		Options: Options{
			SearchResultsPerQuery:   3,
			MinEvidencePerObjective: 1,
			MaxDepth:                0,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// All three URLs should have been fetched — no rank-based filtering.
	if len(fetchedURLs) != 3 {
		t.Fatalf("expected 3 fetch attempts, got %d: %v", len(fetchedURLs), fetchedURLs)
	}
	// Only the URL with actual content becomes a finding.
	rows := result.Findings["objective_a"]
	if len(rows) != 1 {
		t.Fatalf("expected 1 finding (empty/error filtered), got %d", len(rows))
	}
	if rows[0].URL != "https://example.com/full" {
		t.Fatalf("unexpected finding URL: %q", rows[0].URL)
	}
	if strings.TrimSpace(rows[0].Excerpt) == "" {
		t.Fatalf("expected fetched excerpt to be present")
	}
}

func TestEngineRunWithFetcherRequiresFetchedContentForEvidence(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/unfetchable", Title: "Unfetchable", Snippet: "snippet only"},
			}, nil
		},
	}
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			return FetchedDocument{}, fmt.Errorf("fetch failed")
		},
	}
	engine := newEngine(searcher, fetcher)

	result, err := engine.Run(context.Background(), Request{
		Query: "must fetch",
		Objectives: []Objective{
			{Key: "objective_b", Required: true},
		},
		Options: Options{
			SearchResultsPerQuery:   1,
			MinEvidencePerObjective: 1,
			MaxDepth:                0,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	rows := result.Findings["objective_b"]
	if len(rows) != 0 {
		t.Fatalf("expected no evidence findings when fetch fails, got %d", len(rows))
	}
	if len(result.Objectives) != 1 || result.Objectives[0].Status != ObjectiveStatusMissing {
		t.Fatalf("expected missing objective when fetch fails, got %#v", result.Objectives)
	}
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, " | "), "fetch failed") {
		t.Fatalf("expected fetch warning, got %#v", result.Warnings)
	}
}

func TestEngineRunExcludesConfiguredDomains(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://polymarket.com/event/foo", Title: "Blocked", Snippet: "blocked"},
				{URL: "https://reuters.com/world/story", Title: "Allowed", Snippet: "allowed"},
			}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "domain policy",
		Objectives: []Objective{
			{Key: "objective_c", Required: true},
		},
		Options: Options{
			SearchResultsPerQuery:   2,
			MinEvidencePerObjective: 1,
			MaxDepth:                0,
			ExcludedDomains:         []string{"polymarket.com"},
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	rows := result.Findings["objective_c"]
	if len(rows) != 1 {
		t.Fatalf("expected exactly one allowed finding, got %d (%#v)", len(rows), rows)
	}
	if rows[0].Domain != "reuters.com" {
		t.Fatalf("expected allowed domain reuters.com, got %q", rows[0].Domain)
	}
	if !strings.Contains(strings.Join(result.Warnings, " | "), "excluded source skipped") {
		t.Fatalf("expected excluded-source warning, got %#v", result.Warnings)
	}
}

func TestEngineRunSearcherReturnsError(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return nil, fmt.Errorf("upstream timeout")
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "failing search",
		Objectives: []Objective{
			{Key: "obj_a", Required: true},
		},
		Options: Options{
			MaxDepth:                1,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run should not return error, got %v", err)
	}
	if len(result.Findings["obj_a"]) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings["obj_a"]))
	}
	if len(result.Objectives) != 1 || result.Objectives[0].Status != ObjectiveStatusMissing {
		t.Fatalf("expected missing objective, got %#v", result.Objectives)
	}
	joined := strings.Join(result.Warnings, " | ")
	if !strings.Contains(joined, "upstream timeout") {
		t.Fatalf("expected warning about upstream timeout, got %#v", result.Warnings)
	}
}

func TestEngineRunSearcherReturnsEmptyResults(t *testing.T) {
	callCount := 0
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			callCount++
			return []SearchHit{}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "empty results",
		Objectives: []Objective{
			{Key: "obj_empty", Required: true},
		},
		Options: Options{
			MaxDepth:                2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Findings["obj_empty"]) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings["obj_empty"]))
	}
	if result.Objectives[0].Status != ObjectiveStatusMissing {
		t.Fatalf("expected missing objective, got %s", result.Objectives[0].Status)
	}
	// MaxDepth=2 means depth 0,1,2 → 3 rounds of searching
	if result.Rounds != 3 {
		t.Fatalf("expected 3 rounds (recurse to MaxDepth), got %d", result.Rounds)
	}
}

func TestEngineRunFetcherErrorDiscardsFindings(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/fail", Title: "Failing", Snippet: "will fail"},
				{URL: "https://example.com/ok", Title: "Working", Snippet: "will succeed"},
			}, nil
		},
	}
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			if url == "https://example.com/fail" {
				return FetchedDocument{}, fmt.Errorf("connection refused")
			}
			return FetchedDocument{URL: url, Title: "Working Page", Content: "Full page content here"}, nil
		},
	}
	engine := newEngine(searcher, fetcher)

	result, err := engine.Run(context.Background(), Request{
		Query: "partial fetch",
		Objectives: []Objective{
			{Key: "obj_fetch", Required: true},
		},
		Options: Options{
			MaxDepth:                0,
			SearchResultsPerQuery:   2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	rows := result.Findings["obj_fetch"]
	if len(rows) != 1 {
		t.Fatalf("expected 1 finding (failed one discarded), got %d", len(rows))
	}
	if rows[0].URL != "https://example.com/ok" {
		t.Fatalf("expected ok URL, got %q", rows[0].URL)
	}
	joined := strings.Join(result.Warnings, " | ")
	if !strings.Contains(joined, "connection refused") {
		t.Fatalf("expected fetch warning, got %#v", result.Warnings)
	}
	if result.Objectives[0].Status != ObjectiveStatusSatisfied {
		t.Fatalf("expected satisfied (1 finding >= minEvidence=1), got %s", result.Objectives[0].Status)
	}
}

func TestEngineRunAllFetchersFail(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/a", Title: "A", Snippet: "a"},
				{URL: "https://example.com/b", Title: "B", Snippet: "b"},
			}, nil
		},
	}
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			return FetchedDocument{}, fmt.Errorf("dns error for %s", url)
		},
	}
	engine := newEngine(searcher, fetcher)

	result, err := engine.Run(context.Background(), Request{
		Query: "all fetches fail",
		Objectives: []Objective{
			{Key: "obj_all_fail", Required: true},
		},
		Options: Options{
			MaxDepth:                1,
			SearchResultsPerQuery:   2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Findings["obj_all_fail"]) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings["obj_all_fail"]))
	}
	if result.Objectives[0].Status != ObjectiveStatusMissing {
		t.Fatalf("expected missing objective, got %s", result.Objectives[0].Status)
	}
	foundA := false
	foundB := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "dns error for https://example.com/a") {
			foundA = true
		}
		if strings.Contains(w, "dns error for https://example.com/b") {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Fatalf("expected fetch warnings for both URLs, got %#v", result.Warnings)
	}
}

func TestEngineRunContextCancelledMidSearch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			cancel() // cancel during the first round's search
			return []SearchHit{{URL: "https://example.com/partial", Title: "Partial", Snippet: "partial"}}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(ctx, Request{
		Query: "cancel test",
		Objectives: []Objective{
			{Key: "obj_cancel", Required: true},
		},
		Options: Options{
			MaxDepth:                3,
			MinEvidencePerObjective: 3, // high threshold so objective stays unsatisfied after round 1
		},
	})
	if err == nil {
		t.Fatalf("expected context error, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	// Should have partial results from the first round
	if result.Query != "cancel test" {
		t.Fatalf("expected query in partial result, got %q", result.Query)
	}
}

func TestEngineRunEmptySchema(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/default", Title: "Default", Snippet: "default evidence"},
			}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query:   "no schema test",
		Schema:  map[string]any{},
		Options: Options{MaxDepth: 0, MinEvidencePerObjective: 1},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Objectives) != 1 {
		t.Fatalf("expected 1 default objective, got %d", len(result.Objectives))
	}
	if result.Objectives[0].Objective.Key != "research" {
		t.Fatalf("expected default 'overall' objective, got %q", result.Objectives[0].Objective.Key)
	}
	if result.Objectives[0].Status != ObjectiveStatusSatisfied {
		t.Fatalf("expected satisfied, got %s", result.Objectives[0].Status)
	}
	if len(result.Findings["research"]) != 1 {
		t.Fatalf("expected 1 finding under 'overall', got %d", len(result.Findings["research"]))
	}
}

func TestEngineRunMaxDepthZero(t *testing.T) {
	// MaxDepth=0 is normalized to 2 by withDefaults(), so the engine runs
	// depths 0..2 (3 rounds). Verify the engine exhausts all rounds when
	// objectives remain unsatisfied.
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return nil, nil // no results, objective stays unsatisfied
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "single round only",
		Objectives: []Objective{
			{Key: "obj_zero", Required: true},
		},
		Options: Options{
			MaxDepth:                0,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// MaxDepth 0 → defaults to 2 → depths 0,1,2 → 3 rounds
	if result.Rounds != 3 {
		t.Fatalf("expected 3 rounds (MaxDepth=0 defaults to 2), got %d", result.Rounds)
	}
	if result.Objectives[0].Status != ObjectiveStatusMissing {
		t.Fatalf("expected missing, got %s", result.Objectives[0].Status)
	}
	// 1 search call per round × 3 rounds
	if searcher.callCount() != 3 {
		t.Fatalf("expected 3 search calls, got %d", searcher.callCount())
	}
}

func TestEngineRunAllObjectivesSatisfiedFirstRound(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: fmt.Sprintf("https://example.com/%s/1", req.ObjectiveKey), Title: "E1", Snippet: "evidence 1"},
				{URL: fmt.Sprintf("https://example.com/%s/2", req.ObjectiveKey), Title: "E2", Snippet: "evidence 2"},
			}, nil
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "all satisfied",
		Objectives: []Objective{
			{Key: "alpha", Required: true},
			{Key: "beta", Required: true},
		},
		Options: Options{
			MaxDepth:                5,
			MinEvidencePerObjective: 2,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Rounds != 1 {
		t.Fatalf("expected 1 round (all satisfied), got %d", result.Rounds)
	}
	for _, obj := range result.Objectives {
		if obj.Status != ObjectiveStatusSatisfied {
			t.Fatalf("expected all satisfied, %s is %s", obj.Objective.Key, obj.Status)
		}
	}
	if result.SchemaSatisfied != true {
		t.Fatalf("expected schema satisfied")
	}
	if len(result.MissingObjectiveKeys) != 0 {
		t.Fatalf("expected no missing keys, got %#v", result.MissingObjectiveKeys)
	}
}

func TestEngineRunPlannerFails(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/fallback", Title: "Fallback", Snippet: "fallback evidence"},
			}, nil
		},
	}
	planner := &mockPlanner{
		results: func(req PlanningRequest) (PlanningResult, error) {
			return PlanningResult{}, fmt.Errorf("LLM rate limited")
		},
	}
	engine := newEngineWithPlanner(searcher, nil, planner)

	result, err := engine.Run(context.Background(), Request{
		Query: "planner fail",
		Objectives: []Objective{
			{Key: "obj_plan", Required: true},
		},
		Options: Options{
			MaxDepth:                0,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	joined := strings.Join(result.Warnings, " | ")
	if !strings.Contains(joined, "planner failed") || !strings.Contains(joined, "LLM rate limited") {
		t.Fatalf("expected planner failure warning, got %#v", result.Warnings)
	}
	// Engine should still produce results via default queries
	if result.Objectives[0].Status != ObjectiveStatusSatisfied {
		t.Fatalf("expected objective satisfied via fallback queries, got %s", result.Objectives[0].Status)
	}
	if planner.callCount() != 1 {
		t.Fatalf("expected 1 planner call, got %d", planner.callCount())
	}
}

func TestEngineRunCheckerFails(t *testing.T) {
	round := 0
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			round++
			if round <= 1 {
				return nil, nil // first round: no results
			}
			return []SearchHit{
				{URL: "https://example.com/r2", Title: "Round2", Snippet: "evidence"},
			}, nil
		},
	}
	checker := &mockCompletenessChecker{
		results: func(req CompletenessRequest) (CompletenessResult, error) {
			return CompletenessResult{}, fmt.Errorf("checker model unavailable")
		},
	}
	engine := NewEngineWithReasoning(searcher, nil, nil, checker, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "checker fail",
		Objectives: []Objective{
			{Key: "obj_check", Required: true},
		},
		Options: Options{
			MaxDepth:                2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	joined := strings.Join(result.Warnings, " | ")
	if !strings.Contains(joined, "completeness checker failed") || !strings.Contains(joined, "checker model unavailable") {
		t.Fatalf("expected checker failure warning, got %#v", result.Warnings)
	}
	// Engine should fall back to local unsatisfied check and recurse
	if result.Rounds < 2 {
		t.Fatalf("expected at least 2 rounds (local fallback should recurse), got %d", result.Rounds)
	}
	if checker.callCount() == 0 {
		t.Fatalf("expected checker to be called at least once")
	}
}

func TestEngineRunDedupesRepeatedWarnings(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return nil, fmt.Errorf("upstream unavailable")
		},
	}
	engine := newEngine(searcher, nil)

	result, err := engine.Run(context.Background(), Request{
		Query: "latest policy update",
		Options: Options{
			MaxDepth:                2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	count := 0
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "search failed for research: upstream unavailable") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected deduped warning count=1, got %d warnings=%#v", count, result.Warnings)
	}
}

func TestEngineRunFetch404KeepsSnippetFinding(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/gone", Title: "Gone Page", Snippet: "useful snippet from search"},
				{URL: "https://example.com/ok", Title: "OK Page", Snippet: "ok snippet"},
			}, nil
		},
	}
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			if url == "https://example.com/gone" {
				return FetchedDocument{}, &fetchHTTPError{StatusCode: 404, Status: "404 Not Found"}
			}
			return FetchedDocument{URL: url, Title: "OK Page", Content: "Full page content"}, nil
		},
	}
	engine := newEngine(searcher, fetcher)

	result, err := engine.Run(context.Background(), Request{
		Query: "404 keep snippet",
		Objectives: []Objective{
			{Key: "obj_404", Required: true},
		},
		Options: Options{
			MaxDepth:                0,
			SearchResultsPerQuery:   2,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	rows := result.Findings["obj_404"]
	if len(rows) != 2 {
		t.Fatalf("expected 2 findings (404 kept with snippet + OK), got %d", len(rows))
	}
	// The 404 finding should keep its snippet and have no excerpt.
	var found404 bool
	for _, f := range rows {
		if f.URL == "https://example.com/gone" {
			found404 = true
			if f.Snippet != "useful snippet from search" {
				t.Fatalf("expected snippet preserved, got %q", f.Snippet)
			}
			if f.Excerpt != "" {
				t.Fatalf("expected no excerpt on 404 finding, got %q", f.Excerpt)
			}
		}
	}
	if !found404 {
		t.Fatalf("expected 404 finding to be kept")
	}
	if result.Objectives[0].Status != ObjectiveStatusSatisfied {
		t.Fatalf("expected satisfied, got %s", result.Objectives[0].Status)
	}
}

func TestEngineRunFetch404EmptySnippetDiscarded(t *testing.T) {
	searcher := &mockSearcher{
		results: func(req SearchRequest) ([]SearchHit, error) {
			return []SearchHit{
				{URL: "https://example.com/gone", Title: "Gone Page", Snippet: ""},
			}, nil
		},
	}
	fetcher := &mockFetcher{
		results: func(url string) (FetchedDocument, error) {
			return FetchedDocument{}, &fetchHTTPError{StatusCode: 404, Status: "404 Not Found"}
		},
	}
	engine := newEngine(searcher, fetcher)

	result, err := engine.Run(context.Background(), Request{
		Query: "404 empty snippet",
		Objectives: []Objective{
			{Key: "obj_empty_404", Required: true},
		},
		Options: Options{
			MaxDepth:                0,
			SearchResultsPerQuery:   1,
			MinEvidencePerObjective: 1,
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.Findings["obj_empty_404"]) != 0 {
		t.Fatalf("expected no findings when 404 and empty snippet, got %d", len(result.Findings["obj_empty_404"]))
	}
}

func TestCollapseFetchWarnings(t *testing.T) {
	warnings := []string{
		"search failed for obj_a: upstream timeout",
		"fetch failed for https://a.com/1: HTTP 404 404 Not Found",
		"fetch failed for https://b.com/2: HTTP 404 404 Not Found",
		"fetch failed for https://c.com/3: HTTP 404 404 Not Found",
		"fetch failed for https://d.com/4: HTTP 404 404 Not Found",
		"fetch failed for https://e.com/5: HTTP 500 Internal Server Error",
		"fetched content was empty for https://f.com/6",
	}
	collapsed := collapseFetchWarnings(warnings)

	// Non-404 warnings should be preserved as-is.
	foundUpstream := false
	found500 := false
	foundEmpty := false
	for _, w := range collapsed {
		if strings.Contains(w, "upstream timeout") {
			foundUpstream = true
		}
		if strings.Contains(w, "HTTP 500") {
			found500 = true
		}
		if strings.Contains(w, "fetched content was empty") {
			foundEmpty = true
		}
	}
	if !foundUpstream {
		t.Fatalf("expected upstream timeout warning preserved")
	}
	if !found500 {
		t.Fatalf("expected HTTP 500 warning preserved")
	}
	if !foundEmpty {
		t.Fatalf("expected empty content warning preserved")
	}

	// The four 404 warnings should be collapsed into one.
	var summary404 string
	for _, w := range collapsed {
		if strings.Contains(w, "fetch failed (HTTP 404) for") {
			summary404 = w
		}
	}
	if summary404 == "" {
		t.Fatalf("expected collapsed 404 summary, got warnings: %#v", collapsed)
	}
	if !strings.Contains(summary404, "4 URLs") {
		t.Fatalf("expected 4 URLs in summary, got %q", summary404)
	}
	if !strings.Contains(summary404, "and 1 more") {
		t.Fatalf("expected '(and 1 more)' in summary, got %q", summary404)
	}
}

func TestCollapseFetchWarningsNoCollapse(t *testing.T) {
	warnings := []string{
		"search failed for obj_a: timeout",
		"fetch failed for https://a.com/1: HTTP 500 Internal Server Error",
	}
	collapsed := collapseFetchWarnings(warnings)
	if len(collapsed) != len(warnings) {
		t.Fatalf("expected no collapsing, got %d vs %d", len(collapsed), len(warnings))
	}
}

func TestFetchHTTPErrorAs(t *testing.T) {
	origErr := &fetchHTTPError{StatusCode: 404, Status: "404 Not Found"}
	wrapped := fmt.Errorf("fetch failed: %w", origErr)

	var httpErr *fetchHTTPError
	if !errors.As(wrapped, &httpErr) {
		t.Fatalf("expected errors.As to find fetchHTTPError")
	}
	if httpErr.StatusCode != 404 {
		t.Fatalf("expected status 404, got %d", httpErr.StatusCode)
	}
}
