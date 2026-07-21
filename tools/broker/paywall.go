package broker

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools"
)

// PaywallTools proxies crypto paywall operations through the broker API.
type PaywallTools struct {
	client *Client
}

// NewPaywallTools creates broker-backed paywall tools.
func NewPaywallTools(client *Client) *PaywallTools {
	return &PaywallTools{client: client}
}

func (p *PaywallTools) CreateCryptoPaywallTool(ctx context.Context, input tools.CreateCryptoPaywallInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/paywall/create", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}
