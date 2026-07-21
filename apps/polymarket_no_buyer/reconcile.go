package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// reconcilePass is the run's final trading step: it drives every eligible market
// toward its desired steady state — exactly ONE open NO buy order priced at the
// normalized NO midpoint, expiring OrderExpiryBeforeClose before close, sized to
// bring held NO shares up to the per-market 1% target. It runs after the snapshot
// and discovery passes.
//
// The model is idempotency-critical. Per-market sizing uses committed = held NO
// shares ONLY (never held + open orders): the very order being reconciled is what
// provides the rest, so once a right-sized order exists the next run reproduces the
// same desired order and leaves it untouched. This converges rather than oscillating
// against the stale-cancel dedup.
//
// The shared run budget is seeded from snapshot.WalletUSDC (the spendable Polygon
// collateral — never an exchange/CLOB balance); per-market sizing targets
// snapshot.Total (wallet USDC plus owned-share value). Open orders and positions are
// fetched once up front; the NO book, midpoint, eligibility, and venue minimum are
// re-checked FRESH immediately before acting on each market, so a market that drifted
// out of band since discovery places nothing.
//
// Each market is processed independently: a fetch/eligibility/min-order/place/cancel
// error for one market is logged and the pass continues to the next — one bad market
// never aborts the pass. In dry-run mode every place/maintain/cancel-replace decision
// is logged with full detail but no mutating client method is called; the local
// budget is still reserved so the planned numbers stay realistic.
func (a *App) reconcilePass(ctx context.Context, logger *Logger, snapshot accountSnapshot, eligible []eligibleMarket) {
	budget := newRunBudget(snapshot.WalletUSDC)

	orders, err := a.trading.GetOrders(ctx, "")
	if err != nil {
		logger.Event("reconcile_error", map[string]any{"stage": "get_orders", "error": err.Error()})
		return
	}
	positions, err := a.trading.GetPositions(ctx)
	if err != nil {
		logger.Event("reconcile_error", map[string]any{"stage": "get_positions", "error": err.Error()})
		return
	}

	// Group every NO-buy candidate order by its market so each market's existing
	// orders are located once. The per-market candidate set is filtered to the
	// market's NO token (side + asset) inside reconcileMarket.
	ordersByMarket := map[string][]polymarket.Order{}
	for _, o := range orders {
		ordersByMarket[o.Market] = append(ordersByMarket[o.Market], o)
	}

	owned := ownedTokenIDs(positions)

	logger.Event("reconcile_start", map[string]any{
		"markets_eligible": len(eligible),
		"wallet_usdc":      snapshot.WalletUSDC,
		"total":            snapshot.Total,
		"budget_remaining": budget.Remaining(),
		"dry_run":          a.cfg.DryRun,
	})

	for _, m := range eligible {
		a.reconcileMarket(ctx, logger, snapshot, budget, m, positions, owned, ordersByMarket[m.Market.ConditionID])
	}

	logger.Event("reconcile_done", map[string]any{
		"markets_eligible": len(eligible),
		"budget_remaining": budget.Remaining(),
	})
}

