package deepresearch

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func deepResearchJSONCandidateResponse(raw string) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: raw}},
				},
			},
		},
	}
}

func TestGeminiDeepResearchPlannerPlanParsesAndFiltersQueries(t *testing.T) {
	var gotModel string
	var gotCfg *genai.GenerateContentConfig

	planner := &geminiDeepResearchPlanner{
		model: "planner-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			gotModel = model
			gotCfg = cfg
			return deepResearchJSONCandidateResponse(`{
				"queries":[
					{"objective_key":"research","query":"fresh evidence query","rationale":"latest primary sources"},
					{"objective_key":"other","query":"should be filtered"},
					{"objective_key":"research","query":"  "}
				],
				"reasoning":"Need current and primary evidence."
			}`), nil
		},
	}

	req := PlanningRequest{
		Query:    "Will event happen?",
		Guidance: "Do not use prediction markets.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"probability": map[string]any{"type": "number"},
			},
		},
		MissingObjectives: []Objective{
			{Key: "research"},
		},
		Depth: 0,
		Round: 1,
	}

	result, err := planner.Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if gotModel != "planner-model" {
		t.Fatalf("model = %q, want %q", gotModel, "planner-model")
	}
	if gotCfg == nil || gotCfg.ResponseSchema == nil || gotCfg.ResponseMIMEType != "application/json" {
		t.Fatalf("planner config missing structured-json settings: %#v", gotCfg)
	}
	if len(result.Queries) != 1 {
		t.Fatalf("expected 1 filtered query, got %d (%#v)", len(result.Queries), result.Queries)
	}
	if result.Queries[0].ObjectiveKey != "research" || result.Queries[0].Query != "fresh evidence query" {
		t.Fatalf("unexpected filtered query: %#v", result.Queries[0])
	}
	if !strings.Contains(result.Reasoning, "current and primary evidence") {
		t.Fatalf("unexpected reasoning: %q", result.Reasoning)
	}
}

func TestGeminiDeepResearchPlannerPlanWithNoMissingObjectives(t *testing.T) {
	called := false
	planner := &geminiDeepResearchPlanner{
		model: "planner-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			called = true
			return nil, errors.New("should not be called")
		},
	}

	result, err := planner.Plan(context.Background(), PlanningRequest{})
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if called {
		t.Fatalf("expected generateContent not to be called")
	}
	if len(result.Queries) != 0 {
		t.Fatalf("expected no queries, got %#v", result.Queries)
	}
}

func TestGeminiDeepResearchPlannerPlanReturnsJSONError(t *testing.T) {
	planner := &geminiDeepResearchPlanner{
		model: "planner-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return deepResearchJSONCandidateResponse(`{"queries":[`), nil
		},
	}

	_, err := planner.Plan(context.Background(), PlanningRequest{
		Query:             "q",
		MissingObjectives: []Objective{{Key: "research"}},
	})
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if !strings.Contains(err.Error(), "planner returned invalid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeminiDeepResearchPlannerPlanReturnsEmptyResponseError(t *testing.T) {
	planner := &geminiDeepResearchPlanner{
		model: "planner-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return deepResearchJSONCandidateResponse("   "), nil
		},
	}

	_, err := planner.Plan(context.Background(), PlanningRequest{
		Query:             "q",
		MissingObjectives: []Objective{{Key: "research"}},
	})
	if err == nil {
		t.Fatalf("expected empty-response error")
	}
	if !strings.Contains(err.Error(), "planner returned empty response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewGeminiDeepResearchPlannerRequiresAPIKey(t *testing.T) {
	orig := os.Getenv("GEMINI_API_KEY")
	defer func() { _ = os.Setenv("GEMINI_API_KEY", orig) }()
	_ = os.Unsetenv("GEMINI_API_KEY")

	_, err := NewGeminiPlanner()
	if err == nil {
		t.Fatalf("expected error when GEMINI_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "Gemini API not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

