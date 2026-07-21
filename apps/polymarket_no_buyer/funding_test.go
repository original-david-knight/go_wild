package main

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

const fundingEps = 1e-9

// TestRunBudgetRemainingLifecycle asserts the ledger starts EXACTLY at walletUSDC,
// canCover compares against the remaining balance, and reserve decrements by exactly
// the supplied notional.
func TestRunBudgetRemainingLifecycle(t *testing.T) {
	const wallet = 1234.56
	b := newRunBudget(wallet)
	if math.Abs(b.Remaining()-wallet) > fundingEps {
		t.Fatalf("Remaining() = %v, want walletUSDC %v", b.Remaining(), wallet)
	}

	if !b.canCover(wallet) {
		t.Errorf("canCover(wallet) = false, want true (remaining == notional)")
	}
	if b.canCover(wallet + 0.01) {
		t.Errorf("canCover(wallet+0.01) = true, want false")
	}

	b.reserve(34.56)
	if math.Abs(b.Remaining()-1200.0) > fundingEps {
		t.Errorf("after reserve(34.56) Remaining() = %v, want 1200", b.Remaining())
	}
	b.reserve(1200.0)
	if math.Abs(b.Remaining()) > fundingEps {
		t.Errorf("after draining Remaining() = %v, want 0", b.Remaining())
	}
}

// TestBudgetReservedExactlyNotional verifies that placing the full desired top-up
// decrements run_usdc_remaining by EXACTLY shares*midpoint and reserves exactly once.
func TestBudgetReservedExactlyNotional(t *testing.T) {
	const wallet = 1000.0
	const midpoint = 0.95
	const minOrder = 5.0

	b := newRunBudget(wallet)
	// A plain fundable top-up of 100 shares.
	d := sizingDecision{TopupShares: 100, TargetNoShares: 100, CommittedNoShares: 0}
	res := fundOrder(d, midpoint, minOrder, b)
	if !res.Place || res.PartialFill || res.MinOrderException {
		t.Fatalf("want full place, got %+v", res)
	}
	wantNotional := 100 * midpoint
	if math.Abs(res.Notional-wantNotional) > fundingEps {
		t.Errorf("Notional = %v, want %v", res.Notional, wantNotional)
	}
	if math.Abs(b.Remaining()-(wallet-wantNotional)) > fundingEps {
		t.Errorf("Remaining() = %v, want %v (decremented by exactly the notional)", b.Remaining(), wallet-wantNotional)
	}
}

// TestBudgetNeverExceedsWalletAcrossManyMarkets drives a long sequence of markets
// that each want funding through one shared budget and asserts the sum of every
// placed notional never exceeds walletUSDC, and each placed notional matches the
// per-order decrement.
func TestBudgetNeverExceedsWalletAcrossManyMarkets(t *testing.T) {
	const wallet = 500.0
	const midpoint = 0.90
	const minOrder = 1.0

	b := newRunBudget(wallet)
	sumPlaced := 0.0
	prev := b.Remaining()
	for i := 0; i < 200; i++ {
		// Each market wants a 30-share top-up (notional 27); the budget caps total.
		d := sizingDecision{TopupShares: 30, TargetNoShares: 30, CommittedNoShares: 0}
		res := fundOrder(d, midpoint, minOrder, b)
		if res.Place {
			sumPlaced += res.Notional
			decrement := prev - b.Remaining()
			if math.Abs(decrement-res.Notional) > fundingEps {
				t.Fatalf("market %d: budget decremented by %v, want notional %v", i, decrement, res.Notional)
			}
		}
		prev = b.Remaining()
	}
	if sumPlaced > wallet+fundingEps {
		t.Fatalf("sum of placed notionals %v exceeds walletUSDC %v", sumPlaced, wallet)
	}
	// The leftover must be below the minimum order notional once funding stops.
	if b.Remaining() >= minOrder*midpoint {
		t.Errorf("budget exhausted incorrectly: remaining %v still covers min notional %v", b.Remaining(), minOrder*midpoint)
	}
}

