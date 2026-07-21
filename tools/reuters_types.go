package tools

import (
	"context"
	"net/http"
	"time"
)

// reutersArticle represents a parsed Reuters news article.
type reutersArticle struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Authors     []string `json:"authors,omitempty"`
	PublishedAt string   `json:"published_at,omitempty"`
	Content     string   `json:"content"`
}

// ReutersNewsInput defines input for the reuters_news tool.
type ReutersNewsInput struct {
	MaxArticles int `json:"max_articles,omitempty" description:"Maximum number of articles to fetch from the frontpage (default: 10, max: 30)"`
}

// SearchReutersNewsInput defines input for the search_reuters_news tool.
type SearchReutersNewsInput struct {
	Query       string `json:"query" description:"The search query to find Reuters articles about a specific topic"`
	MaxArticles int    `json:"max_articles,omitempty" description:"Maximum number of articles to return (default: 10, max: 50)"`
}

// ReadReutersArticleInput defines input for the read_reuters_article tool.
type ReadReutersArticleInput struct {
	URL string `json:"url" description:"The full Reuters article URL to read" required:"true"`
}

const reutersArticleTTL = 3 * time.Hour

// articleCache is the interface used by ReutersTools for caching fetched articles.
// Satisfied by gowild_data.Cache (direct DB) or any broker-backed cache that
// implements GetJSON/SetJSON.
type articleCache interface {
	GetJSON(ctx context.Context, key string, dest any) bool
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
}

// ReutersTools provides Reuters news fetching and searching tools.
type ReutersTools struct {
	httpClient *http.Client
	cache      articleCache
}
