package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/ledongthuc/pdf"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ContentCompressor compresses web content by removing off-topic material.
type ContentCompressor func(ctx context.Context, markdown string) (string, error)

// Chrome-like User-Agent to avoid bot detection
const chromeUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// ReadWebpageInput defines the input for the webpage reader tool.
type ReadWebpageInput struct {
	URL     string `json:"url" description:"The full URL of the webpage to read" required:"true"`
	RawHTML bool   `json:"raw_html,omitempty" description:"If true, return raw HTML instead of converting to markdown. Use this when you need to see the exact HTML structure, parse specific elements, or when markdown conversion loses important information."`
}

// Minimum content size to trigger local LLM compression.
const compressMinBytes = 4096

// Response size limits.
const (
	maxFetchBytes = 50 * 1024 * 1024 // general webpage fetch ceiling
	// maxPDFBytes bounds PDF size. The pdf library requires a ReaderAt over the
	// full document and allocates additional memory per page during extraction,
	// so we cap PDFs below the general fetch limit.
	maxPDFBytes = 20 * 1024 * 1024
)

const (
	compressTimeout             = 20 * time.Second
	compressMinRemaining        = 15 * time.Second
	compressFailureCooldown     = 5 * time.Minute
	compressSkipLogCooldown     = 2 * time.Minute
	compressDeadlineSkipMessage = "web content compression skipped: insufficient time remaining"
	oldRedditHost               = "old.reddit.com"
)

type webpageFetchHTTPError struct {
	StatusCode int
	Status     string
}

func (e *webpageFetchHTTPError) Error() string {
	return fmt.Sprintf("HTTP error: %d %s", e.StatusCode, e.Status)
}

type webpageFetchResult struct {
	body        []byte
	contentType string
}

// WebReaderTools provides web reading tools.
type WebReaderTools struct {
	httpClient *http.Client
	compress   ContentCompressor

	compressMu             sync.Mutex
	compressDisabledUntil  time.Time
	lastCompressionSkipLog time.Time
}

// NewWebReaderTools creates a new WebReaderTools instance.
func NewWebReaderTools(compress ContentCompressor) *WebReaderTools {
	return &WebReaderTools{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		compress: compress,
	}
}

// ReadWebpageTool fetches a webpage and converts it to markdown.
func (w *WebReaderTools) ReadWebpageTool(ctx context.Context, input ReadWebpageInput) (*loop.ToolResult, error) {
	if input.URL == "" {
		return loop.NewErrorResult("url is required"), nil
	}

	// Validate URL
	parsedURL, err := url.Parse(input.URL)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("invalid URL: %v", err)), nil
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return loop.NewErrorResult("URL must use http or https scheme"), nil
	}

	fetchResult, err := w.fetchWebpageWithFallback(ctx, parsedURL)
	if err != nil {
		var httpErr *webpageFetchHTTPError
		if errors.As(err, &httpErr) {
			return loop.NewErrorResult(httpErr.Error()), nil
		}
		return loop.NewErrorResult(fmt.Sprintf("failed to fetch URL: %v", err)), nil
	}

	// Check content type
	contentType := fetchResult.contentType
	isHTML := strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml")
	isMarkdown := strings.Contains(contentType, "text/markdown") || strings.Contains(contentType, "text/x-markdown")
	isPlainText := strings.Contains(contentType, "text/plain")
	isPDF := strings.Contains(contentType, "application/pdf")

	// Also detect PDFs by URL extension when content type is generic (e.g. application/octet-stream)
	if !isPDF && !isHTML && !isMarkdown && !isPlainText {
		if strings.HasSuffix(strings.ToLower(parsedURL.Path), ".pdf") {
			isPDF = true
		}
	}

	if !isHTML && !isMarkdown && !isPlainText && !isPDF {
		return loop.NewErrorResult(fmt.Sprintf("unsupported content type: %s (expected HTML, markdown, plain text, or PDF)", contentType)), nil
	}

	body := fetchResult.body

	var content string
	var format string
	truncated := false

	if isPDF {
		content, err = extractPDFText(body)
		if err != nil {
			return loop.NewErrorResult(fmt.Sprintf("failed to extract text from PDF: %v", err)), nil
		}
		format = "text"
		if len(content) > 1024*1024 {
			content = content[:1024*1024] + "\n\n... [content truncated]"
			truncated = true
		}
	} else if isMarkdown || (isPlainText && looksLikeMarkdown(string(body))) {
		// Already markdown - return as-is
		content = string(body)
		format = "markdown"
		// Truncate if too long (1MB of markdown)
		if len(content) > 1024*1024 {
			content = content[:1024*1024] + "\n\n... [content truncated]"
			truncated = true
		}
	} else if isPlainText {
		// Plain text - return as-is
		content = string(body)
		format = "text"
		if len(content) > 1024*1024 {
			content = content[:1024*1024] + "\n\n... [content truncated]"
			truncated = true
		}
	} else if input.RawHTML {
		// Return raw HTML
		content = string(body)
		format = "html"
		// Truncate if too long (2MB for HTML since it's more verbose)
		if len(content) > 2*1024*1024 {
			content = content[:2*1024*1024] + "\n\n<!-- content truncated -->"
			truncated = true
		}
	} else {
		// Convert HTML to Markdown
		content, err = w.htmlToMarkdown(string(body), input.URL)
		if err != nil {
			return loop.NewErrorResult(fmt.Sprintf("failed to convert to markdown: %v", err)), nil
		}
		format = "markdown"
		// Use local LLM to strip off-topic content
		if len(content) >= compressMinBytes {
			if compressed, err := w.compressContent(ctx, content); err != nil {
				log.Printf("web content compression skipped: %v", err)
			} else {
				content = compressed
			}
		}
		// Truncate if still too long (1MB of markdown)
		if len(content) > 1024*1024 {
			content = content[:1024*1024] + "\n\n... [content truncated]"
			truncated = true
		}
	}

	var title string
	if !isPDF {
		title = extractTitle(string(body))
	}

	return loop.NewSuccessResult(map[string]any{
		"url":       input.URL,
		"title":     title,
		"content":   content,
		"format":    format,
		"length":    len(content),
		"truncated": truncated,
	}), nil
}

