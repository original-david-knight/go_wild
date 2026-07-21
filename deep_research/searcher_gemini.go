package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

type geminiGroundedDeepResearchSearcher struct {
	client          *genai.Client
	model           string
	generateContent func(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

func NewGeminiGroundedSearcher() (*geminiGroundedDeepResearchSearcher, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API not configured (set GEMINI_API_KEY)")
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	model := deepResearchModelFromEnv("DEEP_RESEARCH_SEARCH_MODEL", "FAST_MODEL")
	return &geminiGroundedDeepResearchSearcher{
		client: client,
		model:  model,
		generateContent: retryOnRateLimit(func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return client.Models.GenerateContent(ctx, model, contents, cfg)
		}),
	}, nil
}

func (s *geminiGroundedDeepResearchSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if s == nil || s.generateContent == nil {
		return nil, fmt.Errorf("gemini searcher model client is not configured")
	}
	searchCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	prompt := deepResearchGeminiSearchPrompt(req, limit)
	temp := float32(0.2)
	cfg := &genai.GenerateContentConfig{
		Temperature:     &temp,
		MaxOutputTokens: 4096,
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
	}

	resp, err := s.generateContent(searchCtx, s.model, genai.Text(prompt), cfg)
	if err != nil {
		return nil, fmt.Errorf("gemini grounded search API call failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("gemini grounded search returned nil response")
	}
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("gemini grounded search returned 0 candidates")
	}

	candidate := resp.Candidates[0]
	if candidate == nil {
		return nil, fmt.Errorf("gemini grounded search returned nil candidate")
	}

	// Log raw response for debugging.
	text := deepResearchCandidateText(candidate.Content)
	log.Printf("[deep-research] gemini search raw text (%d chars): %.500s", len(text), text)

	var groundingChunkCount int
	var groundingURLs []string
	if candidate.GroundingMetadata != nil {
		groundingChunkCount = len(candidate.GroundingMetadata.GroundingChunks)
		for _, chunk := range candidate.GroundingMetadata.GroundingChunks {
			if chunk != nil && chunk.Web != nil {
				groundingURLs = append(groundingURLs, strings.TrimSpace(chunk.Web.URI))
			}
		}
	}
	log.Printf("[deep-research] gemini search grounding: %d chunks, urls=%v", groundingChunkCount, groundingURLs)

	// Parse JSON results from text.
	var hits []SearchHit
	seen := make(map[string]struct{})
	appendUniqueHit := func(hit SearchHit) {
		hit.URL = strings.TrimSpace(hit.URL)
		if hit.URL == "" {
			return
		}
		if _, ok := seen[hit.URL]; ok {
			return
		}
		seen[hit.URL] = struct{}{}
		hits = append(hits, hit)
	}
	extracted := extractJSON(text)
	log.Printf("[deep-research] gemini search extractJSON: input_len=%d output_len=%d first_char=%q", len(text), len(extracted), safeFirstChar(extracted))
	if strings.TrimSpace(extracted) != "" {
		type payload struct {
			Results []geminiSearchResultItem `json:"results"`
		}
		var parsed payload
		if err := json.Unmarshal([]byte(extracted), &parsed); err != nil {
			// JSON is likely truncated by MaxOutputTokens. Try to salvage
			// complete result objects from the partial array.
			log.Printf("[deep-research] gemini search JSON truncated (%d chars), attempting salvage", len(extracted))
			parsed.Results = salvageTruncatedSearchResults(extracted)
		}
		log.Printf("[deep-research] gemini search parsed results_count=%d", len(parsed.Results))
		for _, row := range parsed.Results {
			url := strings.TrimSpace(row.URL)
			if url == "" {
				continue
			}
			hit := SearchHit{
				URL:     url,
				Title:   truncateField(row.Title, 200),
				Snippet: truncateField(row.Snippet, 500),
			}
			if ts := strings.TrimSpace(row.PublishedAt); ts != "" {
				if t, parseErr := time.Parse(time.RFC3339, ts); parseErr == nil {
					hit.PublishedAt = t
				}
			}
			appendUniqueHit(hit)
		}
	} else {
		log.Printf("[deep-research] gemini search extractJSON returned empty from %d chars of text", len(text))
	}

	// Add grounding chunk URLs not already in hits.
	for _, url := range groundingURLs {
		if strings.Contains(url, "vertexaisearch.cloud.google.com") {
			resolved := resolveVertexRedirect(url)
			if resolved == "" {
				log.Printf("[deep-research] gemini search failed to resolve vertex redirect: %s", url)
				continue
			}
			log.Printf("[deep-research] gemini search resolved vertex redirect: %s -> %s", url, resolved)
			url = resolved
		}
		appendUniqueHit(SearchHit{URL: url})
	}

	if len(hits) == 0 {
		preview := text
		if len(preview) > 500 {
			preview = preview[:500]
		}
		log.Printf("[deep-research] gemini search 0 hits, raw text preview: %s", preview)
		return nil, fmt.Errorf("gemini grounded search returned no results (text_len=%d, grounding_chunks=%d, grounding_urls=%v)", len(text), groundingChunkCount, groundingURLs)
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func deepResearchGeminiSearchPrompt(req SearchRequest, limit int) string {
	problem := strings.TrimSpace(req.Guidance)
	if problem == "" {
		problem = strings.TrimSpace(req.Query)
	}
	excludedDomains := strings.TrimSpace(strings.Join(req.ExcludedDomains, ", "))
	if excludedDomains == "" {
		excludedDomains = "none"
	}

	now := deepResearchCurrentTime()

	return fmt.Sprintf(`You are working on task within the following problem:
%s

Current date and time: %s

You are a research search operator.
Use Google Search grounding to find authoritative web sources for the objective.
Return concise, factual results only.

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
- Search snippets are discovery hints only, not evidence.
- Prefer URLs with substantial full-page content that can be read end-to-end.
- Avoid thin pages, link directories, and snippet-only aggregators.
- Never return results from excluded domains.

Prioritize official docs, primary reporting, and recent sources when relevant.`,
		problem,
		now,
		strings.TrimSpace(req.ObjectiveKey),
		req.Depth,
		strings.TrimSpace(req.Query),
		excludedDomains,
		limit,
	)
}

func deepResearchGeminiSearchResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"results": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"url": {
							Type: genai.TypeString,
						},
						"title": {
							Type: genai.TypeString,
						},
						"snippet": {
							Type: genai.TypeString,
						},
						"published_at": {
							Type: genai.TypeString,
						},
					},
					Required: []string{"url"},
				},
			},
			"reasoning": {
				Type: genai.TypeString,
			},
		},
		Required: []string{"results"},
	}
}

