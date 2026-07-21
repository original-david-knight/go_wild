package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/supplier/providers/cjdropshipping"
	"github.com/original-david-knight/go_wild/tools/shopify"
)

type inventorySyncToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, companyID string, inputJSON []byte) (any, error)

var inventorySyncToolHandlers = map[string]inventorySyncToolHandlerFunc{
	"shopify_sync_inventory": func(h *BrokerToolsHandler, ctx context.Context, companyID string, _ []byte) (any, error) {
		return h.handleSyncInventory(ctx, companyID)
	},
	"shopify_create_listing": func(h *BrokerToolsHandler, ctx context.Context, companyID string, inputJSON []byte) (any, error) {
		return h.handleCreateListing(ctx, companyID, inputJSON)
	},
	"shopify_list_listings": func(h *BrokerToolsHandler, ctx context.Context, companyID string, _ []byte) (any, error) {
		return h.handleListListings(ctx, companyID)
	},
	"shopify_delete_listing": func(h *BrokerToolsHandler, ctx context.Context, companyID string, inputJSON []byte) (any, error) {
		return h.handleDeleteListing(ctx, companyID, inputJSON)
	},
}

func isInventorySyncTool(toolName string) bool {
	_, ok := inventorySyncToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callInventorySyncTools(ctx context.Context, agentID, toolName string, inputJSON []byte) (bool, any, error) {
	if !isInventorySyncTool(toolName) {
		return false, nil, nil
	}

	member, err := data.GetCompanyMemberForAgent(ctx, h.db, agentID)
	if err != nil {
		return true, nil, fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return true, nil, fmt.Errorf("inventory sync tools require company membership")
	}
	companyID := strings.TrimSpace(member.CompanyID)

	handler, ok := inventorySyncToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, companyID, inputJSON)
	if err != nil {
		return true, nil, err
	}
	return true, annotateShopifyResult(result, companyID), nil
}

// handleSyncInventory pulls supplier stock for all active listings and updates Shopify.
func (h *BrokerToolsHandler) handleSyncInventory(ctx context.Context, companyID string) (any, error) {
	listings, err := data.ListProductListings(ctx, h.db, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to list product listings: %w", err)
	}
	if len(listings) == 0 {
		return map[string]any{
			"synced":  0,
			"changed": 0,
			"message": "no active product listings found",
		}, nil
	}

	shopifyClient, err := h.companyShopifyClient(ctx, companyID)
	if err != nil {
		return nil, err
	}

	// Group listings by supplier to reuse clients.
	cjListings := make([]data.ProductListing, 0)
	var skipped []string
	for _, l := range listings {
		if isCJInventorySupplier(l.SupplierName) {
			cjListings = append(cjListings, l)
			continue
		}
		skipped = append(skipped, fmt.Sprintf("%s: supplier %q not yet supported", l.ID, l.SupplierName))
	}

	var cjClient *cjdropshipping.Client
	if len(cjListings) > 0 {
		cjClient, err = h.companyCJClientForCompany(ctx, companyID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve CJ client: %w", err)
		}
	}

	type syncDetail struct {
		ListingID string `json:"listing_id"`
		VariantID string `json:"supplier_variant_id"`
		OldQty    int    `json:"old_quantity"`
		NewQty    int    `json:"new_quantity"`
		Updated   bool   `json:"updated"`
		Error     string `json:"error,omitempty"`
	}
	var details []syncDetail
	var errors []string
	changed := 0
	synced := 0

	for _, l := range cjListings {
		detail := syncDetail{
			ListingID: l.ID,
			VariantID: l.SupplierVariantID,
			OldQty:    l.LastSyncQuantity,
		}

		stockItems, err := cjClient.GetStockByVID(ctx, l.SupplierVariantID)
		if err != nil {
			detail.Error = err.Error()
			errors = append(errors, fmt.Sprintf("listing %s: %s", l.ID, err.Error()))
			details = append(details, detail)
			continue
		}

		newQty := sumCJStock(stockItems, l.SupplierCountryCode)
		detail.NewQty = newQty
		synced++

		if newQty != l.LastSyncQuantity {
			_, setErr := shopifyClient.SetInventoryLevel(ctx, shopify.SetInventoryLevelInput{
				InventoryItemID: l.ShopifyInventoryItemID,
				LocationID:      l.ShopifyLocationID,
				Quantity:        newQty,
			})
			if setErr != nil {
				detail.Error = setErr.Error()
				errors = append(errors, fmt.Sprintf("listing %s shopify update: %s", l.ID, setErr.Error()))
				details = append(details, detail)
				continue
			}
			detail.Updated = true
			changed++
		}

		// Update listing record.
		now := time.Now()
		l.LastSyncedAt = now
		l.LastSyncQuantity = newQty
		l.UpdatedAt = now
		if updateErr := data.UpsertProductListing(ctx, h.db, &l); updateErr != nil {
			log.Printf("inventory sync: failed to update listing %s: %v", l.ID, updateErr)
		}

		details = append(details, detail)
	}

	result := map[string]any{
		"synced":  synced,
		"changed": changed,
		"total":   len(listings),
		"details": details,
	}
	if len(errors) > 0 {
		result["errors"] = errors
	}
	if len(skipped) > 0 {
		result["skipped"] = skipped
	}
	return result, nil
}

