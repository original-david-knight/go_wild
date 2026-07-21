package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools/shopify"
)

type shopifyToolHandler func(ctx context.Context, client *shopify.ShopifyClient, inputJSON []byte) (any, error)

var shopifyToolHandlers = map[string]shopifyToolHandler{
	// Products
	"shopify_create_product": shopifyToolWithTools(
		shopify.NewShopifyProductTools,
		(*shopify.ShopifyProductTools).ShopifyCreateProductTool,
	),
	"shopify_update_product": shopifyToolWithTools(
		shopify.NewShopifyProductTools,
		(*shopify.ShopifyProductTools).ShopifyUpdateProductTool,
	),
	"shopify_get_product": shopifyToolWithTools(
		shopify.NewShopifyProductTools,
		(*shopify.ShopifyProductTools).ShopifyGetProductTool,
	),
	"shopify_list_products": shopifyToolWithTools(
		shopify.NewShopifyProductTools,
		(*shopify.ShopifyProductTools).ShopifyListProductsTool,
	),
	"shopify_delete_product": shopifyToolWithTools(
		shopify.NewShopifyProductTools,
		(*shopify.ShopifyProductTools).ShopifyDeleteProductTool,
	),

	// Variants
	"shopify_update_variant": shopifyToolWithTools(
		shopify.NewShopifyVariantTools,
		(*shopify.ShopifyVariantTools).ShopifyUpdateVariantTool,
	),
	"shopify_list_variants": shopifyToolWithTools(
		shopify.NewShopifyVariantTools,
		(*shopify.ShopifyVariantTools).ShopifyListVariantsTool,
	),

	// Orders
	"shopify_list_orders": shopifyToolWithTools(
		shopify.NewShopifyOrderTools,
		(*shopify.ShopifyOrderTools).ShopifyListOrdersTool,
	),
	"shopify_get_order": shopifyToolWithTools(
		shopify.NewShopifyOrderTools,
		(*shopify.ShopifyOrderTools).ShopifyGetOrderTool,
	),
	"shopify_update_order": shopifyToolWithTools(
		shopify.NewShopifyOrderTools,
		(*shopify.ShopifyOrderTools).ShopifyUpdateOrderTool,
	),
	"shopify_create_fulfillment": shopifyToolWithTools(
		shopify.NewShopifyOrderTools,
		(*shopify.ShopifyOrderTools).ShopifyCreateFulfillmentTool,
	),

	// Customers
	"shopify_get_customer": shopifyToolWithTools(
		shopify.NewShopifyCustomerTools,
		(*shopify.ShopifyCustomerTools).ShopifyGetCustomerTool,
	),
	"shopify_list_customers": shopifyToolWithTools(
		shopify.NewShopifyCustomerTools,
		(*shopify.ShopifyCustomerTools).ShopifyListCustomersTool,
	),
	"shopify_search_customers": shopifyToolWithTools(
		shopify.NewShopifyCustomerTools,
		(*shopify.ShopifyCustomerTools).ShopifySearchCustomersTool,
	),

	// Inventory
	"shopify_get_inventory_level": shopifyToolWithTools(
		shopify.NewShopifyInventoryTools,
		(*shopify.ShopifyInventoryTools).ShopifyGetInventoryLevelTool,
	),
	"shopify_set_inventory_level": shopifyToolWithTools(
		shopify.NewShopifyInventoryTools,
		(*shopify.ShopifyInventoryTools).ShopifySetInventoryLevelTool,
	),

	// Analytics
	"shopify_get_reports": shopifyToolWithTools(
		shopify.NewShopifyAnalyticsTools,
		(*shopify.ShopifyAnalyticsTools).ShopifyGetReportsTool,
	),
	"shopify_get_orders_summary": shopifyToolWithTools(
		shopify.NewShopifyAnalyticsTools,
		(*shopify.ShopifyAnalyticsTools).ShopifyGetOrdersSummaryTool,
	),

	// Images
	"shopify_upload_image": shopifyToolWithTools(
		shopify.NewShopifyImageTools,
		(*shopify.ShopifyImageTools).ShopifyUploadImageTool,
	),
	"shopify_list_images": shopifyToolWithTools(
		shopify.NewShopifyImageTools,
		(*shopify.ShopifyImageTools).ShopifyListImagesTool,
	),

	// Themes and assets
	"shopify_list_themes": shopifyToolWithTools(
		shopify.NewShopifyThemeTools,
		(*shopify.ShopifyThemeTools).ShopifyListThemesTool,
	),
	"shopify_get_theme": shopifyToolWithTools(
		shopify.NewShopifyThemeTools,
		(*shopify.ShopifyThemeTools).ShopifyGetThemeTool,
	),
	"shopify_list_assets": shopifyToolWithTools(
		shopify.NewShopifyThemeTools,
		(*shopify.ShopifyThemeTools).ShopifyListAssetsTool,
	),
	"shopify_get_asset": shopifyToolWithTools(
		shopify.NewShopifyThemeTools,
		(*shopify.ShopifyThemeTools).ShopifyGetAssetTool,
	),
	"shopify_update_asset": shopifyToolWithTools(
		shopify.NewShopifyThemeTools,
		(*shopify.ShopifyThemeTools).ShopifyUpdateAssetTool,
	),
	"shopify_delete_asset": shopifyToolWithTools(
		shopify.NewShopifyThemeTools,
		(*shopify.ShopifyThemeTools).ShopifyDeleteAssetTool,
	),

	// Pages
	"shopify_list_pages": shopifyToolWithTools(
		shopify.NewShopifyPageTools,
		(*shopify.ShopifyPageTools).ShopifyListPagesTool,
	),
	"shopify_get_page": shopifyToolWithTools(
		shopify.NewShopifyPageTools,
		(*shopify.ShopifyPageTools).ShopifyGetPageTool,
	),
	"shopify_create_page": shopifyToolWithTools(
		shopify.NewShopifyPageTools,
		(*shopify.ShopifyPageTools).ShopifyCreatePageTool,
	),
	"shopify_update_page": shopifyToolWithTools(
		shopify.NewShopifyPageTools,
		(*shopify.ShopifyPageTools).ShopifyUpdatePageTool,
	),
	"shopify_delete_page": shopifyToolWithTools(
		shopify.NewShopifyPageTools,
		(*shopify.ShopifyPageTools).ShopifyDeletePageTool,
	),
}

