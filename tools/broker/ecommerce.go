package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools/ecommerce"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// EcommerceTools proxies e-commerce analytics operations through the broker API.
type EcommerceTools struct {
	client *Client
}

// NewEcommerceTools creates a new EcommerceTools instance.
func NewEcommerceTools(client *Client) *EcommerceTools {
	return &EcommerceTools{client: client}
}

// EcommerceProductPnlTool gets P&L for a specific product via broker.
func (s *EcommerceTools) EcommerceProductPnlTool(ctx context.Context, input ecommerce.ProductPnLInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ecommerce_product_pnl", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// EcommerceDailyPnlTool gets daily P&L summary via broker.
func (s *EcommerceTools) EcommerceDailyPnlTool(ctx context.Context, input ecommerce.DailyPnLInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ecommerce_daily_pnl", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// EcommerceCalculateMarginTool calculates margins via broker.
func (s *EcommerceTools) EcommerceCalculateMarginTool(ctx context.Context, input ecommerce.CalculateMarginInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ecommerce_calculate_margin", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// EcommerceSuggestPriceTool suggests pricing via broker.
func (s *EcommerceTools) EcommerceSuggestPriceTool(ctx context.Context, input ecommerce.SuggestPriceInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ecommerce_suggest_price", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool returns the description for a tool by name.
func (s *EcommerceTools) DescribeTool(name string) string {
	pnl := ecommerce.NewPnLTools()
	if d := pnl.DescribeTool(name); d != "" {
		return d
	}
	pricing := ecommerce.NewPricingTools()
	return pricing.DescribeTool(name)
}
