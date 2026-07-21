package main

import (
	"context"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/supplier/providers/cjdropshipping"
)

func TestStripGIDPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gid://shopify/InventoryItem/12345", "12345"},
		{"gid://shopify/ProductVariant/99999", "99999"},
		{"gid://shopify/Product/1", "1"},
		{"12345", "12345"},
		{"", ""},
		{"plain-id", "plain-id"},
		{"gid://shopify/InventoryItem/", ""},
	}
	for _, tt := range tests {
		got := stripGIDPrefix(tt.input)
		if got != tt.want {
			t.Errorf("stripGIDPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSumCJStock(t *testing.T) {
	items := []cjdropshipping.StockByVIDItem{
		{CountryCode: "US", CjInventoryNum: 10},
		{CountryCode: "CN", CjInventoryNum: 25},
		{CountryCode: "US", CjInventoryNum: 5},
		{CountryCode: "DE", CjInventoryNum: 8},
	}

	tests := []struct {
		name        string
		countryCode string
		want        int
	}{
		{"all countries", "", 48},
		{"filter US", "US", 15},
		{"filter CN", "CN", 25},
		{"filter DE", "DE", 8},
		{"filter case insensitive", "us", 15},
		{"filter with whitespace", " US ", 15},
		{"filter no match", "JP", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sumCJStock(items, tt.countryCode)
			if got != tt.want {
				t.Errorf("sumCJStock(items, %q) = %d, want %d", tt.countryCode, got, tt.want)
			}
		})
	}

	t.Run("empty items", func(t *testing.T) {
		got := sumCJStock(nil, "")
		if got != 0 {
			t.Errorf("sumCJStock(nil, \"\") = %d, want 0", got)
		}
	})
}

func TestResolveVariantAndInventoryItem(t *testing.T) {
	// Helper to build a product response matching the Shopify GraphQL shape.
	makeProduct := func(variants ...map[string]any) map[string]any {
		edges := make([]any, len(variants))
		for i, v := range variants {
			edges[i] = map[string]any{"node": v}
		}
		return map[string]any{
			"variants": map[string]any{"edges": edges},
		}
	}

	variant1 := map[string]any{
		"id":            "gid://shopify/ProductVariant/111",
		"inventoryItem": map[string]any{"id": "gid://shopify/InventoryItem/AAA"},
	}
	variant2 := map[string]any{
		"id":            "gid://shopify/ProductVariant/222",
		"inventoryItem": map[string]any{"id": "gid://shopify/InventoryItem/BBB"},
	}

	t.Run("no variant ID returns first", func(t *testing.T) {
		vid, iid := resolveVariantAndInventoryItem(makeProduct(variant1, variant2), "")
		if vid != "111" || iid != "AAA" {
			t.Errorf("got vid=%q iid=%q, want 111/AAA", vid, iid)
		}
	})

	t.Run("match by bare numeric ID", func(t *testing.T) {
		vid, iid := resolveVariantAndInventoryItem(makeProduct(variant1, variant2), "222")
		if vid != "222" || iid != "BBB" {
			t.Errorf("got vid=%q iid=%q, want 222/BBB", vid, iid)
		}
	})

	t.Run("match by full GID", func(t *testing.T) {
		vid, iid := resolveVariantAndInventoryItem(makeProduct(variant1, variant2), "gid://shopify/ProductVariant/222")
		if vid != "222" || iid != "BBB" {
			t.Errorf("got vid=%q iid=%q, want 222/BBB", vid, iid)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		vid, iid := resolveVariantAndInventoryItem(makeProduct(variant1, variant2), "999")
		if vid != "" || iid != "" {
			t.Errorf("got vid=%q iid=%q, want empty", vid, iid)
		}
	})

	t.Run("empty variants returns empty", func(t *testing.T) {
		vid, iid := resolveVariantAndInventoryItem(map[string]any{}, "")
		if vid != "" || iid != "" {
			t.Errorf("got vid=%q iid=%q, want empty", vid, iid)
		}
	})
}

func TestExtractPrimaryLocationID(t *testing.T) {
	t.Run("returns first active location", func(t *testing.T) {
		locData := map[string]any{
			"locations": map[string]any{
				"edges": []any{
					map[string]any{"node": map[string]any{"id": "gid://shopify/Location/100", "isActive": false}},
					map[string]any{"node": map[string]any{"id": "gid://shopify/Location/200", "isActive": true}},
					map[string]any{"node": map[string]any{"id": "gid://shopify/Location/300", "isActive": true}},
				},
			},
		}
		got := extractPrimaryLocationID(locData)
		if got != "200" {
			t.Errorf("extractPrimaryLocationID() = %q, want %q", got, "200")
		}
	})

	t.Run("no active locations", func(t *testing.T) {
		locData := map[string]any{
			"locations": map[string]any{
				"edges": []any{
					map[string]any{"node": map[string]any{"id": "gid://shopify/Location/100", "isActive": false}},
				},
			},
		}
		got := extractPrimaryLocationID(locData)
		if got != "" {
			t.Errorf("extractPrimaryLocationID() = %q, want empty", got)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		got := extractPrimaryLocationID(map[string]any{})
		if got != "" {
			t.Errorf("extractPrimaryLocationID() = %q, want empty", got)
		}
	})
}

func TestCallInventorySyncToolsDispatchAndMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	ctx := context.Background()
	agentID := "inventory-sync-no-company"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	handled, result, err := h.callInventorySyncTools(ctx, agentID, "not_a_shopify_inventory_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error for unknown tool: %v", err)
	}
	if handled {
		t.Fatalf("expected unknown tool to be unhandled, got result=%#v", result)
	}

	handled, _, err = h.callInventorySyncTools(ctx, agentID, "shopify_sync_inventory", nil)
	if !handled {
		t.Fatalf("expected shopify_sync_inventory to be handled")
	}
	if err == nil {
		t.Fatalf("expected company membership error")
	}
	if !strings.Contains(err.Error(), "company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsInventorySyncToolRecognition(t *testing.T) {
	if !isInventorySyncTool("shopify_sync_inventory") {
		t.Fatalf("expected shopify_sync_inventory to be recognized")
	}
	if !isInventorySyncTool("shopify_create_listing") {
		t.Fatalf("expected shopify_create_listing to be recognized")
	}
	if !isInventorySyncTool("shopify_list_listings") {
		t.Fatalf("expected shopify_list_listings to be recognized")
	}
	if !isInventorySyncTool("shopify_delete_listing") {
		t.Fatalf("expected shopify_delete_listing to be recognized")
	}
	if isInventorySyncTool("shopify_not_real") {
		t.Fatalf("unexpectedly recognized unknown inventory sync tool")
	}
}

func TestIsCJInventorySupplier(t *testing.T) {
	if !isCJInventorySupplier("cjdropshipping") {
		t.Fatalf("expected cjdropshipping to be recognized")
	}
	if !isCJInventorySupplier("CJ") {
		t.Fatalf("expected CJ to be recognized case-insensitively")
	}
	if isCJInventorySupplier(" cj ") {
		t.Fatalf("expected whitespace-wrapped alias to be rejected without trimming")
	}
	if isCJInventorySupplier("topdawg") {
		t.Fatalf("expected non-CJ supplier to be rejected")
	}
}