// reconcileMarket drives a single eligible market toward its desired order. It
// re-fetches the NO book FRESH, recomputes the midpoint and reads the venue tick,
// re-runs the FULL eligibility predicate against the fresh midpoint, resolves the
// venue minimum order size, sizes against the snapshot Total (committed = held NO
// shares only), and plans funding against the budget's remaining balance WITHOUT
// reserving. It then either skips (cancelling any existing NO-buy orders), maintains
// an already-correct order (reserving its notional and cancelling extras), or
// cancel-replaces on divergence (cancel all existing, then place). Every fetch/place/
// cancel error is logged and isolated to this market.
func (a *App) reconcileMarket(
	ctx context.Context,
	logger *Logger,
	snapshot accountSnapshot,
	budget *runBudget,
	m eligibleMarket,
	positions []polymarket.Position,
	owned map[string]bool,
	marketOrders []polymarket.Order,
) {
	conditionID := m.Market.ConditionID
	noToken := m.Tokens.NoTokenID

	// 1. Re-fetch the NO order book and recompute the midpoint + venue tick FRESH,
	// immediately before acting. A book fetch error skips this market only.
	book, err := a.trading.GetOrderBookDetailed(ctx, noToken)
	if err != nil {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id": conditionID,
			"no_token_id":  noToken,
			"stage":        "get_book",
			"reason":       skipNoTwoSidedBook.String(),
			"error":        err.Error(),
		})
		return
	}
	mid, midReason := computeNoMidpoint(book)
	haveMidpoint := midReason == ""

	var tickSize float64
	haveTick := false
	if v := float64(book.TickSize); v > 0 {
		tickSize = v
		haveTick = true
	}

	// 2. Re-run the FULL eligibility check against the FRESH midpoint. A market that
	// drifted out of band (midpoint <= MinNoMidpoint or > MaxNoMidpoint, etc.) since
	// discovery places nothing — log the deciding reason and skip ordering.
	if reason := isMarketEligible(m.Market, a.now(), a.cfg, owned, m.Tokens, "", mid, haveMidpoint); reason != "" {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id": conditionID,
			"no_token_id":  noToken,
			"stage":        "eligibility",
			"reason":       reason.String(),
		})
		return
	}

	// 3. Resolve the venue minimum order size from the fresh book. Undeterminable
	// skips this market.
	minOrderSize, _, minReason := a.resolveMinOrderSize(ctx, logger, conditionID, book)
	if minReason != "" {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id": conditionID,
			"no_token_id":  noToken,
			"stage":        "min_order_size",
			"reason":       minReason.String(),
		})
		return
	}

	// 4. Size against the snapshot Total. committed = held NO shares ONLY (the order
	// being reconciled provides the rest); eligible markets never own YES.
	held := heldNoShares(positions, noToken)
	decision := sizeMarket(snapshot.Total, mid.Midpoint, minOrderSize, held, false, a.cfg)

	// 5. Plan funding against the budget's remaining balance WITHOUT reserving. The
	// reservation happens once on the maintain/place branch only.
	plan := planFunding(decision, mid.Midpoint, minOrderSize, budget.Remaining())

	// 6. Desired price and GTD expiration. Without a known tick the price is not on a
	// provable grid, so skip ordering rather than place on an unknown grid.
	if !haveTick {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id": conditionID,
			"no_token_id":  noToken,
			"stage":        "tick_size",
			"reason":       "venue_tick_undeterminable",
		})
		return
	}
	desiredPrice := normalizePrice(mid.Midpoint, tickSize)
	desiredExpiry := m.CloseAt.Add(-a.cfg.OrderExpiryBeforeClose).Unix()

	// The market's existing NO-buy orders (right side + right asset). These are the
	// candidates to maintain, cancel, or replace.
	existing := existingNoBuyOrders(marketOrders, noToken)

	// 7. Skip branch: no order is wanted. Cancel every existing NO-buy order (none is
	// desired) and reserve nothing.
	if plan.Skip {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id": conditionID,
			"no_token_id":  noToken,
			"stage":        "funding",
			"reason":       plan.Reason.String(),
		})
		for _, o := range existing {
			a.reconcileCancel(ctx, logger, conditionID, o, "no_order_wanted")
		}
		return
	}

	// An order is wanted. Reject a non-future expiration before any place/maintain.
	if desiredExpiry <= a.now().Unix() {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id": conditionID,
			"no_token_id":  noToken,
			"stage":        "expiration",
			"reason":       "expiration_not_in_future",
			"expiration":   desiredExpiry,
		})
		for _, o := range existing {
			a.reconcileCancel(ctx, logger, conditionID, o, "expiration_not_in_future")
		}
		return
	}

	// 8. Reason about size on the venue's FLOOR grid: the client truncates size to
	// two decimals when building the order, so the venue stores (and reports) the
	// floored value. Computing the desired size with the same floor makes the value we
	// place, compare, and reserve byte-identical to the venue's, which is what keeps
	// reconciliation idempotent for non-round share counts (e.g. 200/0.94).
	desiredShares := floorToSizePrecision(plan.Shares)

	// Flooring must not silently place a sub-minimum order. planFunding guarantees
	// plan.Shares >= minOrderSize, but the floor can drop a value sitting exactly on
	// the minimum below it; if so, fail closed (skip placement) rather than submit an
	// order the venue would reject.
	if desiredShares < minOrderSize {
		logger.Event("reconcile_skip", map[string]any{
			"condition_id":   conditionID,
			"no_token_id":    noToken,
			"stage":          "size_floor",
			"reason":         "floored_below_min_order_size",
			"desired_shares": desiredShares,
			"min_order_size": minOrderSize,
		})
		for _, o := range existing {
			a.reconcileCancel(ctx, logger, conditionID, o, "floored_below_min_order_size")
		}
		return
	}

	// Look for an existing order matching the desired order exactly: side BUY,
	// asset == NO token, on-tick price == desired price, parsed expiration ==
	// desired expiration, and floored remaining size == floored desired size.
	matchIdx := -1
	for i, o := range existing {
		if orderMatchesDesired(o, noToken, desiredPrice, desiredExpiry, desiredShares, tickSize) {
			matchIdx = i
			break
		}
	}

	if matchIdx >= 0 {
		// Maintain the matching order: reserve its notional (intentionally left open),
		// log it unchanged, and cancel every OTHER existing NO-buy order so exactly one
		// remains.
		budget.reserve(plan.Notional)
		a.logReconcileAction(logger, "maintained", m, noToken, mid.Midpoint, plan, desiredShares, desiredPrice, desiredExpiry, budget)
		for i, o := range existing {
			if i == matchIdx {
				continue
			}
			a.reconcileCancel(ctx, logger, conditionID, o, "duplicate_order")
		}
		return
	}

	// 9. No match: cancel-replace on divergence. Cancel all existing NO-buy orders,
	// then place a fresh order at the desired price/size/expiration.
	for _, o := range existing {
		a.reconcileCancel(ctx, logger, conditionID, o, "diverged")
	}
	a.reconcilePlace(ctx, logger, m, noToken, mid.Midpoint, plan, desiredShares, desiredPrice, desiredExpiry, budget)
}

