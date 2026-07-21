package tools

import (
	"fmt"
	"strings"
)

func matchesQuery(title, articleURL string, queryWords []string) bool {
	combined := strings.ToLower(title + " " + articleURL)
	for _, word := range queryWords {
		if !strings.Contains(combined, word) {
			return false
		}
	}
	return true
}

// isNonEnglishURL checks if a Reuters URL is for a non-English locale.
func isNonEnglishURL(u string) bool {
	path := strings.TrimPrefix(u, "https://www.reuters.com/")
	path = strings.TrimPrefix(path, "http://www.reuters.com/")
	prefixes := []string{"es/", "pt/", "fr/", "de/", "ja/", "zh/", "ar/"}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// isReutersArticleURL checks if a URL path looks like a Reuters article link.
// Real articles have 3+ path segments (e.g. /world/us/some-article-2026-02-06/)
// and a date-like slug, distinguishing them from section pages (/world/africa/).
func isReutersArticleURL(href string) bool {
	lower := strings.ToLower(href)

	// Must be in a known Reuters section
	sections := []string{"/world/", "/business/", "/markets/", "/technology/", "/science/", "/sports/", "/lifestyle/", "/legal/", "/sustainability/"}
	inSection := false
	for _, section := range sections {
		if strings.Contains(lower, section) {
			inSection = true
			break
		}
	}
	if !inSection {
		return false
	}

	// Strip domain if present
	path := lower
	if idx := strings.Index(path, "reuters.com"); idx >= 0 {
		path = path[idx+len("reuters.com"):]
	}

	// Require 3+ path segments to exclude section/subsection index pages.
	// e.g. /world/us/article-slug-2026-02-06/ has segments [world, us, article-slug-2026-02-06]
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 3 {
		return false
	}

	// Exclude market quote pages (/markets/quote/.SPX/)
	if strings.Contains(lower, "/markets/quote/") {
		return false
	}

	return true
}

// resolveReutersURL makes a potentially relative Reuters URL absolute.
func resolveReutersURL(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return "https://www.reuters.com" + href
}

// formatArticles formats a slice of articles into readable markdown text.
func formatArticles(articles []reutersArticle) string {
	var sb strings.Builder
	for i, article := range articles {
		fmt.Fprintf(&sb, "## %d. %s\n", i+1, article.Title)
		fmt.Fprintf(&sb, "URL: %s\n", article.URL)
		if len(article.Authors) > 0 {
			fmt.Fprintf(&sb, "By: %s\n", strings.Join(article.Authors, ", "))
		}
		if article.PublishedAt != "" {
			fmt.Fprintf(&sb, "Published: %s\n", article.PublishedAt)
		}
		content := article.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		fmt.Fprintf(&sb, "\n%s\n\n", content)
	}
	return sb.String()
}
