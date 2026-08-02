package main

import (
	"context"
	"strconv"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// noMidpoint is the two-sided NO quote derived from the live order book: the
// best (highest) NO bid, the best (lowest) NO ask, and their midpoint. It is
// only produced when the book is genuinely two-sided.
type noMidpoint struct {
	BestNoBid float64
	BestNoAsk float64
	Midpoint  float64
}

// yesMidpoint is the two-sided YES quote used to price the reversed order. The
// strategy still qualifies markets with noMidpoint, then executes on this quote.
type yesMidpoint struct {
	BestYesBid float64
	BestYesAsk float64
	Midpoint   float64
}

// bestBidPrice returns the highest bid price in the book. Order-book ordering is
// not guaranteed, so the best bid is computed as the MAX over all bid prices.
// The bool is false when there is no parseable bid (empty side or all entries
// unparseable).
func bestBidPrice(bids []polymarket.OrderBookEntry) (float64, bool) {
	best := 0.0
	found := false
	for _, b := range bids {
		price, err := strconv.ParseFloat(b.Price, 64)
		if err != nil {
			continue
		}
		if !found || price > best {
			best = price
			found = true
		}
	}
	return best, found
}

// bestAskPrice returns the lowest ask price in the book. Order-book ordering is
// not guaranteed, so the best ask is computed as the MIN over all ask prices.
// The bool is false when there is no parseable ask (empty side or all entries
// unparseable).
func bestAskPrice(asks []polymarket.OrderBookEntry) (float64, bool) {
	best := 0.0
	found := false
	for _, a := range asks {
		price, err := strconv.ParseFloat(a.Price, 64)
		if err != nil {
			continue
		}
		if !found || price < best {
			best = price
			found = true
		}
	}
	return best, found
}

// computeNoMidpoint derives the NO midpoint from a detailed order book. It
// returns a non-empty skipReason ("no usable two-sided NO order book") instead
// of a midpoint when the book is nil, empty, or one-sided (missing best bid or
// best ask) — the app must never act on a half-quoted book.
func computeNoMidpoint(book *polymarket.OrderBookDetail) (noMidpoint, skipReason) {
	if book == nil {
		return noMidpoint{}, skipNoTwoSidedBook
	}
	bid, haveBid := bestBidPrice(book.Bids)
	ask, haveAsk := bestAskPrice(book.Asks)
	if !haveBid || !haveAsk {
		return noMidpoint{}, skipNoTwoSidedBook
	}
	return noMidpoint{
		BestNoBid: bid,
		BestNoAsk: ask,
		Midpoint:  (bid + ask) / 2,
	}, ""
}

// computeYesMidpoint derives the YES midpoint from the YES token's detailed
// order book. Like computeNoMidpoint, it requires a genuinely two-sided book.
func computeYesMidpoint(book *polymarket.OrderBookDetail) (yesMidpoint, skipReason) {
	if book == nil {
		return yesMidpoint{}, skipYesTwoSidedBook
	}
	bid, haveBid := bestBidPrice(book.Bids)
	ask, haveAsk := bestAskPrice(book.Asks)
	if !haveBid || !haveAsk {
		return yesMidpoint{}, skipYesTwoSidedBook
	}
	return yesMidpoint{
		BestYesBid: bid,
		BestYesAsk: ask,
		Midpoint:   (bid + ask) / 2,
	}, ""
}

// fetchNoMidpoint fetches the NO token's order book via the trading client and
// computes its midpoint, emitting a structured per-market log line. It returns a
// non-empty skipReason when the book cannot be fetched or is not two-sided, in
// which case no midpoint is produced. It is deterministic given a fixed book and
// idempotent: repeated calls fetch fresh state but mutate nothing.
func (a *App) fetchNoMidpoint(ctx context.Context, logger *Logger, conditionID string, tokens binaryTokens) (noMidpoint, skipReason) {
	book, err := a.trading.GetOrderBookDetailed(ctx, tokens.NoTokenID)
	if err != nil {
		logger.Event("no_midpoint", map[string]any{
			"condition_id": conditionID,
			"yes_token_id": tokens.YesTokenID,
			"no_token_id":  tokens.NoTokenID,
			"status":       "skipped",
			"skip_reason":  skipNoTwoSidedBook.String(),
			"fetch_error":  err.Error(),
		})
		return noMidpoint{}, skipNoTwoSidedBook
	}

	mid, reason := computeNoMidpoint(book)
	if reason != "" {
		logger.Event("no_midpoint", map[string]any{
			"condition_id": conditionID,
			"yes_token_id": tokens.YesTokenID,
			"no_token_id":  tokens.NoTokenID,
			"status":       "skipped",
			"skip_reason":  reason.String(),
		})
		return noMidpoint{}, reason
	}

	logger.Event("no_midpoint", map[string]any{
		"condition_id": conditionID,
		"yes_token_id": tokens.YesTokenID,
		"no_token_id":  tokens.NoTokenID,
		"status":       "ok",
		"best_no_bid":  mid.BestNoBid,
		"best_no_ask":  mid.BestNoAsk,
		"no_midpoint":  mid.Midpoint,
	})
	return mid, ""
}
