package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/ecommerce"
)

type ecommerceToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error)

var ecommerceToolHandlers = map[string]ecommerceToolHandlerFunc{
	"ecommerce_product_pnl": func(h *BrokerToolsHandler, ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[ecommerce.ProductPnLInput](inputJSON, func(input ecommerce.ProductPnLInput) (any, error) {
			return h.computeProductPnL(ctx, svc, input)
		})
	},
	"ecommerce_daily_pnl": func(h *BrokerToolsHandler, ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[ecommerce.DailyPnLInput](inputJSON, func(input ecommerce.DailyPnLInput) (any, error) {
			return h.computeDailyPnL(ctx, svc, input)
		})
	},
	"ecommerce_calculate_margin": func(_ *BrokerToolsHandler, ctx context.Context, _ *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[ecommerce.CalculateMarginInput](inputJSON, func(input ecommerce.CalculateMarginInput) (any, error) {
			pricing := ecommerce.NewPricingTools()
			r, err := pricing.EcommerceCalculateMarginTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ecommerce_suggest_price": func(_ *BrokerToolsHandler, ctx context.Context, _ *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[ecommerce.SuggestPriceInput](inputJSON, func(input ecommerce.SuggestPriceInput) (any, error) {
			pricing := ecommerce.NewPricingTools()
			r, err := pricing.EcommerceSuggestPriceTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func (h *BrokerToolsHandler) callEcommerceTools(ctx context.Context, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	handler, ok := ecommerceToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}

	result, err := handler(h, ctx, svc, inputJSON)
	return true, result, err
}

type productPnLAccumulator struct {
	revenue     float64
	cogs        float64
	adSpend     float64
	unitsSold   int
	orderCount  int
	returnCount int
}

type productPnLCategoryHandlerFunc func(amount float64, accum *productPnLAccumulator)

var productPnLCategoryHandlers = map[string]productPnLCategoryHandlerFunc{
	"orders": func(amount float64, accum *productPnLAccumulator) {
		accum.revenue += amount
		accum.unitsSold++
		accum.orderCount++
	},
	"cogs": func(amount float64, accum *productPnLAccumulator) {
		accum.cogs += amount
	},
	"ads": func(amount float64, accum *productPnLAccumulator) {
		accum.adSpend += amount
	},
	"returns": func(amount float64, accum *productPnLAccumulator) {
		accum.returnCount++
		accum.revenue -= amount
	},
}

type dailyPnLDayAccum struct {
	revenue     float64
	cogs        float64
	adSpend     float64
	orderCount  int
	returnCount int
}

type dailyPnLCategoryHandlerFunc func(amount float64, accum *dailyPnLDayAccum)

var dailyPnLCategoryHandlers = map[string]dailyPnLCategoryHandlerFunc{
	"orders": func(amount float64, accum *dailyPnLDayAccum) {
		accum.revenue += amount
		accum.orderCount++
	},
	"cogs": func(amount float64, accum *dailyPnLDayAccum) {
		accum.cogs += amount
	},
	"ads": func(amount float64, accum *dailyPnLDayAccum) {
		accum.adSpend += amount
	},
	"returns": func(amount float64, accum *dailyPnLDayAccum) {
		accum.returnCount++
		accum.revenue -= amount
	},
}

func isProductPnLCategory(category string) bool {
	_, ok := productPnLCategoryHandlers[category]
	return ok
}

func isDailyPnLCategory(category string) bool {
	_, ok := dailyPnLCategoryHandlers[category]
	return ok
}

func applyProductPnLCategory(category string, amount float64, accum *productPnLAccumulator) {
	handler, ok := productPnLCategoryHandlers[category]
	if !ok || accum == nil {
		return
	}
	handler(amount, accum)
}

func applyDailyPnLCategory(category string, amount float64, accum *dailyPnLDayAccum) {
	handler, ok := dailyPnLCategoryHandlers[category]
	if !ok || accum == nil {
		return
	}
	handler(amount, accum)
}

// computeProductPnL queries the spend ledger for order/ad data and computes P&L
// for a single Shopify product over a date range.
func (h *BrokerToolsHandler) computeProductPnL(ctx context.Context, svc *data.AgentService, input ecommerce.ProductPnLInput) (any, error) {
	from, to, err := parseInclusiveDateRange(input.DateFrom, input.DateTo)
	if err != nil {
		return nil, err
	}

	entries, err := svc.GetSpendHistory(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query spend history: %w", err)
	}

	accum := productPnLAccumulator{}

	for _, e := range entries {
		// Match entries that reference this product in the detail field
		if e.Detail != input.ShopifyProductID && e.Detail != "" {
			continue
		}
		applyProductPnLCategory(e.Category, e.Amount, &accum)
	}

	// Shopify fees: ~2.9% + $0.30 per transaction
	shopifyFees := accum.revenue*0.029 + float64(accum.orderCount)*0.30
	netProfit := accum.revenue - accum.cogs - accum.adSpend - shopifyFees

	var marginPct, roas, returnRatePct float64
	if accum.revenue > 0 {
		marginPct = (netProfit / accum.revenue) * 100
	}
	if accum.adSpend > 0 {
		roas = accum.revenue / accum.adSpend
	}
	if accum.unitsSold > 0 {
		returnRatePct = (float64(accum.returnCount) / float64(accum.unitsSold)) * 100
	}

	return map[string]any{
		"product_id":      input.ShopifyProductID,
		"title":           "", // Would be populated from Shopify API if available
		"revenue":         round2(accum.revenue),
		"cogs":            round2(accum.cogs),
		"ad_spend":        round2(accum.adSpend),
		"shopify_fees":    round2(shopifyFees),
		"net_profit":      round2(netProfit),
		"margin_pct":      round2(marginPct),
		"roas":            round2(roas),
		"units_sold":      accum.unitsSold,
		"return_rate_pct": round2(returnRatePct),
		"date_from":       input.DateFrom,
		"date_to":         input.DateTo,
	}, nil
}

// computeDailyPnL queries the spend ledger and aggregates P&L by day.
func (h *BrokerToolsHandler) computeDailyPnL(ctx context.Context, svc *data.AgentService, input ecommerce.DailyPnLInput) (any, error) {
	from, to, err := parseInclusiveDateRange(input.DateFrom, input.DateTo)
	if err != nil {
		return nil, err
	}

	entries, err := svc.GetSpendHistory(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query spend history: %w", err)
	}

	days := make(map[string]*dailyPnLDayAccum)
	for _, e := range entries {
		day := e.CreatedAt.UTC().Format("2006-01-02")
		d, ok := days[day]
		if !ok {
			d = &dailyPnLDayAccum{}
			days[day] = d
		}
		applyDailyPnLCategory(e.Category, e.Amount, d)
	}

	// Build sorted daily results
	var results []map[string]any
	current := from
	for current.Before(to) {
		dayStr := current.Format("2006-01-02")
		d, ok := days[dayStr]
		if ok {
			fees := d.revenue*0.029 + float64(d.orderCount)*0.30
			netProfit := d.revenue - d.cogs - d.adSpend - fees
			var marginPct float64
			if d.revenue > 0 {
				marginPct = (netProfit / d.revenue) * 100
			}
			results = append(results, map[string]any{
				"date":           dayStr,
				"total_revenue":  round2(d.revenue),
				"total_cogs":     round2(d.cogs),
				"total_ad_spend": round2(d.adSpend),
				"total_fees":     round2(fees),
				"net_profit":     round2(netProfit),
				"margin_pct":     round2(marginPct),
				"order_count":    d.orderCount,
				"return_count":   d.returnCount,
			})
		}
		current = current.AddDate(0, 0, 1)
	}

	if results == nil {
		results = []map[string]any{}
	}

	return map[string]any{
		"days":      results,
		"date_from": input.DateFrom,
		"date_to":   input.DateTo,
	}, nil
}

func parseInclusiveDateRange(dateFrom, dateTo string) (time.Time, time.Time, error) {
	from, err := time.Parse("2006-01-02", dateFrom)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date_from: %w", err)
	}
	to, err := time.Parse("2006-01-02", dateTo)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date_to: %w", err)
	}
	return from, to.Add(24 * time.Hour), nil
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
