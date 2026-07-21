package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ReutersTools proxies Reuters operations through the broker API.
type ReutersTools struct {
	client *Client
}

// NewReutersTools creates broker-backed Reuters tools.
func NewReutersTools(client *Client) *ReutersTools {
	return &ReutersTools{client: client}
}

func (r *ReutersTools) ReutersNewsTool(ctx context.Context, input tools.ReutersNewsInput) (*loop.ToolResult, error) {
	result, err := r.client.CallTool(ctx, "reuters_news", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (r *ReutersTools) SearchReutersNewsTool(ctx context.Context, input tools.SearchReutersNewsInput) (*loop.ToolResult, error) {
	result, err := r.client.CallTool(ctx, "search_reuters_news", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (r *ReutersTools) ReadReutersArticleTool(ctx context.Context, input tools.ReadReutersArticleInput) (*loop.ToolResult, error) {
	result, err := r.client.CallTool(ctx, "read_reuters_article", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (r *ReutersTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"reuters_news":         "List the latest Reuters headlines from the public news sitemap. Returns URL, title, and publish date only — article bodies are not available because Reuters blocks direct scraping.",
		"search_reuters_news":  "Search the Reuters news sitemap for recent headlines matching a query. Returns URL, title, and publish date only — article bodies are not available because Reuters blocks direct scraping.",
		"read_reuters_article": "Attempt to fetch a Reuters article body by URL. Usually fails with HTTP 401 (DataDome bot protection); if you need the article content, fall back to a general web search.",
	}
	return descriptions[name]
}
