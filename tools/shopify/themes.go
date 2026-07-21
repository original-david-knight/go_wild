package shopify

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyThemeTools provides tools for Shopify theme and asset management.
type ShopifyThemeTools struct {
	client *ShopifyClient
}

// NewShopifyThemeTools creates a new ShopifyThemeTools instance.
func NewShopifyThemeTools(client *ShopifyClient) *ShopifyThemeTools {
	return &ShopifyThemeTools{client: client}
}

// DescribeTool returns the description for a tool by name.
func (t *ShopifyThemeTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_list_themes":  "List all themes in the Shopify store (published, unpublished, demo).",
		"shopify_get_theme":    "Get details for a single theme by ID.",
		"shopify_list_assets":  "List all asset files (Liquid templates, CSS, JSON, images) in a theme.",
		"shopify_get_asset":    "Get the content of a single theme asset file by key.",
		"shopify_update_asset": "Create or update a theme asset file (Liquid, CSS, JSON, etc.).",
		"shopify_delete_asset": "Delete a theme asset file by key.",
	}
	return descriptions[name]
}

// ListThemesInput defines input for listing themes.
type ListThemesInput struct{}

// ShopifyListThemesTool lists all themes in the store.
func (t *ShopifyThemeTools) ShopifyListThemesTool(ctx context.Context, input ListThemesInput) (*loop.ToolResult, error) {
	result, err := t.client.ListThemes(ctx)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// GetThemeInput defines input for getting a single theme.
type GetThemeInput struct {
	ThemeID string `json:"theme_id" description:"Shopify theme ID" required:"true"`
}

// ShopifyGetThemeTool retrieves a single theme.
func (t *ShopifyThemeTools) ShopifyGetThemeTool(ctx context.Context, input GetThemeInput) (*loop.ToolResult, error) {
	result, err := t.client.GetTheme(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// ListAssetsInput defines input for listing theme assets.
type ListAssetsInput struct {
	ThemeID string `json:"theme_id" description:"Shopify theme ID" required:"true"`
}

// ShopifyListAssetsTool lists all assets in a theme.
func (t *ShopifyThemeTools) ShopifyListAssetsTool(ctx context.Context, input ListAssetsInput) (*loop.ToolResult, error) {
	result, err := t.client.ListAssets(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// GetAssetInput defines input for getting a single asset.
type GetAssetInput struct {
	ThemeID  string `json:"theme_id" description:"Shopify theme ID" required:"true"`
	AssetKey string `json:"asset_key" description:"Asset key path (e.g. templates/index.liquid, assets/custom.css)" required:"true"`
}

// ShopifyGetAssetTool retrieves a single theme asset.
func (t *ShopifyThemeTools) ShopifyGetAssetTool(ctx context.Context, input GetAssetInput) (*loop.ToolResult, error) {
	result, err := t.client.GetAsset(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// UpdateAssetInput defines input for creating or updating an asset.
type UpdateAssetInput struct {
	ThemeID  string `json:"theme_id" description:"Shopify theme ID" required:"true"`
	AssetKey string `json:"asset_key" description:"Asset key path (e.g. templates/page.custom.liquid)" required:"true"`
	Value    string `json:"value" description:"Asset content (Liquid, CSS, JSON, etc.)" required:"true"`
}

// ShopifyUpdateAssetTool creates or updates a theme asset.
func (t *ShopifyThemeTools) ShopifyUpdateAssetTool(ctx context.Context, input UpdateAssetInput) (*loop.ToolResult, error) {
	result, err := t.client.UpdateAsset(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DeleteAssetInput defines input for deleting an asset.
type DeleteAssetInput struct {
	ThemeID  string `json:"theme_id" description:"Shopify theme ID" required:"true"`
	AssetKey string `json:"asset_key" description:"Asset key path to delete" required:"true"`
}

// ShopifyDeleteAssetTool deletes a theme asset.
func (t *ShopifyThemeTools) ShopifyDeleteAssetTool(ctx context.Context, input DeleteAssetInput) (*loop.ToolResult, error) {
	result, err := t.client.DeleteAsset(ctx, input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}
