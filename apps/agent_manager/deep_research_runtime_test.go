package main

import (
	"context"
	"errors"
	"testing"

	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

type testDeepResearchSearcher struct {
	hits  []deepresearch.SearchHit
	err   error
	calls int
}

func (s *testDeepResearchSearcher) Search(ctx context.Context, req deepresearch.SearchRequest) ([]deepresearch.SearchHit, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.hits, nil
}

type testDeepResearchFetcher struct{}

func (f *testDeepResearchFetcher) Fetch(ctx context.Context, url string) (deepresearch.FetchedDocument, error) {
	return deepresearch.FetchedDocument{URL: url, Content: "ok"}, nil
}

func TestNewDeepResearchSearcherSelection(t *testing.T) {
	origGeminiSearcher := buildGeminiDeepResearchSearcher
	origClaudeSearcher := buildClaudeDeepResearchSearcher
	defer func() {
		buildGeminiDeepResearchSearcher = origGeminiSearcher
		buildClaudeDeepResearchSearcher = origClaudeSearcher
	}()

	gemini := &testDeepResearchSearcher{hits: []deepresearch.SearchHit{{URL: "https://gemini.example.com"}}}
	claude := &testDeepResearchSearcher{hits: []deepresearch.SearchHit{{URL: "https://claude.example.com"}}}

	tests := []struct {
		name        string
		llmBackend  string
		searchErr   error
		wantName    string
		wantErrPart string
	}{
		{
			name:       "gemini available",
			llmBackend: "gemini",
			wantName:   "gemini_grounded_search",
		},
		{
			name:       "claude available",
			llmBackend: "claude",
			wantName:   "claude_web_search",
		},
		{
			name:        "gemini unavailable",
			llmBackend:  "gemini",
			searchErr:   errors.New("gemini unavailable"),
			wantErrPart: "deep research searcher unavailable",
		},
		{
			name:        "claude unavailable",
			llmBackend:  "claude",
			searchErr:   errors.New("claude unavailable"),
			wantErrPart: "deep research searcher unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buildGeminiDeepResearchSearcher = func() (deepresearch.Searcher, error) {
				if tc.llmBackend != "gemini" {
					t.Fatalf("gemini searcher should not be used for backend %q", tc.llmBackend)
				}
				if tc.searchErr != nil {
					return nil, tc.searchErr
				}
				return gemini, nil
			}
			buildClaudeDeepResearchSearcher = func() (deepresearch.Searcher, error) {
				if tc.llmBackend != "claude" {
					t.Fatalf("claude searcher should not be used for backend %q", tc.llmBackend)
				}
				if tc.searchErr != nil {
					return nil, tc.searchErr
				}
				return claude, nil
			}

			searcher, name, err := newDeepResearchSearcher(tc.llmBackend)
			if tc.wantErrPart != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErrPart)
				}
				if !containsStr(err.Error(), tc.wantErrPart) {
					t.Fatalf("unexpected error: %v", err)
				}
				if searcher != nil || name != "" {
					t.Fatalf("expected nil searcher and empty name on error, got searcher=%T name=%q", searcher, name)
				}
				return
			}

			if err != nil {
				t.Fatalf("newDeepResearchSearcher failed: %v", err)
			}
			if name != tc.wantName {
				t.Fatalf("searcher name = %q, want %q", name, tc.wantName)
			}
			if searcher == nil {
				t.Fatalf("expected non-nil searcher")
			}
		})
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewDeepResearchFetcher(t *testing.T) {
	origBuildFetcher := buildDeepResearchFetcher
	defer func() { buildDeepResearchFetcher = origBuildFetcher }()

	buildDeepResearchFetcher = func() (deepresearch.Fetcher, error) {
		return &testDeepResearchFetcher{}, nil
	}
	fetcher, name, err := newDeepResearchFetcher()
	if err != nil {
		t.Fatalf("newDeepResearchFetcher failed: %v", err)
	}
	if fetcher == nil {
		t.Fatalf("expected non-nil fetcher")
	}
	if name != "read_webpage_tool_fetcher" {
		t.Fatalf("fetcher name = %q, want %q", name, "read_webpage_tool_fetcher")
	}

	buildDeepResearchFetcher = func() (deepresearch.Fetcher, error) {
		return nil, errors.New("fetcher unavailable")
	}
	fetcher, name, err = newDeepResearchFetcher()
	if err == nil {
		t.Fatalf("expected error")
	}
	if fetcher != nil || name != "" {
		t.Fatalf("expected nil fetcher and empty name on error, got fetcher=%T name=%q", fetcher, name)
	}
}
