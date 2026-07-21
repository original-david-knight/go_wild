package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// NewReutersTools creates a new ReutersTools instance.
// If cache is non-nil, fetched articles will be cached for 3 hours.
func NewReutersTools(cache articleCache) *ReutersTools {
	return &ReutersTools{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: cache,
	}
}

// ReutersNewsTool returns the most recent Reuters headlines from the news sitemap.
// Reuters' HTML and JSON endpoints are behind DataDome bot protection; only the
// sitemap is reachable, so this tool returns headline metadata (URL, title,
// publish date) without article bodies.
func (rt *ReutersTools) ReutersNewsTool(ctx context.Context, input ReutersNewsInput) (*loop.ToolResult, error) {
	maxArticles := input.MaxArticles
	if maxArticles <= 0 {
		maxArticles = 10
	}
	if maxArticles > 30 {
		maxArticles = 30
	}

	entries, err := rt.fetchNewsSitemap(ctx, "https://www.reuters.com/arc/outboundfeeds/news-sitemap/?outputType=xml")
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to fetch sitemap: %v", err)), nil
	}

	articles := make([]reutersArticle, 0, maxArticles)
	for _, entry := range entries {
		if !isReutersArticleURL(entry.URL) || isNonEnglishURL(entry.URL) {
			continue
		}
		articles = append(articles, reutersArticle{
			URL:         entry.URL,
			Title:       entry.Title,
			PublishedAt: entry.PublishedAt,
		})
		if len(articles) >= maxArticles {
			break
		}
	}

	return loop.NewSuccessResult(map[string]any{
		"article_count": len(articles),
		"articles":      formatArticles(articles),
	}), nil
}

// SearchReutersNewsTool searches for Reuters articles matching a query.
func (rt *ReutersTools) SearchReutersNewsTool(ctx context.Context, input SearchReutersNewsInput) (*loop.ToolResult, error) {
	query := input.Query
	if query == "" {
		return loop.NewErrorResult("query is required"), nil
	}

	maxArticles := input.MaxArticles
	if maxArticles <= 0 {
		maxArticles = 10
	}
	if maxArticles > 50 {
		maxArticles = 50
	}

	// Reuters publishes a sitemap with recent news articles
	entries, err := rt.fetchNewsSitemap(ctx, "https://www.reuters.com/arc/outboundfeeds/news-sitemap/?outputType=xml")
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to fetch sitemap: %v", err)), nil
	}

	queryWords := strings.Fields(strings.ToLower(query))
	articles := make([]reutersArticle, 0, maxArticles)

	for _, entry := range entries {
		if !matchesQuery(entry.Title, entry.URL, queryWords) {
			continue
		}
		articles = append(articles, reutersArticle{
			URL:         entry.URL,
			Title:       entry.Title,
			PublishedAt: entry.PublishedAt,
		})
		if len(articles) >= maxArticles {
			break
		}
	}

	return loop.NewSuccessResult(map[string]any{
		"query":         input.Query,
		"article_count": len(articles),
		"articles":      formatArticles(articles),
	}), nil
}

// ReadReutersArticleTool attempts to fetch a Reuters article body.
// Reuters article pages sit behind DataDome bot protection and typically
// return HTTP 401, so callers should expect this to fail and route around
// it (e.g. search the web for a mirror or a summary).
func (rt *ReutersTools) ReadReutersArticleTool(ctx context.Context, input ReadReutersArticleInput) (*loop.ToolResult, error) {
	if input.URL == "" {
		return loop.NewErrorResult("url is required"), nil
	}

	article, err := rt.fetchArticle(ctx, input.URL)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to fetch article (Reuters blocks direct article fetches; use web search instead): %v", err)), nil
	}
	if article == nil {
		return loop.NewErrorResult("could not extract content from article"), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"url":          article.URL,
		"title":        article.Title,
		"authors":      article.Authors,
		"published_at": article.PublishedAt,
		"content":      article.Content,
	}), nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (rt *ReutersTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"reuters_news":         "List the latest Reuters headlines from the public news sitemap. Returns URL, title, and publish date only — article bodies are not available because Reuters blocks direct scraping.",
		"search_reuters_news":  "Search the Reuters news sitemap for recent headlines matching a query. Returns URL, title, and publish date only — article bodies are not available because Reuters blocks direct scraping.",
		"read_reuters_article": "Attempt to fetch a Reuters article body by URL. Usually fails with HTTP 401 (DataDome bot protection); if you need the article content, fall back to a general web search.",
	}
	return descriptions[name]
}