// sumCJStock sums available stock from CJ stock items, optionally filtered by country code.
func sumCJStock(items []cjdropshipping.StockByVIDItem, countryCode string) int {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	total := 0
	for _, item := range items {
		if countryCode != "" && strings.ToUpper(strings.TrimSpace(item.CountryCode)) != countryCode {
			continue
		}
		total += item.CjInventoryNum.Int()
	}
	return total
}

type createListingInput struct {
	ShopifyProductID    string `json:"shopify_product_id"`
	ShopifyVariantID    string `json:"shopify_variant_id,omitempty"`
	SupplierName        string `json:"supplier_name"`
	SupplierProductID   string `json:"supplier_product_id"`
	SupplierVariantID   string `json:"supplier_variant_id"`
	SupplierCountryCode string `json:"supplier_country_code,omitempty"`
}

func isCJInventorySupplier(name string) bool {
	switch strings.ToLower(name) {
	case "cjdropshipping", "cj":
		return true
	default:
		return false
	}
}

// handleCreateListing creates a product listing mapping.
func (h *BrokerToolsHandler) handleCreateListing(ctx context.Context, companyID string, inputJSON []byte) (any, error) {
	var input createListingInput
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input: %w", err)
	}
	if strings.TrimSpace(input.ShopifyProductID) == "" {
		return nil, fmt.Errorf("shopify_product_id is required")
	}
	if strings.TrimSpace(input.SupplierName) == "" {
		return nil, fmt.Errorf("supplier_name is required")
	}
	if strings.TrimSpace(input.SupplierVariantID) == "" {
		return nil, fmt.Errorf("supplier_variant_id is required")
	}

	shopifyClient, err := h.companyShopifyClient(ctx, companyID)
	if err != nil {
		return nil, err
	}

	// Resolve variant's InventoryItemID from Shopify.
	product, err := shopifyClient.GetProduct(ctx, shopify.GetProductInput{ProductID: input.ShopifyProductID})
	if err != nil {
		return nil, fmt.Errorf("failed to get shopify product: %w", err)
	}

	variantID, inventoryItemID := resolveVariantAndInventoryItem(product, input.ShopifyVariantID)
	if variantID == "" || inventoryItemID == "" {
		return nil, fmt.Errorf("could not resolve variant/inventory item from shopify product")
	}

	// Get the primary location.
	locData, err := shopifyClient.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list shopify locations: %w", err)
	}
	locationID := extractPrimaryLocationID(locData)
	if locationID == "" {
		return nil, fmt.Errorf("no active shopify location found")
	}

	listing := &data.ProductListing{
		CompanyID:              companyID,
		ShopifyProductID:       input.ShopifyProductID,
		ShopifyVariantID:       variantID,
		ShopifyInventoryItemID: inventoryItemID,
		ShopifyLocationID:      locationID,
		SupplierName:           input.SupplierName,
		SupplierProductID:      input.SupplierProductID,
		SupplierVariantID:      input.SupplierVariantID,
		SupplierCountryCode:    strings.ToUpper(strings.TrimSpace(input.SupplierCountryCode)),
		Status:                 "active",
	}

	// Initial inventory sync: pull supplier stock and set Shopify quantity.
	// If the supplier lookup fails, set to zero so the product isn't oversold;
	// a later shopify_sync_inventory call will correct it.
	var syncQty int
	var syncErr string
	if isCJInventorySupplier(strings.TrimSpace(input.SupplierName)) {
		if cjClient, err := h.companyCJClientForCompany(ctx, companyID); err != nil {
			syncErr = fmt.Sprintf("failed to resolve CJ client: %v", err)
		} else if stockItems, err := cjClient.GetStockByVID(ctx, input.SupplierVariantID); err != nil {
			syncErr = fmt.Sprintf("failed to query CJ stock: %v", err)
		} else {
			syncQty = sumCJStock(stockItems, listing.SupplierCountryCode)
		}
	} else {
		syncErr = fmt.Sprintf("supplier %q stock query not yet supported", input.SupplierName)
	}

	if _, setErr := shopifyClient.SetInventoryLevel(ctx, shopify.SetInventoryLevelInput{
		InventoryItemID: inventoryItemID,
		LocationID:      locationID,
		Quantity:        syncQty,
	}); setErr != nil {
		syncErr = fmt.Sprintf("failed to set shopify inventory: %v", setErr)
	} else {
		listing.LastSyncedAt = time.Now()
		listing.LastSyncQuantity = syncQty
	}

	if err := data.UpsertProductListing(ctx, h.db, listing); err != nil {
		return nil, fmt.Errorf("failed to create listing: %w", err)
	}

	result := map[string]any{
		"listing_id":                listing.ID,
		"shopify_variant_id":        listing.ShopifyVariantID,
		"shopify_inventory_item_id": listing.ShopifyInventoryItemID,
		"shopify_location_id":       listing.ShopifyLocationID,
		"status":                    listing.Status,
		"synced_quantity":           syncQty,
	}
	if syncErr != "" {
		result["sync_error"] = syncErr
	}
	return result, nil
}

