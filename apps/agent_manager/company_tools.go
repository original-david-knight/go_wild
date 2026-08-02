package main

import (
	"context"
	"log"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
	agentnode "github.com/original-david-knight/go_wild/agent_node"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/tools/amazon"
	"github.com/original-david-knight/go_wild/tools/shopify"
	"github.com/original-david-knight/go_wild/tools/supplier"
	"github.com/original-david-knight/go_wild/tools/supplier/providers"
)

// buildCompanyToolLoader returns a CompanyToolLoader that resolves company-scoped
// tools (Shopify, Amazon, Supplier) with proper credentials from the database.
func buildCompanyToolLoader(db gowild_data.Database) func(ctx context.Context, companyID string) agentnode.ToolRegistry {
	return func(ctx context.Context, companyID string) agentnode.ToolRegistry {
		registry := agentnode.ToolRegistry{}
		if companyID == "" {
			return registry
		}

		loadShopifyTools(ctx, db, companyID, registry)
		loadAmazonTools(ctx, db, companyID, registry)
		loadSupplierTools(ctx, db, companyID, registry)

		if len(registry) > 0 {
			log.Printf("[company-tools] loaded %d tools for company %s", len(registry), companyID)
		}
		return registry
	}
}

// loadShopifyTools adds all Shopify tool categories if the company has an active connection.
func loadShopifyTools(ctx context.Context, db gowild_data.Database, companyID string, registry agentnode.ToolRegistry) {
	conn, err := data.GetCompanyShopifyConnection(ctx, db, companyID)
	if err != nil || conn == nil || !conn.Enabled {
		return
	}
	shopURL := normalizeShopifyShopURL(conn.ShopURL)
	if shopURL == "" {
		return
	}
	token, err := resolveCompanyShopifyAccessToken(ctx, db, conn)
	if err != nil {
		log.Printf("[company-tools] shopify token error for %s: %v", companyID, err)
		return
	}
	client := shopify.NewShopifyClient(shopURL, normalizeShopifyAPIVersion(conn.APIVersion), token)

	// Wrap all 9 Shopify tool categories
	toolSets := []any{
		shopify.NewShopifyProductTools(client),
		shopify.NewShopifyOrderTools(client),
		shopify.NewShopifyCustomerTools(client),
		shopify.NewShopifyInventoryTools(client),
		shopify.NewShopifyAnalyticsTools(client),
		shopify.NewShopifyImageTools(client),
		shopify.NewShopifyThemeTools(client),
		shopify.NewShopifyPageTools(client),
		shopify.NewShopifyVariantTools(client),
	}
	for _, ts := range toolSets {
		for _, t := range loop.WrapToolsWithDescriptions(ts) {
			registry[t.Name()] = t
		}
	}
}

// loadAmazonTools adds Amazon product tools if the company has an active connection.
func loadAmazonTools(ctx context.Context, db gowild_data.Database, companyID string, registry agentnode.ToolRegistry) {
	conn, err := data.GetCompanyAmazonConnection(ctx, db, companyID)
	if err != nil || conn == nil || !conn.Enabled {
		return
	}
	accessKey := strings.TrimSpace(conn.AccessKeyEnc)
	secretKey := strings.TrimSpace(conn.SecretKeyEnc)
	partnerTag := strings.TrimSpace(conn.PartnerTag)
	if accessKey == "" || secretKey == "" || partnerTag == "" {
		return
	}
	client := amazon.NewPAAClient(accessKey, secretKey, partnerTag, conn.Marketplace)
	tools := amazon.NewAmazonTools(client)
	for _, t := range loop.WrapToolsWithDescriptions(tools) {
		registry[t.Name()] = t
	}
}

// loadSupplierTools adds supplier tools (TopDawg or CJ Dropshipping) if the company has a connection.
func loadSupplierTools(ctx context.Context, db gowild_data.Database, companyID string, registry agentnode.ToolRegistry) {
	var supplierClient supplier.Supplier

	// Try CJ Dropshipping first
	cjConn, err := data.GetCompanyCJDropshippingConnection(ctx, db, companyID)
	if err == nil && cjConn != nil && cjConn.Enabled {
		accessToken, err := resolveCompanyCJDropshippingAccessToken(ctx, db, cjConn)
		if err != nil {
			log.Printf("[company-tools] cjdropshipping token error for %s: %v", companyID, err)
		} else {
			supplierClient = providers.NewCJDropshipping(accessToken, cjConn.PlatformTokenEnc, cjConn.DefaultFromCountryCode)
		}
	}

	// Fall back to TopDawg
	if supplierClient == nil {
		tdConn, err := data.GetCompanyTopDawgConnection(ctx, db, companyID)
		if err == nil && tdConn != nil && tdConn.Enabled {
			apiKey := strings.TrimSpace(tdConn.APIKeyEnc)
			supplierID := strings.TrimSpace(tdConn.SupplierID)
			if apiKey != "" && supplierID != "" {
				supplierClient = providers.NewTopDawg(apiKey, supplierID)
			}
		}
	}

	if supplierClient == nil {
		return
	}

	// Wrap all 3 supplier tool categories
	toolSets := []any{
		supplier.NewSupplierProductTools(supplierClient),
		supplier.NewSupplierOrderTools(supplierClient),
		supplier.NewSupplierTrackingTools(supplierClient),
	}
	for _, ts := range toolSets {
		for _, t := range loop.WrapToolsWithDescriptions(ts) {
			registry[t.Name()] = t
		}
	}
}
