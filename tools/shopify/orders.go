package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyOrderTools provides tools for Shopify order management.
type ShopifyOrderTools struct {
	client *ShopifyClient
}

// NewShopifyOrderTools creates a new ShopifyOrderTools instance.
func NewShopifyOrderTools(client *ShopifyClient) *ShopifyOrderTools {
	return &ShopifyOrderTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyOrderTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_list_orders":       "List orders with optional status and fulfillment filters.",
		"shopify_get_order":         "Get full details for a single order including line items, shipping, and fulfillments.",
		"shopify_update_order":      "Update order notes or tags.",
		"shopify_create_fulfillment": "Create a fulfillment for an order with tracking information.",
	}
	return descriptions[name]
}

// ListOrdersInput defines input for listing orders.
type ListOrdersInput struct {
	Status            string `json:"status" description:"Filter by order status" enum:"open,closed,cancelled,any"`
	FulfillmentStatus string `json:"fulfillment_status" description:"Filter by fulfillment status" enum:"shipped,partial,unshipped,unfulfilled"`
	Limit             int    `json:"limit" description:"Max orders to return (default 20, max 250)"`
	Cursor            string `json:"cursor" description:"Pagination cursor from previous response"`
}

// ShopifyListOrdersTool lists orders in the store.
func (t *ShopifyOrderTools) ShopifyListOrdersTool(ctx context.Context, input ListOrdersInput) (*loop.ToolResult, error) {
	data, err := t.client.ListOrders(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}

// GetOrderInput defines input for getting an order.
type GetOrderInput struct {
	OrderID string `json:"order_id" description:"Shopify order ID" required:"true"`
}

// ShopifyGetOrderTool retrieves a single order.
func (t *ShopifyOrderTools) ShopifyGetOrderTool(ctx context.Context, input GetOrderInput) (*loop.ToolResult, error) {
	order, err := t.client.GetOrder(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(order), nil
}

// UpdateOrderInput defines input for updating an order.
type UpdateOrderInput struct {
	OrderID string   `json:"order_id" description:"Shopify order ID" required:"true"`
	Note    string   `json:"note" description:"Order note"`
	Tags    []string `json:"tags" description:"Order tags"`
}

// ShopifyUpdateOrderTool updates an order's notes or tags.
func (t *ShopifyOrderTools) ShopifyUpdateOrderTool(ctx context.Context, input UpdateOrderInput) (*loop.ToolResult, error) {
	result, err := t.client.UpdateOrder(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// CreateFulfillmentInput defines input for creating a fulfillment.
type CreateFulfillmentInput struct {
	OrderID         string `json:"order_id" description:"Shopify order ID to fulfill" required:"true"`
	TrackingNumber  string `json:"tracking_number" description:"Shipping tracking number"`
	TrackingURL     string `json:"tracking_url" description:"Tracking URL"`
	TrackingCompany string `json:"tracking_company" description:"Shipping carrier name"`
	NotifyCustomer  bool   `json:"notify_customer" description:"Send shipment notification to customer"`
}

// ShopifyCreateFulfillmentTool creates a fulfillment for an order.
func (t *ShopifyOrderTools) ShopifyCreateFulfillmentTool(ctx context.Context, input CreateFulfillmentInput) (*loop.ToolResult, error) {
	fulfillment, err := t.client.CreateFulfillment(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(fulfillment), nil
}
