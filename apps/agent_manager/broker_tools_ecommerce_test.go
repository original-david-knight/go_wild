package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/ecommerce"
)

func TestCallEcommerceToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "ecommerce-unknown-tool"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	handled, result, err := h.callEcommerceTools(ctx, svc, "not_a_real_ecommerce_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestCallEcommerceToolsCalculateMarginHandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "ecommerce-margin-tool"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	input, err := json.Marshal(map[string]any{
		"retail_price":           100.0,
		"supplier_cost":          40.0,
		"estimated_ad_spend_pct": 20.0,
		"shopify_fee_pct":        2.9,
	})
	if err != nil {
		t.Fatalf("Marshal input failed: %v", err)
	}

	handled, result, err := h.callEcommerceTools(ctx, svc, "ecommerce_calculate_margin", input)
	if err != nil {
		t.Fatalf("callEcommerceTools failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected ecommerce_calculate_margin to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if _, ok := resultMap["net_margin"]; !ok {
		t.Fatalf("expected net_margin key in result, got %#v", resultMap)
	}
}

func TestComputeProductPnLFromSpendHistory(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "ecommerce-product-pnl"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	if err := svc.RecordSpend(ctx, "orders", 100, "order_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(orders) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "cogs", 40, "cogs_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(cogs) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "ads", 20, "ads_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(ads) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "returns", 10, "returns_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(returns) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "orders", 55, "order_tool", "product-2"); err != nil {
		t.Fatalf("RecordSpend(unrelated order) failed: %v", err)
	}

	from := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	to := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	result, err := h.computeProductPnL(ctx, svc, ecommerce.ProductPnLInput{
		ShopifyProductID: "product-1",
		DateFrom:         from,
		DateTo:           to,
	})
	if err != nil {
		t.Fatalf("computeProductPnL failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	assertFloatEqual(t, resultMap["revenue"], 90.0)
	assertFloatEqual(t, resultMap["cogs"], 40.0)
	assertFloatEqual(t, resultMap["ad_spend"], 20.0)
	assertFloatEqual(t, resultMap["shopify_fees"], 2.91)
	assertFloatEqual(t, resultMap["net_profit"], 27.09)
	assertFloatEqual(t, resultMap["roas"], 4.5)
	assertFloatEqual(t, resultMap["return_rate_pct"], 100.0)
	if got := toInt(resultMap["units_sold"]); got != 1 {
		t.Fatalf("expected units_sold=1, got %d", got)
	}
}

func TestComputeDailyPnLAggregatesByDay(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "ecommerce-daily-pnl"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	if err := svc.RecordSpend(ctx, "orders", 100, "order_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(orders) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "cogs", 40, "cogs_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(cogs) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "ads", 20, "ads_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(ads) failed: %v", err)
	}
	if err := svc.RecordSpend(ctx, "returns", 10, "returns_tool", "product-1"); err != nil {
		t.Fatalf("RecordSpend(returns) failed: %v", err)
	}

	from := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	to := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	result, err := h.computeDailyPnL(ctx, svc, ecommerce.DailyPnLInput{
		DateFrom: from,
		DateTo:   to,
	})
	if err != nil {
		t.Fatalf("computeDailyPnL failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	days, ok := resultMap["days"].([]map[string]any)
	if !ok {
		rawDays, ok := resultMap["days"].([]any)
		if !ok {
			t.Fatalf("unexpected days type: %T", resultMap["days"])
		}
		days = make([]map[string]any, 0, len(rawDays))
		for _, raw := range rawDays {
			entry, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("unexpected day entry type: %T", raw)
			}
			days = append(days, entry)
		}
	}
	if len(days) != 1 {
		t.Fatalf("expected 1 daily entry, got %d (%#v)", len(days), days)
	}

	day := days[0]
	assertFloatEqual(t, day["total_revenue"], 90.0)
	assertFloatEqual(t, day["total_cogs"], 40.0)
	assertFloatEqual(t, day["total_ad_spend"], 20.0)
	assertFloatEqual(t, day["total_fees"], 2.91)
	assertFloatEqual(t, day["net_profit"], 27.09)
	assertFloatEqual(t, day["margin_pct"], 30.1)
	if got := toInt(day["order_count"]); got != 1 {
		t.Fatalf("expected order_count=1, got %d", got)
	}
	if got := toInt(day["return_count"]); got != 1 {
		t.Fatalf("expected return_count=1, got %d", got)
	}
}

func assertFloatEqual(t *testing.T, raw any, want float64) {
	t.Helper()
	got, ok := raw.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T (%#v)", raw, raw)
	}
	if got != want {
		t.Fatalf("expected %.2f, got %.2f", want, got)
	}
}

func toInt(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func TestEcommercePnLCategoryRecognitionHelpers(t *testing.T) {
	if !isProductPnLCategory("orders") || !isProductPnLCategory("cogs") || !isProductPnLCategory("ads") || !isProductPnLCategory("returns") {
		t.Fatalf("expected orders/cogs/ads/returns to be recognized product pnl categories")
	}
	if isProductPnLCategory("not-real") {
		t.Fatalf("expected unknown product pnl category to be rejected")
	}

	if !isDailyPnLCategory("orders") || !isDailyPnLCategory("cogs") || !isDailyPnLCategory("ads") || !isDailyPnLCategory("returns") {
		t.Fatalf("expected orders/cogs/ads/returns to be recognized daily pnl categories")
	}
	if isDailyPnLCategory("not-real") {
		t.Fatalf("expected unknown daily pnl category to be rejected")
	}
}

func TestEcommercePnLCategoryUnknownNoop(t *testing.T) {
	productAccum := &productPnLAccumulator{revenue: 10}
	applyProductPnLCategory("not-real", 100, productAccum)
	if productAccum.revenue != 10 {
		t.Fatalf("expected unknown product category to be no-op, got revenue %.2f", productAccum.revenue)
	}

	dailyAccum := &dailyPnLDayAccum{revenue: 20}
	applyDailyPnLCategory("not-real", 100, dailyAccum)
	if dailyAccum.revenue != 20 {
		t.Fatalf("expected unknown daily category to be no-op, got revenue %.2f", dailyAccum.revenue)
	}
}
