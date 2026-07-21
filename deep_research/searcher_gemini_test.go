package deepresearch

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestGeminiGroundedDeepResearchSearcherSearchRequiresQuery(t *testing.T) {
	searcher := &geminiGroundedDeepResearchSearcher{}
	_, err := searcher.Search(context.Background(), SearchRequest{})
	if err == nil {
		t.Fatalf("expected query-required error")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeminiGroundedDeepResearchSearcherFailsOnAPIError(t *testing.T) {
	searcher := &geminiGroundedDeepResearchSearcher{
		model: "search-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return nil, errors.New("API error")
		},
	}

	_, err := searcher.Search(context.Background(), SearchRequest{
		Query: "test query",
		Limit: 5,
	})
	if err == nil {
		t.Fatalf("expected error on API failure")
	}
	if !strings.Contains(err.Error(), "API call failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeminiGroundedDeepResearchSearcherMergesTextAndGrounding(t *testing.T) {
	searcher := &geminiGroundedDeepResearchSearcher{
		model: "search-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{
								Text: `{
									"results": [
										{
											"url": "https://example.com/a",
											"title": "A",
											"snippet": "snippet A",
											"published_at": "2026-02-14T12:00:00Z"
										}
									]
								}`,
							}},
						},
						GroundingMetadata: &genai.GroundingMetadata{
							GroundingChunks: []*genai.GroundingChunk{
								{Web: &genai.GroundingChunkWeb{URI: "https://example.com/b", Title: "B"}},
								{Web: &genai.GroundingChunkWeb{URI: "https://example.com/a", Title: "A Duplicate"}},
							},
						},
					},
				},
			}, nil
		},
	}

	hits, err := searcher.Search(context.Background(), SearchRequest{
		Query: "test query",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 deduped hits, got %#v", hits)
	}
	if hits[0].URL != "https://example.com/a" || hits[0].Title != "A" {
		t.Fatalf("unexpected first hit: %#v", hits[0])
	}
	if hits[0].PublishedAt.IsZero() || !hits[0].PublishedAt.Equal(time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected published_at: %v", hits[0].PublishedAt)
	}
	if hits[1].URL != "https://example.com/b" {
		t.Fatalf("unexpected second hit: %#v", hits[1])
	}
}

func TestGeminiGroundedDeepResearchSearcherSearchNoResults(t *testing.T) {
	searcher := &geminiGroundedDeepResearchSearcher{
		model: "search-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return deepResearchJSONCandidateResponse(`{"results":[]}`), nil
		},
	}

	_, err := searcher.Search(context.Background(), SearchRequest{
		Query: "test query",
	})
	if err == nil {
		t.Fatalf("expected no-results error")
	}
	if !strings.Contains(err.Error(), "returned no results") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeminiGroundedDeepResearchSearcherRespectsLimit(t *testing.T) {
	searcher := &geminiGroundedDeepResearchSearcher{
		model: "search-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
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
			}, nil
		},
	}

	hits, err := searcher.Search(context.Background(), SearchRequest{
		Query: "test",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].URL != "https://example.com/1" || hits[1].URL != "https://example.com/2" {
		t.Fatalf("unexpected hit order: %#v", hits)
	}
}

func TestGeminiGroundedDeepResearchSearcherDeduplicatesParsedResultsBeforeLimit(t *testing.T) {
	searcher := &geminiGroundedDeepResearchSearcher{
		model: "search-model",
		generateContent: func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{
								Text: `{"results":[
									{"url":"https://example.com/1","title":"one"},
									{"url":"https://example.com/1","title":"one duplicate"},
									{"url":"https://example.com/2","title":"two"}
								]}`,
							}},
						},
					},
				},
			}, nil
		},
	}

	hits, err := searcher.Search(context.Background(), SearchRequest{
		Query: "test",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 unique hits, got %#v", hits)
	}
	if hits[0].URL != "https://example.com/1" || hits[1].URL != "https://example.com/2" {
		t.Fatalf("unexpected hit order after dedupe: %#v", hits)
	}
}

func TestDeepResearchGeminiSearchPromptIncludesSnippetPolicy(t *testing.T) {
	prompt := deepResearchGeminiSearchPrompt(SearchRequest{
		Query:           "latest policy update",
		ObjectiveKey:    "research",
		Depth:           1,
		Guidance:        "Gather primary evidence",
		ExcludedDomains: []string{"example.com"},
	}, 5)

	checks := []string{
		"working on task within the following problem",
		"Search snippets are discovery hints only",
		"Prefer URLs with substantial full-page content",
		"Excluded domains",
	}
	for _, check := range checks {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(check)) {
			t.Fatalf("prompt missing %q: %s", check, prompt)
		}
	}
}

func TestNewGeminiGroundedSearcherRequiresAPIKey(t *testing.T) {
	orig := os.Getenv("GEMINI_API_KEY")
	defer func() { _ = os.Setenv("GEMINI_API_KEY", orig) }()
	_ = os.Unsetenv("GEMINI_API_KEY")

	_, err := NewGeminiGroundedSearcher()
	if err == nil {
		t.Fatalf("expected error when GEMINI_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "Gemini API not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}
