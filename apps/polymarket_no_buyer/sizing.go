package main

import (
	"strconv"
	"strings"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Sizing skip reasons. These extend the closed skipReason enum in tokens.go with
// the two non-eligibility outcomes the pure sizing decision can produce. The
// opposing-NO-owned case reuses skipNoSharesOwned (defined in eligibility.go).
const (
	// The account's committed YES exposure (held YES shares plus the unfilled
	// remainder of open YES buys) is already at or above the per-market target, so
	// no additional YES shares are wanted.
	skipAtOrOverTarget skipReason = "at_or_over_target"

	// The computed top-up (target minus committed) is below the venue minimum
	// order size. The minimum-order bump exception for brand-new positions is a
	// later rung; here a below-minimum top-up simply skips.
	skipTopupBelowMin skipReason = "topup_below_min"
)

// heldOutcomeShares sums the size of every owned position on the selected token.
// Only positions whose Asset equals tokenID and whose Size is positive
// contribute; a zero/negative size is treated as no holding.
func heldOutcomeShares(positions []polymarket.Position, tokenID string) float64 {
	total := 0.0
	for _, p := range positions {
		if p.Asset == tokenID && p.Size > 0 {
			total += p.Size
		}
	}
	return total
}

// openOutcomeBuyRemaining sums the unfilled remainder of every open buy order for
// this market. An order contributes only when it is a buy (Side == "BUY",
// case-insensitive) on tokenID; its contribution is
// OriginalSize - SizeMatched parsed from the string fields. A fully-matched order
// contributes 0, a partially-filled order contributes only its unfilled
// remainder, and the per-order contribution is clamped at 0 so a never-negative
// remainder is summed (an unparseable size field contributes 0).
func openOutcomeBuyRemaining(orders []polymarket.Order, tokenID string) float64 {
	total := 0.0
	for _, o := range orders {
		if !strings.EqualFold(strings.TrimSpace(o.Side), "BUY") {
			continue
		}
		if o.AssetID != tokenID {
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

// committedOutcomeShares is the total selected-outcome exposure already committed
// for a market: held shares plus the unfilled remainder of open buy orders on it.
// Sizing brings this value up to — never over — the per-market target.
func committedOutcomeShares(positions []polymarket.Position, orders []polymarket.Order, tokenID string) float64 {
	return heldOutcomeShares(positions, tokenID) + openOutcomeBuyRemaining(orders, tokenID)
}

// sizingDecision is the pure outcome of sizing a single market: either an explicit
// skip (with the deciding reason) or an intended selected-outcome top-up quantity. It is
// non-mutating — it never places or cancels orders; it only describes intent.
type sizingDecision struct {
	Skip            bool
	Reason          skipReason // set when Skip
	TargetShares    float64
	CommittedShares float64
	TopupShares     float64 // intended selected-outcome quantity to place; 0 when Skip
}

// sizeMarket is the PURE per-market sizing decision. Given the account-value total,
// the selected-outcome midpoint, the venue minimum order size, the already-committed
// exposure, and whether the account owns the opposing outcome, it computes the
// selected-outcome top-up or an explicit skip. It performs no I/O, consults no clock, and
// uses no randomness: identical inputs yield a byte-identical decision.
//
// The 1% (cfg.TargetExposurePct) is a per-market target derived solely from the
// snapshot Total — it is never accumulated across markets. The returned TopupShares
// is exactly target - committed, so committed exposure is brought UP TO, and never
// pushed OVER, the target.
//
// Decision order (first match wins):
//  1. Opposing outcome owned => skip (skipNoSharesOwned), BEFORE committed/target math.
//  2. midpoint <= 0    => skip (skipYesTwoSidedBook); execution guarantees a
//     positive midpoint, but defend against a divide-by-zero / Inf here.
//  3. committed >= target => skip (skipAtOrOverTarget).
//  4. topup < min      => skip (skipTopupBelowMin).
//  5. otherwise        => top up by target - committed.
func sizeMarket(totalAccountValue, midpoint, minOrderSize, committedShares float64, opposingOwned bool, cfg *Config) sizingDecision {
	// 1. NO shares owned: not a YES-buying candidate. Decided before any target or
	// committed math so the ownership branch always precedes sizing.
	if opposingOwned {
		return sizingDecision{Skip: true, Reason: skipNoSharesOwned}
	}

	// 2. A non-positive midpoint cannot price the target. Eligibility guarantees a
	// positive midpoint, so this is a defensive skip rather than a divide-by-zero.
	if midpoint <= 0 {
		return sizingDecision{Skip: true, Reason: skipYesTwoSidedBook}
	}

	targetNotional := totalAccountValue * cfg.TargetExposurePct
	targetShares := targetNotional / midpoint

	// 3. Already at or over the per-market target: nothing to buy.
	if committedShares >= targetShares {
		return sizingDecision{
			Skip:            true,
			Reason:          skipAtOrOverTarget,
			TargetShares:    targetShares,
			CommittedShares: committedShares,
		}
	}

	// 4. The top-up that would bring committed exactly up to target. Never exceeds
	// target - committed, so committed is never pushed over target.
	topupShares := targetShares - committedShares
	if topupShares < minOrderSize {
		return sizingDecision{
			Skip:            true,
			Reason:          skipTopupBelowMin,
			TargetShares:    targetShares,
			CommittedShares: committedShares,
		}
	}

	// 5. Eligible top-up.
	return sizingDecision{
		TargetShares:    targetShares,
		CommittedShares: committedShares,
		TopupShares:     topupShares,
	}
}

// logSizing emits a structured per-market "sizing" event describing the decision:
// a skipped line carrying the skip reason, or a resolved line carrying the target,
// committed, and top-up share counts. It is a thin wrapper around the pure
// sizeMarket so the math stays trivially unit-testable without a logger.
func logSizing(logger *Logger, conditionID, yesTokenID string, d sizingDecision) {
	fields := map[string]any{
		"condition_id":     conditionID,
		"yes_token_id":     yesTokenID,
		"target_shares":    d.TargetShares,
		"committed_shares": d.CommittedShares,
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
