package main

import (
	"bytes"
	"testing"
	"time"
)

// TestBudgetSizesTotalBudgetsWallet locks in that the funding planner sizes the 2%
// per-market target against TOTAL account value while gating funding against the
// (smaller) wallet USDC budget. With total=10000 but wallet=100, sizing wants a
// large order that the budget can only partially fund — proving the two values are
// not conflated. If sizing had (incorrectly) used walletUSDC, the order would be a
// tiny fully-funded 4 shares instead of a budget-bound 200-share partial fill.
func TestBudgetSizesTotalBudgetsWallet(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_x")
	cfg := defaultConfig()
	cfg.TargetExposurePct = 0.02

	const total = 10_000.0
	const wallet = 100.0
	market := loopMarket("m1", time.Unix(0, 0).UTC())
	inputs := func(eligibleMarket) fundingInputs {
		return fundingInputs{Midpoint: 0.50, MinOrderSize: 1.0, Committed: 0, YesOwned: false}
	}

	planned := planFundedOrders(logger, &cfg, total, wallet, []eligibleMarket{market}, inputs)
	if len(planned) != 1 {
		t.Fatalf("expected one planned order, got %d", len(planned))
	}
	f := planned[0].Funding
	// target = 10000*0.02/0.5 = 400 shares; budget 100 funds 100/0.5 = 200 shares.
	if !f.PartialFill {
		t.Errorf("expected a partial fill (budget-bound), got full: %+v", f)
	}
	if f.Shares != 200 {
		t.Errorf("placed shares = %v, want 200 (total-sized target, wallet-bound budget)", f.Shares)
	}
	if f.Notional > wallet+1e-9 {
		t.Errorf("notional %v exceeds wallet budget %v", f.Notional, wallet)
	}
}
