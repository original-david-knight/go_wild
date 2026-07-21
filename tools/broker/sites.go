package broker

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools"
)

// SiteTools proxies static site operations through the broker API.
type SiteTools struct {
	client *Client
}

// NewSiteTools creates broker-backed site tools.
func NewSiteTools(client *Client) *SiteTools {
	return &SiteTools{client: client}
}

func (s *SiteTools) PublishSiteTool(ctx context.Context, input tools.PublishSiteInput) (*loop.ToolResult, error) {
	result, err := s.client.Post(ctx, "/broker/v1/sites/publish", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SiteTools) ListSitesTool(ctx context.Context, input tools.ListSitesInput) (*loop.ToolResult, error) {
	result, err := s.client.Post(ctx, "/broker/v1/sites/list", nil)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}
