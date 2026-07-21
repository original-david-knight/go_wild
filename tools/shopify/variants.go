package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyVariantTools provides tools for Shopify variant management.
type ShopifyVariantTools struct {
	client *ShopifyClient
}

// NewShopifyVariantTools creates a new ShopifyVariantTools instance.
func NewShopifyVariantTools(client *ShopifyClient) *ShopifyVariantTools {
	return &ShopifyVariantTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyVariantTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_update_variant": "Update a product variant's price, compare-at price, or SKU.",
		"shopify_list_variants":  "List all variants for a given product including prices and inventory.",
	}
	return descriptions[name]
}

// UpdateVariantInput defines input for updating a variant.
type UpdateVariantInput struct {
	VariantID string `json:"variant_id" description:"Shopify variant ID" required:"true"`
	Price     string `json:"price" description:"New price"`
	CompareAt string `json:"compare_at_price" description:"New compare-at price"`
	SKU       string `json:"sku" description:"New SKU"`
}

// ShopifyUpdateVariantTool updates a product variant.
func (t *ShopifyVariantTools) ShopifyUpdateVariantTool(ctx context.Context, input UpdateVariantInput) (*loop.ToolResult, error) {
	result, err := t.client.UpdateVariant(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// ListVariantsInput defines input for listing variants.
type ListVariantsInput struct {
	ProductID string `json:"product_id" description:"Shopify product ID" required:"true"`
	Limit     int    `json:"limit" description:"Max variants to return (default 50)"`
}

// ShopifyListVariantsTool lists variants for a product.
func (t *ShopifyVariantTools) ShopifyListVariantsTool(ctx context.Context, input ListVariantsInput) (*loop.ToolResult, error) {
	data, err := t.client.ListVariants(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}
