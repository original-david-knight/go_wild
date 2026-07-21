package main

import (
	"context"
	"sort"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// redeemableCondition is a unique resolved/closed market condition that holds at
// least one redeemable position. It is used for the pre-redemption scan
// (telemetry and the dry-run enumeration); live redemption settles every
// condition in a single RedeemWinnings call rather than iterating these.
type redeemableCondition struct {
	ConditionID string
	Outcome     string
	Size        float64
}

// selectRedeemableConditions returns the deterministic, de-duplicated set of
// conditions that have at least one redeemable position (Redeemable && Size > 0),
// sorted by condition ID for stable, idempotent ordering. Open/unresolved markets
// and zero-balance positions are excluded.
func selectRedeemableConditions(positions []polymarket.Position) []redeemableCondition {
	byCondition := map[string]*redeemableCondition{}
	for _, p := range positions {
		if !p.Redeemable || p.Size <= 0 || p.ConditionID == "" {
			continue
		}
		rc, ok := byCondition[p.ConditionID]
		if !ok {
			rc = &redeemableCondition{ConditionID: p.ConditionID, Outcome: p.Outcome}
			byCondition[p.ConditionID] = rc
		}
		rc.Size += p.Size
	}

	out := make([]redeemableCondition, 0, len(byCondition))
	for _, rc := range byCondition {
		out = append(out, *rc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConditionID < out[j].ConditionID })
	return out
}

// redeemPass redeems all redeemable positions as the first trading step of a run,
// before any cancel/snapshot/buy logic. In live mode it settles every redeemable
// condition in a single RedeemWinnings call (empty conditionID): the client
// redeems all standard markets via ConditionalTokens and all negative-risk
// markets via the NegRiskAdapter, returning a per-condition transaction
// breakdown. Per-condition failures are isolated inside that call and surfaced as
// individual failed transactions — one bad condition never blocks the others. In
// dry-run mode it logs the intended redemptions and submits nothing. It always
// returns nil: a redemption problem must not stop the rest of the run.
func (a *App) redeemPass(ctx context.Context, logger *Logger) error {
	positions, err := a.trading.GetPositions(ctx)
	if err != nil {
		logger.Event("redeem_error", map[string]any{"error": err.Error()})
		return nil
	}

	conditions := selectRedeemableConditions(positions)
	logger.Event("redeem_scan", map[string]any{
		"positions_total":       len(positions),
		"redeemable_conditions": len(conditions),
		"dry_run":               a.cfg.DryRun,
	})

	// Nothing to redeem: skip the on-chain call entirely (and avoid the client's
	// "no redeemable positions" error path).
	if len(conditions) == 0 {
		return nil
	}

	// Dry-run: enumerate the intended redemptions; submit nothing.
	if a.cfg.DryRun {
		for _, rc := range conditions {
			logger.Event("redeem_attempt", map[string]any{
				"condition_id": rc.ConditionID,
				"outcome":      rc.Outcome,
				"size":         rc.Size,
				"status":       "would_redeem",
			})
		}
		return nil
	}

	// Live: redeem every redeemable condition in one call.
	res, err := a.trading.RedeemWinnings(ctx, "", nil, "", false)
	if res != nil {
		for _, tx := range res.Transactions {
			fields := map[string]any{"condition_id": tx.ConditionID}
			if tx.Error != "" {
				fields["status"] = "failed"
				fields["reason"] = tx.Error
			} else {
				fields["status"] = "succeeded"
				fields["collateral_payout"] = tx.CollateralPayout
			}
			logger.Event("redeem_attempt", fields)
		}
		logger.Event("redeem_result", map[string]any{
			"conditions_redeemed":     res.ConditionsRedeemed,
			"conditions_failed":       res.ConditionsFailed,
			"conditions_submitted":    res.ConditionsSubmitted,
			"total_collateral_payout": res.TotalCollateralPayout,
		})
	}
	if err != nil {
		logger.Event("redeem_error", map[string]any{"error": err.Error()})
	}
	return nil
}
