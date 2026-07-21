package deepresearch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewClaudeSearcherConfiguresWebSearchOnly(t *testing.T) {
	t.Setenv("CLAUDE_FAST_MODEL", "claude-sonnet-test")

	searcher := NewClaudeSearcher()
	if searcher == nil {
		t.Fatalf("expected non-nil searcher")
	}
	if searcher.client == nil {
		t.Fatalf("expected non-nil client")
	}
	if searcher.client.Model != "claude-sonnet-test" {
		t.Fatalf("model = %q, want %q", searcher.client.Model, "claude-sonnet-test")
	}
	if !searcher.client.StrictMCPConfig {
		t.Fatalf("expected StrictMCPConfig=true")
	}
	if len(searcher.client.Tools) != 1 || searcher.client.Tools[0] != "WebSearch" {
		t.Fatalf("tools = %#v, want [\"WebSearch\"]", searcher.client.Tools)
	}
	if len(searcher.client.DisallowedTools) != 1 || searcher.client.DisallowedTools[0] != "WebFetch" {
		t.Fatalf("disallowed tools = %#v, want [\"WebFetch\"]", searcher.client.DisallowedTools)
	}
	if searcher.generate == nil {
		t.Fatalf("expected generate function to be configured")
	}
}

func TestClaudeSearcherSearchRequiresQuery(t *testing.T) {
	searcher := &claudeDeepResearchSearcher{}
	_, err := searcher.Search(context.Background(), SearchRequest{})
	if err == nil {
		t.Fatalf("expected query-required error")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClaudeSearcherSearchFailsOnGenerateError(t *testing.T) {
	searcher := &claudeDeepResearchSearcher{
		generate: func(ctx context.Context, prompt, systemPrompt string) (string, error) {
			return "", errors.New("claude failed")
		},
	}

	_, err := searcher.Search(context.Background(), SearchRequest{Query: "latest market news"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "claude deep research searcher") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClaudeSearcherSearchParsesResultsAndRespectsLimit(t *testing.T) {
	searcher := &claudeDeepResearchSearcher{
		generate: func(ctx context.Context, prompt, systemPrompt string) (string, error) {
			if !strings.Contains(prompt, "WebSearch") {
				t.Fatalf("prompt should instruct Claude to use WebSearch: %s", prompt)
			}
			return `{
				"results": [
					{
						"url": "https://example.com/a",
						"title": "Example A",
						"snippet": "Snippet A",
						"published_at": "2026-02-14T12:00:00Z"
					},
					{
						"url": "https://example.com/a",
						"title": "Example A duplicate",
						"snippet": "Duplicate"
					},
					{
						"url": "https://example.com/b",
						"title": "Example B",
						"snippet": "Snippet B"
					}
				]
			}`, nil
		},
	}

	hits, err := searcher.Search(context.Background(), SearchRequest{
		Query:        "latest market news",
		ObjectiveKey: "market_news",
		Limit:        2,
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %#v", hits)
	}
	if hits[0].URL != "https://example.com/a" || hits[0].Title != "Example A" {
		t.Fatalf("unexpected first hit: %#v", hits[0])
	}
	if hits[0].PublishedAt.IsZero() || !hits[0].PublishedAt.Equal(time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected published_at: %v", hits[0].PublishedAt)
	}
	if hits[1].URL != "https://example.com/b" {
		t.Fatalf("unexpected second hit: %#v", hits[1])
	}
}

func TestClaudeSearcherSearchFailsOnInvalidJSON(t *testing.T) {
	searcher := &claudeDeepResearchSearcher{
		generate: func(ctx context.Context, prompt, systemPrompt string) (string, error) {
			return `not json`, nil
		},
	}

	_, err := searcher.Search(context.Background(), SearchRequest{Query: "latest market news"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClaudeSearcherSearchFailsOnNoResults(t *testing.T) {
	searcher := &claudeDeepResearchSearcher{
		generate: func(ctx context.Context, prompt, systemPrompt string) (string, error) {
			return `{"results":[]}`, nil
		},
	}

	_, err := searcher.Search(context.Background(), SearchRequest{Query: "latest market news"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "returned no results") {
		t.Fatalf("unexpected error: %v", err)
	}
}