// resolveVertexRedirect follows a Vertex AI Search redirect URL to get the
// real destination URL. Returns empty string on failure.
func resolveVertexRedirect(redirectURL string) string {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Head(redirectURL)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" || strings.Contains(location, "vertexaisearch.cloud.google.com") {
		return ""
	}
	return location
}

type geminiSearchResultItem struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
	PublishedAt string `json:"published_at"`
}

// salvageTruncatedSearchResults extracts complete result objects from a
// truncated JSON array. The output was cut off by MaxOutputTokens, so the
// outer object is invalid, but individual result objects may be complete.
func salvageTruncatedSearchResults(raw string) []geminiSearchResultItem {
	var results []geminiSearchResultItem
	// Find each complete {...} block that contains a "url" field.
	depth := 0
	start := -1
	for i, ch := range raw {
		if ch == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 && start >= 0 {
				candidate := raw[start : i+1]
				var item geminiSearchResultItem
				if err := json.Unmarshal([]byte(candidate), &item); err == nil && strings.TrimSpace(item.URL) != "" {
					results = append(results, item)
				}
				start = -1
			}
		}
	}
	return results
}

func truncateField(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return strings.TrimSpace(s[:maxLen]) + "..."
}

func safeFirstChar(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return s[:1]
}

func deepResearchCandidateText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range content.Parts {
		if part == nil || part.Thought {
			continue
		}
		if strings.TrimSpace(part.Text) != "" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