func (w *WebReaderTools) fetchWebpageWithFallback(ctx context.Context, parsedURL *url.URL) (*webpageFetchResult, error) {
	candidates := webpageFetchCandidates(parsedURL)
	var lastErr error
	for i, candidate := range candidates {
		result, err := w.fetchWebpageOnce(ctx, candidate)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i+1 < len(candidates) {
			log.Printf("read_webpage retry: %s -> %s after error: %v", candidate, candidates[i+1], err)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("failed to fetch URL")
	}
	return nil, lastErr
}

func (w *WebReaderTools) fetchWebpageOnce(ctx context.Context, rawURL string) (*webpageFetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", chromeUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &webpageFetchHTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	contentType := resp.Header.Get("Content-Type")
	limit := int64(maxFetchBytes)
	if looksLikePDFResponse(rawURL, contentType) {
		limit = int64(maxPDFBytes)
		if resp.ContentLength > limit {
			return nil, fmt.Errorf("PDF too large to process: %d bytes (limit: %d bytes)", resp.ContentLength, limit)
		}
	}

	// Read limit+1 bytes so we can distinguish "fits within limit" from "exceeds limit".
	// io.LimitReader alone would silently truncate at limit and hide the overflow.
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > limit {
		if looksLikePDFResponse(rawURL, contentType) {
			return nil, fmt.Errorf("PDF too large to process: exceeds limit of %d bytes", limit)
		}
		body = body[:limit]
	}

	return &webpageFetchResult{
		body:        body,
		contentType: contentType,
	}, nil
}

// looksLikePDFResponse detects a PDF response from the Content-Type header or
// URL extension so the fetch loop can apply a tighter size cap.
func looksLikePDFResponse(rawURL, contentType string) bool {
	if strings.Contains(strings.ToLower(contentType), "application/pdf") {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

func webpageFetchCandidates(parsedURL *url.URL) []string {
	if parsedURL == nil {
		return nil
	}
	original := parsedURL.String()
	if !isRedditFetchHost(parsedURL.Hostname()) {
		return []string{original}
	}

	oldReddit := *parsedURL
	oldReddit.Host = oldRedditHost
	oldURL := oldReddit.String()
	if oldURL == original {
		return []string{original}
	}
	return []string{oldURL, original}
}

func isRedditFetchHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "reddit.com", "www.reddit.com", "np.reddit.com":
		return true
	default:
		return false
	}
}

// htmlToMarkdown converts HTML to markdown with absolute image URLs.
func (w *WebReaderTools) htmlToMarkdown(html string, baseURL string) (string, error) {
	// Parse base URL for resolving relative links
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	// Convert HTML to markdown using the v2 API
	markdown, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return "", err
	}

	// Fix relative URLs in images and links
	markdown = w.resolveRelativeURLs(markdown, base)

	// Clean up excessive whitespace
	markdown = cleanupMarkdown(markdown)

	return markdown, nil
}