// handleListListings returns all active listings for the company.
func (h *BrokerToolsHandler) handleListListings(ctx context.Context, companyID string) (any, error) {
	listings, err := data.ListProductListings(ctx, h.db, companyID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"listings": listings,
		"count":    len(listings),
	}, nil
}

type deleteListingInput struct {
	ListingID string `json:"listing_id"`
}

// handleDeleteListing removes a product listing.
func (h *BrokerToolsHandler) handleDeleteListing(ctx context.Context, companyID string, inputJSON []byte) (any, error) {
	var input deleteListingInput
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input: %w", err)
	}
	listingID := strings.TrimSpace(input.ListingID)
	if listingID == "" {
		return nil, fmt.Errorf("listing_id is required")
	}

	listing, err := data.GetProductListing(ctx, h.db, listingID)
	if err != nil {
		return nil, fmt.Errorf("listing not found: %w", err)
	}
	if listing.CompanyID != companyID {
		return nil, fmt.Errorf("listing does not belong to this company")
	}

	if err := data.DeleteProductListing(ctx, h.db, listingID); err != nil {
		return nil, fmt.Errorf("failed to delete listing: %w", err)
	}
	return map[string]any{"deleted": true, "listing_id": listingID}, nil
}

// companyCJClientForCompany resolves a raw CJ Dropshipping client for a company.
func (h *BrokerToolsHandler) companyCJClientForCompany(ctx context.Context, companyID string) (*cjdropshipping.Client, error) {
	cjConn, err := data.GetCompanyCJDropshippingConnection(ctx, h.db, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to load company cjdropshipping connection: %w", err)
	}
	if cjConn == nil || !cjConn.Enabled {
		return nil, fmt.Errorf("company cjdropshipping connection is missing or disabled")
	}
	accessToken, err := resolveCompanyCJDropshippingAccessToken(ctx, h.db, cjConn)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve cjdropshipping access token: %w", err)
	}
	return newCJDropshippingAPIClient(accessToken, cjConn.PlatformTokenEnc), nil
}

// stripGIDPrefix extracts the numeric ID from a Shopify GID like "gid://shopify/InventoryItem/12345" → "12345".
// Returns the input unchanged if it doesn't contain a GID prefix.
func stripGIDPrefix(gid string) string {
	if i := strings.LastIndex(gid, "/"); i >= 0 && strings.HasPrefix(gid, "gid://") {
		return gid[i+1:]
	}
	return gid
}

// resolveVariantAndInventoryItem extracts the variant ID and inventory item ID from a product response.
// Returns bare numeric IDs (GID prefixes stripped) suitable for passing to SetInventoryLevel.
// If shopifyVariantID is provided, it matches that specific variant; otherwise uses the first variant.
func resolveVariantAndInventoryItem(product map[string]any, shopifyVariantID string) (string, string) {
	variants, _ := product["variants"].(map[string]any)
	edges, _ := variants["edges"].([]any)
	if len(edges) == 0 {
		return "", ""
	}

	shopifyVariantID = strings.TrimSpace(shopifyVariantID)

	for _, e := range edges {
		edge, _ := e.(map[string]any)
		node, _ := edge["node"].(map[string]any)
		if node == nil {
			continue
		}
		vid, _ := node["id"].(string)
		if shopifyVariantID != "" && !strings.HasSuffix(vid, "/"+shopifyVariantID) && vid != shopifyVariantID && stripGIDPrefix(vid) != shopifyVariantID {
			continue
		}
		invItem, _ := node["inventoryItem"].(map[string]any)
		invItemID, _ := invItem["id"].(string)
		return stripGIDPrefix(vid), stripGIDPrefix(invItemID)
	}

	// Fallback to first variant if no specific match requested.
	if shopifyVariantID == "" {
		edge, _ := edges[0].(map[string]any)
		node, _ := edge["node"].(map[string]any)
		vid, _ := node["id"].(string)
		invItem, _ := node["inventoryItem"].(map[string]any)
		invItemID, _ := invItem["id"].(string)
		return stripGIDPrefix(vid), stripGIDPrefix(invItemID)
	}
	return "", ""
}

// extractPrimaryLocationID returns the first active location's numeric ID (GID prefix stripped).
func extractPrimaryLocationID(locData map[string]any) string {
	locations, _ := locData["locations"].(map[string]any)
	edges, _ := locations["edges"].([]any)
	for _, e := range edges {
		edge, _ := e.(map[string]any)
		node, _ := edge["node"].(map[string]any)
		if node == nil {
			continue
		}
		active, _ := node["isActive"].(bool)
		if active {
			id, _ := node["id"].(string)
			return stripGIDPrefix(id)
		}
	}
	return ""
}
