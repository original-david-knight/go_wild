package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func pipelineBuiltinPolymarketSnapshot(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, _ PipelineStep, params map[string]any) (map[string]any, error) {
	snapshot, err := loadBuiltinPolymarketSnapshot(ctx, pe, run, params)
	if err != nil {
		return nil, err
	}
	result := buildBuiltinPolymarketSnapshotResult(snapshot)

	staleOnly, staleOK := boolParam(params, "stale_only")
	log.Printf("[snapshot] stale_only=%v (parsed=%v), pe=%v, db=%v, snapshot=%v, companyID=%q", staleOnly, staleOK, pe != nil, pe != nil && pe.db != nil, snapshot != nil, snapshot.companyID)
	if staleOnly && pe != nil && pe.db != nil && snapshot != nil && strings.TrimSpace(snapshot.companyID) != "" {
		result = filterBuiltinPolymarketSnapshotStale(ctx, pe.db, snapshot.companyID, result)
	}

	return result, nil
}

func loadBuiltinPolymarketSnapshot(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, params map[string]any) (*builtinPolymarketSnapshot, error) {
	companyID, err := resolvePolymarketRunCompanyID(run, stringParam(params, "company_id"))
	if err != nil {
		return nil, err
	}

	includePositions := true
	if v, ok := boolParam(params, "include_positions"); ok {
		includePositions = v
	}
	includeOrders := true
	if v, ok := boolParam(params, "include_orders"); ok {
		includeOrders = v
	}
	if !includePositions && !includeOrders {
		return nil, fmt.Errorf("at least one of include_positions or include_orders must be true")
	}

	orderMarket := strings.TrimSpace(stringParam(params, "order_market"))
	if orderMarket == "" {
		orderMarket = firstStringParam(params, "market", "condition_id")
	}

	client, resolvedCompanyID, err := getBuiltinPolymarketClient(ctx, pe, companyID)
	if err != nil {
		return nil, err
	}

	var positions []polymarket.Position
	if includePositions {
		positions, err = client.GetPositions(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list polymarket positions: %w", err)
		}
	}

	var orders []polymarket.Order
	if includeOrders {
		orders, err = client.GetOrders(ctx, orderMarket)
		if err != nil {
			return nil, fmt.Errorf("failed to list polymarket orders: %w", err)
		}
	}

	reviewContext := buildBuiltinPolymarketReviewContext(positions, 0, 0, false)
	if liquidUSDBalance, balanceErr := getBuiltinPolymarketLiquidUSDBalance(ctx, pe, resolvedCompanyID); balanceErr == nil {
		usdcBalance, _ := getBuiltinPolymarketUSDCBalance(ctx, pe, resolvedCompanyID)
		reviewContext = buildBuiltinPolymarketReviewContext(positions, usdcBalance, liquidUSDBalance, true)
		setCachedBuiltinPolymarketSizingContext(resolvedCompanyID, reviewContext)
	}

	return &builtinPolymarketSnapshot{
		companyID:     resolvedCompanyID,
		positions:     positions,
		orders:        orders,
		reviewContext: reviewContext,
	}, nil
}

func buildBuiltinPolymarketSnapshotResult(snapshot *builtinPolymarketSnapshot) map[string]any {
	if snapshot == nil {
		return map[string]any{
			"source":          "polymarket",
			"company_id":      "",
			"positions":       []polymarket.Position{},
			"orders":          []polymarket.Order{},
			"items":           []map[string]any{},
			"positions_found": 0,
			"orders_found":    0,
		}
	}
	result := map[string]any{
		"source":          "polymarket",
		"company_id":      snapshot.companyID,
		"positions":       snapshot.positions,
		"orders":          snapshot.orders,
		"items":           buildBuiltinPolymarketItems(snapshot.companyID, snapshot.positions, snapshot.orders, snapshot.reviewContext),
		"positions_found": len(snapshot.positions),
		"orders_found":    len(snapshot.orders),
	}
	applyBuiltinPolymarketReviewContextToResult(result, snapshot.reviewContext)
	return result
}

