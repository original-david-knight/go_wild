package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SearchTools proxies search operations through the broker API.
type SearchTools struct {
	client *Client
}

// NewSearchTools creates broker-backed search tools.
func NewSearchTools(client *Client) *SearchTools {
	return &SearchTools{client: client}
}

func (s *SearchTools) WebSearchTool(ctx context.Context, input tools.WebSearchInput) (*loop.ToolResult, error) {
	result, err := s.client.Post(ctx, "/broker/v1/search/web", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (s *SearchTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"web_search": "Use this to search the web for information on any topic. Useful when you need to find current information, verify facts, or research topics.\n\n**CRITICAL: Search results only contain brief snippets — they are NOT sufficient for answering questions or completing tasks.** You MUST use read_webpage to open and read the full content of the most relevant URLs from the results. Never treat snippets as complete or reliable information. Always follow up a web_search with read_webpage calls on the top results before drawing conclusions or reporting findings to the user.",
	}
	return descriptions[name]
}
