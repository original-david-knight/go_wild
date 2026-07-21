package main

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// This file drives the FULL runOnce pipeline (redeem -> stale-cancel -> account
// snapshot -> discover -> reconcile) through a fully-configured fakeTradingClient
// and fakeWallet. It is the pipeline-level analogue of the per-pass unit tests:
// the per-pass behavior is verified elsewhere; here we prove the WHOLE pipeline
// is resilient and idempotent end-to-end.

// pipelineApp wires an App with both a trading client and a wallet at the fixed
// eligibility run time (runNow) so close-time arithmetic lines up with markets
// built via marketWith(...). A configured wallet is required for runOnce to enter
// the snapshot/discover/reconcile block.
func pipelineApp(fake *fakeTradingClient, wallet walletClient, dryRun bool) *App {
	cfg := defaultConfig()
	cfg.DryRun = dryRun
	return &App{
		cfg:      &cfg,
		trading:  fake,
		wallet:   wallet,
		newRunID: seqRunID(),
		now:      func() time.Time { return runNow },
	}
}

// pipelineMarketIDs returns the deterministic condition/NO/YES token IDs for the
// nth synthetic eligible market.
func pipelineMarketIDs(n int) (condID, noID, yesID string) {
	s := strconv.Itoa(n)
	return "0xmkt" + s, "no" + s, "yes" + s
}

// pipelineCloseIn is the close offset for the nth eligible market, staggered so
// each closes at a distinct time strictly inside the (48h, 336h) window. All are
// well within the band so discovery accepts them on close time.
func pipelineCloseIn(n int) time.Duration {
	return time.Duration(72+n*12) * time.Hour
}

// buildEligibleMarkets constructs n eligible markets plus the matching two-sided
// NO books (midpoint 0.94, tick 0.01, min order size 5) keyed by NO token ID, so
// the same books map serves discovery's midpoint fetch and reconcile's fresh
// re-fetch. It returns the markets and the books map.
func buildEligibleMarkets(n int) ([]polymarket.Market, map[string]*polymarket.OrderBookDetail) {
	markets := make([]polymarket.Market, 0, n)
	books := map[string]*polymarket.OrderBookDetail{}
	for i := 0; i < n; i++ {
		condID, noID, yesID := pipelineMarketIDs(i)
		markets = append(markets, marketWith(condID, noID, yesID, pipelineCloseIn(i), "10000"))
		books[noID] = reconcileBook("0.93", "0.95", 5)
	}
	return markets, books
}

// placedByToken indexes the fake's recorded placements by NO token ID. Each
// eligible market is expected to produce at most one placement.
func placedByToken(fake *fakeTradingClient) map[string]placedOrder {
	out := map[string]placedOrder{}
	for _, p := range fake.placedOrders {
		out[p.tokenID] = p
	}
	return out
}

