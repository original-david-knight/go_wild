package main

import (
	"strconv"
	"strings"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Sizing skip reasons. These extend the closed skipReason enum in tokens.go with
// the two non-eligibility outcomes the pure sizing decision can produce. The
// YES-owned case reuses skipYesSharesOwned (defined in eligibility.go).
const (
	// The account's committed NO exposure (held NO shares plus the unfilled
	// remainder of open NO buys) is already at or above the per-market target, so
	// no additional NO shares are wanted.
	skipAtOrOverTarget skipReason = "at_or_over_target"

	// The computed top-up (target minus committed) is below the venue minimum
	// order size. The minimum-order bump exception for brand-new positions is a
	// later rung; here a below-minimum top-up simply skips.
	skipTopupBelowMin skipReason = "topup_below_min"
)

// heldNoShares sums the size of every owned position on the market's NO token.
// Only positions whose Asset equals the NO token ID and whose Size is positive
// contribute; a zero/negative size is treated as no holding.
func heldNoShares(positions []polymarket.Position, noTokenID string) float64 {
	total := 0.0
	for _, p := range positions {
		if p.Asset == noTokenID && p.Size > 0 {
			total += p.Size
		}
	}
	return total
}

// openNoBuyRemaining sums the unfilled remainder of every open NO buy order for
// this market. An order contributes only when it is a buy (Side == "BUY",
// case-insensitive) on the market's NO token; its contribution is
// OriginalSize - SizeMatched parsed from the string fields. A fully-matched order
// contributes 0, a partially-filled order contributes only its unfilled
// remainder, and the per-order contribution is clamped at 0 so a never-negative
// remainder is summed (an unparseable size field contributes 0).
func openNoBuyRemaining(orders []polymarket.Order, noTokenID string) float64 {
	total := 0.0
	for _, o := range orders {
		if !strings.EqualFold(strings.TrimSpace(o.Side), "BUY") {
			continue
		}
		if o.AssetID != noTokenID {
			continue
		}
		original, err := strconv.ParseFloat(strings.TrimSpace(o.OriginalSize), 64)
		if err != nil {
			continue
		}
		matched, err := strconv.ParseFloat(strings.TrimSpace(o.SizeMatched), 64)
		if err != nil {
			continue
		}
		remaining := original - matched
		if remaining <= 0 {
			continue
		}
		total += remaining
	}
	return total
}

// committedNoShares is the total NO exposure already committed for a market: the
// held NO shares plus the unfilled remainder of every open NO buy order on it.
// Sizing brings this value up to — never over — the per-market target.
func committedNoShares(positions []polymarket.Position, orders []polymarket.Order, noTokenID string) float64 {
	return heldNoShares(positions, noTokenID) + openNoBuyRemaining(orders, noTokenID)
}

// sizingDecision is the pure outcome of sizing a single market: either an explicit
// skip (with the deciding reason) or an intended NO-share top-up quantity. It is
// non-mutating — it never places or cancels orders; it only describes intent.
type sizingDecision struct {
	Skip              bool
	Reason            skipReason // set when Skip
	TargetNoShares    float64
	CommittedNoShares float64
	TopupShares       float64 // intended NO-share quantity to place; 0 when Skip
}

// sizeMarket is the PURE per-market sizing decision. Given the account-value total,
// the NO midpoint, the venue minimum order size, the already-committed NO exposure,
// and whether the account owns YES shares for the market, it computes the intended
// NO-share top-up or an explicit skip. It performs no I/O, consults no clock, and
// uses no randomness: identical inputs yield a byte-identical decision.
//
// The 1% (cfg.TargetExposurePct) is a per-market target derived solely from the
// snapshot Total — it is never accumulated across markets. The returned TopupShares
// is exactly target - committed, so committed exposure is brought UP TO, and never
// pushed OVER, the target.
//
// Decision order (first match wins):
//  1. YES owned        => skip (skipYesSharesOwned), BEFORE any committed/target math.
//  2. midpoint <= 0    => skip (skipNoTwoSidedBook); eligibility guarantees a
//     positive midpoint, but defend against a divide-by-zero / Inf here.
//  3. committed >= target => skip (skipAtOrOverTarget).
//  4. topup < min      => skip (skipTopupBelowMin).
//  5. otherwise        => top up by target - committed.
func sizeMarket(totalAccountValue, noMidpoint, minOrderSize, committedNoShares float64, yesOwned bool, cfg *Config) sizingDecision {
	// 1. YES shares owned: not a NO-buying candidate. Decided before any target or
	// committed math so the YES branch always precedes sizing.
	if yesOwned {
		return sizingDecision{Skip: true, Reason: skipYesSharesOwned}
	}

	// 2. A non-positive midpoint cannot price the target. Eligibility guarantees a
	// positive midpoint, so this is a defensive skip rather than a divide-by-zero.
	if noMidpoint <= 0 {
		return sizingDecision{Skip: true, Reason: skipNoTwoSidedBook}
	}

	targetNotional := totalAccountValue * cfg.TargetExposurePct
	targetNoShares := targetNotional / noMidpoint

	// 3. Already at or over the per-market target: nothing to buy.
	if committedNoShares >= targetNoShares {
		return sizingDecision{
			Skip:              true,
			Reason:            skipAtOrOverTarget,
			TargetNoShares:    targetNoShares,
			CommittedNoShares: committedNoShares,
		}
	}

	// 4. The top-up that would bring committed exactly up to target. Never exceeds
	// target - committed, so committed is never pushed over target.
	topupShares := targetNoShares - committedNoShares
	if topupShares < minOrderSize {
		return sizingDecision{
			Skip:              true,
			Reason:            skipTopupBelowMin,
			TargetNoShares:    targetNoShares,
			CommittedNoShares: committedNoShares,
		}
	}

	// 5. Eligible top-up.
	return sizingDecision{
		TargetNoShares:    targetNoShares,
		CommittedNoShares: committedNoShares,
		TopupShares:       topupShares,
	}
}

// logSizing emits a structured per-market "sizing" event describing the decision:
// a skipped line carrying the skip reason, or a resolved line carrying the target,
// committed, and top-up share counts. It is a thin wrapper around the pure
// sizeMarket so the math stays trivially unit-testable without a logger.
func logSizing(logger *Logger, conditionID, noTokenID string, d sizingDecision) {
	fields := map[string]any{
		"condition_id":        conditionID,
		"no_token_id":         noTokenID,
		"target_no_shares":    d.TargetNoShares,
		"committed_no_shares": d.CommittedNoShares,
	}
	if d.Skip {
		fields["status"] = "skipped"
		fields["skip_reason"] = d.Reason.String()
		logger.Event("sizing", fields)
		return
	}
	fields["status"] = "ok"
	fields["topup_shares"] = d.TopupShares
	logger.Event("sizing", fields)
}