// resolveRelativeURLs converts relative URLs to absolute URLs in markdown.
func (w *WebReaderTools) resolveRelativeURLs(markdown string, base *url.URL) string {
	// Match markdown images: ![alt](url)
	imgRe := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	markdown = imgRe.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := imgRe.FindStringSubmatch(match)
		if len(parts) == 3 {
			alt := parts[1]
			imgURL := parts[2]
			absURL := resolveURL(base, imgURL)
			return fmt.Sprintf("![%s](%s)", alt, absURL)
		}
		return match
	})

	// Match markdown links: [text](url) - but not images
	linkRe := regexp.MustCompile(`([^!])\[([^\]]+)\]\(([^)]+)\)`)
	markdown = linkRe.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		if len(parts) == 4 {
			prefix := parts[1]
			text := parts[2]
			linkURL := parts[3]
			absURL := resolveURL(base, linkURL)
			return fmt.Sprintf("%s[%s](%s)", prefix, text, absURL)
		}
		return match
	})

	return markdown
}

// resolveURL resolves a potentially relative URL against a base URL.
func resolveURL(base *url.URL, rawURL string) string {
	// Already absolute
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}

	// Data URLs - leave as is
	if strings.HasPrefix(rawURL, "data:") {
		return rawURL
	}

	// Parse and resolve
	ref, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	return base.ResolveReference(ref).String()
}

// extractTitle extracts the title from HTML.
func extractTitle(html string) string {
	titleRe := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	matches := titleRe.FindStringSubmatch(html)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// cleanupMarkdown removes excessive whitespace and empty lines.
func cleanupMarkdown(markdown string) string {
	// Replace multiple newlines with double newline
	multiNewline := regexp.MustCompile(`\n{3,}`)
	markdown = multiNewline.ReplaceAllString(markdown, "\n\n")

	// Trim leading/trailing whitespace
	markdown = strings.TrimSpace(markdown)

	return markdown
}

// looksLikeMarkdown checks if plain text content appears to be markdown.
func looksLikeMarkdown(content string) bool {
	// Check for common markdown patterns
	markdownPatterns := []string{
		"^#+ ",             // Headers
		"^\\* ",            // Unordered lists
		"^- ",              // Unordered lists
		"^\\d+\\. ",        // Ordered lists
		"\\[.+\\]\\(.+\\)", // Links
		"```",              // Code blocks
		"\\*\\*",           // Bold
		"^>",               // Blockquotes
	}

	lines := strings.Split(content, "\n")
	// Check first 50 lines for markdown patterns
	checkLines := lines
	if len(checkLines) > 50 {
		checkLines = lines[:50]
	}

	patternCount := 0
	for _, line := range checkLines {
		for _, pattern := range markdownPatterns {
			if matched, _ := regexp.MatchString(pattern, line); matched {
				patternCount++
				if patternCount >= 3 {
					return true
				}
				break
			}
		}
	}

	return false
}

// errPDFPageNull signals that a page slot in the page tree resolved to a
// null object and should be counted as a skipped null page rather than an
// extraction error.
var errPDFPageNull = errors.New("pdf page is null")

// extractPDFPageText pulls text from a single page, recovering any panic
// raised by the ledongthuc/pdf library. Scoping the recover per page means a
// panic on page N does not discard text already extracted from earlier pages.
func extractPDFPageText(reader *pdf.Reader, num int) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("extractPDFText: recovered panic on page %d: %v\n%s", num, r, debug.Stack())
			text = ""
			err = fmt.Errorf("failed to parse PDF page %d: %v", num, r)
		}
	}()
	page := reader.Page(num)
	if page.V.IsNull() {
		return "", errPDFPageNull
	}
	return page.GetPlainText(nil)
}

