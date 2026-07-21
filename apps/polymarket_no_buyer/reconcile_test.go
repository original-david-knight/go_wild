package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// reconcileTestApp wires an App to the fake at the fixed eligibility run time so
// close-time arithmetic lines up with the markets built via marketWith(...).
func reconcileTestApp(fake *fakeTradingClient, dryRun bool) *App {
	cfg := defaultConfig()
	cfg.DryRun = dryRun
	return &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return runNow },
	}
}

// reconcileBook builds a two-sided NO book with the given bid/ask, the 0.01 venue
// tick, and a positive min_order_size so resolveMinOrderSize reads it directly from
// the book (no GetClobMarket call). Built via JSON because TickSize / MinOrderSize use
// unexported types the test package cannot name.
func reconcileBook(bid, ask string, minOrderSize float64) *polymarket.OrderBookDetail {
	raw := fmt.Sprintf(`{
		"bids": [{"price": %q, "size": "1000"}],
		"asks": [{"price": %q, "size": "1000"}],
		"tick_size": "0.01",
		"min_order_size": "%g"
	}`, bid, ask, minOrderSize)
	var book polymarket.OrderBookDetail
	if err := json.Unmarshal([]byte(raw), &book); err != nil {
		panic(err)
	}
	return &book
}

// reconcileEligible builds the eligibleMarket discovery would have produced for a
// market built with marketWith(condID, noID, yesID, closeIn, ...): the source market,
// decoded tokens, parsed close time, and the supplied midpoint.
func reconcileEligible(condID, noID, yesID string, closeIn time.Duration, midpoint float64) eligibleMarket {
	m := marketWith(condID, noID, yesID, closeIn, "10000")
	tokens, _ := decodeBinaryTokens(m)
	return eligibleMarket{
		Market:   m,
		Tokens:   tokens,
		CloseAt:  runNow.Add(closeIn),
		Midpoint: noMidpoint{Midpoint: midpoint},
	}
}

// reconcileExpiry is the desired GTD expiration for a market closing closeIn out:
// close - OrderExpiryBeforeClose, in unix seconds.
func reconcileExpiry(closeIn time.Duration) int64 {
	return runNow.Add(closeIn).Add(-defaultOrderExpiryBefore).Unix()
}

func snapWith(walletUSDC, total float64) accountSnapshot {
	return accountSnapshot{WalletUSDC: walletUSDC, PositionsValue: total - walletUSDC, Total: total}
}

// TestReconcileFreshRecheckEarliestCloseFirst verifies markets are processed
// earliest-close-first and that each one triggers a FRESH book fetch (and thus a
// fresh midpoint/eligibility re-check) before any order decision.
func TestReconcileFreshRecheckEarliestCloseFirst(t *testing.T) {
	early := reconcileEligible("0xearly", "noEarly", "yesEarly", 3*24*time.Hour, 0.94)
	late := reconcileEligible("0xlate", "noLate", "yesLate", 10*24*time.Hour, 0.94)

	fake := &fakeTradingClient{
		books: map[string]*polymarket.OrderBookDetail{
			"noEarly": reconcileBook("0.93", "0.95", 5),
			"noLate":  reconcileBook("0.93", "0.95", 5),
		},
	}
	app := reconcileTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_fresh")
	// Supplied earliest-close-first (as discovery guarantees).
	app.reconcilePass(context.Background(), logger, snapWith(10_000, 10_000), []eligibleMarket{early, late})

	// Each market's NO book was fetched fresh, in earliest-close-first order.
	var noBookFetches []string
	for _, tok := range fake.bookCalls {
		if tok == "noEarly" || tok == "noLate" {
			noBookFetches = append(noBookFetches, tok)
		}
	}
	if len(noBookFetches) != 2 || noBookFetches[0] != "noEarly" || noBookFetches[1] != "noLate" {
		t.Fatalf("fresh NO book fetch order = %v, want [noEarly noLate]", noBookFetches)
	}
}

