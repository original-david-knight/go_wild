package main

import (
	"context"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Sources a resolved venue minimum order size can be attributed to. They mirror
// the precedence the resolver applies: the already-fetched execution order book wins
// over the clob-market mos, which in turn wins over the test-only fallback. Real
// venue data always beats the fallback so the app fails closed on bad config.
const (
	minOrderSourceOrderBook = "order_book"
	minOrderSourceClobMos   = "clob_markets_mos"
	minOrderSourceFallback  = "fallback"
)

// resolveMinOrderSize determines the per-market venue minimum order size and the
// source it was read from, applying a fail-closed precedence:
//
//  1. order_book      — the already-fetched YES book's min_order_size, if positive.
//     GetClobMarket is NOT called in this case.
//  2. clob_markets_mos — else GetClobMarket(conditionID).MinOrderSize(), if positive.
//  3. fallback         — else the test-only Config.MinOrderSizeFallback, but only
//     when MinOrderSizeFallbackSet is true and the value is positive.
//  4. undeterminable   — else a non-empty skipReason; the caller excludes the
//     market from live ordering.
//
// It emits one structured "min_order_size" event per market: a resolved line
// carrying the size and source, or a skipped line carrying the skip reason and
// condition_id when nothing is determinable. It returns (size, source, reason)
// where reason == "" means resolved. It is deterministic given fixed venue
// metadata and mutates nothing.
func (a *App) resolveMinOrderSize(ctx context.Context, logger *Logger, conditionID string, book *polymarket.OrderBookDetail) (float64, string, skipReason) {
	// 1. Prefer the already-fetched order book's min order size. A positive value
	// short-circuits so the clob-markets endpoint is never hit redundantly.
	if book != nil {
		if v := float64(book.MinOrderSize); v > 0 {
			a.logMinOrderSizeResolved(logger, conditionID, v, minOrderSourceOrderBook)
			return v, minOrderSourceOrderBook, ""
		}
	}

	// 2. Fall back to the clob-market mos. A fetch failure is fatal for this
	// market: we skip rather than guess a minimum.
	market, err := a.trading.GetClobMarket(ctx, conditionID)
	if err != nil {
		logger.Event("min_order_size", map[string]any{
			"condition_id": conditionID,
			"status":       "skipped",
			"skip_reason":  skipMinOrderSizeUndeterminable.String(),
			"fetch_error":  err.Error(),
		})
		return 0, "", skipMinOrderSizeUndeterminable
	}
	if v := market.MinOrderSize(); v > 0 {
		a.logMinOrderSizeResolved(logger, conditionID, v, minOrderSourceClobMos)
		return v, minOrderSourceClobMos, ""
	}

	// 3. Test-only fallback: honored only when explicitly set to a positive value.
	// Production leaves the fallback unset and falls through to fail closed.
	if a.cfg.MinOrderSizeFallbackSet && a.cfg.MinOrderSizeFallback > 0 {
		v := a.cfg.MinOrderSizeFallback
		a.logMinOrderSizeResolved(logger, conditionID, v, minOrderSourceFallback)
		return v, minOrderSourceFallback, ""
	}

	// 4. Undeterminable: fail closed and skip live ordering for this market.
	logger.Event("min_order_size", map[string]any{
		"condition_id": conditionID,
		"status":       "skipped",
		"skip_reason":  skipMinOrderSizeUndeterminable.String(),
	})
	return 0, "", skipMinOrderSizeUndeterminable
}

// logMinOrderSizeResolved emits the resolved "min_order_size" event carrying the
// per-market minimum and the source it was attributed to.
func (a *App) logMinOrderSizeResolved(logger *Logger, conditionID string, size float64, source string) {
	logger.Event("min_order_size", map[string]any{
		"condition_id":   conditionID,
		"status":         "ok",
		"min_order_size": size,
		"source":         source,
	})
}
