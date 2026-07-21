package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/codexllm"
)

type codexDeepResearchSearcher struct {
	client   *codexllm.Client
	generate func(context.Context, string, string) (string, []string, error)
}

// newCodexSearcher shallow-copies the caller-provided client and forces
// WebSearch / RequireWebSearchUse on the copy; the caller's original pointer
// is never mutated, so any other code path sharing that client keeps its
// original behavior (no race, no silent flag-flip for unrelated callers).
func newCodexSearcher(client *codexllm.Client) *codexDeepResearchSearcher {
	c := *client
	c.WebSearch = true
	c.RequireWebSearchUse = true
	return &codexDeepResearchSearcher{client: &c, generate: c.GenerateWithObserved}
}

// DefaultCodexSearcher constructs a searcher with a client built from
// DEEP_RESEARCH_CODEX_SEARCH_PROFILE (or the CODEX_FAST_PROFILE tier
// fallback). Returns an error if neither env var is set.
func DefaultCodexSearcher() (*codexDeepResearchSearcher, error) {
	// newCodexSearcher shallow-copies and force-sets WebSearch +
	// RequireWebSearchUse, so we intentionally don't set them here.
	client, err := buildCodexClient("DEEP_RESEARCH_CODEX_SEARCH_PROFILE", "CODEX_FAST_PROFILE", "deep-research-searcher", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	return newCodexSearcher(client), nil
}

func (s *codexDeepResearchSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	raw, observedURLs, err := s.generate(ctx, deepResearchCodexSearchPrompt(req, limit)+codexJSONSuffix, "")
	if err != nil {
		return nil, fmt.Errorf("codex deep research searcher: %w", err)
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
		return nil, fmt.Errorf("codex deep research searcher returned invalid JSON: %w", err)
	}

	// Tool-observed URL set: only URLs that actually appeared in a web_search
	// `open_page` event (see codexllm.extractCodexURLs) are considered
	// attested. Any URL the model returns that isn't in this set is treated
	// as fabricated and dropped. The prompt (see deepResearchCodexSearchPrompt)
	// tells the model to open each candidate page, so an honest run populates
	// this set; a run that shortcuts to fabricated URLs produces zero overlap
	// and the call fails below.
	//
	// Defensive normalize: the production client already normalizes these via
	// the same codexllm.NormalizeURL, but test stubs and future injection
	// points may not. Calling it again is idempotent.
	attested := make(map[string]struct{}, len(observedURLs))
	for _, u := range observedURLs {
		if n := codexllm.NormalizeURL(u); n != "" {
			attested[n] = struct{}{}
		}
	}

	hits := make([]SearchHit, 0, len(parsed.Results))
	seen := make(map[string]struct{}, len(parsed.Results))
	rejected := 0
	for _, row := range parsed.Results {
		url := strings.TrimSpace(row.URL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}

		if !urlAttested(url, attested) {
			rejected++
			continue
		}

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
		if rejected > 0 {
			return nil, fmt.Errorf("codex deep research searcher returned no attested results: %d candidate URL(s) did not appear in any web_search `open_page` event — model is likely fabricating", rejected)
		}
		return nil, fmt.Errorf("codex deep research searcher returned no results")
	}
	return hits, nil
}

// urlAttested reports whether a model-produced URL has an exact match in the
// tool-observed URL set (after codexllm.NormalizeURL is applied to both).
//
// We do NOT fall back to stripping the query string: on many sites the query
// selects the resource (GitHub gist `?id=`, YouTube `?v=`, Polymarket
// `?market=`, signed S3 URLs, API pagination). A query-strip bypass would
// let the model open `https://example.com/docs` and then "attest" a
// fabricated `https://example.com/docs?market=evil-slug` that points at a
// completely different resource. Strictness wins; if the model wants to
// return a query-bearing URL it must open_page exactly that URL.
func urlAttested(candidate string, attested map[string]struct{}) bool {
	if len(attested) == 0 {
		return false
	}
	n := codexllm.NormalizeURL(candidate)
	if n == "" {
		return false
	}
	_, ok := attested[n]
	return ok
}

func deepResearchCodexSearchPrompt(req SearchRequest, limit int) string {
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
You MUST call the native web_search tool at least once for the given query. Never answer from memory — if web_search is unavailable, stop and return an error rather than fabricating URLs.

CRITICAL RESTRICTIONS:
- DO NOT fetch, read, browse, curl, wget, or use shell commands to access the web.
- ONLY use your built-in web_search tool. Use its "search" action to discover candidate URLs, then use its "open_page" action on each URL you intend to return. The caller verifies that every returned URL was observed in an open_page event; any URL you include that you did not open_page will be rejected as fabricated.
- Return ONLY URLs that you actually opened via web_search open_page. Do NOT add URLs from memory, pre-training, or speculation.
- The open_page fetch is a provenance check, not a content read — you still don't need to summarize the page; a separate system does the full fetch and read later.

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