// TestMinOrderExceptionBumpsNewPosition verifies a NEW market (committed 0) whose
// top-up is below the venue minimum is bumped to EXACTLY minOrderSize when the
// budget covers, with MinOrderException set and the minimum notional reserved.
func TestMinOrderExceptionBumpsNewPosition(t *testing.T) {
	const midpoint = 0.95
	const minOrder = 5.0

	b := newRunBudget(1000.0)
	// A new position whose computed top-up fell below the venue minimum.
	d := sizingDecision{Skip: true, Reason: skipTopupBelowMin, TopupShares: 0, CommittedNoShares: 0, TargetNoShares: 2}
	res := fundOrder(d, midpoint, minOrder, b)

	if !res.Place {
		t.Fatalf("want place via min-order exception, got %+v", res)
	}
	if !res.MinOrderException {
		t.Errorf("MinOrderException = false, want true")
	}
	if res.PartialFill {
		t.Errorf("PartialFill = true, want false for a min-order bump")
	}
	if math.Abs(res.Shares-minOrder) > fundingEps {
		t.Errorf("Shares = %v, want exactly minOrderSize %v", res.Shares, minOrder)
	}
	wantNotional := minOrder * midpoint
	if math.Abs(res.Notional-wantNotional) > fundingEps {
		t.Errorf("Notional = %v, want %v", res.Notional, wantNotional)
	}
	if math.Abs(b.Remaining()-(1000.0-wantNotional)) > fundingEps {
		t.Errorf("Remaining() = %v, want %v", b.Remaining(), 1000.0-wantNotional)
	}
}

// TestMinOrderExceptionSkipsWhenBudgetShort verifies the min-order bump is skipped
// with budget_below_min when the remaining budget cannot cover the minimum notional,
// and that nothing is reserved.
func TestMinOrderExceptionSkipsWhenBudgetShort(t *testing.T) {
	const midpoint = 0.95
	const minOrder = 5.0
	minNotional := minOrder * midpoint // 4.75

	b := newRunBudget(minNotional - 0.01) // just short of the minimum notional
	d := sizingDecision{Skip: true, Reason: skipTopupBelowMin, CommittedNoShares: 0}
	res := fundOrder(d, midpoint, minOrder, b)

	if !res.Skip || res.Reason != skipBudgetBelowMin {
		t.Fatalf("want skip budget_below_min, got %+v", res)
	}
	if string(res.Reason) != "budget_below_min" {
		t.Errorf("reason string = %q, want budget_below_min", res.Reason)
	}
	if res.Place || res.MinOrderException {
		t.Errorf("nothing should be placed, got %+v", res)
	}
	if math.Abs(b.Remaining()-(minNotional-0.01)) > fundingEps {
		t.Errorf("budget mutated on skip: Remaining() = %v", b.Remaining())
	}
}

// TestMinOrderExceptionOnlyForNewPosition verifies an EXISTING position's below-min
// top-up (committed > 0) is NOT bumped: it propagates topup_below_min unchanged and
// reserves nothing.
func TestMinOrderExceptionOnlyForNewPosition(t *testing.T) {
	const midpoint = 0.95
	const minOrder = 5.0

	b := newRunBudget(1000.0)
	d := sizingDecision{Skip: true, Reason: skipTopupBelowMin, CommittedNoShares: 12.5}
	res := fundOrder(d, midpoint, minOrder, b)

	if !res.Skip || res.Reason != skipTopupBelowMin {
		t.Fatalf("existing position below-min must propagate topup_below_min, got %+v", res)
	}
	if res.MinOrderException {
		t.Errorf("MinOrderException = true, want false (only NEW positions are bumped)")
	}
	if math.Abs(b.Remaining()-1000.0) > fundingEps {
		t.Errorf("budget mutated on skip: Remaining() = %v", b.Remaining())
	}
}

