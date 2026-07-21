// Package tools provides agent tools for the GoWild agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/my"
)

// WebSearchInput defines the input for the web search tool.
type WebSearchInput struct {
	Query string `json:"query" description:"The search query to submit"`
}

// WebTools provides web-related tools using Gemini Grounding with Google Search.
type WebTools struct {
	apiKey          string
	model           string
	generateContent func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

const defaultWebSearchModel = "gemini-3-flash-preview"

// NewWebTools creates a new WebTools instance using Gemini Grounding with Google Search.
func NewWebTools(apiKey string) *WebTools {
	gowild_my.LoadEnv()
	if strings.TrimSpace(apiKey) == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}

	model := strings.TrimSpace(os.Getenv("WEB_SEARCH_MODEL"))
	if model == "" {
		model = defaultWebSearchModel
	}

	wt := &WebTools{
		apiKey: apiKey,
		model:  model,
	}

	if strings.TrimSpace(apiKey) != "" {
		client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
		if err == nil {
			wt.generateContent = func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
				return client.Models.GenerateContent(ctx, model, contents, cfg)
			}
		}
	}

	return wt
}

// Available returns true when Gemini API is configured.
func (w *WebTools) Available() bool {
	return strings.TrimSpace(w.apiKey) != "" && w.generateContent != nil
}

// WebSearchTool performs a web search using Gemini Grounding with Google Search.
func (w *WebTools) WebSearchTool(ctx context.Context, input WebSearchInput) (*loop.ToolResult, error) {
	if input.Query == "" {
		return loop.NewErrorResult("query is required"), nil
	}
	if w.generateContent == nil {
		return loop.NewErrorResult("web search not configured (GEMINI_API_KEY required)"), nil
	}

	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Search the web for: %s

Return the results as JSON with this shape:
{
  "results": [
    {"url": "https://...", "title": "...", "snippet": "brief description"}
  ]
}

Return up to 10 results. Prioritize authoritative, recent sources.`, input.Query)

	temp := float32(0.2)
	cfg := &genai.GenerateContentConfig{
		Temperature:     &temp,
		MaxOutputTokens: 2048,
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
		ResponseMIMEType: "application/json",
		ResponseSchema:   webSearchResponseSchema(),
	}

	resp, err := w.generateContent(searchCtx, w.model, genai.Text(prompt), cfg)
	if err != nil {
		// Fallback: retry without structured output schema
		resp, err = w.generateContent(searchCtx, w.model, genai.Text(prompt), &genai.GenerateContentConfig{
			Temperature:     &temp,
			MaxOutputTokens: 2048,
			Tools: []*genai.Tool{
				{GoogleSearch: &genai.GoogleSearch{}},
			},
		})
		if err != nil {
			return loop.NewErrorResult(fmt.Sprintf("search failed: %v", err)), nil
		}
	}

	hits := hitsFromGeminiResponse(resp, 10)

	// Format results as markdown
	var sb strings.Builder
	sb.WriteString("# Search Results\n\n")

	if len(hits) == 0 {
		sb.WriteString("No results found.\n")
	} else {
		for _, hit := range hits {
			sb.WriteString(fmt.Sprintf("## %s\n", hit.Title))
			sb.WriteString(fmt.Sprintf("URL: %s\n", hit.URL))
			if hit.Snippet != "" {
				sb.WriteString(fmt.Sprintf("%s\n", hit.Snippet))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("---\n**REMINDER: The snippets above are incomplete. You MUST use read_webpage on the most relevant URLs to get the full information before responding to the user.**\n")
	}

	return loop.NewSuccessResult(map[string]any{
		"query":        input.Query,
		"result_count": len(hits),
		"results":      sb.String(),
	}), nil
}

func webSearchResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"results": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"url":     {Type: genai.TypeString},
						"title":   {Type: genai.TypeString},
						"snippet": {Type: genai.TypeString},
					},
					Required: []string{"url"},
				},
			},
		},
		Required: []string{"results"},
	}
}

type webSearchHit struct {
	URL     string
	Title   string
	Snippet string
}

func hitsFromGeminiResponse(resp *genai.GenerateContentResponse, limit int) []webSearchHit {
	type resultItem struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Snippet string `json:"snippet"`
	}
	type payload struct {
		Results []resultItem `json:"results"`
	}

	byURL := map[string]webSearchHit{}
	order := []string{}

	addHit := func(hit webSearchHit) {
		hit.URL = strings.TrimSpace(hit.URL)
		if hit.URL == "" {
			return
		}
		existing, ok := byURL[hit.URL]
		if !ok {
			byURL[hit.URL] = hit
			order = append(order, hit.URL)
			return
		}
		if strings.TrimSpace(existing.Title) == "" && strings.TrimSpace(hit.Title) != "" {
			existing.Title = strings.TrimSpace(hit.Title)
		}
		if strings.TrimSpace(existing.Snippet) == "" && strings.TrimSpace(hit.Snippet) != "" {
			existing.Snippet = strings.TrimSpace(hit.Snippet)
		}
		byURL[hit.URL] = existing
	}

	if resp != nil {
		for _, candidate := range resp.Candidates {
			if candidate == nil {
				continue
			}
			text := candidateText(candidate.Content)
			if strings.TrimSpace(text) != "" {
				var parsed payload
				if err := json.Unmarshal([]byte(text), &parsed); err == nil {
					for _, row := range parsed.Results {
						addHit(webSearchHit{
							URL:     strings.TrimSpace(row.URL),
							Title:   strings.TrimSpace(row.Title),
							Snippet: strings.TrimSpace(row.Snippet),
						})
					}
				}
			}

			if candidate.GroundingMetadata != nil {
				for _, chunk := range candidate.GroundingMetadata.GroundingChunks {
					if chunk == nil || chunk.Web == nil {
						continue
					}
					addHit(webSearchHit{
						URL:   strings.TrimSpace(chunk.Web.URI),
						Title: strings.TrimSpace(chunk.Web.Title),
					})
				}
			}
		}
	}

	out := make([]webSearchHit, 0, len(order))
	for _, u := range order {
		if hit, ok := byURL[u]; ok {
			out = append(out, hit)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func candidateText(content *genai.Content) string {
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

// DescribeTool implements ToolProvider for tool descriptions.
func (w *WebTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"web_search": "Use this to search the web for information on any topic. Useful when you need to find current information, verify facts, or research topics.\n\n**CRITICAL: Search results only contain brief snippets — they are NOT sufficient for answering questions or completing tasks.** You MUST use read_webpage to open and read the full content of the most relevant URLs from the results. Never treat snippets as complete or reliable information. Always follow up a web_search with read_webpage calls on the top results before drawing conclusions or reporting findings to the user.",
	}
	return descriptions[name]
}

