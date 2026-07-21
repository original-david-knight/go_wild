package deepresearch

import (
	"context"
	"errors"
	"testing"
)

type testDeepResearchSearcher struct {
	hits  []SearchHit
	err   error
	calls int
}

func (s *testDeepResearchSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.hits, nil
}

type testDeepResearchFetcher struct{}

func (f *testDeepResearchFetcher) Fetch(ctx context.Context, url string) (FetchedDocument, error) {
	return FetchedDocument{URL: url, Content: "ok"}, nil
}

func TestNewDeepResearchSearcherSelection(t *testing.T) {
	origSearcher := buildSearcher
	defer func() {
		buildSearcher = origSearcher
	}()

	gemini := &testDeepResearchSearcher{hits: []SearchHit{{URL: "https://c"}}}

	tests := []struct {
		name        string
		searchErr   error
		wantName    string
		wantErrPart string
	}{
		{
			name:     "gemini available",
			wantName: "gemini_grounded_search",
		},
		{
			name:        "gemini unavailable",
			searchErr:   errors.New("gemini unavailable"),
			wantErrPart: "deep research searcher unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buildSearcher = func() (Searcher, error) {
				if tc.searchErr != nil {
					return nil, tc.searchErr
				}
				return gemini, nil
			}

			searcher, name, err := NewSearcher()
			if tc.wantErrPart != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErrPart)
				}
				if !contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("unexpected error: %v", err)
				}
				if searcher != nil || name != "" {
					t.Fatalf("expected nil searcher and empty name on error, got searcher=%T name=%q", searcher, name)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewSearcher failed: %v", err)
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
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
	origBuildFetcher := buildFetcher
	defer func() { buildFetcher = origBuildFetcher }()

	buildFetcher = func() (Fetcher, error) {
		return &testDeepResearchFetcher{}, nil
	}
	fetcher, name, err := NewFetcher()
	if err != nil {
		t.Fatalf("NewFetcher failed: %v", err)
	}
	if fetcher == nil {
		t.Fatalf("expected non-nil fetcher")
	}
	if name != "read_webpage_tool_fetcher" {
		t.Fatalf("fetcher name = %q, want %q", name, "read_webpage_tool_fetcher")
	}

	buildFetcher = func() (Fetcher, error) {
		return nil, errors.New("fetcher unavailable")
	}
	fetcher, name, err = NewFetcher()
	if err == nil {
		t.Fatalf("expected error")
	}
	if fetcher != nil || name != "" {
		t.Fatalf("expected nil fetcher and empty name on error, got fetcher=%T name=%q", fetcher, name)
	}
}
