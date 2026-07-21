package deepresearch

import (
	"context"
	"os"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestDeepResearchCompletenessPromptEnforcesStrictJSONContract(t *testing.T) {
	req := CompletenessRequest{
		Query: "probability of statement",
		Depth: 1,
		Round: 2,
		ObjectiveResults: []ObjectiveResult{
			{
				Objective:     Objective{Key: "research"},
				Status:        ObjectiveStatusPartial,
				EvidenceCount: 1,
			},
		},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"probability": map[string]any{"type": "number"},
			},
		},
	}

	prompt := deepResearchCompletenessPrompt(req)
	checks := []string{
		"Return exactly one JSON object and nothing else",
		"Never wrap JSON in markdown code fences",
		"Always include missing_objectives",
		"objective_key must always be",
		"\"complete\": <boolean>",
		"evaluate holistically",
		"all fields must be coverable",
	}
	for _, check := range checks {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(check)) {
			t.Fatalf("prompt missing instruction %q: %s", check, prompt)
		}
	}
}

func TestDeepResearchCompletenessPromptIncludesTotalEvidence(t *testing.T) {
	req := CompletenessRequest{
		Query: "test query",
		Round: 1,
		ObjectiveResults: []ObjectiveResult{
			{
				Objective:     Objective{Key: "research"},
				Status:        ObjectiveStatusSatisfied,
				EvidenceCount: 7,
			},
		},
	}

	prompt := deepResearchCompletenessPrompt(req)
	if !strings.Contains(prompt, "Total sources collected: 7") {
		t.Fatalf("prompt missing total evidence count: %s", prompt)
	}
}

func TestGeminiDeepResearchCompletenessCheckerCheckParsesResult(t *testing.T) {
	checker := &geminiDeepResearchCompletenessChecker{
		model: "checker-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			if cfg == nil || cfg.ResponseSchema == nil || cfg.ResponseMIMEType != "application/json" {
				t.Fatalf("unexpected config: %#v", cfg)
			}
			return deepResearchJSONCandidateResponse(`{
				"complete": false,
				"reasoning": "Need stronger source triangulation.",
				"missing_objectives": [
					{"objective_key":"research","question":"Need one more primary source."}
				]
			}`), nil
		},
	}

	result, err := checker.Check(context.Background(), CompletenessRequest{
		Query:      "probability of event",
		Objectives: []Objective{{Key: "research"}},
		ObjectiveResults: []ObjectiveResult{
			{
				Objective:     Objective{Key: "research"},
				Status:        ObjectiveStatusPartial,
				EvidenceCount: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result.Complete {
		t.Fatalf("expected complete=false")
	}
	if len(result.MissingObjectives) != 1 {
		t.Fatalf("expected 1 missing objective, got %#v", result.MissingObjectives)
	}
	if result.MissingObjectives[0].ObjectiveKey != "research" {
		t.Fatalf("unexpected missing objective: %#v", result.MissingObjectives[0])
	}
}

func TestGeminiDeepResearchCompletenessCheckerCheckFallbackMissingObjectives(t *testing.T) {
	checker := &geminiDeepResearchCompletenessChecker{
		model: "checker-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return deepResearchJSONCandidateResponse(`{
				"complete": false,
				"reasoning": "Still missing evidence.",
				"missing_objectives": []
			}`), nil
		},
	}

	result, err := checker.Check(context.Background(), CompletenessRequest{
		Query: "q",
		Objectives: []Objective{
			{Key: "research"},
		},
		ObjectiveResults: []ObjectiveResult{
			{
				Objective:     Objective{Key: "research"},
				Status:        ObjectiveStatusPartial,
				EvidenceCount: 0,
			},
		},
	})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if len(result.MissingObjectives) != 1 {
		t.Fatalf("expected fallback missing objective, got %#v", result.MissingObjectives)
	}
	if result.MissingObjectives[0].ObjectiveKey != "research" {
		t.Fatalf("unexpected fallback missing objective: %#v", result.MissingObjectives[0])
	}
}

func TestGeminiDeepResearchCompletenessCheckerCheckInvalidJSON(t *testing.T) {
	checker := &geminiDeepResearchCompletenessChecker{
		model: "checker-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return deepResearchJSONCandidateResponse(`{"complete":`), nil
		},
	}

	_, err := checker.Check(context.Background(), CompletenessRequest{
		Query:      "q",
		Objectives: []Objective{{Key: "research"}},
	})
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if !strings.Contains(err.Error(), "completeness checker returned invalid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeminiDeepResearchCompletenessCheckerCheckNoObjectivesIsComplete(t *testing.T) {
	checker := &geminiDeepResearchCompletenessChecker{}
	result, err := checker.Check(context.Background(), CompletenessRequest{})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !result.Complete {
		t.Fatalf("expected complete=true with no objectives")
	}
}

func TestNewGeminiDeepResearchCompletenessCheckerRequiresAPIKey(t *testing.T) {
	orig := os.Getenv("GEMINI_API_KEY")
	defer func() { _ = os.Setenv("GEMINI_API_KEY", orig) }()
	_ = os.Unsetenv("GEMINI_API_KEY")

	_, err := NewGeminiCompletenessChecker()
	if err == nil {
		t.Fatalf("expected error when GEMINI_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "Gemini API not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}