func buildBuiltinPolymarketItems(companyID string, positions []polymarket.Position, orders []polymarket.Order, reviewContext builtinPolymarketReviewContext) []map[string]any {
	items := make([]map[string]any, 0, len(positions)+len(orders))
	for _, position := range positions {
		priority, reason := builtinPolymarketPositionReevaluationPriority(position)
		item := map[string]any{
			"item_type":             "position",
			"company_id":            strings.TrimSpace(companyID),
			"condition_id":          strings.TrimSpace(position.ConditionID),
			"asset":                 strings.TrimSpace(position.Asset),
			"position":              position,
			"reevaluation_priority": priority,
			"reevaluation_reason":   reason,
			"reevaluation_notional": roundBuiltinPolymarketFloat(math.Max(position.CurrentValue, 0), 2),
		}
		applyBuiltinPolymarketReviewContextToItem(item, strings.TrimSpace(position.ConditionID), reviewContext)
		items = append(items, item)
	}
	for _, order := range orders {
		priority, reason := builtinPolymarketOrderReevaluationPriority(order)
		item := map[string]any{
			"item_type":             "order",
			"company_id":            strings.TrimSpace(companyID),
			"condition_id":          strings.TrimSpace(order.Market),
			"asset":                 strings.TrimSpace(order.AssetID),
			"order":                 order,
			"reevaluation_priority": priority,
			"reevaluation_reason":   reason,
			"reevaluation_notional": roundBuiltinPolymarketFloat(builtinPolymarketOrderRemainingNotional(order), 2),
		}
		applyBuiltinPolymarketReviewContextToItem(item, strings.TrimSpace(order.Market), reviewContext)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := floatParam(items[i], "reevaluation_priority")
		right, _ := floatParam(items[j], "reevaluation_priority")
		if left != right {
			return left > right
		}
		return strings.Compare(strings.TrimSpace(fmt.Sprint(items[i]["condition_id"])), strings.TrimSpace(fmt.Sprint(items[j]["condition_id"]))) < 0
	})
	return items
}

func buildBuiltinPolymarketReviewContext(positions []polymarket.Position, usdcBalance, liquidUSDBalance float64, balanceLoaded bool) builtinPolymarketReviewContext {
	ctx := builtinPolymarketReviewContext{
		BalanceLoaded:            balanceLoaded,
		CurrentSharesByCondition: make(map[string]float64),
	}
	for _, position := range positions {
		conditionID := strings.TrimSpace(position.ConditionID)
		if conditionID != "" && position.Size > 0 {
			ctx.CurrentSharesByCondition[conditionID] = roundBuiltinPolymarketFloat(ctx.CurrentSharesByCondition[conditionID]+position.Size, 4)
		}
		if position.CurrentValue > 0 {
			ctx.PositionValue += position.CurrentValue
		}
	}
	ctx.PositionValue = roundBuiltinPolymarketFloat(ctx.PositionValue, 2)
	if !balanceLoaded {
		return ctx
	}
	ctx.USDCBalance = roundBuiltinPolymarketFloat(math.Max(usdcBalance, 0), 2)
	ctx.LiquidUSDBalance = roundBuiltinPolymarketFloat(math.Max(liquidUSDBalance, 0), 2)
	ctx.AUM = roundBuiltinPolymarketFloat(ctx.LiquidUSDBalance+ctx.PositionValue, 2)
	if ctx.AUM > 0 {
		ctx.MaxAllowed = roundBuiltinPolymarketFloat(ctx.AUM/20, 2)
	}
	return ctx
}

func cloneBuiltinPolymarketReviewContext(reviewContext builtinPolymarketReviewContext) builtinPolymarketReviewContext {
	cloned := reviewContext
	if len(reviewContext.CurrentSharesByCondition) == 0 {
		cloned.CurrentSharesByCondition = nil
		return cloned
	}
	cloned.CurrentSharesByCondition = make(map[string]float64, len(reviewContext.CurrentSharesByCondition))
	for conditionID, shares := range reviewContext.CurrentSharesByCondition {
		cloned.CurrentSharesByCondition[conditionID] = shares
	}
	return cloned
}

func getCachedBuiltinPolymarketSizingContext(companyID string) (builtinPolymarketReviewContext, bool) {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return builtinPolymarketReviewContext{}, false
	}
	builtinPolymarketSizingCache.mu.Lock()
	defer builtinPolymarketSizingCache.mu.Unlock()

	entry, ok := builtinPolymarketSizingCache.entries[companyID]
	if !ok {
		return builtinPolymarketReviewContext{}, false
	}
	if time.Since(entry.createdAt) > builtinPolymarketSizingCacheTTL {
		delete(builtinPolymarketSizingCache.entries, companyID)
		return builtinPolymarketReviewContext{}, false
	}
	return cloneBuiltinPolymarketReviewContext(entry.reviewContext), true
}

