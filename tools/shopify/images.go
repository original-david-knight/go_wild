package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyImageTools provides tools for Shopify product image management.
type ShopifyImageTools struct {
	client *ShopifyClient
}

// NewShopifyImageTools creates a new ShopifyImageTools instance.
func NewShopifyImageTools(client *ShopifyClient) *ShopifyImageTools {
	return &ShopifyImageTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyImageTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_upload_image": "Upload a product image from a URL to a Shopify product.",
		"shopify_list_images":  "List all images for a given product.",
	}
	return descriptions[name]
}

// UploadImageInput defines input for uploading a product image.
type UploadImageInput struct {
	ProductID string `json:"product_id" description:"Shopify product ID" required:"true"`
	ImageURL  string `json:"image_url" description:"URL of the image to upload" required:"true"`
	AltText   string `json:"alt_text" description:"Alt text for accessibility"`
	Position  int    `json:"position" description:"Image position (1-based, lower is first)"`
}

// ShopifyUploadImageTool uploads an image to a product.
func (t *ShopifyImageTools) ShopifyUploadImageTool(ctx context.Context, input UploadImageInput) (*loop.ToolResult, error) {
	result, err := t.client.UploadImage(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// ListImagesInput defines input for listing product images.
type ListImagesInput struct {
	ProductID string `json:"product_id" description:"Shopify product ID" required:"true"`
	Limit     int    `json:"limit" description:"Max images to return (default 20)"`
}

// ShopifyListImagesTool lists images for a product.
func (t *ShopifyImageTools) ShopifyListImagesTool(ctx context.Context, input ListImagesInput) (*loop.ToolResult, error) {
	data, err := t.client.ListImages(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(data), nil
}
