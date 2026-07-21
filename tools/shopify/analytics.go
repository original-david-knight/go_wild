package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyAnalyticsTools provides tools for Shopify analytics and reports.
type ShopifyAnalyticsTools struct {
	client *ShopifyClient
}

// NewShopifyAnalyticsTools creates a new ShopifyAnalyticsTools instance.
func NewShopifyAnalyticsTools(client *ShopifyClient) *ShopifyAnalyticsTools {
	return &ShopifyAnalyticsTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyAnalyticsTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_get_reports":        "List available Shopify reports.",
		"shopify_get_orders_summary": "Get an aggregate summary of orders within a date range including total revenue and order counts.",
	}
	return descriptions[name]
}

// GetReportsInput defines input for listing reports.
type GetReportsInput struct {
	Limit int `json:"limit" description:"Max reports to return"`
}

// ShopifyGetReportsTool lists available Shopify reports.
func (t *ShopifyAnalyticsTools) ShopifyGetReportsTool(ctx context.Context, input GetReportsInput) (*loop.ToolResult, error) {
	data, err := t.client.GetReports(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}

// GetOrdersSummaryInput defines input for orders summary.
type GetOrdersSummaryInput struct {
	DateFrom string `json:"date_from" description:"Start date (YYYY-MM-DD)" required:"true"`
	DateTo   string `json:"date_to" description:"End date (YYYY-MM-DD)" required:"true"`
}

// ShopifyGetOrdersSummaryTool gets an orders summary for a date range.
func (t *ShopifyAnalyticsTools) ShopifyGetOrdersSummaryTool(ctx context.Context, input GetOrdersSummaryInput) (*loop.ToolResult, error) {
	data, err := t.client.GetOrdersSummary(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}