// extractPDFText extracts all text content from a PDF byte slice.
//
// The underlying ledongthuc/pdf library uses panic() as a parse-error channel
// in NewReader, NumPage, Page, and the generic Value accessors, and only some
// entry points recover. Since we feed this extractor PDFs fetched from
// untrusted URLs, a malformed or malicious PDF could otherwise crash the
// process.
//
// Panic containment is split into two scopes:
//   - The outer recover (here) catches panics during document setup
//     (NewReader/NumPage) where there is no accumulated text to preserve.
//   - extractPDFPageText has its own per-page recover so a panic on one page
//     is counted like any other page error and does not discard text already
//     extracted from earlier pages.
//
// Note: panic recovery does not bound CPU or memory consumed by a malicious
// PDF (decompression bombs, pathological parser input). The 20 MB input cap
// is the only resource bound. For stronger isolation, the caller would need
// to run extraction in a subprocess or container with a hard execution
// budget.
func extractPDFText(data []byte) (result string, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("extractPDFText: recovered panic during PDF setup: %v\n%s", r, debug.Stack())
			result = ""
			retErr = fmt.Errorf("failed to parse PDF: %v", r)
		}
	}()

	if len(data) > maxPDFBytes {
		return "", fmt.Errorf("PDF too large to process: %d bytes (limit: %d bytes)", len(data), maxPDFBytes)
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	var buf strings.Builder
	var firstExtractErr error
	nullPages := 0
	errorPages := 0
	numPages := reader.NumPage()
	for i := 1; i <= numPages; i++ {
		text, err := extractPDFPageText(reader, i)
		if errors.Is(err, errPDFPageNull) {
			nullPages++
			continue
		}
		if err != nil {
			errorPages++
			if firstExtractErr == nil {
				firstExtractErr = err
			}
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(strings.TrimSpace(text))
	}

	text := strings.TrimSpace(buf.String())
	skipped := nullPages + errorPages
	if text == "" {
		if errorPages > 0 {
			return "", fmt.Errorf("PDF text extraction failed on all %d pages (%d errored, %d null; first error: %v)", numPages, errorPages, nullPages, firstExtractErr)
		}
		return "", fmt.Errorf("PDF contains no extractable text (may be image-based or encrypted)")
	}
	if skipped > 0 {
		if errorPages > 0 {
			log.Printf("extractPDFText: partial extraction — skipped %d/%d pages (%d null, %d errored; first error: %v)", skipped, numPages, nullPages, errorPages, firstExtractErr)
		} else {
			log.Printf("extractPDFText: partial extraction — skipped %d/%d null pages", skipped, numPages)
		}
	}
	return text, nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (w *WebReaderTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"read_webpage": `Fetch and read a webpage's content. By default returns clean markdown for easy reading. Also supports PDF files — text is extracted automatically.

OUTPUT FORMATS:
- Markdown (default): Clean, readable text with links preserved. Best for articles, docs, blog posts.
- Raw HTML (raw_html: true): Exact HTML source. Use when you need to:
  - See the precise DOM structure
  - Find specific elements, classes, or IDs
  - Debug why something isn't rendering
  - Extract data from structured HTML (tables, lists, forms)
- PDF: Text is extracted automatically when URL points to a PDF. Returns plain text.

NOTE: This fetches static HTML only. For JavaScript-rendered content (React, Vue, Angular apps), use the browse() tool instead which runs a real browser.`,
	}
	return descriptions[name]
}

// compressContent uses the broker's local LLM to strip off-topic content from a webpage.
func (w *WebReaderTools) compressContent(ctx context.Context, markdown string) (string, error) {
	if w.compress == nil {
		return markdown, nil
	}

	now := time.Now()
	if w.isCompressionDisabled(now) {
		return markdown, nil
	}

	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < compressMinRemaining {
		w.maybeLogCompressionSkip(compressDeadlineSkipMessage)
		return markdown, nil
	}
	if err := ctx.Err(); err != nil {
		return markdown, nil
	}

	// Cap input at 320KB to stay within Gemini Flash context window
	input := markdown
	if len(input) > 320*1024 {
		input = input[:320*1024]
	}

	// Keep request-scoped values but decouple cancellation/deadline so compression
	// does not immediately fail when the parent context is nearly expired.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), compressTimeout)
	defer cancel()

	compressed, err := w.compress(ctx, input)
	if err != nil {
		if isCompressionTimeoutError(err) {
			w.disableCompressionUntil(time.Now().Add(compressFailureCooldown))
			w.maybeLogCompressionSkip("web content compression temporarily disabled after timeout")
			return markdown, nil
		}
		return "", err
	}

	if strings.TrimSpace(compressed) == "" {
		return markdown, nil
	}

	return compressed, nil
}

func (w *WebReaderTools) isCompressionDisabled(now time.Time) bool {
	w.compressMu.Lock()
	defer w.compressMu.Unlock()
	return now.Before(w.compressDisabledUntil)
}

func (w *WebReaderTools) disableCompressionUntil(until time.Time) {
	w.compressMu.Lock()
	defer w.compressMu.Unlock()
	if until.After(w.compressDisabledUntil) {
		w.compressDisabledUntil = until
	}
}

func (w *WebReaderTools) maybeLogCompressionSkip(message string) {
	now := time.Now()
	w.compressMu.Lock()
	defer w.compressMu.Unlock()
	if now.Sub(w.lastCompressionSkipLog) < compressSkipLogCooldown {
		return
	}
	w.lastCompressionSkipLog = now
	log.Printf("%s", message)
}

func isCompressionTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "context canceled")
}
