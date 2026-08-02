package main

import (
	"math"
	"time"
)

// Funding skip reason. This extends the closed skipReason enum (tokens.go) with the
// single budget-exhaustion outcome the funding decision can produce on top of the
// pure sizing reasons. A market is skipped for this reason when the run's remaining
// USDC budget cannot cover even the venue minimum order notional for that market.
const (
	// The remaining per-run USDC budget cannot fund the market's minimum order
	// notional (minOrderSize * YES midpoint), so no order is placed. Distinct from
	// the sizing reason topup_below_min (the desired top-up itself is below the
	// venue minimum) — here the desired order is large enough but the run has run
	// out of spendable wallet USDC.
	skipBudgetBelowMin skipReason = "budget_below_min"
)

// runBudget is the per-run USDC ledger. It starts equal to the snapshot
// WalletUSDC (the Polygon wallet collateral — never an exchange/CLOB balance) and
// is decremented by the notional (shares × YES midpoint) of every order the run
// places or intentionally leaves open, so total planned new/maintained notional
// across ALL markets never exceeds the wallet balance. There is no portfolio-wide
// hard cap beyond this single shared budget.
type runBudget struct {
	remaining float64
}

// newRunBudget seeds the ledger from the wallet USDC cash balance. The argument
// must come from the account-value snapshot's WalletUSDC (the wallet helper), not
// any exchange/CLOB available-balance field.
func newRunBudget(walletUSDC float64) *runBudget {
	return &runBudget{remaining: walletUSDC}
}

// Remaining returns the un-reserved USDC still available to fund orders this run.
func (b *runBudget) Remaining() float64 { return b.remaining }

// canCover reports whether the remaining budget can fund the given notional in
// full (remaining >= notional).
func (b *runBudget) canCover(notional float64) bool { return b.remaining >= notional }

// reserve decrements the remaining budget by a placed/maintained order's notional.
// It is called EXACTLY once per placed order, for the placed notional; skips
// reserve nothing.
func (b *runBudget) reserve(notional float64) { b.remaining -= notional }

// fundingResult is the per-market funding outcome layered on top of a sizing
// decision: either a fundable order (Place, with the shares and the notional
// reserved from the budget) or a skip (with the deciding reason). It is produced
// by fundOrder and is non-mutating except for the single budget reservation a
// Place performs.
type fundingResult struct {
	Place             bool
	Shares            float64    // shares to place (0 when Skip)
	Notional          float64    // Shares * midpoint (reserved from the budget when Place)
	Skip              bool       // true when no order is funded
	Reason            skipReason // set when Skip
	MinOrderException bool       // true when a NEW position was bumped up to the venue minimum
	PartialFill       bool       // true when funded below the full desired top-up due to the budget
}

// planFunding is the PURE per-market funding decision given a sizing decision, the
// YES midpoint, the venue minimum order size, and the budget still REMAINING this
// run. It performs no I/O, consults no clock, uses no randomness, and — unlike
// fundOrder — does NOT mutate any budget: it reads `remaining` and returns the
// intended outcome only. The caller reserves the returned Notional itself, exactly
// once, on the branch that actually keeps or places the order. midpoint > 0 is
// assumed (sizing guarantees a positive midpoint before producing a fundable
// top-up). The minimum order notional is minOrderSize * midpoint.
//
// Decision order (first match wins):
//  1. NEW-position min-order exception: d.Skip with reason topup_below_min AND
//     committed == 0. Bump the order up to exactly minOrderSize. Place it
//     (MinOrderException=true) when `remaining` covers the minimum notional; else
//     skip budget_below_min.
//  2. Any other sizing skip (existing-position topup_below_min, no_owned,
//     at_or_over_target, etc.): propagate the skip with d.Reason, fund nothing.
//  3. Fundable top-up (d.TopupShares >= minOrderSize):
//     a. `remaining` covers the full desired notional => place the full desired top-up.
//     b. else if `remaining` still covers the minimum notional => PARTIAL: place the
//     largest order that is both within target (<= desired) and affordable
//     (<= remaining/midpoint), which is >= minOrderSize here.
//     c. else => skip budget_below_min.
func planFunding(d sizingDecision, midpoint, minOrderSize, remaining float64) fundingResult {
	minNotional := minOrderSize * midpoint

	if d.Skip {
		// 1. NEW-position minimum-order exception: a brand-new YES position
		// (committed == 0) whose 1% target order fell below the venue minimum is
		// bumped UP TO the minimum, budget permitting.
		if d.Reason == skipTopupBelowMin && d.CommittedShares == 0 {
			if remaining >= minNotional {
				return fundingResult{
					Place:             true,
					Shares:            minOrderSize,
					Notional:          minNotional,
					MinOrderException: true,
				}
			}
			return fundingResult{Skip: true, Reason: skipBudgetBelowMin}
		}

		// 2. Every other sizing skip propagates unchanged: an EXISTING position's
		// below-min top-up, no_owned, at_or_over_target, etc. Fund nothing.
		return fundingResult{Skip: true, Reason: d.Reason}
	}

	// 3. Fundable top-up. desired is the exact target-minus-committed top-up;
	// sizing guarantees desired >= minOrderSize.
	desired := d.TopupShares
	desiredNotional := desired * midpoint

	// 3a. Full fund: the budget covers the entire desired top-up.
	if remaining >= desiredNotional {
		return fundingResult{Place: true, Shares: desired, Notional: desiredNotional}
	}

	// 3b. Partial fund: the budget cannot cover the full desired top-up but still
	// covers the minimum order notional. Place the largest order that is both within
	// target (never above desired) and affordable (never above remaining/midpoint).
	if remaining >= minNotional {
		affordable := remaining / midpoint
		shares := math.Min(desired, affordable)
		if shares >= minOrderSize {
			notional := shares * midpoint
			return fundingResult{Place: true, Shares: shares, Notional: notional, PartialFill: true}
		}
	}

	// 3c. The remaining budget is below the minimum order notional: skip.
	return fundingResult{Skip: true, Reason: skipBudgetBelowMin}
}