// existingNoBuyOrders filters a market's orders to its open NO-buy orders: side BUY
// (case-insensitive) on the market's NO token. These are the only orders the
// reconciliation pass maintains, cancels, or replaces.
func existingNoBuyOrders(orders []polymarket.Order, noTokenID string) []polymarket.Order {
	var out []polymarket.Order
	for _, o := range orders {
		if !strings.EqualFold(strings.TrimSpace(o.Side), "BUY") {
			continue
		}
		if o.AssetID != noTokenID {
			continue
		}
		out = append(out, o)
	}
	return out
}

// orderMatchesDesired reports whether an open order already IS the desired order:
// BUY side on the NO token, on-tick price equal to the desired price on the venue
// tick, parsed expiration equal to the desired GTD expiration, and the floored
// remaining size (OriginalSize - SizeMatched) equal to the (already-floored) desired
// size. Price equality is on the venue tick grid; size equality is on the venue's
// two-decimal FLOOR grid — desiredShares MUST already be floorToSizePrecision'd by
// the caller so both sides compare on the grid the venue actually stored; expiration
// equality is exact unix seconds. A match means the order is left unchanged.
func orderMatchesDesired(o polymarket.Order, noTokenID string, desiredPrice float64, desiredExpiry int64, desiredShares, tickSize float64) bool {
	if !strings.EqualFold(strings.TrimSpace(o.Side), "BUY") {
		return false
	}
	if o.AssetID != noTokenID {
		return false
	}

	price, err := strconv.ParseFloat(strings.TrimSpace(o.Price), 64)
	if err != nil || !pricesEqual(price, desiredPrice, tickSize) {
		return false
	}

	exp, err := strconv.ParseInt(strings.TrimSpace(o.Expiration), 10, 64)
	if err != nil || exp != desiredExpiry {
		return false
	}

	original, err := strconv.ParseFloat(strings.TrimSpace(o.OriginalSize), 64)
	if err != nil {
		return false
	}
	matched := 0.0
	if s := strings.TrimSpace(o.SizeMatched); s != "" {
		matched, err = strconv.ParseFloat(s, 64)
		if err != nil {
			return false
		}
	}
	remaining := floorToSizePrecision(original - matched)
	return remaining == desiredShares
}