// TestReconcileFreshMidpointOutOfBandPlacesNothing verifies a market that passed
// discovery but whose RE-CHECKED midpoint has fallen to <= MinNoMidpoint places no
// order (the fresh eligibility re-check rejects it).
func TestReconcileFreshMidpointOutOfBandPlacesNothing(t *testing.T) {
	// Discovery saw 0.94; the fresh book now quotes a midpoint of 0.50 (bid 0.49,
	// ask 0.51), far below MinNoMidpoint (0.89).
	m := reconcileEligible("0xdrift", "noDrift", "yesDrift", 7*24*time.Hour, 0.94)
	fake := &fakeTradingClient{
		books: map[string]*polymarket.OrderBookDetail{
			"noDrift": reconcileBook("0.49", "0.51", 5),
		},
	}
	app := reconcileTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_drift")
	app.reconcilePass(context.Background(), logger, snapWith(10_000, 10_000), []eligibleMarket{m})

	if len(fake.placedOrders) != 0 {
		t.Fatalf("placed %d orders, want 0 (fresh midpoint out of band)", len(fake.placedOrders))
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("canceled %d orders, want 0", len(fake.canceledOrders))
	}
	skips := eventsNamed(parseEvents(t, buf.String()), "reconcile_skip")
	found := false
	for _, e := range skips {
		if e["condition_id"] == "0xdrift" && e["reason"] == skipMidpointTooLow.String() {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a reconcile_skip for 0xdrift with reason %q; skips = %v", skipMidpointTooLow, skips)
	}
}

// TestReconcileMaintainsMatchingOrder verifies an existing order matching
// side+asset+normalized-price+remaining-size+expiration is LEFT unchanged (zero
// cancel, zero place) and its notional is reserved against the budget.
func TestReconcileMaintainsMatchingOrder(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	m := reconcileEligible("0xmatch", "noMatch", "yesMatch", closeIn, 0.94)

	// Target: 2% of Total(10_000) = 200 notional / 0.94 midpoint = 212.7659... shares.
	// Held 0, so the desired order size is exactly that, FLOORED to the venue's
	// 2-decimal grid (212.76, NOT the round-half 212.77) so the control order matches
	// what the venue actually stores.
	wantShares := (10_000 * defaultTargetExposurePct) / 0.94
	existing := polymarket.Order{
		ID:           "ord-match",
		Market:       "0xmatch",
		AssetID:      "noMatch",
		Side:         "BUY",
		Price:        "0.94",
		OriginalSize: strconv.FormatFloat(floorToSizePrecision(wantShares), 'f', 2, 64),
		SizeMatched:  "0",
		Expiration:   strconv.FormatInt(reconcileExpiry(closeIn), 10),
	}

	fake := &fakeTradingClient{
		openOrders: []polymarket.Order{existing},
		books:      map[string]*polymarket.OrderBookDetail{"noMatch": reconcileBook("0.93", "0.95", 5)},
	}
	app := reconcileTestApp(fake, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_match")
	app.reconcilePass(context.Background(), logger, snapWith(10_000, 10_000), []eligibleMarket{m})

	if len(fake.placedOrders) != 0 {
		t.Fatalf("placed %d orders, want 0 (matching order maintained)", len(fake.placedOrders))
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("canceled %d orders, want 0 (matching order maintained)", len(fake.canceledOrders))
	}

	// The maintained notional was reserved: the done event's remaining budget reflects
	// it (wallet - shares*midpoint).
	done := eventsNamed(parseEvents(t, buf.String()), "reconcile_done")
	if len(done) != 1 {
		t.Fatalf("reconcile_done count = %d, want 1", len(done))
	}
	gotRemaining, _ := done[0]["budget_remaining"].(float64)
	// The reserved notional is the funding notional (shares * midpoint), where shares
	// is the full un-normalized top-up: target notional == Total * pct == 200.
	wantRemaining := 10_000 - (10_000 * defaultTargetExposurePct)
	if absDiff(gotRemaining, wantRemaining) > 1e-6 {
		t.Errorf("budget_remaining = %v, want %v (maintained notional reserved once)", gotRemaining, wantRemaining)
	}

	// And the order was logged maintained.
	if !hasReconcileStatus(t, buf.String(), "0xmatch", "maintained") {
		t.Errorf("expected a maintained reconcile_order for 0xmatch")
	}
}

// TestReconcileCancelReplaceOnDivergence verifies an existing order diverging on any
// of price, size, side, or expiration triggers exactly one cancel + one place, and
// the replacement's price == normalized midpoint and expiration == close - 24h.
func TestReconcileCancelReplaceOnDivergence(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	wantExpiry := reconcileExpiry(closeIn)

	cases := []struct {
		name   string
		mutate func(o *polymarket.Order)
	}{
		{"price diverges", func(o *polymarket.Order) { o.Price = "0.90" }},
		{"size diverges", func(o *polymarket.Order) { o.OriginalSize = "1.00" }},
		{"side diverges", func(o *polymarket.Order) { o.Side = "SELL" }},
		{"expiration diverges", func(o *polymarket.Order) { o.Expiration = strconv.FormatInt(wantExpiry+60, 10) }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := reconcileEligible("0xdiv", "noDiv", "yesDiv", closeIn, 0.94)
			wantShares := (10_000 * defaultTargetExposurePct) / 0.94
			o := polymarket.Order{
				ID:           "ord-div",
				Market:       "0xdiv",
				AssetID:      "noDiv",
				Side:         "BUY",
				Price:        "0.94",
				OriginalSize: strconv.FormatFloat(floorToSizePrecision(wantShares), 'f', 2, 64),
				SizeMatched:  "0",
				Expiration:   strconv.FormatInt(wantExpiry, 10),
			}
			c.mutate(&o)

			fake := &fakeTradingClient{
				openOrders: []polymarket.Order{o},
				books:      map[string]*polymarket.OrderBookDetail{"noDiv": reconcileBook("0.93", "0.95", 5)},
			}
			app := reconcileTestApp(fake, false)

			var buf bytes.Buffer
			logger := NewLogger(&buf, "run_div")
			app.reconcilePass(context.Background(), logger, snapWith(10_000, 10_000), []eligibleMarket{m})

			// A SELL order is not a NO-buy candidate, so it is NOT canceled; the pass
			// simply places the (missing) desired order. Every other divergence cancels
			// the one NO-buy order and replaces it.
			if c.name == "side diverges" {
				if len(fake.canceledOrders) != 0 {
					t.Fatalf("side divergence canceled %v, want 0 (SELL is not a NO-buy)", fake.canceledOrders)
				}
			} else {
				if len(fake.canceledOrders) != 1 || fake.canceledOrders[0] != "ord-div" {
					t.Fatalf("canceled = %v, want exactly [ord-div]", fake.canceledOrders)
				}
			}
			if len(fake.placedOrders) != 1 {
				t.Fatalf("placed %d orders, want exactly 1", len(fake.placedOrders))
			}
			placed := fake.placedOrders[0]
			if placed.tokenID != "noDiv" || placed.side != polymarket.Buy {
				t.Errorf("placed token/side = %q/%q, want noDiv/BUY", placed.tokenID, placed.side)
			}
			// Replacement price == normalized midpoint (0.94 on the 0.01 tick).
			if absDiff(placed.price, 0.94) > 1e-9 {
				t.Errorf("placed price = %v, want normalized midpoint 0.94", placed.price)
			}
			// Replacement expiration == close - 24h, to the second.
			if placed.expiration != wantExpiry {
				t.Errorf("placed expiration = %d, want %d (close - 24h)", placed.expiration, wantExpiry)
			}
		})
	}
}

// TestReconcileExpiryExactAndPastSkips verifies the placed GTD expiration equals
// (close - OrderExpiryBeforeClose).Unix() exactly, and that a market whose computed
// expiration is in the past places nothing.
func TestReconcileExpiryExactAndPastSkips(t *testing.T) {
	t.Run("exact expiration", func(t *testing.T) {
		const closeIn = 7 * 24 * time.Hour
		m := reconcileEligible("0xexp", "noExp", "yesExp", closeIn, 0.94)
		fake := &fakeTradingClient{
			books: map[string]*polymarket.OrderBookDetail{"noExp": reconcileBook("0.93", "0.95", 5)},
		}
		app := reconcileTestApp(fake, false)

		var buf bytes.Buffer
		app.reconcilePass(context.Background(), NewLogger(&buf, "run_exp"), snapWith(10_000, 10_000), []eligibleMarket{m})

		if len(fake.placedOrders) != 1 {
			t.Fatalf("placed %d orders, want 1", len(fake.placedOrders))
		}
		want := runNow.Add(closeIn).Add(-defaultOrderExpiryBefore).Unix()
		if fake.placedOrders[0].expiration != want {
			t.Errorf("expiration = %d, want %d (close - OrderExpiryBeforeClose, exact)", fake.placedOrders[0].expiration, want)
		}
	})

	t.Run("past expiration skips placement", func(t *testing.T) {
		// Close 12h out; minus the 24h pre-close expiry yields an expiration 12h BEFORE
		// runNow — already in the past. (Close-time window eligibility is bypassed here
		// because the fresh predicate re-check would also reject too-soon close; to
		// isolate the expiration guard we hand-build an eligibleMarket whose close
		// passes the window but whose expiry math is forced negative via a tiny
		// OrderExpiryBeforeClose-exceeding close. Use a close inside the window but a
		// large OrderExpiryBeforeClose.)
		const closeIn = 50 * time.Hour // just inside (48h, 336h)
		m := reconcileEligible("0xpast", "noPast", "yesPast", closeIn, 0.94)
		fake := &fakeTradingClient{
			books: map[string]*polymarket.OrderBookDetail{"noPast": reconcileBook("0.93", "0.95", 5)},
		}
		app := reconcileTestApp(fake, false)
		// Make the pre-close expiry window exceed the close offset so expiry < now.
		app.cfg.OrderExpiryBeforeClose = 100 * time.Hour

		var buf bytes.Buffer
		app.reconcilePass(context.Background(), NewLogger(&buf, "run_past"), snapWith(10_000, 10_000), []eligibleMarket{m})

		if len(fake.placedOrders) != 0 {
			t.Fatalf("placed %d orders, want 0 (expiration in the past)", len(fake.placedOrders))
		}
		skips := eventsNamed(parseEvents(t, buf.String()), "reconcile_skip")
		found := false
		for _, e := range skips {
			if e["condition_id"] == "0xpast" && e["reason"] == "expiration_not_in_future" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a reconcile_skip for 0xpast with reason expiration_not_in_future; skips = %v", skips)
		}
	})
}

// TestReconcileDryRunLogsButSubmitsNothing verifies dry-run drives a place decision
// with the full detail fields but the fake records ZERO submitted places and ZERO
// cancels.
func TestReconcileDryRunLogsButSubmitsNothing(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	m := reconcileEligible("0xdry", "noDry", "yesDry", closeIn, 0.94)

	// A diverging existing order so the decision is a cancel-replace (exercises both
	// the cancel and place log paths under dry-run).
	o := polymarket.Order{
		ID: "ord-dry", Market: "0xdry", AssetID: "noDry", Side: "BUY",
		Price: "0.90", OriginalSize: "1.00", SizeMatched: "0",
		Expiration: strconv.FormatInt(reconcileExpiry(closeIn), 10),
	}
	fake := &fakeTradingClient{
		openOrders: []polymarket.Order{o},
		books:      map[string]*polymarket.OrderBookDetail{"noDry": reconcileBook("0.93", "0.95", 5)},
	}
	app := reconcileTestApp(fake, true) // dry-run

	var buf bytes.Buffer
	app.reconcilePass(context.Background(), NewLogger(&buf, "run_dry"), snapWith(10_000, 10_000), []eligibleMarket{m})

	if len(fake.placedOrders) != 0 {
		t.Fatalf("dry-run placed %d orders, want 0", len(fake.placedOrders))
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("dry-run canceled %d orders, want 0", len(fake.canceledOrders))
	}

	events := parseEvents(t, buf.String())
	// A would_place line with the full detail set.
	var wouldPlace map[string]any
	for _, e := range eventsNamed(events, "reconcile_order") {
		if e["condition_id"] == "0xdry" && e["status"] == "would_place" {
			wouldPlace = e
		}
	}
	if wouldPlace == nil {
		t.Fatalf("expected a would_place reconcile_order for 0xdry")
	}
	for _, field := range []string{"question", "no_token_id", "midpoint", "shares", "notional", "close_at", "expiration", "run_usdc_remaining", "min_order_exception", "partial_fill"} {
		if _, ok := wouldPlace[field]; !ok {
			t.Errorf("would_place missing detail field %q; got %v", field, wouldPlace)
		}
	}
	// A would_cancel line for the diverging order.
	if !hasReconcileStatus(t, buf.String(), "0xdry", "would_cancel") {
		t.Errorf("expected a would_cancel reconcile_order for 0xdry")
	}
}

// TestReconcileIdempotency is the critical convergence test: running reconcilePass
// twice against a frozen fixture must place the intended order on the first run and
// then, with that exact order present in open orders, place ZERO new orders and
// cancel ZERO orders on the second run (it is maintained).
func TestReconcileIdempotency(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	m := reconcileEligible("0xidem", "noIdem", "yesIdem", closeIn, 0.94)
	snap := snapWith(10_000, 10_000)

	fake := &fakeTradingClient{
		books: map[string]*polymarket.OrderBookDetail{"noIdem": reconcileBook("0.93", "0.95", 5)},
	}
	app := reconcileTestApp(fake, false)

	// First run: no open orders, so exactly one order is placed.
	var buf1 bytes.Buffer
	app.reconcilePass(context.Background(), NewLogger(&buf1, "run_idem_1"), snap, []eligibleMarket{m})
	if len(fake.placedOrders) != 1 {
		t.Fatalf("first run placed %d orders, want 1", len(fake.placedOrders))
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("first run canceled %d orders, want 0", len(fake.canceledOrders))
	}

	// Feed the placed order back as an open order, exactly as the venue would report
	// it (BUY on the NO token, at the placed price, full size, the placed expiration).
	// The fake already records placed.size on the venue's FLOOR grid, so it carries no
	// sub-cent precision — format it straight to 2 decimals.
	placed := fake.placedOrders[0]
	fake.openOrders = []polymarket.Order{{
		ID:           "ord-idem",
		Market:       "0xidem",
		AssetID:      placed.tokenID,
		Side:         polymarket.Buy,
		Price:        strconv.FormatFloat(placed.price, 'f', -1, 64),
		OriginalSize: strconv.FormatFloat(placed.size, 'f', 2, 64),
		SizeMatched:  "0",
		Expiration:   strconv.FormatInt(placed.expiration, 10),
	}}
	placesAfterFirst := len(fake.placedOrders)
	cancelsAfterFirst := len(fake.canceledOrders)

	// Second run against the SAME frozen fixture: the order is maintained.
	var buf2 bytes.Buffer
	app.reconcilePass(context.Background(), NewLogger(&buf2, "run_idem_2"), snap, []eligibleMarket{m})

	if newPlaces := len(fake.placedOrders) - placesAfterFirst; newPlaces != 0 {
		t.Fatalf("second run placed %d NEW orders, want 0 (must converge/maintain)", newPlaces)
	}
	if newCancels := len(fake.canceledOrders) - cancelsAfterFirst; newCancels != 0 {
		t.Fatalf("second run canceled %d orders, want 0 (must converge/maintain)", newCancels)
	}
	if !hasReconcileStatus(t, buf2.String(), "0xidem", "maintained") {
		t.Errorf("second run did not maintain the existing order")
	}
}

// TestReconcileIdempotencyPartialFill is the convergence guarantee for budget-bound
// PARTIAL fills. With a wallet (budget) too small to cover the full desired top-up but
// larger than the venue minimum notional, the first run places a partial order sized
// to the affordable share count, FLOORED to the venue grid. Feeding that floored order
// back, the second run must place ZERO new orders and cancel ZERO: the partial is
// maintained, never replaced or topped up. This is exactly the case the floor bug
// broke — the affordable share count (106.3829...) is non-round, so a round-half match
// would diverge from the venue's floored 106.38 and churn forever.
func TestReconcileIdempotencyPartialFill(t *testing.T) {
	const closeIn = 7 * 24 * time.Hour
	m := reconcileEligible("0xpart", "noPart", "yesPart", closeIn, 0.94)

	// Total 10_000 => full desired top-up = 2% / 0.94 = 212.766 shares (notional 200).
	// Wallet (budget) is only 100 USDC: it cannot cover the full 200 notional but far
	// exceeds the minimum order notional (5 * 0.94 = 4.7), so funding places a PARTIAL
	// order of affordable = 100/0.94 = 106.383 shares, floored to 106.38.
	snap := snapWith(100, 10_000)

	fake := &fakeTradingClient{
		books: map[string]*polymarket.OrderBookDetail{"noPart": reconcileBook("0.93", "0.95", 5)},
	}
	app := reconcileTestApp(fake, false)

	// First run: no open orders, so exactly one PARTIAL order is placed.
	var buf1 bytes.Buffer
	app.reconcilePass(context.Background(), NewLogger(&buf1, "run_part_1"), snap, []eligibleMarket{m})
	if len(fake.placedOrders) != 1 {
		t.Fatalf("first run placed %d orders, want 1 (one partial)", len(fake.placedOrders))
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("first run canceled %d orders, want 0", len(fake.canceledOrders))
	}

	// The placed (and now floored-on-record) size is the venue-stored partial.
	placed := fake.placedOrders[0]
	wantPartial := floorToSizePrecision(100.0 / 0.94)
	if absDiff(placed.size, wantPartial) > 1e-9 {
		t.Fatalf("partial placed size = %v, want floored %v", placed.size, wantPartial)
	}

	// Feed the floored partial order back as an open order, exactly as the venue would
	// report it. placed.size already carries no sub-cent precision (the fake floored
	// it on record), so format straight to 2 decimals.
	fake.openOrders = []polymarket.Order{{
		ID:           "ord-part",
		Market:       "0xpart",
		AssetID:      placed.tokenID,
		Side:         polymarket.Buy,
		Price:        strconv.FormatFloat(placed.price, 'f', -1, 64),
		OriginalSize: strconv.FormatFloat(placed.size, 'f', 2, 64),
		SizeMatched:  "0",
		Expiration:   strconv.FormatInt(placed.expiration, 10),
	}}
	placesAfterFirst := len(fake.placedOrders)
	cancelsAfterFirst := len(fake.canceledOrders)

	// Second run against the SAME frozen fixture: the partial is maintained, NOT
	// replaced or topped up.
	var buf2 bytes.Buffer
	app.reconcilePass(context.Background(), NewLogger(&buf2, "run_part_2"), snap, []eligibleMarket{m})

	if newPlaces := len(fake.placedOrders) - placesAfterFirst; newPlaces != 0 {
		t.Fatalf("second run placed %d NEW orders, want 0 (partial must be maintained)", newPlaces)
	}
	if newCancels := len(fake.canceledOrders) - cancelsAfterFirst; newCancels != 0 {
		t.Fatalf("second run canceled %d orders, want 0 (partial must be maintained)", newCancels)
	}
	if !hasReconcileStatus(t, buf2.String(), "0xpart", "maintained") {
		t.Errorf("second run did not maintain the existing partial order")
	}
}

// --- helpers ---

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

func hasReconcileStatus(t *testing.T, out, conditionID, status string) bool {
	t.Helper()
	for _, e := range eventsNamed(parseEvents(t, out), "reconcile_order") {
		if e["condition_id"] == conditionID && e["status"] == status {
			return true
		}
	}
	return false
}
