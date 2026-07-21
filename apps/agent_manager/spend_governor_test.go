package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestSpendGovernorEstimateCostByCategory(t *testing.T) {
	g := NewSpendGovernor(nil)

	adsInput, err := json.Marshal(map[string]any{"daily_budget": 42.5})
	if err != nil {
		t.Fatalf("marshal ads input failed: %v", err)
	}
	cost, category := g.EstimateCost("ads_meta_create_campaign", adsInput)
	if category != "ads" || cost != 42.5 {
		t.Fatalf("expected ads cost/category 42.5/ads, got %.2f/%s", cost, category)
	}

	orderInput, err := json.Marshal(map[string]any{"total": 150.0})
	if err != nil {
		t.Fatalf("marshal orders input failed: %v", err)
	}
	cost, category = g.EstimateCost("supplier_place_order", orderInput)
	if category != "orders" || cost != 150.0 {
		t.Fatalf("expected orders cost/category 150/orders, got %.2f/%s", cost, category)
	}

	cost, category = g.EstimateCost("shopify_create_product", nil)
	if category != "shopify" || cost != 1.0 {
		t.Fatalf("expected shopify cost/category 1/shopify, got %.2f/%s", cost, category)
	}

	cost, category = g.EstimateCost("not_real_tool", nil)
	if category != "" || cost != 0 {
		t.Fatalf("expected unknown tool to return 0/empty category, got %.2f/%s", cost, category)
	}
}

func TestSpendGovernorCategoryEstimatorRecognition(t *testing.T) {
	if !isSpendCategoryEstimator("ads") || !isSpendCategoryEstimator("orders") || !isSpendCategoryEstimator("shopify") {
		t.Fatalf("expected ads/orders/shopify to be recognized spend estimator categories")
	}
	if isSpendCategoryEstimator("not-real") {
		t.Fatalf("expected unknown spend estimator category to be rejected")
	}
}

func TestSpendGovernorCheckBudgetUnderLimit(t *testing.T) {
	db := setupManagerTestDB(t)
	g := NewSpendGovernor(db)
	ctx := context.Background()
	svc := data.NewAgentService(db, "budget-agent")
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	// Default ads limit is 100.0 — requesting 50 should succeed
	if err := g.CheckBudget(ctx, "budget-agent", "ads", 50.0); err != nil {
		t.Fatalf("CheckBudget under limit should succeed, got: %v", err)
	}
}

func TestSpendGovernorCheckBudgetExceedsLimit(t *testing.T) {
	db := setupManagerTestDB(t)
	g := NewSpendGovernor(db)
	ctx := context.Background()
	svc := data.NewAgentService(db, "over-budget-agent")
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	// Record 80.0 of spend against the default 100.0 ads limit
	if err := svc.RecordSpend(ctx, "ads", 80.0, "ads_meta_create_campaign", ""); err != nil {
		t.Fatalf("RecordSpend failed: %v", err)
	}

	// Requesting 30 more should exceed the 100 limit (80+30=110)
	err := g.CheckBudget(ctx, "over-budget-agent", "ads", 30.0)
	if err == nil {
		t.Fatal("CheckBudget should fail when spend exceeds limit")
	}
	if !strings.Contains(err.Error(), "BUDGET_EXCEEDED") {
		t.Fatalf("expected BUDGET_EXCEEDED error, got: %v", err)
	}
}

func TestSpendGovernorRecordSpend(t *testing.T) {
	db := setupManagerTestDB(t)
	g := NewSpendGovernor(db)
	ctx := context.Background()
	svc := data.NewAgentService(db, "record-agent")
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	g.RecordSpend(ctx, "record-agent", "orders", 42.50, "supplier_place_order")

	spent, err := svc.GetTodaySpend(ctx, "orders")
	if err != nil {
		t.Fatalf("GetTodaySpend failed: %v", err)
	}
	if spent != 42.50 {
		t.Fatalf("expected today spend 42.50, got %.2f", spent)
	}
}

func TestSpendGovernorGetDailyLimitDefaultAndOverride(t *testing.T) {
	db := setupManagerTestDB(t)
	g := NewSpendGovernor(db)
	ctx := context.Background()
	svc := data.NewAgentService(db, "spend-governor-agent")
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	limit, err := g.getDailyLimit(ctx, svc, "ads")
	if err != nil {
		t.Fatalf("getDailyLimit default failed: %v", err)
	}
	if limit != 100.0 {
		t.Fatalf("expected ads default limit 100.0, got %.2f", limit)
	}

	if err := svc.SetSpendLimit(ctx, "ads", 250.0); err != nil {
		t.Fatalf("SetSpendLimit failed: %v", err)
	}
	limit, err = g.getDailyLimit(ctx, svc, "ads")
	if err != nil {
		t.Fatalf("getDailyLimit override failed: %v", err)
	}
	if limit != 250.0 {
		t.Fatalf("expected ads override limit 250.0, got %.2f", limit)
	}
}
