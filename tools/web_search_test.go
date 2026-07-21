package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestWebSearchTool_Success(t *testing.T) {
	webTools := &WebTools{
		apiKey: "test-key",
		model:  "test-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{
								Text: `{"results":[
									{"url":"https://example.com/1","title":"Test Result 1","snippet":"This is the first result"},
									{"url":"https://example.com/2","title":"Test Result 2","snippet":"This is the second result"}
								]}`,
							}},
						},
						GroundingMetadata: &genai.GroundingMetadata{
							GroundingChunks: []*genai.GroundingChunk{
								{Web: &genai.GroundingChunkWeb{URI: "https://example.com/1", Title: "Test Result 1"}},
								{Web: &genai.GroundingChunkWeb{URI: "https://example.com/3", Title: "Grounding Result"}},
							},
						},
					},
				},
			}, nil
		},
	}

	result, err := webTools.WebSearchTool(context.Background(), WebSearchInput{Query: "test query"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	data := result.Content.(map[string]any)
	if data["result_count"].(int) != 3 {
		t.Fatalf("expected 3 results (2 JSON + 1 grounding deduped), got %d", data["result_count"].(int))
	}
	results := data["results"].(string)
	if !strings.Contains(results, "Test Result 1") {
		t.Fatalf("expected result 1 in output: %s", results)
	}
	if !strings.Contains(results, "Grounding Result") {
		t.Fatalf("expected grounding result in output: %s", results)
	}
}

func TestWebSearchTool_EmptyQuery(t *testing.T) {
	webTools := &WebTools{
		apiKey: "test-key",
		model:  "test-model",
	}

	result, err := webTools.WebSearchTool(context.Background(), WebSearchInput{Query: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for empty query")
	}
	if result.Error != "query is required" {
		t.Errorf("expected error 'query is required', got '%s'", result.Error)
	}
}

func TestWebSearchTool_NotConfigured(t *testing.T) {
	webTools := &WebTools{
		apiKey: "test-key",
		model:  "test-model",
		// generateContent is nil
	}

	result, err := webTools.WebSearchTool(context.Background(), WebSearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure when not configured")
	}
	if !strings.Contains(result.Error, "not configured") {
		t.Errorf("expected 'not configured' in error, got '%s'", result.Error)
	}
}

func TestWebSearchTool_FallbackOnSchemaRejection(t *testing.T) {
	callCount := 0
	webTools := &WebTools{
		apiKey: "test-key",
		model:  "test-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			callCount++
			if callCount == 1 {
				if cfg == nil || cfg.ResponseSchema == nil {
					t.Fatalf("expected first call with response schema")
				}
				return nil, errors.New("model rejected schema")
			}
			if cfg == nil || cfg.ResponseSchema != nil {
				t.Fatalf("expected fallback call without response schema")
			}
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{
								Text: `{"results":[{"url":"https://example.com/a","title":"A","snippet":"snippet A"}]}`,
							}},
						},
						GroundingMetadata: &genai.GroundingMetadata{
							GroundingChunks: []*genai.GroundingChunk{
								{Web: &genai.GroundingChunkWeb{URI: "https://example.com/b", Title: "B"}},
							},
						},
					},
				},
			}, nil
		},
	}

	result, err := webTools.WebSearchTool(context.Background(), WebSearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 generate calls, got %d", callCount)
	}
}

func TestWebSearchTool_GenerateError(t *testing.T) {
	webTools := &WebTools{
		apiKey: "test-key",
		model:  "test-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return nil, errors.New("api error")
		},
	}

	result, err := webTools.WebSearchTool(context.Background(), WebSearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure on generate error")
	}
	if !strings.Contains(result.Error, "search failed") {
		t.Errorf("expected 'search failed' in error, got '%s'", result.Error)
	}
}

func TestWebTools_DescribeTool(t *testing.T) {
	webTools := &WebTools{apiKey: "key"}

	desc := webTools.DescribeTool("web_search")
	if desc == "" {
		t.Error("expected non-empty description for web_search")
	}

	desc = webTools.DescribeTool("unknown_tool")
	if desc != "" {
		t.Error("expected empty description for unknown tool")
	}
}

func TestWebTools_Available(t *testing.T) {
	notConfigured := &WebTools{apiKey: ""}
	if notConfigured.Available() {
		t.Error("expected Available() to be false without API key")
	}

	noFunc := &WebTools{apiKey: "key"}
	if noFunc.Available() {
		t.Error("expected Available() to be false without generateContent")
	}

	configured := &WebTools{
		apiKey: "key",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return nil, nil
		},
	}
	if !configured.Available() {
		t.Error("expected Available() to be true when configured")
	}
}

func TestHitsFromGeminiResponse_Deduplication(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{{
						Text: `{"results":[
							{"url":"https://example.com/1","title":"One","snippet":"snippet one"},
							{"url":"https://example.com/2","title":"Two","snippet":"snippet two"}
						]}`,
					}},
				},
				GroundingMetadata: &genai.GroundingMetadata{
					GroundingChunks: []*genai.GroundingChunk{
						{Web: &genai.GroundingChunkWeb{URI: "https://example.com/1", Title: "One Duplicate"}},
						{Web: &genai.GroundingChunkWeb{URI: "https://example.com/3", Title: "Three"}},
					},
				},
			},
		},
	}

	hits := hitsFromGeminiResponse(resp, 10)
	if len(hits) != 3 {
		t.Fatalf("expected 3 deduped hits, got %d", len(hits))
	}
	if hits[0].URL != "https://example.com/1" || hits[0].Title != "One" {
		t.Fatalf("unexpected first hit: %+v", hits[0])
	}
	if hits[2].URL != "https://example.com/3" {
		t.Fatalf("unexpected third hit: %+v", hits[2])
	}
}

func TestHitsFromGeminiResponse_RespectsLimit(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{{
						Text: `{"results":[
							{"url":"https://example.com/1","title":"one"},
							{"url":"https://example.com/2","title":"two"},
							{"url":"https://example.com/3","title":"three"}
						]}`,
					}},
				},
			},
		},
	}

	hits := hitsFromGeminiResponse(resp, 2)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
}

func TestNewWebTools_UsesEnvFallbackWhenArgEmpty(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "env-key")

	webTools := NewWebTools("")
	if webTools.apiKey != "env-key" {
		t.Fatalf("apiKey = %q, want env-key", webTools.apiKey)
	}
}