func shopifyToolHandlerFor[T any](invoke func(context.Context, *shopify.ShopifyClient, T) (*loop.ToolResult, error)) shopifyToolHandler {
	return func(ctx context.Context, client *shopify.ShopifyClient, inputJSON []byte) (any, error) {
		return callWithInput[T](inputJSON, func(input T) (any, error) {
			return toolResultContent(invoke(ctx, client, input))
		})
	}
}

func shopifyToolWithTools[TInput any, TTools any](
	newTools func(*shopify.ShopifyClient) TTools,
	invoke func(TTools, context.Context, TInput) (*loop.ToolResult, error),
) shopifyToolHandler {
	return shopifyToolHandlerFor(func(ctx context.Context, client *shopify.ShopifyClient, input TInput) (*loop.ToolResult, error) {
		tools := newTools(client)
		return invoke(tools, ctx, input)
	})
}

func (h *BrokerToolsHandler) callShopifyTools(ctx context.Context, agentID, toolName string, inputJSON []byte) (bool, any, error) {
	toolName = normalizeShopifyToolName(toolName)
	handler, ok := shopifyToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}

	client, companyID, resolveErr := h.companyShopifyClientForAgent(ctx, agentID)
	if resolveErr != nil {
		return true, nil, resolveErr
	}

	result, err := handler(ctx, client, inputJSON)
	if err != nil {
		return true, nil, err
	}
	return true, annotateShopifyResult(result, companyID), nil
}

func isShopifyTool(toolName string) bool {
	_, ok := shopifyToolHandlers[toolName]
	return ok
}

func normalizeShopifyToolName(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if strings.HasPrefix(toolName, "company_commerce_") {
		return strings.TrimPrefix(toolName, "company_commerce_")
	}
	return toolName
}

func (h *BrokerToolsHandler) companyShopifyClientForAgent(ctx context.Context, agentID string) (*shopify.ShopifyClient, string, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return nil, "", fmt.Errorf("shopify tools require company membership")
	}
	client, err := h.companyShopifyClient(ctx, member.CompanyID)
	if err != nil {
		return nil, "", err
	}
	return client, member.CompanyID, nil
}

// companyShopifyClient resolves a Shopify client from a company ID.
func (h *BrokerToolsHandler) companyShopifyClient(ctx context.Context, companyID string) (*shopify.ShopifyClient, error) {
	conn, err := data.GetCompanyShopifyConnection(ctx, h.db, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to load company shopify connection: %w", err)
	}
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("company shopify connection is missing or disabled")
	}
	shopURL := normalizeShopifyShopURL(conn.ShopURL)
	if shopURL == "" {
		return nil, fmt.Errorf("company shopify connection is incomplete")
	}
	token, err := resolveCompanyShopifyAccessToken(ctx, h.db, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve shopify access token: %w", err)
	}
	return shopify.NewShopifyClient(shopURL, normalizeShopifyAPIVersion(conn.APIVersion), token), nil
}

func annotateShopifyResult(result any, companyID string) any {
	if companyID == "" {
		return result
	}
	if payload, ok := result.(map[string]any); ok {
		payload["identity_scope"] = "company"
		payload["company_id"] = companyID
		return payload
	}
	return map[string]any{
		"result":         result,
		"identity_scope": "company",
		"company_id":     companyID,
	}
}