func setCachedBuiltinPolymarketSizingContext(companyID string, reviewContext builtinPolymarketReviewContext) {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" || !reviewContext.BalanceLoaded || reviewContext.AUM <= 0 {
		return
	}
	builtinPolymarketSizingCache.mu.Lock()
	defer builtinPolymarketSizingCache.mu.Unlock()
	builtinPolymarketSizingCache.entries[companyID] = builtinPolymarketSizingCacheEntry{
		reviewContext: cloneBuiltinPolymarketReviewContext(reviewContext),
		createdAt:     time.Now(),
	}
}

func hydrateBuiltinPolymarketManageSizingContext(ctx context.Context, pe *PipelineEngine, companyID string, payload *builtinPolymarketPayload, positions []polymarket.Position, allPositions []polymarket.Position) string {
	if payload == nil {
		return "payload"
	}

	reviewContext := buildBuiltinPolymarketReviewContext(positions, 0, 0, false)
	conditionID := strings.TrimSpace(payload.ConditionID)
	currentPosition := 0.0
	if reviewContext.CurrentSharesByCondition != nil {
		currentPosition = reviewContext.CurrentSharesByCondition[conditionID]
	}
	payload.CurrentPosition = roundBuiltinPolymarketFloat(math.Max(currentPosition, 0), 4)

	// User-defined review pipelines often emit only the research fields and
	// drop AUM/capacity fields. When that happens, recover the latest capital
	// context directly from the live portfolio instead of falling back to the
	// current share count as the cap.
	if payload.AUM > 0 || payload.MaxAllowed > 0 {
		return "payload"
	}
	if cachedReviewContext, ok := getCachedBuiltinPolymarketSizingContext(companyID); ok {
		payload.AUM = cachedReviewContext.AUM
		payload.MaxAllowed = cachedReviewContext.MaxAllowed
		payload.RemainingCapacity = roundBuiltinPolymarketFloat(math.Max(cachedReviewContext.MaxAllowed-payload.CurrentPosition, 0), 4)
		return "live_cache"
	}
	livePositions := allPositions
	if len(livePositions) == 0 {
		livePositions = positions
	}

	liquidUSDBalance, balanceErr := getBuiltinPolymarketLiquidUSDBalance(ctx, pe, companyID)
	if balanceErr != nil {
		return "current_position_fallback"
	}
	usdcBalance, _ := getBuiltinPolymarketUSDCBalance(ctx, pe, companyID)
	reviewContext = buildBuiltinPolymarketReviewContext(livePositions, usdcBalance, liquidUSDBalance, true)
	if reviewContext.AUM <= 0 {
		return "current_position_fallback"
	}
	setCachedBuiltinPolymarketSizingContext(companyID, reviewContext)

	payload.CurrentPosition = roundBuiltinPolymarketFloat(math.Max(reviewContext.CurrentSharesByCondition[conditionID], 0), 4)
	payload.AUM = reviewContext.AUM
	payload.MaxAllowed = reviewContext.MaxAllowed
	payload.RemainingCapacity = roundBuiltinPolymarketFloat(math.Max(reviewContext.MaxAllowed-payload.CurrentPosition, 0), 4)
	return "live_snapshot"
}

func applyBuiltinPolymarketReviewContextToResult(result map[string]any, reviewContext builtinPolymarketReviewContext) {
	if result == nil || !reviewContext.BalanceLoaded {
		return
	}
	result["usdc_balance"] = reviewContext.USDCBalance
	result["liquid_usd_balance"] = reviewContext.LiquidUSDBalance
	result["position_value"] = reviewContext.PositionValue
	result["aum"] = reviewContext.AUM
	result["max_allowed"] = reviewContext.MaxAllowed
}

func applyBuiltinPolymarketReviewContextToItem(item map[string]any, conditionID string, reviewContext builtinPolymarketReviewContext) {
	if item == nil {
		return
	}
	currentPosition := 0.0
	if reviewContext.CurrentSharesByCondition != nil {
		currentPosition = reviewContext.CurrentSharesByCondition[strings.TrimSpace(conditionID)]
	}
	item["current_position"] = roundBuiltinPolymarketFloat(math.Max(currentPosition, 0), 4)
	if !reviewContext.BalanceLoaded {
		return
	}
	item["aum"] = reviewContext.AUM
	item["max_allowed"] = reviewContext.MaxAllowed
	item["remaining_capacity"] = roundBuiltinPolymarketFloat(math.Max(reviewContext.MaxAllowed-currentPosition, 0), 4)
}

