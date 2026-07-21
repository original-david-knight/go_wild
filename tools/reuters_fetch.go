package tools

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// newRequest creates an HTTP request with full Chrome browser headers.
// Reuters uses DataDome bot protection which requires Sec-Ch-Ua and Sec-Fetch-*
// headers to pass without triggering a 401 JavaScript challenge.
func (rt *ReutersTools) newRequest(ctx context.Context, targetURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Linux"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Connection", "keep-alive")
	return req, nil
}

// fetchArticle fetches and parses a single Reuters article page.
// If caching is enabled, returns cached articles and caches new ones for 3 hours.
func (rt *ReutersTools) fetchArticle(ctx context.Context, articleURL string) (*reutersArticle, error) {
	if rt.cache != nil {
		var cached reutersArticle
		if rt.cache.GetJSON(ctx, "reuters:article:"+articleURL, &cached) {
			return &cached, nil
		}
	}

	req, err := rt.newRequest(ctx, articleURL)
	if err != nil {
		return nil, err
	}

	resp, err := rt.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Reuters returned HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	article := &reutersArticle{URL: articleURL}

	// Title
	article.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	if article.Title == "" {
		article.Title = strings.TrimSpace(doc.Find("title").First().Text())
	}

	// Author and publish date
	doc.Find("meta").Each(func(_ int, s *goquery.Selection) {
		if name, _ := s.Attr("name"); name == "author" {
			if content, _ := s.Attr("content"); content != "" {
				article.Authors = append(article.Authors, strings.TrimSpace(content))
			}
		}
		if prop, _ := s.Attr("property"); prop == "article:published_time" {
			if content, _ := s.Attr("content"); content != "" {
				article.PublishedAt = strings.TrimSpace(content)
			}
		}
	})

	// Main content
	var contentParts []string
	doc.Find("p").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text == "" {
			return
		}

		// Skip boilerplate
		if strings.HasPrefix(text, "Reporting by") || strings.HasPrefix(text, "Editing by") || strings.HasPrefix(text, "Our Standards:") {
			return
		}

		contentParts = append(contentParts, text)
	})

	article.Content = strings.Join(contentParts, "\n\n")

	if article.Content == "" && article.Title == "" {
		return nil, nil
	}

	if rt.cache != nil {
		_ = rt.cache.SetJSON(ctx, "reuters:article:"+articleURL, article, reutersArticleTTL)
	}

	return article, nil
}

type sitemapEntry struct {
	URL         string
	Title       string
	PublishedAt string
}

// fetchNewsSitemap fetches and parses the Reuters news sitemap.
func (rt *ReutersTools) fetchNewsSitemap(ctx context.Context, sitemapURL string) ([]sitemapEntry, error) {
	req, err := rt.newRequest(ctx, sitemapURL)
	if err != nil {
		return nil, err
	}

	resp, err := rt.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Reuters returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var sitemap struct {
		URLs []struct {
			Loc  string `xml:"loc"`
			News struct {
				Title string `xml:"title"`
				Date  string `xml:"publication_date"`
			} `xml:"news"`
		} `xml:"url"`
	}

	if err := xml.Unmarshal(body, &sitemap); err != nil {
		return nil, err
	}

	entries := make([]sitemapEntry, 0, len(sitemap.URLs))
	for _, u := range sitemap.URLs {
		entries = append(entries, sitemapEntry{
			URL:         u.Loc,
			Title:       u.News.Title,
			PublishedAt: u.News.Date,
		})
	}

	return entries, nil
}
