package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyInventoryTools provides tools for Shopify inventory management.
type ShopifyInventoryTools struct {
	client *ShopifyClient
}

// NewShopifyInventoryTools creates a new ShopifyInventoryTools instance.
func NewShopifyInventoryTools(client *ShopifyClient) *ShopifyInventoryTools {
	return &ShopifyInventoryTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyInventoryTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_get_inventory_level": "Get current inventory level for an item across locations.",
		"shopify_set_inventory_level": "Set inventory quantity for an item at a specific location.",
	}
	return descriptions[name]
}

// GetInventoryLevelInput defines input for getting inventory levels.
type GetInventoryLevelInput struct {
	InventoryItemID string `json:"inventory_item_id" description:"Shopify inventory item ID" required:"true"`
}

// ShopifyGetInventoryLevelTool gets inventory levels for an item.
func (t *ShopifyInventoryTools) ShopifyGetInventoryLevelTool(ctx context.Context, input GetInventoryLevelInput) (*loop.ToolResult, error) {
	data, err := t.client.GetInventoryLevel(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}

// SetInventoryLevelInput defines input for setting inventory levels.
type SetInventoryLevelInput struct {
	InventoryItemID string `json:"inventory_item_id" description:"Shopify inventory item ID" required:"true"`
	LocationID      string `json:"location_id" description:"Shopify location ID" required:"true"`
	Quantity        int    `json:"quantity" description:"New on-hand quantity" required:"true"`
}

// ShopifySetInventoryLevelTool sets inventory for an item at a location.
func (t *ShopifyInventoryTools) ShopifySetInventoryLevelTool(ctx context.Context, input SetInventoryLevelInput) (*loop.ToolResult, error) {
	result, err := t.client.SetInventoryLevel(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}