// fundOrder is the budget-mutating wrapper around planFunding: it plans against the
// budget's remaining balance and, when the plan places an order, reserves exactly the
// planned notional from the budget once. It performs no I/O, consults no clock, and
// uses no randomness; its only side effect is the single reservation a Place performs.
// midpoint > 0 is assumed (sizing guarantees a positive midpoint before producing a
// fundable top-up). See planFunding for the full decision order.
func fundOrder(d sizingDecision, midpoint, minOrderSize float64, budget *runBudget) fundingResult {
	res := planFunding(d, midpoint, minOrderSize, budget.Remaining())
	if res.Place {
		budget.reserve(res.Notional)
	}
	return res
}

// fundingInputs are the per-market inputs the planning loop needs once a market is
// known to be eligible: the YES midpoint used to price shares, the resolved venue
// minimum order size, the already-committed YES exposure, and whether the account
// owns NO shares for the market. A non-nil Err marks a market whose inputs could
// not be resolved; the loop logs it and continues to the next market (one bad
// market never stops the rest).
type fundingInputs struct {
	Midpoint      float64
	MinOrderSize  float64
	Committed     float64
	OpposingOwned bool
	Err           error
}

// plannedOrder is a single funded YES-buy intent collected by the planning loop: the
// market it targets plus the resolved funding result. The loop is non-mutating, so a
// plannedOrder records intent only — placement/cancellation is a later rung.
type plannedOrder struct {
	Market  eligibleMarket
	Funding fundingResult
}

// planFundedOrders runs the non-mutating funding plan over the eligible markets.
// The markets are iterated exactly as supplied (discovery already sorts them
// earliest-close-first); each market's inputs are fetched via the provided closure.
// Per-market sizing targets 1% of totalAccountValue (the snapshot Total: wallet
// USDC plus owned-share value), while the shared runBudget that gates funding is
// seeded only from walletUSDC (the spendable Polygon collateral) — the two are
// distinct and must not be conflated. Funded intents are collected in order.
//
// It applies NO aggregate cap: deployment continues across ALL eligible markets,
// constrained only by per-market target exposure, eligibility, minimum order size,
// and the shared budget. Per-market input errors and skips are logged and skipped
// without aborting the plan — one bad market never stops subsequent markets. It
// places and cancels nothing.
func planFundedOrders(
	logger *Logger,
	cfg *Config,
	totalAccountValue float64,
	walletUSDC float64,
	markets []eligibleMarket,
	inputsFor func(eligibleMarket) fundingInputs,
) []plannedOrder {
	budget := newRunBudget(walletUSDC)

	logger.Event("funding_plan_start", map[string]any{
		"markets_eligible":    len(markets),
		"total_account_value": totalAccountValue,
		"wallet_usdc":         walletUSDC,
		"budget_remaining":    budget.Remaining(),
	})

	planned := make([]plannedOrder, 0, len(markets))
	for _, m := range markets {
		in := inputsFor(m)
		if in.Err != nil {
			logFundingDecision(logger, m, fundingResult{Skip: true, Reason: skipMinOrderSizeUndeterminable}, budget, in.Err)
			continue
		}

		// Sizing targets total account value; the budget gates against wallet USDC.
		d := sizeMarket(totalAccountValue, in.Midpoint, in.MinOrderSize, in.Committed, in.OpposingOwned, cfg)
		res := fundOrder(d, in.Midpoint, in.MinOrderSize, budget)
		logFundingDecision(logger, m, res, budget, nil)
		if res.Place {
			planned = append(planned, plannedOrder{Market: m, Funding: res})
		}
	}

	logger.Event("funding_plan_done", map[string]any{
		"markets_eligible": len(markets),
		"orders_planned":   len(planned),
		"budget_remaining": budget.Remaining(),
	})
	return planned
}

// logFundingDecision emits one structured per-market "funding" event: a placed line
// carrying the funded shares/notional and the min-order-exception / partial-fill
// flags, or a skipped line carrying the deciding reason (and any input error). Every
// line records the running remaining budget so the ledger is observable per market.
func logFundingDecision(logger *Logger, m eligibleMarket, res fundingResult, budget *runBudget, inputErr error) {
	fields := map[string]any{
		"condition_id":     m.Market.ConditionID,
		"yes_token_id":     m.Tokens.YesTokenID,
		"close_at":         m.CloseAt.Format(time.RFC3339),
		"budget_remaining": budget.Remaining(),
	}
	if res.Skip {
		fields["status"] = "skipped"
		fields["skip_reason"] = res.Reason.String()
		if inputErr != nil {
			fields["input_error"] = inputErr.Error()
		}
		logger.Event("funding", fields)
		return
	}
	fields["status"] = "placed"
	fields["shares"] = res.Shares
	fields["notional"] = res.Notional
	fields["min_order_exception"] = res.MinOrderException
	fields["partial_fill"] = res.PartialFill
	logger.Event("funding", fields)
}
