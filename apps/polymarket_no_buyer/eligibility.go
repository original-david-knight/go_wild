package main

import (
	"strconv"
	"strings"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Eligibility skip reasons. These extend the closed skipReason enum in tokens.go
// with the market-discovery rejection criteria evaluated by isMarketEligible.
// Token/outcome decode reasons (skipOutcomes*, skipTokenIDs*, skipNotYesNoLabels,
// etc.) and the NO order-book reason (skipNoTwoSidedBook) are reused as-is — the
// predicate propagates decodeBinaryTokens' reason and fetchNoMidpoint's reason
// directly rather than re-deriving them.
const (
	// Market-state skips: the market is not currently a tradeable, unresolved
	// binary market.
	skipNotActive          skipReason = "market_not_active"
	skipNotAcceptingOrders skipReason = "market_not_accepting_orders"
	skipMarketClosed       skipReason = "market_closed"

	// Topic skips: this strategy excludes whole market categories outright,
	// regardless of their economics. Currently crypto-price markets, stock-price
	// markets, and sports exact-score (scoreline) props (see category_filter.go);
	// more categories may be added later.
	skipCryptoMarket     skipReason = "crypto_market"
	skipStockMarket      skipReason = "stock_market"
	skipExactScoreMarket skipReason = "exact_score_market"

	// Close-time skips: the market's EndDate is missing/unparseable, already in
	// the past, or falls outside the [now+MinHoursToClose, now+MaxHoursToClose]
	// open interval.
	skipEndDateMissing  skipReason = "end_date_missing"
	skipEndDateUnparsed skipReason = "end_date_unparseable"
	skipCloseInPast     skipReason = "close_time_in_past"
	skipCloseTooSoon    skipReason = "close_time_too_soon"
	skipCloseTooFar     skipReason = "close_time_too_far"

	// Liquidity skips: liquidity missing/unparseable or below the configured
	// minimum.
	skipLiquidityMissing  skipReason = "liquidity_missing"
	skipLiquidityUnparsed skipReason = "liquidity_unparseable"
	skipLiquidityTooLow   skipReason = "liquidity_below_min"

	// Midpoint skips: the NO midpoint exists but is outside the configured
	// (MinNoMidpoint, MaxNoMidpoint] band.
	skipMidpointTooLow  skipReason = "no_midpoint_too_low"
	skipMidpointTooHigh skipReason = "no_midpoint_too_high"

	// Ownership skip: the account already holds NO shares for this market, so it
	// is not a YES-buying candidate.
	skipNoSharesOwned skipReason = "no_shares_owned"
)

// endDateLayouts are the accepted EndDate (endDateIso/endDate) string layouts, in
// the order they are tried. The Gamma API returns either a full RFC3339 timestamp
// (e.g. "2026-03-31T12:00:00Z") or a bare calendar date (e.g. "2026-03-31"); a
// bare date is interpreted as midnight UTC. Parsing fails closed if neither
// layout matches.
var endDateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02",
}

// parseEndDate parses a market's EndDate string into a UTC time. It accepts the
// layouts in endDateLayouts (RFC3339 with or without sub-seconds, and bare
// calendar dates as midnight UTC). It returns ok=false on an empty or
// unparseable value — the caller fails closed and skips the market rather than
// guessing a close time.
func parseEndDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range endDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// marketPreCheck runs every eligibility criterion that does NOT require the live
// order book — market state, binary structure, close-time window, liquidity, and
// NO ownership. Discovery runs this against every scanned market and only fetches
// the (expensive) order book for the few that pass, so a run does not make a
// network call per market. It returns skipReason("") when the cheap checks pass.
func marketPreCheck(
	m polymarket.Market,
	now time.Time,
	cfg *Config,
	ownedTokenIDs map[string]bool,
	tokens binaryTokens,
	tokensReason skipReason,
) skipReason {
	// 1. Market state: active, accepting orders, and not resolved.
	if !m.Active {
		return skipNotActive
	}
	if !m.AcceptingOrders {
		return skipNotAcceptingOrders
	}
	if m.Closed {
		return skipMarketClosed
	}

	// 2. Topic exclusion: this strategy never trades crypto-price, stock-price, or
	// sports exact-score (scoreline) markets. Checked before the structural/
	// economic criteria so the discovery breakdown attributes these to the topic
	// filter rather than to an incidental liquidity/close-window miss. Uses only
	// listing metadata — no network call.
	if marketLooksCrypto(m) {
		return skipCryptoMarket
	}
	if marketLooksStocks(m) {
		return skipStockMarket
	}
	if marketLooksExactScore(m) {
		return skipExactScoreMarket
	}

	// 3. Binary YES/NO with identifiable token IDs. Propagate the decode reason.
	if tokensReason != "" {
		return tokensReason
	}

	// 4. Close time: parseable, in the future, and within the configured window.
	if strings.TrimSpace(m.EndDate) == "" {
		return skipEndDateMissing
	}
	closeAt, ok := parseEndDate(m.EndDate)
	if !ok {
		return skipEndDateUnparsed
	}
	if !closeAt.After(now) {
		return skipCloseInPast
	}
	// Strictly greater than now+MinHoursToClose: close exactly at the boundary rejects.
	if !closeAt.After(now.Add(cfg.MinHoursToClose)) {
		return skipCloseTooSoon
	}
	// Strictly less than now+MaxHoursToClose: close exactly at the boundary rejects.
	if !closeAt.Before(now.Add(cfg.MaxHoursToClose)) {
		return skipCloseTooFar
	}

	// 5. Liquidity: parse the decimal string and require >= the configured minimum.
	rawLiquidity := strings.TrimSpace(m.Liquidity)
	if rawLiquidity == "" {
		return skipLiquidityMissing
	}
	liquidity, err := strconv.ParseFloat(rawLiquidity, 64)
	if err != nil {
		return skipLiquidityUnparsed
	}
	if liquidity < cfg.MinLiquidityUSD {
		return skipLiquidityTooLow
	}

	// 6. Ownership: the account must not already hold NO shares for this market.
	// YES holdings are part of the desired position and are handled by sizing.
	if ownedTokenIDs[tokens.NoTokenID] {
		return skipNoSharesOwned
	}

	return ""
}

// midpointInBand checks the NO-midpoint criterion: a usable two-sided book must
// exist and the midpoint must land in (MinNoMidpoint, MaxNoMidpoint].
func midpointInBand(mid noMidpoint, haveMidpoint bool, cfg *Config) skipReason {
	if !haveMidpoint {
		return skipNoTwoSidedBook
	}
	if mid.Midpoint <= cfg.MinNoMidpoint {
		return skipMidpointTooLow
	}
	if mid.Midpoint > cfg.MaxNoMidpoint {
		return skipMidpointTooHigh
	}
	return ""
}

// isMarketEligible is the full eligibility predicate (cheap checks plus the NO
// midpoint band). Reconcile uses it to re-verify a market it has already fetched
// the fresh book for.
func isMarketEligible(
	m polymarket.Market,
	now time.Time,
	cfg *Config,
	ownedTokenIDs map[string]bool,
	tokens binaryTokens,
	tokensReason skipReason,
	mid noMidpoint,
	haveMidpoint bool,
) skipReason {
	if r := marketPreCheck(m, now, cfg, ownedTokenIDs, tokens, tokensReason); r != "" {
		return r
	}
	return midpointInBand(mid, haveMidpoint, cfg)
}
