package main

import (
	"context"
	"sort"
	"strconv"
	"strings"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// stalePass cancels clearly-stale open orders as the second trading step of a run,
// after redemption and before any account-value snapshot / discovery / reconcile
// pass. Cancelling here means the later reconciliation pass sees a cleaned
// open-order set. Per-market failures (fetch or cancel) are logged and isolated:
// one bad market never stops cancellations for unrelated markets. In dry-run mode
// it logs every intended cancellation and submits nothing. It always returns nil:
// a cancellation problem must not stop the rest of the run.
//
// Cancellation never depends on order amount (size) — size reconciliation is a
// later rung. An order matching side+asset+normalized-price+expiration for an
// eligible market is KEPT even if its amount differs from any desired size.
func (a *App) stalePass(ctx context.Context, logger *Logger) error {
	orders, err := a.trading.GetOrders(ctx, "")
	if err != nil {
		logger.Event("stale_error", map[string]any{"error": err.Error()})
		return nil
	}

	// Group orders by their market (condition ID) so each market is evaluated once
	// and duplicate detection is scoped to a single market. Iterate markets in a
	// deterministic (sorted) order for reproducible logs.
	byMarket := map[string][]polymarket.Order{}
	for _, o := range orders {
		byMarket[o.Market] = append(byMarket[o.Market], o)
	}
	markets := make([]string, 0, len(byMarket))
	for m := range byMarket {
		markets = append(markets, m)
	}
	sort.Strings(markets)

	logger.Event("stale_scan", map[string]any{
		"open_orders": len(orders),
		"markets":     len(markets),
		"dry_run":     a.cfg.DryRun,
	})

	for _, conditionID := range markets {
		a.staleCancelMarket(ctx, logger, conditionID, byMarket[conditionID])
	}
	return nil
}

// marketCancelContext is the per-market state the cancel decision needs: the
// market-level ineligibility reason (empty when eligible), whether the account
// owns YES shares for it, the market's NO token ID, the normalized NO midpoint and
// its tick size, the expected order expiration, and whether the venue tick is
// known. It is derived once per market and reused for every order on it.
type marketCancelContext struct {
	ineligible     skipReason
	yesOwned       bool
	noTokenID      string
	midpoint       float64
	haveMidpoint   bool
	tickSize       float64
	haveTick       bool
	expectedExpiry int64
}

// orderVerdict is the outcome of classifying a single open order.
type orderVerdict int

const (
	// verdictCancel: the order is provably stale and must be canceled.
	verdictCancel orderVerdict = iota
	// verdictCandidate: a fully-proven matching NO-buy order (side, asset,
	// normalized price, expiration all verified). Dedup-eligible: at most one
	// survives, the rest are canceled as duplicates.
	verdictKeepCandidate
	// verdictKeepUnverifiable: passes side/asset/expiration but the price could
	// not be proven (venue tick or midpoint unknown). Fail closed — keep it and do
	// NOT cancel it as a duplicate, since we cannot prove it stale.
	verdictKeepUnverifiable
)

// staleCancelMarket evaluates and cancels stale orders for a single market.
// A market or book fetch failure aborts cancellations for THIS market only and is
// logged: we fail closed and never cancel an order we cannot fully evaluate.
func (a *App) staleCancelMarket(ctx context.Context, logger *Logger, conditionID string, orders []polymarket.Order) {
	mctx, ok := a.buildMarketCancelContext(ctx, logger, conditionID)
	if !ok {
		// Could not evaluate the market — skip its orders rather than cancel on
		// uncertainty. The failure was already logged by the builder.
		return
	}

	// Track NO-buy orders that match every order-level criterion (right side,
	// asset, normalized price, expiration). At most one such order survives as the
	// reconciliation candidate; any extras are canceled as duplicates. The survivor
	// is chosen deterministically as the lowest order ID.
	var candidates []polymarket.Order

	for _, o := range orders {
		reason, underlying, verdict := a.classifyOrder(o, mctx)
		switch verdict {
		case verdictKeepCandidate:
			candidates = append(candidates, o)
		case verdictKeepUnverifiable:
			// Cannot prove the price stale (unknown tick/midpoint); keep it and do
			// not subject it to duplicate resolution.
			logger.Event("stale_order", map[string]any{
				"condition_id": conditionID,
				"order_id":     o.ID,
				"asset_id":     o.AssetID,
				"status":       "kept_unverified",
			})
		default:
			a.cancelOrder(ctx, logger, conditionID, o, reason, underlying)
		}
	}

	if len(candidates) == 0 {
		return
	}

	// Deterministic survivor: the lowest order ID. Sort a copy so logging order is
	// stable across runs regardless of the input order.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	survivor := candidates[0]
	logger.Event("stale_order", map[string]any{
		"condition_id": conditionID,
		"order_id":     survivor.ID,
		"asset_id":     survivor.AssetID,
		"status":       "kept",
	})
	for _, dup := range candidates[1:] {
		a.cancelOrder(ctx, logger, conditionID, dup, cancelDuplicateOrder, "")
	}
}

// buildMarketCancelContext fetches the Gamma market, decodes its binary tokens,
// fetches the NO book + midpoint, and fetches positions to derive the per-market
// cancel context. It returns ok=false (and logs) when any required fetch fails or
// the market is not a decodable binary market for the purpose of locating its NO
// token — fail closed so the caller skips the market's orders. A market-level
// eligibility reason (closed/inactive/etc.) does NOT make ok=false: the market was
// evaluated successfully and its orders should be canceled.
func (a *App) buildMarketCancelContext(ctx context.Context, logger *Logger, conditionID string) (marketCancelContext, bool) {
	market, err := a.trading.GetMarket(ctx, conditionID)
	if err != nil {
		logger.Event("stale_error", map[string]any{
			"condition_id": conditionID,
			"stage":        "get_market",
			"error":        err.Error(),
		})
		return marketCancelContext{}, false
	}

	tokens, tokensReason := decodeBinaryTokens(*market)

	// Fetch the NO book once for both the midpoint and the venue tick size. The book
	// is only meaningful when tokens decoded; for a non-binary market we cannot
	// locate a NO token, so we skip the book and let the eligibility predicate
	// surface the decode reason as a market-level cancel.
	var mid noMidpoint
	haveMidpoint := false
	var tickSize float64
	haveTick := false
	if tokensReason == "" {
		book, err := a.trading.GetOrderBookDetailed(ctx, tokens.NoTokenID)
		if err != nil {
			// Cannot evaluate price staleness without the book; skip this market's
			// orders rather than cancel on uncertainty.
			logger.Event("stale_error", map[string]any{
				"condition_id": conditionID,
				"stage":        "get_book",
				"error":        err.Error(),
			})
			return marketCancelContext{}, false
		}
		var midReason skipReason
		mid, midReason = computeNoMidpoint(book)
		haveMidpoint = midReason == ""
		if v := float64(book.TickSize); v > 0 {
			tickSize = v
			haveTick = true
		}
	}

	positions, err := a.trading.GetPositions(ctx)
	if err != nil {
		logger.Event("stale_error", map[string]any{
			"condition_id": conditionID,
			"stage":        "get_positions",
			"error":        err.Error(),
		})
		return marketCancelContext{}, false
	}
	owned := ownedTokenIDs(positions)

	now := a.now()
	ineligible := isMarketEligible(*market, now, a.cfg, owned, tokens, tokensReason, mid, haveMidpoint)

	// A YES-owned market is reported by the predicate as skipYesSharesOwned, but it
	// is a distinct cancel reason: surface it separately so the order log states the
	// account holds YES rather than a generic market fault.
	yesOwned := tokensReason == "" && owned[tokens.YesTokenID]

	mctx := marketCancelContext{
		ineligible:   ineligible,
		yesOwned:     yesOwned,
		noTokenID:    tokens.NoTokenID,
		haveMidpoint: haveMidpoint,
		haveTick:     haveTick,
		tickSize:     tickSize,
	}
	if haveMidpoint {
		mctx.midpoint = mid.Midpoint
	}

	// Expected expiration = close_time - OrderExpiryBeforeClose, in unix seconds.
	// Only computed when the close time parses; an unparseable close time is a
	// market-level ineligibility (skipEndDate*) so orders are canceled regardless.
	if closeAt, ok := parseEndDate(market.EndDate); ok {
		mctx.expectedExpiry = closeAt.Add(-a.cfg.OrderExpiryBeforeClose).Unix()
	}

	return mctx, true
}

// classifyOrder applies the order-level cancel criteria in a fixed, deterministic
// order. It returns the deciding cancelReason and an optional underlying reason
// string (the market-level skipReason for cancelMarketIneligible), plus
// matches=true when the order is a fully-matching NO-buy candidate that must be
// KEPT for the reconciliation pass (subject to duplicate resolution by the caller).
//
// Criteria order (first match wins):
//  1. market-level ineligibility (closed/inactive/etc., or non-binary)
//  2. account owns YES shares for the market
//  3. wrong side (not BUY)
//  4. wrong asset (not the NO token)
//  5. normalized price != normalized midpoint
//  6. expiration != close - OrderExpiryBeforeClose
//
// Amount (size) is deliberately never consulted: it is reconciled in a later rung.
func (a *App) classifyOrder(o polymarket.Order, mctx marketCancelContext) (cancelReason, string, orderVerdict) {
	// 1. Market-level ineligibility (excluding the YES-owned case, which we report
	// as its own reason below). A non-binary market surfaces here too.
	if mctx.ineligible != "" && mctx.ineligible != skipYesSharesOwned {
		return cancelMarketIneligible, mctx.ineligible.String(), verdictCancel
	}

	// 2. YES shares owned for this market.
	if mctx.yesOwned {
		return cancelYesSharesOwned, "", verdictCancel
	}

	// 3. Wrong side: only NO BUY orders are part of the strategy.
	if !strings.EqualFold(strings.TrimSpace(o.Side), "BUY") {
		return cancelWrongSide, "", verdictCancel
	}

	// 4. Wrong asset: the order must be on the market's NO token.
	if o.AssetID != mctx.noTokenID {
		return cancelWrongAsset, "", verdictCancel
	}

	// 5. Normalized price must equal the normalized NO midpoint — but only when the
	// price is provable: both the venue tick AND a usable midpoint are known. When
	// either is unknown we cannot prove the price is stale, so we do NOT cancel on a
	// price mismatch (fail closed).
	priceProvable := mctx.haveTick && mctx.haveMidpoint
	if priceProvable {
		price, err := strconv.ParseFloat(strings.TrimSpace(o.Price), 64)
		if err != nil || !pricesEqual(price, mctx.midpoint, mctx.tickSize) {
			return cancelPriceMismatch, "", verdictCancel
		}
	}

	// 6. Expiration must equal close_time - OrderExpiryBeforeClose. The close time is
	// always known for an eligible market (an unparseable close time is a market-level
	// ineligibility handled at criterion 1), so expiration is always provable here.
	exp, err := strconv.ParseInt(strings.TrimSpace(o.Expiration), 10, 64)
	if err != nil || exp != mctx.expectedExpiry {
		return cancelExpirationMismatch, "", verdictCancel
	}

	// Order passes every provable criterion. If the price was proven to match it is a
	// dedup-eligible reconciliation candidate; if the price was unprovable we keep it
	// but must not cancel it as a duplicate (we cannot prove it stale).
	if priceProvable {
		return "", "", verdictKeepCandidate
	}
	return "", "", verdictKeepUnverifiable
}

// cancelOrder logs a single cancellation decision and, outside dry-run, submits the
// cancel. A cancel error is logged and isolated — it never aborts the surrounding
// loop. In dry-run mode it logs the intended cancellation and calls CancelOrder
// zero times.
func (a *App) cancelOrder(ctx context.Context, logger *Logger, conditionID string, o polymarket.Order, reason cancelReason, underlying string) {
	fields := map[string]any{
		"condition_id": conditionID,
		"order_id":     o.ID,
		"asset_id":     o.AssetID,
		"side":         o.Side,
		"reason":       reason.String(),
	}
	if underlying != "" {
		fields["market_reason"] = underlying
	}

	if a.cfg.DryRun {
		fields["status"] = "would_cancel"
		logger.Event("stale_order", fields)
		return
	}

	if err := a.trading.CancelOrder(ctx, o.ID); err != nil {
		fields["status"] = "failed"
		fields["error"] = err.Error()
		logger.Event("stale_order", fields)
		return
	}

	fields["status"] = "canceled"
	logger.Event("stale_order", fields)
}
