package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
	"github.com/original-david-knight/go_wild/tools/shopify"
)

func TestCompanyFinanceGetWalletAddressesUsesDedicatedToolName(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"eth_address": "0xabc",
			"sol_address": "So11111111111111111111111111111111111111112",
		})
	}))

	companyFinance := NewCompanyFinanceTools(c)
	result, err := companyFinance.CompanyFinanceGetWalletAddressesTool(context.Background(), tools.CompanyFinanceGetWalletAddressesInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success result")
	}
	if gotPath != "/broker/v1/tools/company_finance_get_wallet_addresses" {
		t.Fatalf("expected dedicated company finance tool path, got %s", gotPath)
	}
}

func TestShopifyListUsesCorrectToolName(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"products": []any{},
		})
	}))

	shopifyTools := NewShopifyTools(c)
	result, err := shopifyTools.ShopifyListProductsTool(context.Background(), shopify.ListProductsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success result")
	}
	if gotPath != "/broker/v1/tools/shopify_list_products" {
		t.Fatalf("expected shopify tool path, got %s", gotPath)
	}
}