// TestFundOrderPropagatesSizingSkips verifies non-bumpable sizing skips propagate
// their reason and fund nothing.
func TestFundOrderPropagatesSizingSkips(t *testing.T) {
	b := newRunBudget(1000.0)
	for _, reason := range []skipReason{skipYesSharesOwned, skipAtOrOverTarget, skipNoTwoSidedBook} {
		d := sizingDecision{Skip: true, Reason: reason, CommittedNoShares: 0}
		res := fundOrder(d, 0.95, 5.0, b)
		if !res.Skip || res.Reason != reason {
			t.Errorf("reason %q: want propagated skip, got %+v", reason, res)
		}
		if res.Place {
			t.Errorf("reason %q: nothing should be placed", reason)
		}
	}
	if math.Abs(b.Remaining()-1000.0) > fundingEps {
		t.Errorf("budget mutated by skips: Remaining() = %v", b.Remaining())
	}
}

// TestPartialFillLargestAffordable verifies that with remaining strictly between the
// minimum notional and the full desired notional, the placed shares equal the
// largest order that is both <= target and affordable (== remaining/midpoint here),
// PartialFill is set, and the budget is drained to within an order grid of zero.
func TestPartialFillLargestAffordable(t *testing.T) {
	const midpoint = 0.50
	const minOrder = 1.0
	// Desired 100 shares => desired notional 50. Fund only 30 of budget.
	b := newRunBudget(30.0)
	d := sizingDecision{TopupShares: 100, TargetNoShares: 100, CommittedNoShares: 0}
	res := fundOrder(d, midpoint, minOrder, b)

	if !res.Place || !res.PartialFill {
		t.Fatalf("want partial place, got %+v", res)
	}
	// affordable = 30 / 0.50 = 60 shares, which is < desired 100, so shares == 60.
	if math.Abs(res.Shares-60.0) > fundingEps {
		t.Errorf("Shares = %v, want 60 (largest affordable, below target)", res.Shares)
	}
	if res.Shares > 100+fundingEps {
		t.Errorf("partial fill exceeded target: %v > 100", res.Shares)
	}
	if math.Abs(res.Notional-30.0) > fundingEps {
		t.Errorf("Notional = %v, want 30 (== reserved budget)", res.Notional)
	}
	if math.Abs(b.Remaining()) > fundingEps {
		t.Errorf("Remaining() = %v, want 0 (exhausted by partial)", b.Remaining())
	}
}

// TestPartialFillCappedAtTarget verifies a partial fill never exceeds the desired
// top-up even when the budget could afford more shares than the target.
func TestPartialFillCappedAtTarget(t *testing.T) {
	const midpoint = 0.50
	const minOrder = 1.0
	// Budget 30 affords 60 shares, but desired is only 10 => place exactly 10, and
	// since the full desired notional (5) fits, this is a FULL fund (not partial).
	b := newRunBudget(30.0)
	d := sizingDecision{TopupShares: 10, TargetNoShares: 10, CommittedNoShares: 0}
	res := fundOrder(d, midpoint, minOrder, b)
	if !res.Place || res.PartialFill {
		t.Fatalf("want full (non-partial) place capped at target, got %+v", res)
	}
	if math.Abs(res.Shares-10.0) > fundingEps {
		t.Errorf("Shares = %v, want target 10 (never above desired)", res.Shares)
	}
}

// TestPartialFillSkipsBelowMinNotional verifies that when remaining is below the
// minimum order notional, a fundable top-up skips with budget_below_min and reserves
// nothing.
func TestPartialFillSkipsBelowMinNotional(t *testing.T) {
	const midpoint = 0.50
	const minOrder = 10.0
	minNotional := minOrder * midpoint // 5.0
	b := newRunBudget(minNotional - 0.5)
	d := sizingDecision{TopupShares: 100, TargetNoShares: 100, CommittedNoShares: 0}
	res := fundOrder(d, midpoint, minOrder, b)

	if !res.Skip || res.Reason != skipBudgetBelowMin {
		t.Fatalf("want skip budget_below_min, got %+v", res)
	}
	if math.Abs(b.Remaining()-(minNotional-0.5)) > fundingEps {
		t.Errorf("budget mutated on skip: Remaining() = %v", b.Remaining())
	}
}

