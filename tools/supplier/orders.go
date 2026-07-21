package supplier

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SupplierOrderTools provides tools for placing and managing supplier orders.
type SupplierOrderTools struct {
	client Supplier
}

// NewSupplierOrderTools creates a new SupplierOrderTools instance.
func NewSupplierOrderTools(client Supplier) *SupplierOrderTools {
	return &SupplierOrderTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *SupplierOrderTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"supplier_place_order":  "Place an order with the supplier for drop-shipping. Requires product ID, variant, quantity, and full shipping address. Links to a Shopify order ID for tracking. This costs real money — the spend governor enforces daily limits.",
		"supplier_get_order":    "Get the current status of a supplier order by order ID. Returns status (pending, processing, shipped, delivered, cancelled), tracking number if available, and delivery estimates.",
		"supplier_cancel_order": "Cancel a supplier order by order ID. Only works if the order has not yet shipped. Returns confirmation of cancellation or error if too late.",
	}
	return descriptions[name]
}

// PlaceOrderInput defines input for placing a supplier order.
type PlaceOrderInput struct {
	SupplierName    string `json:"supplier_name" description:"Configured supplier adapter key" required:"true"`
	ProductID       string `json:"product_id" description:"Supplier product ID" required:"true"`
	VariantID       string `json:"variant_id" description:"Supplier variant ID" required:"true"`
	Quantity        int    `json:"quantity" description:"Order quantity" required:"true"`
	ShippingName    string `json:"shipping_name" description:"Recipient name" required:"true"`
	ShippingAddress string `json:"shipping_address" description:"Full shipping address" required:"true"`
	ShippingCity    string `json:"shipping_city" description:"Shipping city" required:"true"`
	ShippingState   string `json:"shipping_state" description:"Shipping state/province" required:"true"`
	ShippingZip     string `json:"shipping_zip" description:"Shipping postal code" required:"true"`
	ShippingCountry string `json:"shipping_country" description:"ISO country code" required:"true"`
	ShippingPhone   string `json:"shipping_phone" description:"Recipient phone number" required:"true"`
	ShopifyOrderID  string `json:"shopify_order_id" description:"Linked Shopify order for tracking"`
}

// SupplierPlaceOrderTool places an order with the supplier.
func (t *SupplierOrderTools) SupplierPlaceOrderTool(ctx context.Context, input PlaceOrderInput) (*loop.ToolResult, error) {
	if input.Quantity <= 0 {
		return loop.NewErrorResult("quantity must be greater than 0"), nil
	}

	order := OrderRequest{
		ProductID:       input.ProductID,
		VariantID:       input.VariantID,
		Quantity:        input.Quantity,
		ShippingName:    input.ShippingName,
		ShippingAddress: input.ShippingAddress,
		ShippingCity:    input.ShippingCity,
		ShippingState:   input.ShippingState,
		ShippingZip:     input.ShippingZip,
		ShippingCountry: input.ShippingCountry,
		ShippingPhone:   input.ShippingPhone,
		ShopifyOrderID:  input.ShopifyOrderID,
	}

	confirmation, err := t.client.PlaceOrder(ctx, order)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to place order: %v", err)), nil
	}

	return loop.NewSuccessResult(confirmation), nil
}

// GetOrderInput defines input for getting an order status.
type GetOrderInput struct {
	OrderID string `json:"order_id" description:"Supplier order ID" required:"true"`
}

// SupplierGetOrderTool retrieves the status of a supplier order.
func (t *SupplierOrderTools) SupplierGetOrderTool(ctx context.Context, input GetOrderInput) (*loop.ToolResult, error) {
	status, err := t.client.GetOrder(ctx, input.OrderID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get order: %v", err)), nil
	}

	return loop.NewSuccessResult(status), nil
}

// CancelOrderInput defines input for cancelling an order.
type CancelOrderInput struct {
	OrderID string `json:"order_id" description:"Supplier order ID to cancel" required:"true"`
}

// SupplierCancelOrderTool cancels a supplier order.
// Cancellation is only possible before the order ships.
func (t *SupplierOrderTools) SupplierCancelOrderTool(ctx context.Context, input CancelOrderInput) (*loop.ToolResult, error) {
	// Check current status first
	status, err := t.client.GetOrder(ctx, input.OrderID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get order for cancellation: %v", err)), nil
	}

	if status.Status == "shipped" || status.Status == "delivered" {
		return loop.NewErrorResult(fmt.Sprintf("cannot cancel order %s: already %s", input.OrderID, status.Status)), nil
	}

	if status.Status == "cancelled" {
		return loop.NewSuccessResult(map[string]any{
			"order_id": input.OrderID,
			"status":   "already_cancelled",
			"message":  "Order was already cancelled",
		}), nil
	}

	// For now, cancellation goes through GetOrder — providers that support direct
	// cancellation can override. The broker-side handler manages the actual API call.
	return loop.NewSuccessResult(map[string]any{
		"order_id": input.OrderID,
		"status":   "cancel_requested",
		"message":  "Cancellation request submitted",
	}), nil
}
