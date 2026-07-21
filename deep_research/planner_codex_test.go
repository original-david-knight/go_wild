package deepresearch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestCodexPlannerShortCircuitsWithoutMissingObjectives mirrors the Gemini
// planner's behavior: when the engine has nothing to plan (no missing
// objectives), the planner returns an empty result WITHOUT invoking codex.
// Calling the expensive LLM for a no-op would waste budget and could even
// fabricate queries for satisfied objectives — so skipping the call is the
// correct behavior and this test pins it.
func TestCodexPlannerShortCircuitsWithoutMissingObjectives(t *testing.T) {
	called := false
	p := &codexDeepResearchPlanner{
		generate: func(context.Context, string, string) (string, error) {
			called = true
			return "", errors.New("generate must not be invoked when there's nothing to plan")
		},
	}

	result, err := p.Plan(context.Background(), PlanningRequest{})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if called {
		t.Fatalf("planner must skip the codex call when MissingObjectives is empty")
	}
	if len(result.Queries) != 0 {
		t.Fatalf("expected empty queries, got %#v", result.Queries)
	}
}

// TestCodexPlannerPlanParsesAndFiltersQueries is the happy path + filtering
// guard. The planner must:
//   - parse the JSON envelope the model returns,
//   - trim whitespace on all fields,
//   - drop queries whose objective_key is not in the caller's MissingObjectives
//     (the model occasionally invents keys or suggests work for already-satisfied
//     objectives; treating those as authoritative would derail the pipeline),
//   - drop rows with an empty query or key entirely, and
//   - surface the model's reasoning unchanged.
func TestCodexPlannerPlanParsesAndFiltersQueries(t *testing.T) {
	payload := `{
        "queries":[
            {"objective_key":"research","query":"fresh primary sources","rationale":"latest evidence"},
            {"objective_key":"  research  ","query":"  trimmed query  ","rationale":"  ok  "},
            {"objective_key":"unknown","query":"should be filtered"},
            {"objective_key":"research","query":"   "},
            {"objective_key":"","query":"missing key"}
        ],
        "reasoning":"  need current evidence  "
    }`

	var gotPrompt string
	p := &codexDeepResearchPlanner{
		generate: func(_ context.Context, prompt, _ string) (string, error) {
			gotPrompt = prompt
			return payload, nil
		},
	}

	req := PlanningRequest{
		Query:             "Will event happen?",
		Guidance:          "Avoid prediction markets.",
		MissingObjectives: []Objective{{Key: "research"}},
		Depth:             1,
		Round:             2,
	}
	result, err := p.Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !strings.HasSuffix(gotPrompt, codexJSONSuffix) {
		t.Fatalf("planner prompt must end with the codex JSON-only suffix so the model returns bare JSON; suffix missing from:\n%s", gotPrompt)
	}
	if len(result.Queries) != 2 {
		t.Fatalf("expected 2 queries (unknown/empty filtered), got %d: %#v", len(result.Queries), result.Queries)
	}
	if result.Queries[0].Query != "fresh primary sources" || result.Queries[0].ObjectiveKey != "research" {
		t.Fatalf("first query malformed: %#v", result.Queries[0])
	}
	if result.Queries[1].ObjectiveKey != "research" || result.Queries[1].Query != "trimmed query" || result.Queries[1].Rationale != "ok" {
		t.Fatalf("second query must be trimmed: %#v", result.Queries[1])
	}
	if result.Reasoning != "need current evidence" {
		t.Fatalf("reasoning must be trimmed; got %q", result.Reasoning)
	}
}

// TestCodexPlannerPlanTolerantToFencedJSON locks in extractJSON behavior the
// planner relies on: the codex CLI sometimes returns markdown-fenced JSON
// despite the suffix telling it not to. The planner must still parse it.
func TestCodexPlannerPlanTolerantToFencedJSON(t *testing.T) {
	payload := "```json\n{\"queries\":[{\"objective_key\":\"research\",\"query\":\"q\"}],\"reasoning\":\"r\"}\n```"
	p := &codexDeepResearchPlanner{
		generate: func(context.Context, string, string) (string, error) {
			return payload, nil
		},
	}
	result, err := p.Plan(context.Background(), PlanningRequest{
		MissingObjectives: []Objective{{Key: "research"}},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v (fenced JSON must parse)", err)
	}
	if len(result.Queries) != 1 || result.Queries[0].Query != "q" {
		t.Fatalf("unexpected parsed queries: %#v", result.Queries)
	}
}

// TestCodexPlannerPlanSurfacesGenerateError covers the transport-level failure
// path: if codex itself errors (CLI crash, timeout, RequireWebSearchUse guard
// tripping when called from a searcher), Plan must wrap and propagate rather
// than returning a silent empty PlanningResult — an empty result is
// indistinguishable from "everything satisfied" downstream.
func TestCodexPlannerPlanSurfacesGenerateError(t *testing.T) {
	want := errors.New("codex CLI crashed")
	p := &codexDeepResearchPlanner{
		generate: func(context.Context, string, string) (string, error) {
			return "", want
		},
	}
	_, err := p.Plan(context.Background(), PlanningRequest{
		MissingObjectives: []Objective{{Key: "research"}},
	})
	if err == nil {
		t.Fatalf("expected generate error to propagate")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error chain should wrap the generate error; got %v", err)
	}
	if !strings.Contains(err.Error(), "codex planner") {
		t.Fatalf("wrapped error should name the role for log readability; got %v", err)
	}
}

// TestCodexPlannerPlanReturnsErrorOnInvalidJSON guards against the model
// returning non-JSON content: Plan must fail loudly rather than return an
// empty result the engine would silently accept as "nothing to do".
func TestCodexPlannerPlanReturnsErrorOnInvalidJSON(t *testing.T) {
	p := &codexDeepResearchPlanner{
		generate: func(context.Context, string, string) (string, error) {
			return `{"queries":[`, nil
		},
	}
	_, err := p.Plan(context.Background(), PlanningRequest{
		MissingObjectives: []Objective{{Key: "research"}},
	})
	if err == nil {
		t.Fatalf("expected invalid-JSON error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("error should name the JSON failure mode; got %v", err)
	}
}
