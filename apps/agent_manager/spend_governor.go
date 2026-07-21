package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

// SpendGovernor enforces per-agent, per-category budget limits.
// It acts as middleware in the broker tool dispatch, blocking tool calls
// that would exceed the agent's daily spend limit.
type SpendGovernor struct {
	db gowild_data.Database
}

// toolCategoryMap maps tool names to spend categories.
var toolCategoryMap = map[string]string{
	"ads_meta_create_campaign":                "ads",
	"ads_meta_update_campaign":                "ads",
	"ads_meta_create_adset":                   "ads",
	"ads_meta_create_ad":                      "ads",
	"ads_google_create_campaign":              "ads",
	"ads_google_update_campaign":              "ads",
	"supplier_place_order":                    "orders",
	"shopify_create_product":                  "shopify",
	"shopify_update_product":                  "shopify",
	"shopify_delete_product":                  "shopify",
	"company_commerce_shopify_create_product": "shopify",
	"company_commerce_shopify_update_product": "shopify",
	"company_commerce_shopify_delete_product": "shopify",
}

// defaultDailyLimits defines default spend caps per category.
var defaultDailyLimits = map[string]float64{
	"ads":     100.00,
	"orders":  500.00,
	"shopify": 100.0,
	"llm":     1000.0,
}

type spendCategoryEstimatorFunc func(g *SpendGovernor, inputJSON []byte) float64

var spendCategoryEstimators = map[string]spendCategoryEstimatorFunc{
	"ads": func(g *SpendGovernor, inputJSON []byte) float64 {
		return g.estimateAdsCost(inputJSON)
	},
	"orders": func(g *SpendGovernor, inputJSON []byte) float64 {
		return g.estimateOrderCost(inputJSON)
	},
	"shopify": func(_ *SpendGovernor, _ []byte) float64 {
		// Count-based: 1.0 per call
		return 1.0
	},
}

func isSpendCategoryEstimator(category string) bool {
	_, ok := spendCategoryEstimators[category]
	return ok
}

// NewSpendGovernor creates a new spend governor.
func NewSpendGovernor(db gowild_data.Database) *SpendGovernor {
	return &SpendGovernor{db: db}
}

// EstimateCost returns the estimated cost and spend category for a tool call.
// Returns (0, "") for tools not tracked by the spend governor.
func (g *SpendGovernor) EstimateCost(toolName string, inputJSON []byte) (float64, string) {
	category, ok := toolCategoryMap[toolName]
	if !ok {
		return 0, ""
	}

	if !isSpendCategoryEstimator(category) {
		return 0, ""
	}
	estimator, ok := spendCategoryEstimators[category]
	if !ok {
		return 0, ""
	}
	return estimator(g, inputJSON), category
}

// estimateAdsCost extracts budget from ad tool input params.
func (g *SpendGovernor) estimateAdsCost(inputJSON []byte) float64 {
	var input struct {
		Budget      float64 `json:"budget"`
		DailyBudget float64 `json:"daily_budget"`
	}
	if len(inputJSON) > 0 {
		json.Unmarshal(inputJSON, &input)
	}
	if input.DailyBudget > 0 {
		return input.DailyBudget
	}
	if input.Budget > 0 {
		return input.Budget
	}
	return 1.0 // Default cost if no budget field found
}

// estimateOrderCost extracts order total from input params.
func (g *SpendGovernor) estimateOrderCost(inputJSON []byte) float64 {
	var input struct {
		Total      float64 `json:"total"`
		OrderTotal float64 `json:"order_total"`
		Amount     float64 `json:"amount"`
	}
	if len(inputJSON) > 0 {
		json.Unmarshal(inputJSON, &input)
	}
	if input.Total > 0 {
		return input.Total
	}
	if input.OrderTotal > 0 {
		return input.OrderTotal
	}
	if input.Amount > 0 {
		return input.Amount
	}
	return 1.0 // Default cost if no total field found
}

// CheckBudget verifies the agent has remaining budget for the given category and amount.
func (g *SpendGovernor) CheckBudget(ctx context.Context, agentID, category string, amount float64) error {
	svc := data.NewAgentService(g.db, agentID)

	spent, err := svc.GetTodaySpend(ctx, category)
	if err != nil {
		return fmt.Errorf("failed to check spend: %w", err)
	}

	limit, err := g.getDailyLimit(ctx, svc, category)
	if err != nil {
		return fmt.Errorf("failed to check spend limit: %w", err)
	}

	if spent+amount > limit {
		return fmt.Errorf("BUDGET_EXCEEDED: %s daily limit for %s is %.2f, already spent %.2f",
			category, agentID, limit, spent)
	}
	return nil
}

// RecordSpend logs a spend entry after successful tool execution.
func (g *SpendGovernor) RecordSpend(ctx context.Context, agentID, category string, amount float64, toolName string) {
	svc := data.NewAgentService(g.db, agentID)
	if err := svc.RecordSpend(ctx, category, amount, toolName, ""); err != nil {
		log.Printf("WARNING: failed to record spend for %s/%s: %v", agentID, category, err)
	}
}

// getDailyLimit returns the configured limit, falling back to defaults.
func (g *SpendGovernor) getDailyLimit(ctx context.Context, svc *data.AgentService, category string) (float64, error) {
	limit, err := svc.GetSpendLimit(ctx, category)
	if err != nil {
		return 0, err
	}
	if limit > 0 {
		return limit, nil
	}
	// Fall back to default
	if def, ok := defaultDailyLimits[category]; ok {
		return def, nil
	}
	return 0, nil
}
