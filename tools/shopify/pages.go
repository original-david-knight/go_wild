package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyPageTools provides tools for Shopify page management.
type ShopifyPageTools struct {
	client *ShopifyClient
}

// NewShopifyPageTools creates a new ShopifyPageTools instance.
func NewShopifyPageTools(client *ShopifyClient) *ShopifyPageTools {
	return &ShopifyPageTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyPageTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_list_pages":  "List static pages in the Shopify store.",
		"shopify_get_page":    "Get full details for a single page by ID.",
		"shopify_create_page": "Create a new static page with title, HTML body, and publish status.",
		"shopify_update_page": "Update an existing page's title, body, or publish status.",
		"shopify_delete_page": "Delete a static page by ID.",
	}
	return descriptions[name]
}

// ListPagesInput defines input for listing pages.
type ListPagesInput struct {
	Limit int `json:"limit" description:"Max pages to return (default 50, max 250)"`
}

// ShopifyListPagesTool lists pages in the store.
func (t *ShopifyPageTools) ShopifyListPagesTool(ctx context.Context, input ListPagesInput) (*loop.ToolResult, error) {
	result, err := t.client.ListPages(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// GetPageInput defines input for getting a single page.
type GetPageInput struct {
	PageID string `json:"page_id" description:"Shopify page ID" required:"true"`
}

// ShopifyGetPageTool retrieves a single page.
func (t *ShopifyPageTools) ShopifyGetPageTool(ctx context.Context, input GetPageInput) (*loop.ToolResult, error) {
	result, err := t.client.GetPage(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// CreatePageInput defines input for creating a page.
type CreatePageInput struct {
	Title     string `json:"title" description:"Page title" required:"true"`
	BodyHTML  string `json:"body_html" description:"Page content in HTML" required:"true"`
	Published bool   `json:"published" description:"Whether the page is published (default false)"`
}

// ShopifyCreatePageTool creates a new page.
func (t *ShopifyPageTools) ShopifyCreatePageTool(ctx context.Context, input CreatePageInput) (*loop.ToolResult, error) {
	result, err := t.client.CreatePage(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// UpdatePageInput defines input for updating a page.
type UpdatePageInput struct {
	PageID    string `json:"page_id" description:"Shopify page ID to update" required:"true"`
	Title     string `json:"title" description:"New page title"`
	BodyHTML  string `json:"body_html" description:"New page content in HTML"`
	Published *bool  `json:"published" description:"Whether the page is published"`
}

// ShopifyUpdatePageTool updates an existing page.
func (t *ShopifyPageTools) ShopifyUpdatePageTool(ctx context.Context, input UpdatePageInput) (*loop.ToolResult, error) {
	result, err := t.client.UpdatePage(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DeletePageInput defines input for deleting a page.
type DeletePageInput struct {
	PageID string `json:"page_id" description:"Shopify page ID to delete" required:"true"`
}

// ShopifyDeletePageTool deletes a page.
func (t *ShopifyPageTools) ShopifyDeletePageTool(ctx context.Context, input DeletePageInput) (*loop.ToolResult, error) {
	result, err := t.client.DeletePage(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}
