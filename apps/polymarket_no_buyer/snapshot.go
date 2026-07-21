package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// accountSnapshot is the resolved account value at a point in a run: the
// spendable USDC cash held in the Polygon wallet, the mark-to-book value of all
// owned positions, and their sum. Later rungs size NO-buy orders against Total.
type accountSnapshot struct {
	WalletUSDC           float64
	PositionsValue       float64
	PositionsValueSource string
	Total                float64
}

// priceSource names how a single share was valued, for the structured log.
type priceSource string

const (
	priceSourceMidpoint     priceSource = "midpoint"      // token-book midpoint fallback
	priceSourceBestBid      priceSource = "best_bid"      // best (highest) bid fallback
	priceSourceZero         priceSource = "zero"          // no usable price; valued at zero
	priceSourceCurrentValue priceSource = "current_value" // Data API currentValue/curPrice
	priceSourceValueAPI     priceSource = "value_api"     // Data API aggregate /value
)

type positionsValueClient interface {
	GetPositionsValue(ctx context.Context) (float64, error)
}

// snapshotAccountValue computes the account-value snapshot. The wallet USDC cash
// balance is sourced SOLELY from the Polygon wallet helper against the configured
// USDC token address — never from any Polymarket/CLOB exchange available-balance
// field. Owned positions are valued with Polymarket's own Data API mark first:
// aggregate /value when the client exposes it, then per-position currentValue or
// curPrice, then local token midpoint, then best bid, then zero.
//
// It aborts (returns an error) ONLY when the wallet balance is uncomputable (the
// helper errors or the balance string is unparseable) or when GetPositions
// errors — in those cases Total cannot be trusted, so the run must not proceed.
// A per-position book-fetch error never aborts: that position is valued at zero
// and logged. The snapshot mutates no account state.
func (a *App) snapshotAccountValue(ctx context.Context, logger *Logger) (accountSnapshot, error) {
	walletUSDC, err := a.walletUSDCBalance(ctx, logger)
	if err != nil {
		return accountSnapshot{}, err
	}

	positions, err := a.trading.GetPositions(ctx)
	if err != nil {
		logger.Event("account_value", map[string]any{
			"status": "aborted",
			"stage":  "get_positions",
			"reason": err.Error(),
		})
		return accountSnapshot{}, fmt.Errorf("snapshot positions fetch failed: %w", err)
	}

	positionsValue, positionsValueSource := a.valuePositions(ctx, logger, positions)

	snap := accountSnapshot{
		WalletUSDC:           walletUSDC,
		PositionsValue:       positionsValue,
		PositionsValueSource: positionsValueSource,
		Total:                walletUSDC + positionsValue,
	}

	logger.Event("account_value", map[string]any{
		"status":                 "ok",
		"wallet_usdc":            snap.WalletUSDC,
		"positions_value":        snap.PositionsValue,
		"positions_value_source": snap.PositionsValueSource,
		"total":                  snap.Total,
	})
	return snap, nil
}

// walletUSDCBalance fetches the spendable USDC cash balance from the Polygon
// wallet helper against the configured USDC token and parses the human-readable
// decimal balance string. Any helper error or unparseable balance is fatal for
// the snapshot — there is no exchange/CLOB substitute and no default.
func (a *App) walletUSDCBalance(ctx context.Context, logger *Logger) (float64, error) {
	bal, err := a.wallet.GetTokenBalance(ctx, gowild_crypto.ChainEthereum, a.cfg.USDCTokenAddress)
	if err != nil {
		logger.Event("account_value", map[string]any{
			"status":             "aborted",
			"stage":              "wallet_usdc",
			"usdc_token_address": a.cfg.USDCTokenAddress,
			"reason":             err.Error(),
		})
		return 0, fmt.Errorf("snapshot wallet USDC balance fetch failed: %w", err)
	}

	raw := strings.TrimSpace(bal.Balance)
	usdc, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		logger.Event("account_value", map[string]any{
			"status":             "aborted",
			"stage":              "wallet_usdc",
			"usdc_token_address": a.cfg.USDCTokenAddress,
			"reason":             fmt.Sprintf("unparseable wallet balance %q: %v", bal.Balance, err),
		})
		return 0, fmt.Errorf("snapshot wallet USDC balance %q unparseable: %w", bal.Balance, err)
	}
	return usdc, nil
}

