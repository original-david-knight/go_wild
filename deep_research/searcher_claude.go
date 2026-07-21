package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/claudellm"
)

type claudeDeepResearchSearcher struct {
	client   *claudellm.Client
	generate func(context.Context, string, string) (string, error)
}

func NewClaudeSearcher(client ...*claudellm.Client) *claudeDeepResearchSearcher {
	c := &claudellm.Client{
		Model:           deepResearchClaudeModelFromEnv("DEEP_RESEARCH_SEARCH_MODEL", "CLAUDE_FAST_MODEL"),
		DisallowedTools: []string{"WebFetch"},
		OutputStylePath: claudellm.ResearchOutputStylePath(),
		StrictMCPConfig: true,
		Timeout:         2 * time.Minute,
		Label:           "deep-research-searcher",
		Tools:           []string{"WebSearch"},
	}
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}
	return &claudeDeepResearchSearcher{
		client:   c,
		generate: c.Generate,
	}
}

func (s *claudeDeepResearchSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if s == nil || s.generate == nil {
		return nil, fmt.Errorf("claude searcher model client is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	raw, err := s.generate(ctx, deepResearchClaudeSearchPrompt(req, limit)+claudeJSONSuffix, "")
	if err != nil {
		return nil, fmt.Errorf("claude deep research searcher: %w", err)
	}

	raw = extractJSON(raw)
	var parsed struct {
		Results []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Snippet     string `json:"snippet"`
			PublishedAt string `json:"published_at"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("claude deep research searcher returned invalid JSON: %w", err)
	}

	hits := make([]SearchHit, 0, len(parsed.Results))
	seen := make(map[string]struct{}, len(parsed.Results))
	for _, row := range parsed.Results {
		url := strings.TrimSpace(row.URL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}

		hit := SearchHit{
			URL:     url,
			Title:   strings.TrimSpace(row.Title),
			Snippet: strings.TrimSpace(row.Snippet),
		}
		if ts := strings.TrimSpace(row.PublishedAt); ts != "" {
			if t, parseErr := time.Parse(time.RFC3339, ts); parseErr == nil {
				hit.PublishedAt = t
			}
		}
		hits = append(hits, hit)
		if len(hits) >= limit {
			break
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("claude deep research searcher returned no results")
	}
	return hits, nil
}

func deepResearchClaudeSearchPrompt(req SearchRequest, limit int) string {
	query := strings.TrimSpace(req.Query)
	problem := strings.TrimSpace(req.Guidance)
	if problem == "" {
		problem = query
	}
	excludedDomains := strings.TrimSpace(strings.Join(req.ExcludedDomains, ", "))
	if excludedDomains == "" {
		excludedDomains = "none"
	}

	return fmt.Sprintf(`You are a research search operator for a deep research engine.
You MUST use Claude's built-in WebSearch tool before answering.
Do not fetch or open webpages yourself; another Go-managed read_webpage tool will do that later.

Problem:
%s

Current date and time: %s

Objective key: %s
Depth: %d
Search query: %s
Excluded domains: %s

Return at most %d results in JSON with this shape:
{
  "results": [
    {
      "url": "https://...",
      "title": "...",
      "snippet": "...",
      "published_at": "RFC3339 timestamp if known, otherwise empty"
    }
  ],
  "reasoning": "brief note"
}

Critical evidence policy:
- Search results are discovery hints only, not evidence.
- Prefer URLs with substantial full-page content that can be read end-to-end later.
- Avoid thin pages, link directories, and snippet-only aggregators.
- Never return results from excluded domains.
- Prefer primary sources, official docs, and recent reporting when relevant.`,
		problem,
		deepResearchCurrentTime(),
		strings.TrimSpace(req.ObjectiveKey),
		req.Depth,
		query,
		excludedDomains,
		limit,
	)
}
