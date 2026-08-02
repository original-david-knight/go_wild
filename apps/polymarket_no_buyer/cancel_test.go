package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// staleTestApp wires an App to the fake at the fixed eligibility run time so
// close-time arithmetic lines up with the markets built below.
func staleTestApp(fake *fakeTradingClient, dryRun bool) *App {
	ensureYesBooks(fake)
	cfg := defaultConfig()
	cfg.DryRun = dryRun
	return &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return runNow },
	}
}

// staleBook returns a two-sided NO book whose midpoint is 0.94 (bid 0.93, ask 0.95)
// and which carries the 0.01 venue tick the cancel pass normalizes against. The
// book is built via JSON because OrderBookDetail.TickSize uses an unexported type
// that cannot be named from the test package.
func staleBook() *polymarket.OrderBookDetail {
	const raw = `{
		"bids": [{"price": "0.93", "size": "100"}],
		"asks": [{"price": "0.95", "size": "100"}],
		"tick_size": "0.01"
	}`
	var book polymarket.OrderBookDetail
	if err := json.Unmarshal([]byte(raw), &book); err != nil {
		panic(err)
	}
	return &book
}

// staleExpiry is the expected order expiration for an eligible market built with
// marketWith(...7d...): close = runNow+7d, expiry = close - 24h, as unix seconds.
func staleExpiry(closeIn time.Duration) string {
	closeAt := runNow.Add(closeIn)
	return strconv.FormatInt(closeAt.Add(-defaultOrderExpiryBefore).Unix(), 10)
}

// matchingOrder builds a YES buy order that matches every order-level criterion for
// a market built with marketWith(condID, noID, yesID, closeIn, _): YES token asset, BUY
// side, normalized price == midpoint (0.94), expiration == close-24h. Tests mutate
// one field at a time to isolate a single stale reason.
func matchingOrder(id, condID, noID string, closeIn time.Duration) polymarket.Order {
	yesID := "yes" + strings.TrimPrefix(noID, "no")
	return polymarket.Order{
		ID:           id,
		Market:       condID,
		AssetID:      yesID,
		Side:         "BUY",
		OriginalSize: "100",
		Price:        "0.94",
		Expiration:   staleExpiry(closeIn),
	}
}

// staleFakeFor wires a fake serving one eligible NO market (condID/noID/yesID
// closing closeIn out) with the staleBook midpoint, for the given open orders.
func staleFakeFor(condID, noID, yesID string, closeIn time.Duration, orders []polymarket.Order) *fakeTradingClient {
	m := marketWith(condID, noID, yesID, closeIn, "10000")
	return &fakeTradingClient{
		openOrders:   orders,
		gammaMarkets: map[string]*polymarket.Market{condID: &m},
		books:        map[string]*polymarket.OrderBookDetail{noID: staleBook()},
	}
}

// ---- precision normalization helpers ----

func TestStaleCancel_NormalizePrice(t *testing.T) {
	const tick = 0.01
	cases := []struct {
		raw  float64
		want float64
	}{
		{0.94, 0.94},      // exactly on tick
		{0.944, 0.94},     // just above, rounds down
		{0.946, 0.95},     // just above half, rounds up
		{0.9449999, 0.94}, // float-dirty value below half
	}
	for _, c := range cases {
		if got := normalizePrice(c.raw, tick); !approxEqual(got, c.want) {
			t.Errorf("normalizePrice(%v, %v) = %v, want %v", c.raw, tick, got, c.want)
		}
	}

	// Two raws on the same side of the half-tick boundary quantize to the same tick
	// (both to 0.94) and compare equal.
	if !pricesEqual(0.9412, 0.9444, tick) {
		t.Errorf("0.9412 and 0.9444 should normalize equal on a 0.01 tick (both 0.94)")
	}
	// Two raws straddling the half-tick boundary round to different ticks.
	if pricesEqual(0.9449, 0.9451, tick) {
		t.Errorf("0.9449 (→0.94) and 0.9451 (→0.95) must not normalize equal")
	}
	// A raw a full tick away does not.
	if pricesEqual(0.94, 0.95, tick) {
		t.Errorf("0.94 and 0.95 must not normalize equal on a 0.01 tick")
	}

	// Unknown tick (<=0): unprovable, so pricesEqual is false (fail closed) and
	// normalizePrice returns the input unchanged.
	if pricesEqual(0.94, 0.94, 0) {
		t.Errorf("pricesEqual must be false when tick is unknown")
	}
	if got := normalizePrice(0.937, 0); got != 0.937 {
		t.Errorf("normalizePrice with unknown tick = %v, want passthrough 0.937", got)
	}
}