func (a *App) valuePositions(ctx context.Context, logger *Logger, positions []polymarket.Position) (float64, string) {
	if vc, ok := a.trading.(positionsValueClient); ok {
		value, err := vc.GetPositionsValue(ctx)
		if err == nil && usableAggregatePositionValue(value, positions) {
			return value, string(priceSourceValueAPI)
		}
		fields := map[string]any{
			"status":       "fallback",
			"price_source": string(priceSourceValueAPI),
		}
		if err != nil {
			fields["reason"] = err.Error()
		} else {
			fields["reason"] = fmt.Sprintf("unusable aggregate position value %v", value)
		}
		logger.Event("positions_value_source", fields)
	}

	positionsValue := 0.0
	source := string(priceSourceCurrentValue)
	usedBook := false
	for _, p := range positions {
		if p.Size <= 0 {
			continue
		}
		if currentValue, ok := dataAPIPositionValue(p); ok {
			positionsValue += currentValue
			continue
		}
		usedBook = true
		price := a.valuePositionShare(ctx, logger, p)
		positionsValue += p.Size * price
	}
	if usedBook {
		source = "current_value_or_book"
	}
	return positionsValue, source
}

func usableAggregatePositionValue(value float64, positions []polymarket.Position) bool {
	if !usablePositionValue(value) {
		return false
	}
	if value > 0 {
		return true
	}
	for _, p := range positions {
		if p.Size > 0 {
			return false
		}
	}
	return true
}

func usablePositionValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func dataAPIPositionValue(p polymarket.Position) (float64, bool) {
	if usablePositionValue(p.CurrentValue) && p.CurrentValue > 0 {
		return p.CurrentValue, true
	}
	if p.Size <= 0 {
		return 0, false
	}
	if usablePositionValue(p.CurPrice) && p.CurPrice > 0 {
		return p.Size * p.CurPrice, true
	}
	if usablePositionValue(p.CurrPrice) && p.CurrPrice > 0 {
		return p.Size * p.CurrPrice, true
	}
	return 0, false
}

// valuePositionShare prices a single share of an owned position via the strict
// fallback chain: (1) the position token's book midpoint when a usable two-sided
// book exists; (2) else the best (highest) bid; (3) else zero. Every best-bid and
// zero fallback is logged with the condition ID and token. A book-fetch error for
// one position values it at zero (logged); it never aborts the snapshot.
func (a *App) valuePositionShare(ctx context.Context, logger *Logger, p polymarket.Position) float64 {
	book, err := a.trading.GetOrderBookDetailed(ctx, p.Asset)
	if err != nil {
		a.logShareFallback(logger, p, priceSourceZero, 0, "book_fetch_error", err.Error())
		return 0
	}

	if mid, reason := computeNoMidpoint(book); reason == "" {
		// Usable two-sided book: midpoint is the first local fallback when
		// Polymarket's Data API mark is unavailable.
		return mid.Midpoint
	}

	if bid, ok := bestBidPrice(book.Bids); ok {
		a.logShareFallback(logger, p, priceSourceBestBid, bid, "no_usable_midpoint", "")
		return bid
	}

	a.logShareFallback(logger, p, priceSourceZero, 0, "no_usable_price", "")
	return 0
}

// logShareFallback emits a structured per-position valuation log line for a
// best-bid or zero fallback, recording the condition ID, token, chosen source,
// price, and the deciding condition.
func (a *App) logShareFallback(logger *Logger, p polymarket.Position, source priceSource, price float64, condition, fetchErr string) {
	fields := map[string]any{
		"event_kind":   "position_value_fallback",
		"condition_id": p.ConditionID,
		"token":        p.Asset,
		"price_source": string(source),
		"price":        price,
		"condition":    condition,
	}
	if fetchErr != "" {
		fields["fetch_error"] = fetchErr
	}
	logger.Event("position_value", fields)
}