func builtinPolymarketPositionReevaluationPriority(position polymarket.Position) (float64, string) {
	currentValue := math.Max(position.CurrentValue, 0)
	percentMove := math.Abs(position.PercentPnl)
	daysToEnd := builtinPolymarketDaysUntil(position.EndDate)

	priority := builtinPolymarketNormalizedLogScore(currentValue, 250) * 0.5
	priority += math.Min(percentMove, 1) * 0.2
	priority += builtinPolymarketResolutionWindowScore(daysToEnd) * 0.2
	if position.Redeemable {
		priority += 0.25
	}
	if priority > 1 {
		priority = 1
	}

	switch {
	case position.Redeemable:
		return roundBuiltinPolymarketFloat(priority, 4), "redeemable_position"
	case daysToEnd > 0 && daysToEnd <= 3:
		return roundBuiltinPolymarketFloat(priority, 4), "near_resolution"
	case percentMove >= 0.2:
		return roundBuiltinPolymarketFloat(priority, 4), "large_mark_to_market_move"
	default:
		return roundBuiltinPolymarketFloat(priority, 4), "portfolio_monitoring"
	}
}

func builtinPolymarketOrderReevaluationPriority(order polymarket.Order) (float64, string) {
	notional := builtinPolymarketOrderRemainingNotional(order)
	priority := builtinPolymarketNormalizedLogScore(notional, 250) * 0.7
	if strings.EqualFold(strings.TrimSpace(order.Side), polymarket.Buy) {
		priority += 0.2
	}
	if isBuiltinPolymarketOrderOpen(order) {
		priority += 0.1
	}
	if priority > 1 {
		priority = 1
	}
	if strings.EqualFold(strings.TrimSpace(order.Side), polymarket.Buy) {
		return roundBuiltinPolymarketFloat(priority, 4), "open_entry_order"
	}
	return roundBuiltinPolymarketFloat(priority, 4), "open_exit_order"
}

func builtinPolymarketOrderRemainingNotional(order polymarket.Order) float64 {
	price := 0.0
	if strings.TrimSpace(order.Price) != "" {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(order.Price), 64); err == nil {
			price = parsed
		}
	}
	return math.Max(builtinPolymarketOrderRemainingSize(order), 0) * math.Max(price, 0)
}

func filterBuiltinPolymarketSnapshotStale(ctx context.Context, db gowild_data.Database, companyID string, result map[string]any) map[string]any {
	items, _ := result["items"].([]map[string]any)
	if len(items) == 0 {
		return result
	}
	conditionIDs := make([]string, 0, len(items))
	for _, item := range items {
		cid := strings.TrimSpace(fmt.Sprint(item["condition_id"]))
		if cid != "" {
			conditionIDs = append(conditionIDs, cid)
		}
	}
	propsByMarket, err := data.ListMarketPropertiesBulk(ctx, db, companyID, conditionIDs)
	if err != nil {
		log.Printf("[snapshot-stale] ListMarketPropertiesBulk failed: %v", err)
		return result
	}

	staleCutoff := time.Now().UTC().Add(-24 * time.Hour)
	log.Printf("[snapshot-stale] filtering %d items, company=%s, cutoff=%s, props_loaded=%d", len(items), companyID, staleCutoff.Format(time.RFC3339), len(propsByMarket))
	filtered := make([]map[string]any, 0, len(items))
	skipped := 0
	for _, item := range items {
		cid := strings.TrimSpace(fmt.Sprint(item["condition_id"]))
		props := propsByMarket[cid]
		touched := builtinPolymarketMarketRecentlyTouched(props, staleCutoff)
		if touched {
			skipped++
			log.Printf("[snapshot-stale] skipping %s (recently touched, props=%d)", cid, len(props))
			continue
		}
		if len(props) > 0 {
			for _, p := range props {
				if p != nil {
					log.Printf("[snapshot-stale] keeping %s despite prop key=%s value=%s type=%s", cid, p.Key, p.Value, p.ValueType)
				}
			}
		}
		filtered = append(filtered, item)
	}
	result["items"] = filtered
	result["stale_only"] = true
	result["skipped_stale"] = skipped
	return result
}