func TestStaleCancel_NormalizeSize(t *testing.T) {
	cases := []struct {
		raw  float64
		want float64
	}{
		{12.00, 12.00},
		{12.004, 12.00}, // below half, rounds down
		{12.006, 12.01}, // above half, rounds up
		{12.005, 12.01}, // exactly half, rounds away from zero
	}
	for _, c := range cases {
		if got := normalizeSize(c.raw); !approxEqual(got, c.want) {
			t.Errorf("normalizeSize(%v) = %v, want %v", c.raw, got, c.want)
		}
	}
	// Two raws that quantize equal compare equal after normalization.
	if normalizeSize(12.004) != normalizeSize(11.9999+0.0041) {
		// 11.9999+0.0041 == 12.004 within float noise; both round to 12.00.
		t.Errorf("two raws quantizing to 12.00 should compare equal")
	}
}

func approxEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// ---- per-order stale-reason table ----

func TestStaleCancel_ReasonTable(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	const condID, noID, yesID = "0xstale", "noStale", "yesStale"

	cases := []struct {
		name       string
		mutate     func(o *polymarket.Order)
		market     func(m *polymarket.Market)
		positions  []polymarket.Position
		wantReason cancelReason
	}{
		{
			name:       "market_closed",
			market:     func(m *polymarket.Market) { m.Closed = true },
			wantReason: cancelMarketIneligible,
		},
		{
			name:       "market_inactive",
			market:     func(m *polymarket.Market) { m.Active = false },
			wantReason: cancelMarketIneligible,
		},
		{
			name:       "no_shares_owned",
			positions:  []polymarket.Position{{Asset: noID, Size: 5}},
			wantReason: cancelNoSharesOwned,
		},
		{
			name:       "wrong_side",
			mutate:     func(o *polymarket.Order) { o.Side = "SELL" },
			wantReason: cancelWrongSide,
		},
		{
			name:       "wrong_asset",
			mutate:     func(o *polymarket.Order) { o.AssetID = noID },
			wantReason: cancelWrongAsset,
		},
		{
			name:       "price_mismatch",
			mutate:     func(o *polymarket.Order) { o.Price = "0.90" },
			wantReason: cancelPriceMismatch,
		},
		{
			name:       "expiration_mismatch",
			mutate:     func(o *polymarket.Order) { o.Expiration = "12345" },
			wantReason: cancelExpirationMismatch,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := matchingOrder("ord1", condID, noID, closeIn)
			if c.mutate != nil {
				c.mutate(&o)
			}
			fake := staleFakeFor(condID, noID, yesID, closeIn, []polymarket.Order{o})
			fake.positions = c.positions
			if c.market != nil {
				m := marketWith(condID, noID, yesID, closeIn, "10000")
				c.market(&m)
				fake.gammaMarkets[condID] = &m
			}
			app := staleTestApp(fake, false)

			var buf bytes.Buffer
			logger := NewLogger(&buf, "run_stale")
			if err := app.stalePass(context.Background(), logger); err != nil {
				t.Fatalf("stalePass: %v", err)
			}

			if len(fake.canceledOrders) != 1 || fake.canceledOrders[0] != "ord1" {
				t.Fatalf("canceledOrders = %v, want [ord1]", fake.canceledOrders)
			}
			line := findStaleOrder(t, buf.String(), "ord1")
			if line["status"] != "canceled" {
				t.Errorf("status = %v, want canceled", line["status"])
			}
			if line["reason"] != c.wantReason.String() {
				t.Errorf("reason = %v, want %q", line["reason"], c.wantReason)
			}
		})
	}
}

// TestStaleCancel_SurvivorKept asserts a fully-matching NO order on an eligible
// market is kept (not canceled).
func TestStaleCancel_SurvivorKept(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	o := matchingOrder("keep1", "0xkeep", "noKeep", closeIn)
	fake := staleFakeFor("0xkeep", "noKeep", "yesKeep", closeIn, []polymarket.Order{o})
	app := staleTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_keep")
	if err := app.stalePass(context.Background(), logger); err != nil {
		t.Fatalf("stalePass: %v", err)
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("canceledOrders = %v, want none (matching order is the candidate)", fake.canceledOrders)
	}
	line := findStaleOrder(t, buf.String(), "keep1")
	if line["status"] != "kept" {
		t.Errorf("status = %v, want kept", line["status"])
	}
}