// reconcilePlace places a fresh NO buy order via PlaceOrderWithExpiration and, on
// success, reserves its notional. desiredShares is the venue-floored size (so the
// placed value is byte-identical to what the venue stores); plan.Notional is kept for
// the budget reservation and the log — the sub-0.01-share floor difference is
// immaterial to the budget. In dry-run it logs the intended placement with full
// detail, reserves the local budget, and calls no client method. A place error (or an
// unsuccessful response) is logged and isolated to this market.
func (a *App) reconcilePlace(
	ctx context.Context,
	logger *Logger,
	m eligibleMarket,
	noToken string,
	midpoint float64,
	plan fundingResult,
	desiredShares float64,
	desiredPrice float64,
	desiredExpiry int64,
	budget *runBudget,
) {
	if a.cfg.DryRun {
		// Reserve from the LOCAL budget so subsequent markets see realistic numbers.
		budget.reserve(plan.Notional)
		a.logReconcileAction(logger, "would_place", m, noToken, midpoint, plan, desiredShares, desiredPrice, desiredExpiry, budget)
		return
	}

	resp, err := a.trading.PlaceOrderWithExpiration(ctx, noToken, desiredPrice, desiredShares, polymarket.Buy, desiredExpiry)
	if err != nil {
		logger.Event("reconcile_order", map[string]any{
			"condition_id": m.Market.ConditionID,
			"no_token_id":  noToken,
			"status":       "place_failed",
			"error":        err.Error(),
		})
		return
	}
	if resp == nil || !resp.Success {
		msg := ""
		if resp != nil {
			msg = resp.ErrorMsg
		}
		logger.Event("reconcile_order", map[string]any{
			"condition_id": m.Market.ConditionID,
			"no_token_id":  noToken,
			"status":       "place_rejected",
			"error":        msg,
		})
		return
	}

	budget.reserve(plan.Notional)
	a.logReconcilePlaced(logger, m, noToken, midpoint, plan, desiredShares, desiredPrice, desiredExpiry, budget, resp.OrderID)
}

// reconcileCancel logs and (outside dry-run) submits a single cancellation, isolating
// any error to this market. In dry-run it logs the intended cancellation and calls
// CancelOrder zero times.
func (a *App) reconcileCancel(ctx context.Context, logger *Logger, conditionID string, o polymarket.Order, reason string) {
	fields := map[string]any{
		"condition_id": conditionID,
		"order_id":     o.ID,
		"asset_id":     o.AssetID,
		"reason":       reason,
	}
	if a.cfg.DryRun {
		fields["status"] = "would_cancel"
		logger.Event("reconcile_order", fields)
		return
	}
	if err := a.trading.CancelOrder(ctx, o.ID); err != nil {
		fields["status"] = "cancel_failed"
		fields["error"] = err.Error()
		logger.Event("reconcile_order", fields)
		return
	}
	fields["status"] = "canceled"
	logger.Event("reconcile_order", fields)
}

// logReconcileAction emits a place/maintain decision log with the full detail set:
// condition id, question, NO token, midpoint, shares, notional, close time,
// expiration, the run USDC remaining AFTER reservation, and the min-order-exception /
// partial-fill flags. status names the action (maintained / would_place).
func (a *App) logReconcileAction(
	logger *Logger,
	status string,
	m eligibleMarket,
	noToken string,
	midpoint float64,
	plan fundingResult,
	shares float64,
	desiredPrice float64,
	desiredExpiry int64,
	budget *runBudget,
) {
	logger.Event("reconcile_order", a.reconcileFields(status, m, noToken, midpoint, plan, shares, desiredPrice, desiredExpiry, budget, ""))
}

// logReconcilePlaced emits a successful live-placement log, carrying the same full
// detail set plus the venue order ID.
func (a *App) logReconcilePlaced(
	logger *Logger,
	m eligibleMarket,
	noToken string,
	midpoint float64,
	plan fundingResult,
	shares float64,
	desiredPrice float64,
	desiredExpiry int64,
	budget *runBudget,
	orderID string,
) {
	logger.Event("reconcile_order", a.reconcileFields("placed", m, noToken, midpoint, plan, shares, desiredPrice, desiredExpiry, budget, orderID))
}

// reconcileFields builds the structured field set shared by every place/maintain log
// line so the detail set stays consistent across actions.
func (a *App) reconcileFields(
	status string,
	m eligibleMarket,
	noToken string,
	midpoint float64,
	plan fundingResult,
	shares float64,
	desiredPrice float64,
	desiredExpiry int64,
	budget *runBudget,
	orderID string,
) map[string]any {
	fields := map[string]any{
		"condition_id": m.Market.ConditionID,
		"question":     m.Market.Question,
		"no_token_id":  noToken,
		"status":       status,
		"midpoint":     midpoint,
		"price":        desiredPrice,
		// shares/notional reflect the venue-floored order actually submitted.
		"shares":              shares,
		"notional":            shares * desiredPrice,
		"close_at":            m.CloseAt.Format(time.RFC3339),
		"expiration":          desiredExpiry,
		"run_usdc_remaining":  budget.Remaining(),
		"min_order_exception": plan.MinOrderException,
		"partial_fill":        plan.PartialFill,
	}
	if orderID != "" {
		fields["order_id"] = orderID
	}
	return fields
}
