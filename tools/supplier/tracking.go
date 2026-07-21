package supplier

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SupplierTrackingTools provides tools for tracking supplier shipments.
type SupplierTrackingTools struct {
	client Supplier
}

// NewSupplierTrackingTools creates a new SupplierTrackingTools instance.
func NewSupplierTrackingTools(client Supplier) *SupplierTrackingTools {
	return &SupplierTrackingTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *SupplierTrackingTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"supplier_get_tracking": "Get tracking information for a supplier order. Returns carrier, tracking number, current status, tracking URL, and a list of tracking events with timestamps and locations.",
	}
	return descriptions[name]
}

// GetTrackingInput defines input for getting tracking info.
type GetTrackingInput struct {
	OrderID string `json:"order_id" description:"Supplier order ID" required:"true"`
}

// SupplierGetTrackingTool retrieves tracking information for a supplier order.
func (t *SupplierTrackingTools) SupplierGetTrackingTool(ctx context.Context, input GetTrackingInput) (*loop.ToolResult, error) {
	tracking, err := t.client.GetTracking(ctx, input.OrderID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get tracking: %v", err)), nil
	}

	return loop.NewSuccessResult(tracking), nil
}
