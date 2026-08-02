package main

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// discoverTestApp wires an App to the fake with the fixed run time used by the
// eligibility helpers, so close-time arithmetic lines up with eligMarket().
func discoverTestApp(fake *fakeTradingClient) *App {
	cfg := defaultConfig()
	return &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return runNow },
	}
}

// twoSidedBook returns a NO order book whose midpoint lands in the (0.89, 0.99]
// band: bid 0.93, ask 0.95 ⇒ midpoint 0.94.
func twoSidedBook() *polymarket.OrderBookDetail {
	return &polymarket.OrderBookDetail{
		Bids: entries("0.93"),
		Asks: entries("0.95"),
	}
}

// marketWith builds a candidate market closing `closeIn` from runNow with its own
// condition/token IDs so multiple candidates in one scan stay distinct.
func marketWith(condID, noID, yesID string, closeIn time.Duration, liquidity string) polymarket.Market {
	return polymarket.Market{
		ConditionID:     condID,
		Question:        "Q " + condID,
		Active:          true,
		AcceptingOrders: true,
		Closed:          false,
		EndDate:         runNow.Add(closeIn).Format(time.RFC3339),
		Liquidity:       liquidity,
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["` + yesID + `","` + noID + `"]`,
	}
}

func TestDiscover_ScanSummaryAndRejectReasons(t *testing.T) {
	// One eligible market plus two that each fail a distinct criterion.
	eligibleM := marketWith("0xok", "noOK", "yesOK", 7*24*time.Hour, "10000")
	lowLiquidM := marketWith("0xlowliq", "noLow", "yesLow", 7*24*time.Hour, "100")
	closedM := marketWith("0xclosed", "noClosed", "yesClosed", 7*24*time.Hour, "10000")
	closedM.Closed = true

	fake := &fakeTradingClient{
		markets: []polymarket.Market{eligibleM, lowLiquidM, closedM},
		books: map[string]*polymarket.OrderBookDetail{
			"noOK":  twoSidedBook(),
			"noLow": twoSidedBook(),
		},
		positions: nil,
	}
	app := discoverTestApp(fake)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_disc")
	eligible, err := app.discoverEligibleMarkets(context.Background(), logger)
	if err != nil {
		t.Fatalf("discoverEligibleMarkets: %v", err)
	}
	if len(eligible) != 1 || eligible[0].Market.ConditionID != "0xok" {
		t.Fatalf("eligible = %+v, want exactly [0xok]", eligible)
	}

	events := parseEvents(t, buf.String())

	// Summary carries scanned and eligible counts.
	summaries := eventsNamed(events, "discover_summary")
	if len(summaries) != 1 {
		t.Fatalf("discover_summary count = %d, want 1", len(summaries))
	}
	if summaries[0]["markets_scanned"] != float64(3) {
		t.Errorf("markets_scanned = %v, want 3", summaries[0]["markets_scanned"])
	}
	if summaries[0]["markets_eligible"] != float64(1) {
		t.Errorf("markets_eligible = %v, want 1", summaries[0]["markets_eligible"])
	}

	// One eligibility line per market: accepted for 0xok, rejected with the
	// deciding reason for the others.
	byCondition := map[string]map[string]any{}
	for _, e := range eventsNamed(events, "market_eligibility") {
		cond, _ := e["condition_id"].(string)
		byCondition[cond] = e
	}
	if len(byCondition) != 3 {
		t.Fatalf("expected 3 market_eligibility lines, got %d: %v", len(byCondition), byCondition)
	}
	if byCondition["0xok"]["status"] != "eligible" {
		t.Errorf("0xok status = %v, want eligible", byCondition["0xok"]["status"])
	}
	if byCondition["0xlowliq"]["status"] != "rejected" || byCondition["0xlowliq"]["reason"] != skipLiquidityTooLow.String() {
		t.Errorf("0xlowliq line = %v, want rejected/%q", byCondition["0xlowliq"], skipLiquidityTooLow)
	}
	if byCondition["0xclosed"]["status"] != "rejected" || byCondition["0xclosed"]["reason"] != skipMarketClosed.String() {
		t.Errorf("0xclosed line = %v, want rejected/%q", byCondition["0xclosed"], skipMarketClosed)
	}

	// No state-mutating calls were made in this rung.
	if len(fake.redeemCalls) != 0 {
		t.Errorf("discovery placed/redeemed orders: redeemCalls = %v", fake.redeemCalls)
	}
}

func TestDiscover_SortedByCloseAscendingStable(t *testing.T) {
	// Feed candidates out of close-time order, including a tie. All eligible
	// (close times fit inside the (48h, 336h) window).
	late := marketWith("0xlate", "noLate", "yesLate", 13*24*time.Hour, "10000")
	early := marketWith("0xearly", "noEarly", "yesEarly", 3*24*time.Hour, "10000")
	tieA := marketWith("0xtieA", "noTieA", "yesTieA", 10*24*time.Hour, "10000")
	tieB := marketWith("0xtieB", "noTieB", "yesTieB", 10*24*time.Hour, "10000")

	fake := &fakeTradingClient{
		// Scan order: late, tieA, early, tieB. tieA precedes tieB in input so a
		// stable sort must keep that relative order among the tie.
		markets: []polymarket.Market{late, tieA, early, tieB},
		books: map[string]*polymarket.OrderBookDetail{
			"noLate":  twoSidedBook(),
			"noEarly": twoSidedBook(),
			"noTieA":  twoSidedBook(),
			"noTieB":  twoSidedBook(),
		},
	}
	app := discoverTestApp(fake)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_sort")
	eligible, err := app.discoverEligibleMarkets(context.Background(), logger)
	if err != nil {
		t.Fatalf("discoverEligibleMarkets: %v", err)
	}

	gotOrder := make([]string, 0, len(eligible))
	for _, e := range eligible {
		gotOrder = append(gotOrder, e.Market.ConditionID)
	}
	want := []string{"0xearly", "0xtieA", "0xtieB", "0xlate"}
	if len(gotOrder) != len(want) {
		t.Fatalf("eligible order = %v, want %v", gotOrder, want)
	}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("eligible order = %v, want %v (stable ascending by close)", gotOrder, want)
		}
	}
}

func TestDiscover_OwnedNoSharesRejected(t *testing.T) {
	m := marketWith("0xowned", "noOwned", "yesOwned", 7*24*time.Hour, "10000")
	fake := &fakeTradingClient{
		markets: []polymarket.Market{m},
		books:   map[string]*polymarket.OrderBookDetail{"noOwned": twoSidedBook()},
		// Account already holds the opposing NO token with a positive size.
		positions: []polymarket.Position{{Asset: "noOwned", Size: 5}},
	}
	app := discoverTestApp(fake)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_owned")
	eligible, err := app.discoverEligibleMarkets(context.Background(), logger)
	if err != nil {
		t.Fatalf("discoverEligibleMarkets: %v", err)
	}
	if len(eligible) != 0 {
		t.Fatalf("eligible = %+v, want none (NO shares owned)", eligible)
	}
	rejected := false
	for _, e := range eventsNamed(parseEvents(t, buf.String()), "market_eligibility") {
		if e["condition_id"] == "0xowned" && e["reason"] == skipNoSharesOwned.String() {
			rejected = true
		}
	}
	if !rejected {
		t.Errorf("expected 0xowned rejected for %q", skipNoSharesOwned)
	}
}

func TestDiscover_ListMarketsErrorFatal(t *testing.T) {
	fake := &fakeTradingClient{listMarketsErr: errContext("upstream down")}
	app := discoverTestApp(fake)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_listerr")
	if _, err := app.discoverEligibleMarkets(context.Background(), logger); err == nil {
		t.Fatalf("expected error when ListMarkets fails")
	}
}

func TestListAllMarkets_UsesKeysetPagination(t *testing.T) {
	markets := make([]polymarket.Market, 205)
	for i := range markets {
		markets[i] = marketWith("0x"+strconv.Itoa(i), "no"+strconv.Itoa(i), "yes"+strconv.Itoa(i), 7*24*time.Hour, "10000")
	}

	fake := &fakeTradingClient{markets: markets}
	app := discoverTestApp(fake)

	got, err := app.listAllMarkets(context.Background())
	if err != nil {
		t.Fatalf("listAllMarkets: %v", err)
	}
	if len(got) != len(markets) {
		t.Fatalf("got %d markets, want %d", len(got), len(markets))
	}
	wantCursors := []string{"", "100", "200"}
	if len(fake.listMarketsKeysetCursors) != len(wantCursors) {
		t.Fatalf("keyset cursors = %v, want %v", fake.listMarketsKeysetCursors, wantCursors)
	}
	for i := range wantCursors {
		if fake.listMarketsKeysetCursors[i] != wantCursors[i] {
			t.Fatalf("keyset cursors = %v, want %v", fake.listMarketsKeysetCursors, wantCursors)
		}
	}
	if len(fake.listMarketsCalls) != 0 {
		t.Fatalf("offset pagination was used: %v", fake.listMarketsCalls)
	}
}

func TestListAllMarkets_AllowsFullFinalPageWithoutCursor(t *testing.T) {
	markets := make([]polymarket.Market, discoverPageLimit)
	for i := range markets {
		markets[i] = marketWith("0xfull"+strconv.Itoa(i), "noFull"+strconv.Itoa(i), "yesFull"+strconv.Itoa(i), 7*24*time.Hour, "10000")
	}

	fake := &fakeTradingClient{markets: markets}
	app := discoverTestApp(fake)

	got, err := app.listAllMarkets(context.Background())
	if err != nil {
		t.Fatalf("listAllMarkets: %v", err)
	}
	if len(got) != discoverPageLimit {
		t.Fatalf("got %d markets, want %d", len(got), discoverPageLimit)
	}
	if len(fake.listMarketsKeysetCursors) != 1 || fake.listMarketsKeysetCursors[0] != "" {
		t.Fatalf("keyset cursors = %v, want single empty cursor", fake.listMarketsKeysetCursors)
	}
}

// errContext is a tiny error helper so the test does not import errors twice.
type errContext string

func (e errContext) Error() string { return string(e) }
