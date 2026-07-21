package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyProductTools provides tools for Shopify product CRUD.
type ShopifyProductTools struct {
	client *ShopifyClient
}

// NewShopifyProductTools creates a new ShopifyProductTools instance.
func NewShopifyProductTools(client *ShopifyClient) *ShopifyProductTools {
	return &ShopifyProductTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyProductTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_create_product": "Create a new product in the Shopify store with title, description, pricing, and images.",
		"shopify_update_product": "Update an existing Shopify product's title, description, vendor, tags, status, or metafields. Metafields allow setting structured key-value data (e.g. namespace=custom, key=refill_asin, value=B00XYZ123, type=single_line_text_field) that the storefront theme can read.",
		"shopify_get_product":    "Get full details for a single Shopify product including variants and images.",
		"shopify_list_products":  "List products in the Shopify store with optional status and type filters.",
		"shopify_delete_product": "Permanently delete a product from the Shopify store.",
	}
	return descriptions[name]
}

// CreateProductInput defines input for creating a product.
type CreateProductInput struct {
	Title       string   `json:"title" description:"Product title" required:"true"`
	BodyHTML    string   `json:"body_html" description:"Product description in HTML" required:"true"`
	Vendor      string   `json:"vendor" description:"Product vendor/brand" required:"true"`
	ProductType string   `json:"product_type" description:"Product category" required:"true"`
	Tags        []string `json:"tags" description:"Product tags for organization"`
	Price       string   `json:"price" description:"Price in store currency" required:"true"`
	CompareAt   string   `json:"compare_at_price" description:"Original price for showing discount"`
	SKU         string   `json:"sku" description:"Stock keeping unit identifier"`
	ImageURLs   []string `json:"image_urls" description:"URLs of product images to upload"`
}

// ShopifyCreateProductTool creates a new Shopify product.
func (t *ShopifyProductTools) ShopifyCreateProductTool(ctx context.Context, input CreateProductInput) (*loop.ToolResult, error) {
	product, err := t.client.CreateProduct(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(map[string]any{
		"product_id": product["id"],
		"handle":     product["handle"],
		"status":     product["status"],
		"url":        product["onlineStoreUrl"],
	}), nil
}

// MetafieldInput defines a metafield to set on a product.
type MetafieldInput struct {
	Namespace string `json:"namespace" description:"Metafield namespace (e.g. custom, global)" required:"true"`
	Key       string `json:"key" description:"Metafield key (e.g. refill_asin)" required:"true"`
	Value     string `json:"value" description:"Metafield value (e.g. B00XYZ123)" required:"true"`
	Type      string `json:"type" description:"Metafield type (e.g. single_line_text_field, json, number_integer, boolean, url)" required:"true"`
}

// UpdateProductInput defines input for updating a product.
type UpdateProductInput struct {
	ProductID   string           `json:"product_id" description:"Shopify product ID to update" required:"true"`
	Title       string           `json:"title" description:"New product title"`
	BodyHTML    string           `json:"body_html" description:"New product description in HTML"`
	Vendor      string           `json:"vendor" description:"New vendor/brand"`
	ProductType string           `json:"product_type" description:"New product category"`
	Tags        []string         `json:"tags" description:"New product tags (replaces existing)"`
	Status      string           `json:"status" description:"Product status" enum:"active,draft,archived"`
	Metafields  []MetafieldInput `json:"metafields" description:"Metafields to set on the product (creates or updates by namespace+key)"`
}

// ShopifyUpdateProductTool updates an existing Shopify product.
func (t *ShopifyProductTools) ShopifyUpdateProductTool(ctx context.Context, input UpdateProductInput) (*loop.ToolResult, error) {
	product, err := t.client.UpdateProduct(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	result := map[string]any{
		"product_id": product["id"],
		"title":      product["title"],
		"status":     product["status"],
		"tags":       product["tags"],
		"updated_at": product["updatedAt"],
	}
	if mf, ok := product["metafields"]; ok {
		result["metafields"] = mf
	}
	return loop.NewSuccessResult(result), nil
}

// GetProductInput defines input for getting a product.
type GetProductInput struct {
	ProductID string `json:"product_id" description:"Shopify product ID" required:"true"`
}

// ShopifyGetProductTool retrieves a single Shopify product.
func (t *ShopifyProductTools) ShopifyGetProductTool(ctx context.Context, input GetProductInput) (*loop.ToolResult, error) {
	product, err := t.client.GetProduct(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(product), nil
}

// ListProductsInput defines input for listing products.
type ListProductsInput struct {
	Status      string `json:"status" description:"Filter by status" enum:"active,draft,archived"`
	ProductType string `json:"product_type" description:"Filter by product type"`
	Limit       int    `json:"limit" description:"Max products to return (default 20, max 250)"`
	Cursor      string `json:"cursor" description:"Pagination cursor from previous response"`
}

// ShopifyListProductsTool lists products in the store.
func (t *ShopifyProductTools) ShopifyListProductsTool(ctx context.Context, input ListProductsInput) (*loop.ToolResult, error) {
	data, err := t.client.ListProducts(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}

// DeleteProductInput defines input for deleting a product.
type DeleteProductInput struct {
	ProductID string `json:"product_id" description:"Shopify product ID to delete" required:"true"`
}

// ShopifyDeleteProductTool deletes a Shopify product.
func (t *ShopifyProductTools) ShopifyDeleteProductTool(ctx context.Context, input DeleteProductInput) (*loop.ToolResult, error) {
	result, err := t.client.DeleteProduct(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(map[string]any{
		"deleted":    true,
		"product_id": result["deletedProductId"],
	}), nil
}
