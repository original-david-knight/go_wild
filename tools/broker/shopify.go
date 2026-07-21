package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools/shopify"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ShopifyTools proxies Shopify operations through the broker API.
type ShopifyTools struct {
	client *Client
}

// NewShopifyTools creates a new ShopifyTools instance.
func NewShopifyTools(client *Client) *ShopifyTools {
	return &ShopifyTools{client: client}
}

// --- Product tools ---

func (s *ShopifyTools) ShopifyCreateProductTool(ctx context.Context, input shopify.CreateProductInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_create_product", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyUpdateProductTool(ctx context.Context, input shopify.UpdateProductInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_update_product", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyGetProductTool(ctx context.Context, input shopify.GetProductInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_product", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyListProductsTool(ctx context.Context, input shopify.ListProductsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_products", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyDeleteProductTool(ctx context.Context, input shopify.DeleteProductInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_delete_product", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Variant tools ---

func (s *ShopifyTools) ShopifyUpdateVariantTool(ctx context.Context, input shopify.UpdateVariantInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_update_variant", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyListVariantsTool(ctx context.Context, input shopify.ListVariantsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_variants", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Order tools ---

func (s *ShopifyTools) ShopifyListOrdersTool(ctx context.Context, input shopify.ListOrdersInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_orders", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyGetOrderTool(ctx context.Context, input shopify.GetOrderInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_order", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyUpdateOrderTool(ctx context.Context, input shopify.UpdateOrderInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_update_order", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyCreateFulfillmentTool(ctx context.Context, input shopify.CreateFulfillmentInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_create_fulfillment", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Customer tools ---

func (s *ShopifyTools) ShopifyGetCustomerTool(ctx context.Context, input shopify.GetCustomerInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_customer", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyListCustomersTool(ctx context.Context, input shopify.ListCustomersInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_customers", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifySearchCustomersTool(ctx context.Context, input shopify.SearchCustomersInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_search_customers", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Inventory tools ---

func (s *ShopifyTools) ShopifyGetInventoryLevelTool(ctx context.Context, input shopify.GetInventoryLevelInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_inventory_level", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifySetInventoryLevelTool(ctx context.Context, input shopify.SetInventoryLevelInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_set_inventory_level", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Analytics tools ---

func (s *ShopifyTools) ShopifyGetReportsTool(ctx context.Context, input shopify.GetReportsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_reports", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyGetOrdersSummaryTool(ctx context.Context, input shopify.GetOrdersSummaryInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_orders_summary", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Image tools ---

func (s *ShopifyTools) ShopifyUploadImageTool(ctx context.Context, input shopify.UploadImageInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_upload_image", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyListImagesTool(ctx context.Context, input shopify.ListImagesInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_images", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Inventory Sync tools ---

type SyncInventoryInput struct {
}

func (s *ShopifyTools) ShopifySyncInventoryTool(ctx context.Context, input SyncInventoryInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_sync_inventory", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

type CreateListingInput struct {
	ShopifyProductID    string `json:"shopify_product_id" description:"Shopify product ID (numeric, without gid:// prefix)" required:"true"`
	ShopifyVariantID    string `json:"shopify_variant_id" description:"Specific Shopify variant ID (optional, uses first variant if omitted)"`
	SupplierName        string `json:"supplier_name" description:"Supplier name (e.g. cjdropshipping)" required:"true"`
	SupplierProductID   string `json:"supplier_product_id" description:"Supplier product ID/PID" required:"true"`
	SupplierVariantID   string `json:"supplier_variant_id" description:"Supplier variant ID/VID" required:"true"`
	SupplierCountryCode string `json:"supplier_country_code" description:"Warehouse country code (e.g. US, CN) for stock filtering"`
}

func (s *ShopifyTools) ShopifyCreateListingTool(ctx context.Context, input CreateListingInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_create_listing", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

type ListListingsInput struct {
}

func (s *ShopifyTools) ShopifyListListingsTool(ctx context.Context, input ListListingsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_listings", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

type DeleteListingInput struct {
	ListingID string `json:"listing_id" description:"Product listing ID to delete" required:"true"`
}

func (s *ShopifyTools) ShopifyDeleteListingTool(ctx context.Context, input DeleteListingInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_delete_listing", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Theme & Asset tools ---

func (s *ShopifyTools) ShopifyListThemesTool(ctx context.Context, input shopify.ListThemesInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_themes", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyGetThemeTool(ctx context.Context, input shopify.GetThemeInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_theme", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyListAssetsTool(ctx context.Context, input shopify.ListAssetsInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_assets", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyGetAssetTool(ctx context.Context, input shopify.GetAssetInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_asset", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyUpdateAssetTool(ctx context.Context, input shopify.UpdateAssetInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_update_asset", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyDeleteAssetTool(ctx context.Context, input shopify.DeleteAssetInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_delete_asset", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Page tools ---

func (s *ShopifyTools) ShopifyListPagesTool(ctx context.Context, input shopify.ListPagesInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_list_pages", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyGetPageTool(ctx context.Context, input shopify.GetPageInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_get_page", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyCreatePageTool(ctx context.Context, input shopify.CreatePageInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_create_page", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyUpdatePageTool(ctx context.Context, input shopify.UpdatePageInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_update_page", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *ShopifyTools) ShopifyDeletePageTool(ctx context.Context, input shopify.DeletePageInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "shopify_delete_page", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool returns the description for a tool by name.
func (s *ShopifyTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"shopify_create_product":      "Create a new product in the Shopify store with title, description, pricing, and images.",
		"shopify_update_product":      "Update an existing Shopify product's title, description, vendor, tags, status, or metafields. Metafields allow setting structured key-value data (e.g. namespace=custom, key=refill_asin, value=B00XYZ123, type=single_line_text_field) that the storefront theme can read.",
		"shopify_get_product":         "Get full details for a single Shopify product including variants and images.",
		"shopify_list_products":       "List products in the Shopify store with optional status and type filters.",
		"shopify_delete_product":      "Permanently delete a product from the Shopify store.",
		"shopify_update_variant":      "Update a product variant's price, compare-at price, or SKU.",
		"shopify_list_variants":       "List all variants for a given product including prices and inventory.",
		"shopify_list_orders":         "List orders with optional status and fulfillment filters.",
		"shopify_get_order":           "Get full details for a single order including line items, shipping, and fulfillments.",
		"shopify_update_order":        "Update order notes or tags.",
		"shopify_create_fulfillment":  "Create a fulfillment for an order with tracking information.",
		"shopify_get_customer":        "Get full details for a single customer including addresses and order history.",
		"shopify_list_customers":      "List customers with optional pagination.",
		"shopify_search_customers":    "Search customers by email, name, or other attributes.",
		"shopify_get_inventory_level": "Get current inventory level for an item across locations.",
		"shopify_set_inventory_level": "Set inventory quantity for an item at a specific location.",
		"shopify_get_reports":         "List available Shopify reports.",
		"shopify_get_orders_summary":  "Get an aggregate summary of orders within a date range.",
		"shopify_upload_image":        "Upload a product image from a URL to a Shopify product.",
		"shopify_list_images":         "List all images for a given product.",
		"shopify_list_themes":         "List all themes in the Shopify store (published, unpublished, demo).",
		"shopify_get_theme":           "Get details for a single theme by ID.",
		"shopify_list_assets":         "List all asset files (Liquid templates, CSS, JSON, images) in a theme.",
		"shopify_get_asset":           "Get the content of a single theme asset file by key.",
		"shopify_update_asset":        "Create or update a theme asset file (Liquid, CSS, JSON, etc.).",
		"shopify_delete_asset":        "Delete a theme asset file by key.",
		"shopify_list_pages":          "List static pages in the Shopify store.",
		"shopify_get_page":            "Get full details for a single page by ID.",
		"shopify_create_page":         "Create a new static page with title, HTML body, and publish status.",
		"shopify_update_page":         "Update an existing page's title, body, or publish status.",
		"shopify_delete_page":         "Delete a static page by ID.",
		"shopify_sync_inventory":      "Sync inventory levels from drop-shipping suppliers (CJ Dropshipping, etc.) to Shopify for all active product listings.",
		"shopify_create_listing":      "Create a product listing that links a Shopify variant to a supplier variant for inventory sync.",
		"shopify_list_listings":       "List all active product listings (Shopify-to-supplier mappings) for the company.",
		"shopify_delete_listing":      "Delete a product listing mapping by ID.",
	}
	return descriptions[name]
}