// reconcileStatusByCondition returns, for the given condition ID, the set of
// reconcile_order statuses logged against it.
func reconcileStatusByCondition(events []map[string]any, conditionID string) map[string]bool {
	out := map[string]bool{}
	for _, e := range eventsNamed(events, "reconcile_order") {
		if e["condition_id"] == conditionID {
			if s, ok := e["status"].(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// TestPipeline_CrossStepPerMarketResilience configures redeem, cancel, AND
// place-order to fail for ONE designated condition, with several healthy eligible
// markets alongside. A live (non-dry-run) one-shot run must log-and-skip the
// failing market at every step while EVERY healthy market still produces its
// redeem, stale-cancel, and place action, and the run completes without abort.
func TestPipeline_CrossStepPerMarketResilience(t *testing.T) {
	// Three healthy eligible markets (indices 0,1,2: condIDs 0xmkt0..2, NO tokens
	// no0..2) plus ONE designated failing market. The failing market is itself an
	// eligible market with a HEALTHY book so it reaches every step — and at every
	// step its operation fails: redeem (redeemErrByCondition), stale-cancel
	// (cancelErrByOrderID), and place-order (placeErrByToken). It must be
	// logged-and-skipped at each step while every healthy market still acts, and the
	// run must complete.
	markets, books := buildEligibleMarkets(3)
	failCond, failNo, failYes := "0xfail", "noFail", "yesFail"
	failMarket := marketWith(failCond, failNo, failYes, pipelineCloseIn(3), "10000")
	markets = append(markets, failMarket)
	books[failNo] = reconcileBook("0.93", "0.95", 5) // healthy book: reaches the place step

	// Redeemable positions: one for the failing condition (redeem fails) and one for
	// a healthy, non-eligible redeem-only condition (redeem succeeds). Neither owns a
	// YES token of any eligible market, so discovery is unaffected.
	positions := []polymarket.Position{
		{ConditionID: failCond, Asset: "redeemFailTok", Redeemable: true, Size: 10},
		{ConditionID: "0xredeemok", Asset: "redeemOkTok", Redeemable: true, Size: 10},
	}

	// Stale open orders: a clearly-stale (wrong-side SELL) order on a healthy eligible
	// market whose cancel succeeds, plus one on the failing market whose cancel fails.
	// Stale-cancel needs the Gamma market + NO book to evaluate, so seed gammaMarkets
	// for the markets carrying stale orders.
	healthyStale := stalePipelineOrder("stale-healthy", "0xmkt1", "no1", pipelineCloseIn(1))
	failStale := stalePipelineOrder("stale-fail", failCond, failNo, pipelineCloseIn(3))

	fake := &fakeTradingClient{
		markets:    markets,
		books:      books,
		positions:  positions,
		openOrders: []polymarket.Order{healthyStale, failStale},
		gammaMarkets: map[string]*polymarket.Market{
			"0xmkt1": ptrMarket(marketWith("0xmkt1", "no1", "yes1", pipelineCloseIn(1), "10000")),
			failCond: ptrMarket(failMarket),
		},
		redeemErrByCondition: map[string]error{failCond: errors.New("redeem boom")},
		cancelErrByOrderID:   map[string]error{"stale-fail": errors.New("cancel boom")},
		placeErrByToken:      map[string]error{failNo: errors.New("place boom")},
	}

	app := pipelineApp(fake, &fakeWallet{balance: "100000"}, false)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_resilience")
	app.runOnce(context.Background(), logger)

	events := parseEvents(t, buf.String())

	// The run completed: a run_done event is present (runOnce did not abort).
	if len(eventsNamed(events, "run_done")) != 1 {
		t.Fatalf("expected exactly one run_done; run aborted? events=%v", buf.String())
	}

	// Redeem: the healthy condition redeemed; the failing condition was attempted and
	// logged failed. Both were attempted (isolation: one failure did not skip others).
	redeemed := map[string]string{}
	for _, e := range eventsNamed(events, "redeem_attempt") {
		cond, _ := e["condition_id"].(string)
		status, _ := e["status"].(string)
		redeemed[cond] = status
	}
	if redeemed["0xredeemok"] != "succeeded" {
		t.Errorf("healthy redeem 0xredeemok status = %q, want succeeded", redeemed["0xredeemok"])
	}
	if redeemed[failCond] != "failed" {
		t.Errorf("failing redeem %s status = %q, want failed", failCond, redeemed[failCond])
	}
	// Live redemption is a single redeem-all call; both conditions are settled
	// within it (failure on one isolated as a per-transaction error).
	if len(fake.redeemCalls) != 1 || fake.redeemCalls[0] != "" {
		t.Errorf("redeem calls = %v, want exactly one redeem-all call (empty conditionID)", fake.redeemCalls)
	}

	// Stale-cancel: the healthy stale order was canceled; the failing one was
	// attempted and logged failed. The failure did not stop the healthy cancel.
	if !containsString(fake.canceledOrders, "stale-healthy") {
		t.Errorf("healthy stale order not canceled; canceled=%v", fake.canceledOrders)
	}
	if !containsString(fake.canceledOrders, "stale-fail") {
		t.Errorf("failing stale order cancel not attempted; canceled=%v", fake.canceledOrders)
	}
	if !staleOrderStatus(events, "stale-healthy", "canceled") {
		t.Errorf("expected stale-healthy canceled log")
	}
	if !staleOrderStatus(events, "stale-fail", "failed") {
		t.Errorf("expected stale-fail failed log")
	}

	// Place: EVERY healthy eligible market produced exactly one placement; the failing
	// market reached the place step (healthy book) and was logged-and-skipped when its
	// placement errored — it recorded a place call on the fake but produced a
	// place_failed reconcile_order log, and the run continued.
	placedSet := placedByToken(fake)
	for i := 0; i < 3; i++ {
		_, noID, _ := pipelineMarketIDs(i)
		if _, ok := placedSet[noID]; !ok {
			t.Errorf("healthy market %d (token %s) produced no placement; placed=%v", i, noID, placedSet)
		}
	}
	// The failing market's placement was attempted (recorded) but errored.
	if _, ok := placedSet[failNo]; !ok {
		t.Errorf("failing market placement was not attempted on %s", failNo)
	}
	if !reconcileStatusByCondition(events, failCond)["place_failed"] {
		t.Errorf("failing market %s missing a place_failed reconcile_order", failCond)
	}
	// Three healthy markets succeeded; one failing market errored => 4 place calls, 3
	// of which produced a placed reconcile_order.
	if len(fake.placedOrders) != 4 {
		t.Errorf("total place attempts = %d, want 4 (3 healthy + 1 failing)", len(fake.placedOrders))
	}
	placedOK := 0
	for _, e := range eventsNamed(events, "reconcile_order") {
		if e["status"] == "placed" {
			placedOK++
		}
	}
	if placedOK != 3 {
		t.Errorf("placed (succeeded) reconcile_order count = %d, want 3", placedOK)
	}
}

// TestPipeline_BookFetchErrorIsolatesOneMarket makes GetOrderBookDetailed error
// for ONE eligible market's NO token. That market must be skipped for ordering
// while every sibling market still gets its order. The per-market reconcile logs
// are diffed against the expected set.
func TestPipeline_BookFetchErrorIsolatesOneMarket(t *testing.T) {
	markets, books := buildEligibleMarkets(4)
	// Designate market index 2 as the one whose FRESH reconcile book re-fetch errors.
	// Its first fetch (discovery) succeeds so it passes discovery; its second fetch
	// (reconcile re-fetch) errors, so reconcile skips it at the get_book stage. This
	// exercises the reconcile pass's per-market book isolation directly.
	badCond, badNo, _ := pipelineMarketIDs(2)
	fake := &fakeTradingClient{
		markets: markets,
		books:   books,
		bookErrAfterByToken: map[string]error{
			badNo: errors.New("book unavailable"),
		},
	}
	app := pipelineApp(fake, &fakeWallet{balance: "100000"}, false)

	var buf bytes.Buffer
	app.runOnce(context.Background(), NewLogger(&buf, "run_bookerr"))
	events := parseEvents(t, buf.String())

	// Exactly the three siblings placed; the bad market placed nothing.
	placed := placedByToken(fake)
	for i := 0; i < 4; i++ {
		condID, noID, _ := pipelineMarketIDs(i)
		_, ok := placed[noID]
		if i == 2 {
			if ok {
				t.Errorf("bad market %s placed an order, want none", condID)
			}
		} else if !ok {
			t.Errorf("sibling market %s (token %s) produced no placement", condID, noID)
		}
	}
	if len(fake.placedOrders) != 3 {
		t.Fatalf("placements = %d, want 3 (one per healthy sibling)", len(fake.placedOrders))
	}

	// The bad market logged a reconcile_skip at the get_book stage; siblings logged a
	// placed reconcile_order, not a skip.
	badStatuses := reconcileStatusByCondition(events, badCond)
	if !hasReconcileSkipStage(events, badCond, "get_book") {
		t.Errorf("bad market %s missing reconcile_skip at get_book; statuses=%v", badCond, badStatuses)
	}
	for i := 0; i < 4; i++ {
		if i == 2 {
			continue
		}
		condID, _, _ := pipelineMarketIDs(i)
		if !reconcileStatusByCondition(events, condID)["placed"] {
			t.Errorf("sibling %s missing a placed reconcile_order", condID)
		}
	}
}

// TestPipeline_AbortOnUncomputableAccountValue covers BOTH abort paths: a wallet
// balance fetch error and a positions fetch error during the snapshot. In each
// case the buy pass must abort — ZERO place and ZERO cancel recorded on the fake
// from the reconcile pass — and an account_value_abort log is emitted. (Redeem and
// stale run before the snapshot and may have acted; the assertion is specifically
// that NO reconcile place/cancel happens.)
func TestPipeline_AbortOnUncomputableAccountValue(t *testing.T) {
	markets, books := buildEligibleMarkets(3)

	t.Run("wallet balance error aborts", func(t *testing.T) {
		fake := &fakeTradingClient{markets: markets, books: books}
		// Wallet errors -> snapshot aborts at the wallet_usdc stage, before positions.
		app := pipelineApp(fake, &fakeWallet{err: errors.New("rpc down")}, false)

		var buf bytes.Buffer
		app.runOnce(context.Background(), NewLogger(&buf, "run_abort_wallet"))
		events := parseEvents(t, buf.String())

		if len(eventsNamed(events, "account_value_abort")) != 1 {
			t.Fatalf("expected an account_value_abort log; got %v", buf.String())
		}
		if len(fake.placedOrders) != 0 {
			t.Errorf("placed %d orders after wallet abort, want 0", len(fake.placedOrders))
		}
		if len(fake.canceledOrders) != 0 {
			t.Errorf("canceled %d orders after wallet abort, want 0", len(fake.canceledOrders))
		}
		// Discovery/reconcile never ran: no discover_summary, no reconcile_start.
		if len(eventsNamed(events, "reconcile_start")) != 0 {
			t.Errorf("reconcile ran despite wallet abort")
		}
	})

	t.Run("positions error aborts", func(t *testing.T) {
		// Positions error makes the snapshot abort at the get_positions stage (after the
		// wallet balance succeeds). Redeem also fails to read positions (logged, no
		// redeem submitted); stale cannot evaluate any market (fail-closed). The key
		// assertion: NO reconcile place/cancel.
		fake := &fakeTradingClient{
			markets:      markets,
			books:        books,
			positionsErr: errors.New("positions down"),
		}
		app := pipelineApp(fake, &fakeWallet{balance: "100000"}, false)

		var buf bytes.Buffer
		app.runOnce(context.Background(), NewLogger(&buf, "run_abort_pos"))
		events := parseEvents(t, buf.String())

		if len(eventsNamed(events, "account_value_abort")) != 1 {
			t.Fatalf("expected an account_value_abort log; got %v", buf.String())
		}
		if len(fake.placedOrders) != 0 {
			t.Errorf("placed %d orders after positions abort, want 0", len(fake.placedOrders))
		}
		if len(fake.canceledOrders) != 0 {
			t.Errorf("canceled %d orders after positions abort, want 0", len(fake.canceledOrders))
		}
		if len(eventsNamed(events, "reconcile_start")) != 0 {
			t.Errorf("reconcile ran despite positions abort")
		}
	})
}

// TestPipeline_IdempotentConvergence freezes a fixture (markets, books, positions,
// wallet), runs runOnce once and captures the placed orders, feeds those placed
// orders back into the fake's openOrders (using the venue-floored size the fake
// records), then runs runOnce a SECOND time against the otherwise-identical state.
// The second run must record ZERO new placements and ZERO cancellations: every
// existing order is maintained, proving convergence with no compounding exposure.
func TestPipeline_IdempotentConvergence(t *testing.T) {
	markets, books := buildEligibleMarkets(3)
	fake := &fakeTradingClient{markets: markets, books: books}
	app := pipelineApp(fake, &fakeWallet{balance: "100000"}, false)

	// First run: no open orders, so one order is placed per eligible market.
	var buf1 bytes.Buffer
	app.runOnce(context.Background(), NewLogger(&buf1, "run_idem_1"))
	if len(fake.placedOrders) != 3 {
		t.Fatalf("first run placed %d orders, want 3 (one per market)", len(fake.placedOrders))
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("first run canceled %d orders, want 0", len(fake.canceledOrders))
	}

	// Feed every placed order back as an open order exactly as the venue would report
	// it: BUY on the NO token, at the placed price, full (already venue-floored) size,
	// the placed expiration. Map each placed token to its source market's condition ID.
	tokenToCond := map[string]string{}
	for i := 0; i < 3; i++ {
		condID, noID, _ := pipelineMarketIDs(i)
		tokenToCond[noID] = condID
	}
	var open []polymarket.Order
	for i, p := range fake.placedOrders {
		open = append(open, polymarket.Order{
			ID:           "open-" + strconv.Itoa(i),
			Market:       tokenToCond[p.tokenID],
			AssetID:      p.tokenID,
			Side:         polymarket.Buy,
			Price:        strconv.FormatFloat(p.price, 'f', -1, 64),
			OriginalSize: strconv.FormatFloat(p.size, 'f', 2, 64),
			SizeMatched:  "0",
			Expiration:   strconv.FormatInt(p.expiration, 10),
		})
	}
	fake.openOrders = open
	placesAfterFirst := len(fake.placedOrders)
	cancelsAfterFirst := len(fake.canceledOrders)

	// Second run against the same frozen fixture: every order is maintained.
	var buf2 bytes.Buffer
	app.runOnce(context.Background(), NewLogger(&buf2, "run_idem_2"))

	if newPlaces := len(fake.placedOrders) - placesAfterFirst; newPlaces != 0 {
		t.Fatalf("second run placed %d NEW orders, want 0 (pipeline must converge)", newPlaces)
	}
	if newCancels := len(fake.canceledOrders) - cancelsAfterFirst; newCancels != 0 {
		t.Fatalf("second run canceled %d orders, want 0 (pipeline must converge)", newCancels)
	}

	// Every market logged a maintained reconcile_order on the second run.
	events2 := parseEvents(t, buf2.String())
	for i := 0; i < 3; i++ {
		condID, _, _ := pipelineMarketIDs(i)
		if !reconcileStatusByCondition(events2, condID)["maintained"] {
			t.Errorf("second run did not maintain market %s", condID)
		}
	}
}

// TestPipeline_DryRunMutatesNothing runs the full pipeline in dry-run with
// redeemable positions, a stale order, and eligible markets all present. The fake
// must record ZERO redeems, ZERO cancels, and ZERO places, while the would-* intents
// are logged at every step.
func TestPipeline_DryRunMutatesNothing(t *testing.T) {
	markets, books := buildEligibleMarkets(2)

	// A redeemable position on a distinct condition (redeem intent), plus a stale
	// order on an eligible market (cancel intent). The stale order is a wrong-side
	// SELL so it is unambiguously stale; its market needs a Gamma entry to evaluate.
	positions := []polymarket.Position{
		{ConditionID: "0xredeemdry", Asset: "redeemDryTok", Redeemable: true, Size: 7},
	}
	stale := stalePipelineOrder("stale-dry", "0xmkt0", "no0", pipelineCloseIn(0))
	stale.Price = "0.10" // diverging price => provably stale => would_cancel

	fake := &fakeTradingClient{
		markets:    markets,
		books:      books,
		positions:  positions,
		openOrders: []polymarket.Order{stale},
		gammaMarkets: map[string]*polymarket.Market{
			"0xmkt0": ptrMarket(marketWith("0xmkt0", "no0", "yes0", pipelineCloseIn(0), "10000")),
		},
	}
	app := pipelineApp(fake, &fakeWallet{balance: "100000"}, true) // dry-run

	var buf bytes.Buffer
	app.runOnce(context.Background(), NewLogger(&buf, "run_dry_pipeline"))
	events := parseEvents(t, buf.String())

	// Nothing mutated: zero redeems, zero cancels, zero places recorded on the fake.
	if len(fake.redeemCalls) != 0 {
		t.Errorf("dry-run recorded %d redeems, want 0", len(fake.redeemCalls))
	}
	if len(fake.canceledOrders) != 0 {
		t.Errorf("dry-run recorded %d cancels, want 0", len(fake.canceledOrders))
	}
	if len(fake.placedOrders) != 0 {
		t.Errorf("dry-run recorded %d places, want 0", len(fake.placedOrders))
	}

	// The would-* intents are logged at every step.
	if !redeemStatus(events, "0xredeemdry", "would_redeem") {
		t.Errorf("expected a would_redeem redeem_attempt for 0xredeemdry")
	}
	if !staleOrderStatus(events, "stale-dry", "would_cancel") {
		t.Errorf("expected a would_cancel stale_order for stale-dry")
	}
	// At least one would_place reconcile_order for an eligible market.
	wouldPlace := false
	for _, e := range eventsNamed(events, "reconcile_order") {
		if e["status"] == "would_place" {
			wouldPlace = true
		}
	}
	if !wouldPlace {
		t.Errorf("expected at least one would_place reconcile_order in dry-run")
	}
}

// --- helpers ---

// stalePipelineOrder builds a clearly-stale wrong-side (SELL) NO order for the
// given eligible market. A SELL on the NO token is canceled by the stale pass as a
// wrong-side order (it is not a NO-buy candidate). Tests may override fields to make
// the order stale in a different, provable way.
func stalePipelineOrder(id, condID, noID string, closeIn time.Duration) polymarket.Order {
	return polymarket.Order{
		ID:           id,
		Market:       condID,
		AssetID:      noID,
		Side:         "SELL",
		Price:        "0.94",
		OriginalSize: "1.00",
		SizeMatched:  "0",
		Expiration:   staleExpiry(closeIn),
	}
}

func ptrMarket(m polymarket.Market) *polymarket.Market { return &m }

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func redeemStatus(events []map[string]any, conditionID, status string) bool {
	for _, e := range eventsNamed(events, "redeem_attempt") {
		if e["condition_id"] == conditionID && e["status"] == status {
			return true
		}
	}
	return false
}

func staleOrderStatus(events []map[string]any, orderID, status string) bool {
	for _, e := range eventsNamed(events, "stale_order") {
		if e["order_id"] == orderID && e["status"] == status {
			return true
		}
	}
	return false
}

func hasReconcileSkipStage(events []map[string]any, conditionID, stage string) bool {
	for _, e := range eventsNamed(events, "reconcile_skip") {
		if e["condition_id"] == conditionID && e["stage"] == stage {
			return true
		}
	}
	return false
}
