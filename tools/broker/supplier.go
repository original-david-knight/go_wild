package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools/supplier"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SupplierTools proxies supplier operations through the broker API.
type SupplierTools struct {
	client *Client
}

// NewSupplierTools creates broker-backed supplier tools.
func NewSupplierTools(client *Client) *SupplierTools {
	return &SupplierTools{client: client}
}

func (s *SupplierTools) SupplierSearchProductsTool(ctx context.Context, input supplier.SearchProductsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_search_products", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SupplierTools) SupplierGetProductTool(ctx context.Context, input supplier.GetProductInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_get_product", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SupplierTools) SupplierGetShippingTool(ctx context.Context, input supplier.GetShippingInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_get_shipping", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SupplierTools) SupplierPlaceOrderTool(ctx context.Context, input supplier.PlaceOrderInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_place_order", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SupplierTools) SupplierGetOrderTool(ctx context.Context, input supplier.GetOrderInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_get_order", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SupplierTools) SupplierCancelOrderTool(ctx context.Context, input supplier.CancelOrderInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_cancel_order", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *SupplierTools) SupplierGetTrackingTool(ctx context.Context, input supplier.GetTrackingInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "supplier_get_tracking", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool delegates to the real tool implementations for descriptions.
func (s *SupplierTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"supplier_search_products": "Search the supplier catalog for products matching a query. Returns product listings with pricing, ratings, and shipping info. Use filters to narrow results by category, price range, rating, delivery time, and origin country.",
		"supplier_get_product":     "Get full details for a specific supplier product by ID. Returns complete product info including all variants, images, and shipping estimates.",
		"supplier_get_shipping":    "Get shipping cost and delivery time estimate for a product to a specific country. Returns method, cost, and estimated delivery window.",
		"supplier_place_order":     "Place an order with the supplier for drop-shipping. Requires product ID, variant, quantity, and full shipping address. Links to a Shopify order ID for tracking. This costs real money — the spend governor enforces daily limits.",
		"supplier_get_order":       "Get the current status of a supplier order by order ID. Returns status (pending, processing, shipped, delivered, cancelled), tracking number if available, and delivery estimates.",
		"supplier_cancel_order":    "Cancel a supplier order by order ID. Only works if the order has not yet shipped. Returns confirmation of cancellation or error if too late.",
		"supplier_get_tracking":    "Get tracking information for a supplier order. Returns carrier, tracking number, current status, tracking URL, and a list of tracking events with timestamps and locations.",
	}
	return descriptions[name]
}
