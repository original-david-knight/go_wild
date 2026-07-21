package ecommerce

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestEcommerceCalculateMarginTool(t *testing.T) {
	tools := NewPricingTools()

	result, err := tools.EcommerceCalculateMarginTool(context.Background(), CalculateMarginInput{
		RetailPrice:         100,
		SupplierCost:        40,
		EstimatedAdSpendPct: 20,
		ShopifyFeePct:       2.9,
	})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error=%q", result.Error)
	}

	payload, ok := result.Content.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", result.Content)
	}
	assertFloatClose(t, payload["gross_margin"].(float64), 60.0, 0.0001)
	assertFloatClose(t, payload["shopify_fee"].(float64), 3.2, 0.0001)
	assertFloatClose(t, payload["ad_spend"].(float64), 20.0, 0.0001)
	assertFloatClose(t, payload["net_margin"].(float64), 36.8, 0.0001)
	assertFloatClose(t, payload["net_margin_pct"].(float64), 36.8, 0.0001)
	assertFloatClose(t, payload["break_even_roas"].(float64), 100.0/56.8, 0.0001)
}

func TestEcommerceCalculateMarginToolValidation(t *testing.T) {
	tools := NewPricingTools()
	result, err := tools.EcommerceCalculateMarginTool(context.Background(), CalculateMarginInput{
		RetailPrice: -1,
	})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if result.Success {
		t.Fatalf("expected validation error result")
	}
	if !strings.Contains(result.Error, "retail_price must be positive") {
		t.Fatalf("unexpected error: %q", result.Error)
	}
}

func TestEcommerceSuggestPriceTool(t *testing.T) {
	tools := NewPricingTools()
	result, err := tools.EcommerceSuggestPriceTool(context.Background(), SuggestPriceInput{
		SupplierCost: 10,
		TargetMargin: 30,
		Category:     "electronics",
	})
	if err != nil {
		t.Fatalf("unexpected tool error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error=%q", result.Error)
	}
	payload, ok := result.Content.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", result.Content)
	}
	assertFloatClose(t, payload["suggested_price"].(float64), 21.99, 0.0001)
	if !strings.Contains(payload["notes"].(string), "Electronics typically see") {
		t.Fatalf("expected electronics note, got %q", payload["notes"])
	}
}

func TestPnLToolsDescribeTool(t *testing.T) {
	pnl := NewPnLTools()

	for _, name := range []string{"ecommerce_product_pnl", "ecommerce_daily_pnl"} {
		if pnl.DescribeTool(name) == "" {
			t.Errorf("expected non-empty description for %q", name)
		}
	}

	if pnl.DescribeTool("unknown_tool") != "" {
		t.Error("expected empty description for unknown tool")
	}
}

func assertFloatClose(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("float mismatch: got %.6f want %.6f", got, want)
	}
}