// --- planning-loop tests ---

func quietLoopLogger() (*Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewLogger(&buf, "test-run"), &buf
}

func loopMarket(conditionID string, closeAt time.Time) eligibleMarket {
	return eligibleMarket{
		Market:  polymarket.Market{ConditionID: conditionID},
		Tokens:  binaryTokens{NoTokenID: conditionID + "-NO", YesTokenID: conditionID + "-YES"},
		CloseAt: closeAt,
	}
}

// TestRemainingDecrementsAcrossLoop verifies the shared budget starts at walletUSDC
// and the sum of every planned order's notional stays <= walletUSDC across a loop of
// many funding markets (no aggregate cap, single budget constraint).
func TestRemainingDecrementsAcrossLoop(t *testing.T) {
	logger, _ := quietLoopLogger()
	cfg := sizingCfg()
	cfg.TargetExposurePct = 0.50 // big per-market target so the budget binds, not eligibility

	const wallet = 100.0
	base := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	markets := []eligibleMarket{
		loopMarket("m1", base.Add(1*time.Hour)),
		loopMarket("m2", base.Add(2*time.Hour)),
		loopMarket("m3", base.Add(3*time.Hour)),
		loopMarket("m4", base.Add(4*time.Hour)),
		loopMarket("m5", base.Add(5*time.Hour)),
	}
	inputs := func(eligibleMarket) fundingInputs {
		return fundingInputs{Midpoint: 0.50, MinOrderSize: 1.0, Committed: 0, YesOwned: false}
	}

	planned := planFundedOrders(logger, cfg, wallet, wallet, markets, inputs)

	sum := 0.0
	for _, p := range planned {
		sum += p.Funding.Notional
	}
	if sum > wallet+fundingEps {
		t.Fatalf("sum of planned notionals %v exceeds walletUSDC %v", sum, wallet)
	}
	if len(planned) == 0 {
		t.Fatalf("expected at least one planned order")
	}
}

// TestBudgetLoopOrderingEarliestCloseFirst verifies the loop preserves the
// earliest-close-first ordering of the supplied markets in its planned output.
func TestBudgetLoopOrderingEarliestCloseFirst(t *testing.T) {
	logger, _ := quietLoopLogger()
	cfg := sizingCfg()
	cfg.TargetExposurePct = 0.10

	base := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	// Supplied already earliest-close-first (as discovery guarantees).
	markets := []eligibleMarket{
		loopMarket("early", base.Add(1*time.Hour)),
		loopMarket("mid", base.Add(2*time.Hour)),
		loopMarket("late", base.Add(3*time.Hour)),
	}
	inputs := func(eligibleMarket) fundingInputs {
		return fundingInputs{Midpoint: 0.90, MinOrderSize: 1.0}
	}

	planned := planFundedOrders(logger, cfg, 10_000.0, 10_000.0, markets, inputs)
	if len(planned) != 3 {
		t.Fatalf("expected all 3 markets funded, got %d", len(planned))
	}
	wantOrder := []string{"early", "mid", "late"}
	for i, p := range planned {
		if p.Market.Market.ConditionID != wantOrder[i] {
			t.Errorf("planned[%d] = %q, want %q (earliest-close-first preserved)", i, p.Market.Market.ConditionID, wantOrder[i])
		}
	}
}

