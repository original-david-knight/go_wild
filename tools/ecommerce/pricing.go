package ecommerce

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// PricingTools provides margin and pricing calculation tools for e-commerce.
// These are pure math tools that can run locally or via broker.
type PricingTools struct{}

// NewPricingTools creates a new PricingTools instance.
func NewPricingTools() *PricingTools {
	return &PricingTools{}
}

// DescribeTool returns the description for a tool by name.
func (t *PricingTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"ecommerce_calculate_margin": "Calculate gross margin, net margin, and break-even ROAS for a product given retail price, supplier cost, and estimated fees. Use this to evaluate whether a product is viable before listing.",
		"ecommerce_suggest_price":    "Suggest a retail price for a product given supplier cost, target margin, and product category. Returns a recommended price with expected margin and notes.",
	}
	return descriptions[name]
}

// CalculateMarginInput defines input for margin calculation.
type CalculateMarginInput struct {
	RetailPrice        float64 `json:"retail_price" description:"Retail selling price in USD" required:"true"`
	SupplierCost       float64 `json:"supplier_cost" description:"Supplier/COGS cost in USD" required:"true"`
	EstimatedAdSpendPct float64 `json:"estimated_ad_spend_pct" description:"Estimated ad spend as percentage of revenue (e.g. 25 for 25%)" required:"true"`
	ShopifyFeePct      float64 `json:"shopify_fee_pct" description:"Shopify transaction fee percentage (default ~2.9)" required:"true"`
}

// EcommerceCalculateMarginTool calculates margins for a product.
func (t *PricingTools) EcommerceCalculateMarginTool(ctx context.Context, input CalculateMarginInput) (*loop.ToolResult, error) {
	if input.RetailPrice <= 0 {
		return loop.NewErrorResult("retail_price must be positive"), nil
	}
	if input.SupplierCost < 0 {
		return loop.NewErrorResult("supplier_cost cannot be negative"), nil
	}

	shopifyFeePct := input.ShopifyFeePct
	if shopifyFeePct <= 0 {
		shopifyFeePct = 2.9
	}

	grossMargin := input.RetailPrice - input.SupplierCost
	grossMarginPct := (grossMargin / input.RetailPrice) * 100

	shopifyFee := input.RetailPrice*(shopifyFeePct/100) + 0.30
	adSpend := input.RetailPrice * (input.EstimatedAdSpendPct / 100)
	netMargin := input.RetailPrice - input.SupplierCost - shopifyFee - adSpend
	netMarginPct := (netMargin / input.RetailPrice) * 100

	var breakEvenROAS float64
	if adSpend > 0 {
		// Break-even ROAS: revenue needed per dollar of ad spend to cover all costs
		costExAds := input.SupplierCost + shopifyFee
		breakEvenROAS = input.RetailPrice / (input.RetailPrice - costExAds)
	}

	return loop.NewSuccessResult(map[string]any{
		"retail_price":     input.RetailPrice,
		"supplier_cost":    input.SupplierCost,
		"gross_margin":     grossMargin,
		"gross_margin_pct": grossMarginPct,
		"shopify_fee":      shopifyFee,
		"ad_spend":         adSpend,
		"net_margin":       netMargin,
		"net_margin_pct":   netMarginPct,
		"break_even_roas":  breakEvenROAS,
	}), nil
}

// SuggestPriceInput defines input for price suggestion.
type SuggestPriceInput struct {
	SupplierCost float64 `json:"supplier_cost" description:"Supplier/COGS cost in USD" required:"true"`
	TargetMargin float64 `json:"target_margin" description:"Target net margin percentage (e.g. 30 for 30%)" required:"true"`
	Category     string  `json:"category" description:"Product category (e.g. electronics, fashion, home) for pricing heuristics"`
}

// EcommerceSuggestPriceTool suggests a retail price given costs and target margin.
func (t *PricingTools) EcommerceSuggestPriceTool(ctx context.Context, input SuggestPriceInput) (*loop.ToolResult, error) {
	if input.SupplierCost <= 0 {
		return loop.NewErrorResult("supplier_cost must be positive"), nil
	}
	if input.TargetMargin <= 0 || input.TargetMargin >= 100 {
		return loop.NewErrorResult("target_margin must be between 0 and 100"), nil
	}

	// Estimate overhead: ~2.9% Shopify fees + $0.30/txn + ~20% ad spend baseline
	// Solve: price - cost - price*0.029 - 0.30 - price*0.20 = price * (target_margin/100)
	// price * (1 - 0.029 - 0.20 - target_margin/100) = cost + 0.30
	overhead := 0.029 + 0.20 + (input.TargetMargin / 100)
	if overhead >= 1.0 {
		return loop.NewErrorResult("target_margin too high — combined overhead exceeds 100% of price"), nil
	}
	suggestedPrice := (input.SupplierCost + 0.30) / (1.0 - overhead)

	// Round to .99 pricing
	suggestedPrice = float64(int(suggestedPrice)) + 0.99

	// Compute actual margin at suggested price
	shopifyFee := suggestedPrice*0.029 + 0.30
	adSpend := suggestedPrice * 0.20
	actualNet := suggestedPrice - input.SupplierCost - shopifyFee - adSpend
	actualMarginPct := (actualNet / suggestedPrice) * 100

	notes := "Based on 2.9% + $0.30 Shopify fees and 20% ad spend assumption."
	if input.Category != "" {
		switch input.Category {
		case "electronics":
			notes += " Electronics typically see 15-25% margins; consider competitive pricing."
		case "fashion":
			notes += " Fashion allows higher markups (40-60%); brand positioning matters."
		case "home":
			notes += " Home goods typically see 30-50% margins; shipping costs can erode margin."
		}
	}

	return loop.NewSuccessResult(map[string]any{
		"suggested_price": suggestedPrice,
		"supplier_cost":   input.SupplierCost,
		"expected_margin": actualMarginPct,
		"shopify_fee":     shopifyFee,
		"estimated_ad_spend": adSpend,
		"notes":           notes,
	}), nil
}