// TestStaleCancel_AmountOnlyDifferenceKept asserts an order that matches side,
// asset, normalized price, and expiration but differs ONLY in amount is NOT
// canceled — size reconciliation is a later rung.
func TestStaleCancel_AmountOnlyDifferenceKept(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	o := matchingOrder("amt1", "0xamt", "noAmt", closeIn)
	o.OriginalSize = "999" // differs from any desired size; must not trigger cancel
	fake := staleFakeFor("0xamt", "noAmt", "yesAmt", closeIn, []polymarket.Order{o})
	app := staleTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_amt")
	if err := app.stalePass(context.Background(), logger); err != nil {
		t.Fatalf("stalePass: %v", err)
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("amount-only difference must not cancel; canceled = %v", fake.canceledOrders)
	}
	if findStaleOrder(t, buf.String(), "amt1")["status"] != "kept" {
		t.Errorf("amount-only order should be kept")
	}
}

// TestStaleCancel_DuplicatesKeepLowestID asserts that among N matching YES buy
// orders for one market, exactly one (the lowest ID) is kept and the rest are
// canceled as duplicates, deterministically.
func TestStaleCancel_DuplicatesKeepLowestID(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	// Feed duplicates out of ID order to prove the keep choice is by ID, not input.
	o2 := matchingOrder("ord2", "0xdup", "noDup", closeIn)
	o1 := matchingOrder("ord1", "0xdup", "noDup", closeIn)
	o3 := matchingOrder("ord3", "0xdup", "noDup", closeIn)
	fake := staleFakeFor("0xdup", "noDup", "yesDup", closeIn, []polymarket.Order{o2, o1, o3})
	app := staleTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_dup")
	if err := app.stalePass(context.Background(), logger); err != nil {
		t.Fatalf("stalePass: %v", err)
	}

	if len(fake.canceledOrders) != 2 {
		t.Fatalf("canceled = %v, want 2 duplicates canceled", fake.canceledOrders)
	}
	// ord1 is the lowest ID — it must survive; ord2 and ord3 are canceled.
	for _, id := range fake.canceledOrders {
		if id == "ord1" {
			t.Fatalf("lowest-ID order ord1 was canceled: %v", fake.canceledOrders)
		}
	}
	if findStaleOrder(t, buf.String(), "ord1")["status"] != "kept" {
		t.Errorf("ord1 should be the kept candidate")
	}
	for _, id := range []string{"ord2", "ord3"} {
		line := findStaleOrder(t, buf.String(), id)
		if line["status"] != "canceled" || line["reason"] != cancelDuplicateOrder.String() {
			t.Errorf("%s line = %v, want canceled/%q", id, line, cancelDuplicateOrder)
		}
	}
}

// TestStaleCancel_DryRunNoSubmit asserts dry-run logs intended cancellations with
// reasons and calls CancelOrder zero times.
func TestStaleCancel_DryRunNoSubmit(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	stale := matchingOrder("dry1", "0xdry", "noDry", closeIn)
	stale.Price = "0.50" // price mismatch ⇒ would cancel
	fake := staleFakeFor("0xdry", "noDry", "yesDry", closeIn, []polymarket.Order{stale})
	app := staleTestApp(fake, true) // dry-run

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_dry")
	if err := app.stalePass(context.Background(), logger); err != nil {
		t.Fatalf("stalePass: %v", err)
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("dry-run made %d cancel calls, want 0", len(fake.canceledOrders))
	}
	line := findStaleOrder(t, buf.String(), "dry1")
	if line["status"] != "would_cancel" || line["reason"] != cancelPriceMismatch.String() {
		t.Errorf("dry-run line = %v, want would_cancel/%q", line, cancelPriceMismatch)
	}
}