// TestBudgetLoopResilienceSkipDoesNotStop verifies an injected per-market input
// error and a per-market sizing skip do not stop the loop: subsequent markets are
// still funded.
func TestBudgetLoopResilienceSkipDoesNotStop(t *testing.T) {
	logger, buf := quietLoopLogger()
	cfg := sizingCfg()
	cfg.TargetExposurePct = 0.10

	base := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	markets := []eligibleMarket{
		loopMarket("err", base.Add(1*time.Hour)),   // input error
		loopMarket("yes", base.Add(2*time.Hour)),   // sizing skip (yes owned)
		loopMarket("good1", base.Add(3*time.Hour)), // funds
		loopMarket("good2", base.Add(4*time.Hour)), // funds
	}
	inputs := func(m eligibleMarket) fundingInputs {
		switch m.Market.ConditionID {
		case "err":
			return fundingInputs{Err: errors.New("min order size undeterminable")}
		case "yes":
			return fundingInputs{Midpoint: 0.90, MinOrderSize: 1.0, YesOwned: true}
		default:
			return fundingInputs{Midpoint: 0.90, MinOrderSize: 1.0}
		}
	}

	planned := planFundedOrders(logger, cfg, 10_000.0, 10_000.0, markets, inputs)
	if len(planned) != 2 {
		t.Fatalf("expected 2 funded markets after the error+skip, got %d", len(planned))
	}
	if planned[0].Market.Market.ConditionID != "good1" || planned[1].Market.Market.ConditionID != "good2" {
		t.Errorf("funded the wrong markets: %q, %q", planned[0].Market.Market.ConditionID, planned[1].Market.Market.ConditionID)
	}
	// The injected error must be logged (continued past, not aborted).
	if !strings.Contains(buf.String(), "min order size undeterminable") {
		t.Errorf("input error not logged; loop may have swallowed it")
	}
}

// TestBudgetLoopNoAggregateCapFundsAllMarkets verifies a run with budget for N
// markets funds across ALL N eligible markets — there is no portfolio-wide ceiling
// beyond the shared budget and per-market target.
func TestBudgetLoopNoAggregateCapFundsAllMarkets(t *testing.T) {
	logger, _ := quietLoopLogger()
	cfg := sizingCfg()
	cfg.TargetExposurePct = 0.01 // each market wants a small 1% slice

	const n = 25
	base := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	markets := make([]eligibleMarket, 0, n)
	for i := 0; i < n; i++ {
		markets = append(markets, loopMarket("m"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Hour)))
	}
	// Wallet large enough that 25 * (1% of wallet) << wallet, so the budget never
	// binds and all N markets must fund.
	const wallet = 1_000_000.0
	inputs := func(eligibleMarket) fundingInputs {
		return fundingInputs{Midpoint: 0.90, MinOrderSize: 1.0}
	}

	planned := planFundedOrders(logger, cfg, wallet, wallet, markets, inputs)
	if len(planned) != n {
		t.Fatalf("no-aggregate-cap: funded %d markets, want all %d", len(planned), n)
	}
}

// TestBudgetSourceIsWalletUSDC asserts the budget is seeded from the snapshot's
// WalletUSDC field (the wallet helper), not any exchange/CLOB balance. The snapshot
// struct exposes WalletUSDC and intentionally has no exchange/CLOB-balance accessor.
func TestBudgetSourceIsWalletUSDC(t *testing.T) {
	snap := accountSnapshot{WalletUSDC: 777.0, PositionsValue: 999.0, Total: 1776.0}
	b := newRunBudget(snap.WalletUSDC)
	if math.Abs(b.Remaining()-snap.WalletUSDC) > fundingEps {
		t.Errorf("budget = %v, want WalletUSDC %v", b.Remaining(), snap.WalletUSDC)
	}
	// The budget must NOT be seeded from Total or PositionsValue (no CLOB balance).
	if math.Abs(b.Remaining()-snap.Total) < fundingEps {
		t.Errorf("budget seeded from Total %v; must be WalletUSDC only", snap.Total)
	}
	if math.Abs(b.Remaining()-snap.PositionsValue) < fundingEps {
		t.Errorf("budget seeded from PositionsValue %v; must be WalletUSDC only", snap.PositionsValue)
	}
}
