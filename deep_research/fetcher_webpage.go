package deepresearch

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools"
)

// fetchHTTPError represents a non-2xx HTTP response from a fetch attempt.
type fetchHTTPError struct {
	StatusCode int
	Status     string
}

func (e *fetchHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d %s", e.StatusCode, e.Status)
}

type deepResearchReadWebpageTool interface {
	ReadWebpageTool(ctx context.Context, input tools.ReadWebpageInput) (*loop.ToolResult, error)
}

type deepResearchWebpageFetcher struct {
	reader deepResearchReadWebpageTool
}

func NewWebpageFetcher() (*deepResearchWebpageFetcher, error) {
	return newWebpageFetcherWithReader(tools.NewWebReaderTools(nil)), nil
}

func newWebpageFetcherWithReader(reader deepResearchReadWebpageTool) *deepResearchWebpageFetcher {
	return &deepResearchWebpageFetcher{reader: reader}
}

func (f *deepResearchWebpageFetcher) Fetch(ctx context.Context, rawURL string) (FetchedDocument, error) {
	if f == nil || f.reader == nil {
		return FetchedDocument{}, fmt.Errorf("read_webpage tool is not configured")
	}

	result, err := f.reader.ReadWebpageTool(ctx, tools.ReadWebpageInput{URL: strings.TrimSpace(rawURL)})
	if err != nil {
		return FetchedDocument{}, err
	}
	if result == nil {
		return FetchedDocument{}, fmt.Errorf("read_webpage returned nil result")
	}
	if !result.Success {
		return FetchedDocument{}, deepResearchReadWebpageError(result.Error)
	}

	payload, ok := result.Content.(map[string]any)
	if !ok {
		return FetchedDocument{}, fmt.Errorf("read_webpage returned unexpected content type %T", result.Content)
	}

	docURL, _ := payload["url"].(string)
	docURL = strings.TrimSpace(docURL)
	if docURL == "" {
		docURL = strings.TrimSpace(rawURL)
	}

	title, _ := payload["title"].(string)
	content, _ := payload["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" {
		return FetchedDocument{}, fmt.Errorf("page returned no readable content")
	}

	return FetchedDocument{
		URL:     docURL,
		Title:   strings.TrimSpace(title),
		Content: content,
	}, nil
}

func deepResearchReadWebpageError(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("read_webpage failed")
	}

	const prefix = "HTTP error:"
	if strings.HasPrefix(message, prefix) {
		rest := strings.TrimSpace(strings.TrimPrefix(message, prefix))
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			statusCode, err := strconv.Atoi(fields[0])
			if err == nil {
				status := strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
				if status == "" {
					status = rest
				}
				return &fetchHTTPError{StatusCode: statusCode, Status: status}
			}
		}
	}

	return fmt.Errorf("%s", message)
}
