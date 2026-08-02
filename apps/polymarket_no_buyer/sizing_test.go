package main

import (
	"math"
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

const sizingEps = 1e-9

func sizingCfg() *Config {
	cfg := defaultConfig()
	return &cfg
}

// TestSizingTargetArithmetic verifies the target-notional and target-share math:
// targetNotional == Total * cfg.TargetExposurePct and targetNoShares ==
// targetNotional / midpoint, and that overriding cfg.TargetExposurePct changes it.
func TestSizingTargetArithmetic(t *testing.T) {
	const total = 10_000.0
	const midpoint = 0.95

	cfg := sizingCfg()
	d := sizeMarket(total, midpoint, 0.0, 0.0, false, cfg)
	if d.Skip {
		t.Fatalf("unexpected skip: %+v", d)
	}
	wantNotional := total * cfg.TargetExposurePct
	wantShares := wantNotional / midpoint
	if math.Abs(d.TargetShares-wantShares) > sizingEps {
		t.Errorf("target_no_shares = %v, want %v (notional %v / midpoint %v)", d.TargetShares, wantShares, wantNotional, midpoint)
	}
	// Top-up from zero committed equals the full target.
	if math.Abs(d.TopupShares-wantShares) > sizingEps {
		t.Errorf("topup_shares = %v, want full target %v", d.TopupShares, wantShares)
	}

	// Overriding the exposure pct must change the target proportionally.
	cfg2 := sizingCfg()
	cfg2.TargetExposurePct = 0.05
	d2 := sizeMarket(total, midpoint, 0.0, 0.0, false, cfg2)
	want2 := (total * cfg2.TargetExposurePct) / midpoint
	if math.Abs(d2.TargetShares-want2) > sizingEps {
		t.Errorf("override target_no_shares = %v, want %v", d2.TargetShares, want2)
	}
	if math.Abs(d2.TargetShares-d.TargetShares) < sizingEps {
		t.Errorf("changing TargetExposurePct did not change the target (%v vs %v)", d.TargetShares, d2.TargetShares)
	}
}

// TestSizingCommittedExposure tables the committed-exposure helpers: held-only,
// open-buy-only, and held+open-buy, with partial fills contributing only their
// remainder and fully-matched orders contributing 0.
func TestSizingCommittedExposure(t *testing.T) {
	const noTok = "NO_TOKEN"
	const yesTok = "YES_TOKEN"

	positions := []polymarket.Position{
		{Asset: noTok, Size: 30},    // held NO
		{Asset: noTok, Size: 0},     // zero size ignored
		{Asset: noTok, Size: -5},    // negative size ignored
		{Asset: yesTok, Size: 1000}, // wrong asset ignored
	}
	orders := []polymarket.Order{
		{AssetID: noTok, Side: "BUY", OriginalSize: "40", SizeMatched: "0"},  // remainder 40
		{AssetID: noTok, Side: "buy", OriginalSize: "20", SizeMatched: "15"}, // partial: remainder 5
		{AssetID: noTok, Side: "BUY", OriginalSize: "10", SizeMatched: "10"}, // fully matched: 0
		{AssetID: noTok, Side: "SELL", OriginalSize: "50", SizeMatched: "0"}, // wrong side ignored
		{AssetID: yesTok, Side: "BUY", OriginalSize: "60", SizeMatched: "0"}, // wrong asset ignored
		{AssetID: noTok, Side: "BUY", OriginalSize: "bad", SizeMatched: "0"}, // unparseable ignored
		{AssetID: noTok, Side: "BUY", OriginalSize: "7", SizeMatched: "9"},   // negative remainder clamped to 0
	}

	if got, want := heldOutcomeShares(positions, noTok), 30.0; math.Abs(got-want) > sizingEps {
		t.Errorf("heldOutcomeShares = %v, want %v", got, want)
	}
	if got, want := openOutcomeBuyRemaining(orders, noTok), 45.0; math.Abs(got-want) > sizingEps {
		t.Errorf("openOutcomeBuyRemaining = %v, want %v (40 + 5 + 0 + 0)", got, want)
	}
	if got, want := committedOutcomeShares(positions, orders, noTok), 75.0; math.Abs(got-want) > sizingEps {
		t.Errorf("committedOutcomeShares = %v, want %v (held 30 + open 45)", got, want)
	}

	// Held-only and open-buy-only isolations.
	if got, want := committedOutcomeShares(positions, nil, noTok), 30.0; math.Abs(got-want) > sizingEps {
		t.Errorf("held-only committed = %v, want 30", got)
	}
	if got, want := committedOutcomeShares(nil, orders, noTok), 45.0; math.Abs(got-want) > sizingEps {
		t.Errorf("open-buy-only committed = %v, want 45", got)
	}
}

// TestSizingOpposingOwnedBranch verifies that an opposing NO holding skips with
// reason no_shares_owned before any target/top-up computation: TopupShares is 0 and the
// target is never computed.
func TestSizingOpposingOwnedBranch(t *testing.T) {
	cfg := sizingCfg()
	// Even with a huge total and zero committed (which would otherwise produce a
	// large top-up), opposing ownership short-circuits.
	d := sizeMarket(1_000_000, 0.9, 0.0, 0.0, true, cfg)
	if !d.Skip {
		t.Fatalf("opposing outcome owned must skip, got %+v", d)
	}
	// Reuse the eligibility ownership reason.
	if d.Reason != skipNoSharesOwned {
		t.Errorf("reason = %q, want %q", d.Reason, skipNoSharesOwned)
	}
	if string(d.Reason) != "no_shares_owned" {
		t.Errorf("reason string = %q, want %q", d.Reason, "no_shares_owned")
	}
	if d.TopupShares != 0 {
		t.Errorf("TopupShares = %v, want 0 (no sizing with opposing shares)", d.TopupShares)
	}
	if d.TargetShares != 0 || d.CommittedShares != 0 {
		t.Errorf("ownership branch must precede sizing; target/committed should be unset, got %+v", d)
	}
}

// TestSizingTopupBranching covers the three sizing outcomes once past ownership
// branch: at-or-over target, a normal top-up, and a below-min top-up, and asserts
// the top-up never pushes committed above target.
func TestSizingTopupBranching(t *testing.T) {
	const total = 10_000.0
	const midpoint = 0.95
	cfg := sizingCfg()
	target := (total * cfg.TargetExposurePct) / midpoint // ~210.526

	// committed >= target => at_or_over_target (test both == and >).
	for _, committed := range []float64{target, target + 50} {
		d := sizeMarket(total, midpoint, 1.0, committed, false, cfg)
		if !d.Skip || d.Reason != skipAtOrOverTarget {
			t.Fatalf("committed=%v: want skip at_or_over_target, got %+v", committed, d)
		}
		if string(d.Reason) != "at_or_over_target" {
			t.Errorf("reason string = %q, want at_or_over_target", d.Reason)
		}
		if d.TopupShares != 0 {
			t.Errorf("committed=%v: TopupShares = %v, want 0", committed, d.TopupShares)
		}
	}

	// committed < target, top-up above min => exact top-up = target - committed,
	// and committed + topup must not exceed target.
	committed := 100.0
	d := sizeMarket(total, midpoint, 1.0, committed, false, cfg)
	if d.Skip {
		t.Fatalf("want a top-up, got skip %+v", d)
	}
	wantTopup := target - committed
	if math.Abs(d.TopupShares-wantTopup) > sizingEps {
		t.Errorf("TopupShares = %v, want %v", d.TopupShares, wantTopup)
	}
	if committed+d.TopupShares > target+sizingEps {
		t.Errorf("top-up pushed committed above target: %v + %v > %v", committed, d.TopupShares, target)
	}

	// top-up below min => topup_below_min. With committed just under target, the
	// residual is tiny; a min above it forces the skip.
	nearTarget := target - 0.25
	d2 := sizeMarket(total, midpoint, 1.0, nearTarget, false, cfg)
	if !d2.Skip || d2.Reason != skipTopupBelowMin {
		t.Fatalf("want skip topup_below_min, got %+v", d2)
	}
	if string(d2.Reason) != "topup_below_min" {
		t.Errorf("reason string = %q, want topup_below_min", d2.Reason)
	}
	if d2.TopupShares != 0 {
		t.Errorf("below-min skip TopupShares = %v, want 0", d2.TopupShares)
	}
}

// TestSizingNonPositiveMidpoint verifies the defensive divide-by-zero guard: a
// non-positive midpoint skips rather than producing Inf/NaN shares.
func TestSizingNonPositiveMidpoint(t *testing.T) {
	cfg := sizingCfg()
	for _, mid := range []float64{0.0, -0.5} {
		d := sizeMarket(10_000, mid, 1.0, 0.0, false, cfg)
		if !d.Skip || d.Reason != skipYesTwoSidedBook {
			t.Errorf("midpoint=%v: want skip yes-two-sided-book, got %+v", mid, d)
		}
		if d.TopupShares != 0 || math.IsInf(d.TargetShares, 0) || math.IsNaN(d.TargetShares) {
			t.Errorf("midpoint=%v: must not produce Inf/NaN/topup, got %+v", mid, d)
		}
	}
}

// TestSizingDeterminism verifies that identical inputs derived from the same
// snapshot+book+positions+orders produce a byte-identical decision across repeated
// calls (no time, no randomness).
func TestSizingDeterminism(t *testing.T) {
	const noTok = "NO_TOKEN"
	positions := []polymarket.Position{{Asset: noTok, Size: 30}}
	orders := []polymarket.Order{
		{AssetID: noTok, Side: "BUY", OriginalSize: "40", SizeMatched: "15"},
	}
	cfg := sizingCfg()
	committed := committedOutcomeShares(positions, orders, noTok)

	first := sizeMarket(12_345.67, 0.93, 1.0, committed, false, cfg)
	for i := 0; i < 100; i++ {
		got := sizeMarket(12_345.67, 0.93, 1.0, committed, false, cfg)
		if got != first {
			t.Fatalf("non-deterministic decision on call %d: %+v != %+v", i, got, first)
		}
	}
}