// TestStaleCancel_FailureIsolation asserts a forced CancelOrder error for one
// market does not prevent cancels for unrelated markets, and stalePass returns nil.
func TestStaleCancel_FailureIsolation(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	// Two unrelated markets, each with one stale (wrong-side) order.
	mA := marketWith("0xA", "noA", "yesA", closeIn, "10000")
	mB := marketWith("0xB", "noB", "yesB", closeIn, "10000")
	oA := matchingOrder("ordA", "0xA", "noA", closeIn)
	oA.Side = "SELL"
	oB := matchingOrder("ordB", "0xB", "noB", closeIn)
	oB.Side = "SELL"

	fake := &fakeTradingClient{
		openOrders: []polymarket.Order{oA, oB},
		gammaMarkets: map[string]*polymarket.Market{
			"0xA": &mA,
			"0xB": &mB,
		},
		books: map[string]*polymarket.OrderBookDetail{
			"noA": staleBook(),
			"noB": staleBook(),
		},
		cancelErrByOrderID: map[string]error{"ordA": errContext("cancel boom")},
	}
	app := staleTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_iso")
	if err := app.stalePass(context.Background(), logger); err != nil {
		t.Fatalf("stalePass returned %v, want nil (failure must be isolated)", err)
	}
	// Both cancels were attempted despite ordA failing.
	if len(fake.canceledOrders) != 2 {
		t.Fatalf("canceled attempts = %v, want both ordA and ordB attempted", fake.canceledOrders)
	}
	if findStaleOrder(t, buf.String(), "ordA")["status"] != "failed" {
		t.Errorf("ordA should log status=failed")
	}
	if findStaleOrder(t, buf.String(), "ordB")["status"] != "canceled" {
		t.Errorf("ordB should be canceled despite ordA failing")
	}
}

// TestStaleCancel_FetchErrorSkipsMarket asserts that a market fetch error skips the
// market's orders (fail closed: do not cancel what cannot be evaluated) without
// aborting unrelated markets.
func TestStaleCancel_FetchErrorSkipsMarket(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	mB := marketWith("0xB", "noB", "yesB", closeIn, "10000")
	oA := matchingOrder("ordA", "0xA", "noA", closeIn)
	oA.Side = "SELL" // would be canceled if evaluable
	oB := matchingOrder("ordB", "0xB", "noB", closeIn)
	oB.Side = "SELL"

	fake := &fakeTradingClient{
		openOrders:           []polymarket.Order{oA, oB},
		gammaMarkets:         map[string]*polymarket.Market{"0xB": &mB},
		gammaMarketErrByCond: map[string]error{"0xA": errContext("gamma down")},
		books:                map[string]*polymarket.OrderBookDetail{"noB": staleBook()},
	}
	app := staleTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_fetcherr")
	if err := app.stalePass(context.Background(), logger); err != nil {
		t.Fatalf("stalePass: %v", err)
	}
	// 0xA could not be evaluated ⇒ ordA is NOT canceled; 0xB proceeds.
	if len(fake.canceledOrders) != 1 || fake.canceledOrders[0] != "ordB" {
		t.Fatalf("canceled = %v, want [ordB] (0xA skipped on fetch error)", fake.canceledOrders)
	}
}

// TestStaleCancel_OrderingAfterRedeem asserts the stale pass runs after redemption
// and that runOnce invokes GetOrders only after GetPositions/RedeemWinnings.
func TestStaleCancel_OrderingAfterRedeem(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	o := matchingOrder("ord1", "0xc", "noC", closeIn)
	o.Side = "SELL"
	fake := staleFakeFor("0xc", "noC", "yesC", closeIn, []polymarket.Order{o})
	fake.positions = []polymarket.Position{{ConditionID: "0xredeem", Redeemable: true, Size: 1}}
	app := staleTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_order")
	app.runOnce(context.Background(), logger)

	order := fake.callOrder()
	redeemIdx, getOrdersIdx := -1, -1
	for i, c := range order {
		if c == "RedeemWinnings:" && redeemIdx == -1 {
			redeemIdx = i
		}
		if c == "GetOrders:" && getOrdersIdx == -1 {
			getOrdersIdx = i
		}
	}
	if redeemIdx == -1 || getOrdersIdx == -1 || getOrdersIdx < redeemIdx {
		t.Fatalf("stale pass must run after redeem; call order = %v", order)
	}
}

// findStaleOrder returns the stale_order log line for the given order ID.
func findStaleOrder(t *testing.T, out, orderID string) map[string]any {
	t.Helper()
	for _, e := range eventsNamed(parseEvents(t, out), "stale_order") {
		if e["order_id"] == orderID {
			return e
		}
	}
	t.Fatalf("no stale_order log line for order %q in:\n%s", orderID, out)
	return nil
}
