package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// snapshotTestApp wires an App to a fake trading client and a fake wallet. The
// USDC token address comes from the resolved default config so tests can assert
// the wallet helper was queried against exactly that token.
func snapshotTestApp(fake *fakeTradingClient, wallet walletClient) *App {
	cfg := defaultConfig()
	return &App{
		cfg:      &cfg,
		trading:  fake,
		wallet:   wallet,
		newRunID: seqRunID(),
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

// floatField extracts a float64 from a decoded JSON log field (JSON numbers
// decode to float64). It fails the test if the field is missing or not numeric.
func floatField(t *testing.T, e map[string]any, key string) float64 {
	t.Helper()
	v, ok := e[key].(float64)
	if !ok {
		t.Fatalf("event %v missing numeric field %q", e, key)
	}
	return v
}

// TestAccountValue_PerShareValuation exercises the strict fallback chain for a
// single position: usable two-sided book -> midpoint; bids-only -> best bid;
// neither -> zero; book-fetch error -> zero. It asserts the chosen per-share
// price (via total = wallet + size*price) and that the best-bid and zero cases
// emit a fallback log while the midpoint case does not.
func TestAccountValue_PerShareValuation(t *testing.T) {
	cases := []struct {
		name         string
		book         *polymarket.OrderBookDetail
		bookErr      error
		wantPrice    float64
		wantFallback bool
		wantSource   string
	}{
		{
			name:         "two-sided book uses midpoint",
			book:         &polymarket.OrderBookDetail{Bids: entries("0.90"), Asks: entries("0.92")},
			wantPrice:    0.91,
			wantFallback: false,
		},
		{
			name:         "bids-only falls back to best bid",
			book:         &polymarket.OrderBookDetail{Bids: entries("0.80", "0.85", "0.70")},
			wantPrice:    0.85,
			wantFallback: true,
			wantSource:   "best_bid",
		},
		{
			name:         "no usable price falls back to zero",
			book:         &polymarket.OrderBookDetail{},
			wantPrice:    0,
			wantFallback: true,
			wantSource:   "zero",
		},
		{
			name:         "book fetch error values at zero",
			bookErr:      errors.New("boom"),
			wantPrice:    0,
			wantFallback: true,
			wantSource:   "zero",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const size = 10.0
			fake := &fakeTradingClient{
				positions: []polymarket.Position{
					{ConditionID: "0xc", Asset: "tok", Size: size},
				},
			}
			if tc.book != nil {
				fake.books = map[string]*polymarket.OrderBookDetail{"tok": tc.book}
			}
			if tc.bookErr != nil {
				fake.bookErrByToken = map[string]error{"tok": tc.bookErr}
			}
			wallet := &fakeWallet{balance: "100.0"}
			app := snapshotTestApp(fake, wallet)

			var buf bytes.Buffer
			logger := NewLogger(&buf, "run_share")
			snap, err := app.snapshotAccountValue(context.Background(), logger)
			if err != nil {
				t.Fatalf("snapshotAccountValue: %v", err)
			}

			wantPositions := size * tc.wantPrice
			if snap.PositionsValue != wantPositions {
				t.Errorf("positions_value = %v, want %v (size*price)", snap.PositionsValue, wantPositions)
			}
			if snap.Total != 100.0+wantPositions {
				t.Errorf("total = %v, want %v", snap.Total, 100.0+wantPositions)
			}

			fallbacks := eventsNamed(parseEvents(t, buf.String()), "position_value")
			if tc.wantFallback {
				if len(fallbacks) != 1 {
					t.Fatalf("position_value fallback logs = %d, want 1", len(fallbacks))
				}
				e := fallbacks[0]
				if e["price_source"] != tc.wantSource {
					t.Errorf("price_source = %v, want %q", e["price_source"], tc.wantSource)
				}
				if e["condition_id"] != "0xc" || e["token"] != "tok" {
					t.Errorf("fallback log missing condition/token: %v", e)
				}
			} else if len(fallbacks) != 0 {
				t.Errorf("midpoint path must not emit a fallback log, got %v", fallbacks)
			}
		})
	}
}

// TestAccountValue_TotalIsWalletPlusPositions checks the aggregate on a fixture
// of mixed positions: midpoint-valued, best-bid-valued, zero-valued, and a
// zero-size position that is skipped entirely. Total must equal exactly
// wallet_usdc + sum(size*price) with no rounding slack.
func TestAccountValue_TotalIsWalletPlusPositions(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{
			{ConditionID: "0xmid", Asset: "midtok", Size: 4},   // 4 * 0.91 = 3.64
			{ConditionID: "0xbid", Asset: "bidtok", Size: 5},   // 5 * 0.40 = 2.00
			{ConditionID: "0xzero", Asset: "zerotok", Size: 2}, // 2 * 0    = 0.00
			{ConditionID: "0xskip", Asset: "skiptok", Size: 0}, // skipped (Size<=0)
		},
		books: map[string]*polymarket.OrderBookDetail{
			"midtok":  {Bids: entries("0.90"), Asks: entries("0.92")},
			"bidtok":  {Bids: entries("0.40")},
			"zerotok": {},
		},
	}
	wallet := &fakeWallet{balance: "12.5"}
	app := snapshotTestApp(fake, wallet)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_total")
	snap, err := app.snapshotAccountValue(context.Background(), logger)
	if err != nil {
		t.Fatalf("snapshotAccountValue: %v", err)
	}

	// Accumulate in the exact same order/shape as the production loop so the
	// float result is byte-identical (no reassociated summation).
	wantPositions := 0.0
	wantPositions += 4 * 0.91
	wantPositions += 5 * 0.40
	wantPositions += 2 * 0.0
	if snap.WalletUSDC != 12.5 {
		t.Errorf("wallet_usdc = %v, want 12.5", snap.WalletUSDC)
	}
	if snap.PositionsValue != wantPositions {
		t.Errorf("positions_value = %v, want %v", snap.PositionsValue, wantPositions)
	}
	if snap.Total != 12.5+wantPositions {
		t.Errorf("total = %v, want %v", snap.Total, 12.5+wantPositions)
	}

	// The skipped zero-size position must not trigger a book fetch.
	for _, c := range fake.bookCalls {
		if c == "skiptok" {
			t.Errorf("zero-size position should not be valued/fetched, got book call for %q", c)
		}
	}

	// The structured account_value log line mirrors the snapshot exactly.
	avs := eventsNamed(parseEvents(t, buf.String()), "account_value")
	if len(avs) != 1 {
		t.Fatalf("account_value events = %d, want 1", len(avs))
	}
	av := avs[0]
	if av["status"] != "ok" {
		t.Fatalf("account_value status = %v, want ok", av["status"])
	}
	if floatField(t, av, "wallet_usdc") != snap.WalletUSDC ||
		floatField(t, av, "positions_value") != snap.PositionsValue ||
		floatField(t, av, "total") != snap.Total {
		t.Errorf("account_value log %v does not match snapshot %+v", av, snap)
	}
}

type positionValueTradingClient struct {
	*fakeTradingClient
	positionsValue    float64
	positionsValueErr error
	valueCalls        int
}

func (f *positionValueTradingClient) GetPositionsValue(ctx context.Context) (float64, error) {
	f.record("GetPositionsValue")
	f.valueCalls++
	return f.positionsValue, f.positionsValueErr
}

// TestAccountValue_PrefersPolymarketAggregatePositionValue pins the production
// valuation path closest to the Polymarket UI: when the client exposes the Data
// API /value endpoint, the snapshot uses that aggregate positions value instead
// of reconstructing marks from local bid/ask books.
func TestAccountValue_PrefersPolymarketAggregatePositionValue(t *testing.T) {
	base := &fakeTradingClient{
		positions: []polymarket.Position{
			{ConditionID: "0xwide", Asset: "wideTok", Size: 10},
		},
		books: map[string]*polymarket.OrderBookDetail{
			// Local midpoint would be 0.50, so book valuation would be 5.00.
			"wideTok": {Bids: entries("0.20"), Asks: entries("0.80")},
		},
	}
	trading := &positionValueTradingClient{fakeTradingClient: base, positionsValue: 6.25}
	wallet := &fakeWallet{balance: "100.0"}
	app := snapshotTestApp(base, wallet)
	app.trading = trading

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_value_api")
	snap, err := app.snapshotAccountValue(context.Background(), logger)
	if err != nil {
		t.Fatalf("snapshotAccountValue: %v", err)
	}

	if snap.PositionsValue != 6.25 {
		t.Errorf("positions_value = %v, want Polymarket aggregate 6.25", snap.PositionsValue)
	}
	if snap.PositionsValueSource != "value_api" {
		t.Errorf("positions value source = %q, want value_api", snap.PositionsValueSource)
	}
	if trading.valueCalls != 1 {
		t.Errorf("GetPositionsValue calls = %d, want 1", trading.valueCalls)
	}
	if len(base.bookCalls) != 0 {
		t.Errorf("aggregate value path should not fetch books, got %v", base.bookCalls)
	}
}

// TestAccountValue_UsesPositionCurrentValueBeforeBook covers the non-production
// fallback path used when the aggregate Data API value is unavailable: trust the
// per-position Data API currentValue/curPrice mark before falling back to a
// locally reconstructed book midpoint.
func TestAccountValue_UsesPositionCurrentValueBeforeBook(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{
			{ConditionID: "0xcur", Asset: "curTok", Size: 10, CurrentValue: 7.75},
			{ConditionID: "0xprice", Asset: "priceTok", Size: 4, CurPrice: 0.60},
		},
	}
	wallet := &fakeWallet{balance: "50.0"}
	app := snapshotTestApp(fake, wallet)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_current_value")
	snap, err := app.snapshotAccountValue(context.Background(), logger)
	if err != nil {
		t.Fatalf("snapshotAccountValue: %v", err)
	}

	wantPositions := 7.75 + 4*0.60
	if snap.PositionsValue != wantPositions {
		t.Errorf("positions_value = %v, want %v", snap.PositionsValue, wantPositions)
	}
	if len(fake.bookCalls) != 0 {
		t.Errorf("currentValue/curPrice path should not fetch books, got %v", fake.bookCalls)
	}
}

// TestAccountValue_WalletSourcedThroughHelper asserts the USDC cash balance is
// sourced through the wallet helper against the configured token address on the
// Ethereum chain — never from a Polymarket/CLOB exchange balance accessor. The
// trading client has no balance method; this test pins that contract by routing
// every cash read through the wallet fake.
func TestAccountValue_WalletSourcedThroughHelper(t *testing.T) {
	fake := &fakeTradingClient{positions: nil}
	wallet := &fakeWallet{balance: "777.25"}
	app := snapshotTestApp(fake, wallet)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_wallet")
	snap, err := app.snapshotAccountValue(context.Background(), logger)
	if err != nil {
		t.Fatalf("snapshotAccountValue: %v", err)
	}
	if snap.WalletUSDC != 777.25 || snap.Total != 777.25 {
		t.Errorf("wallet_usdc/total = %v/%v, want 777.25/777.25", snap.WalletUSDC, snap.Total)
	}

	calls := wallet.balanceCalls()
	if len(calls) != 1 {
		t.Fatalf("GetTokenBalance calls = %d, want 1", len(calls))
	}
	if calls[0].tokenAddress != app.cfg.USDCTokenAddress {
		t.Errorf("GetTokenBalance token = %q, want configured %q", calls[0].tokenAddress, app.cfg.USDCTokenAddress)
	}
	if calls[0].chain != gowild_crypto.ChainEthereum {
		t.Errorf("GetTokenBalance chain = %q, want %q", calls[0].chain, gowild_crypto.ChainEthereum)
	}
}

// TestAccountValue_AbortPaths covers the two fatal conditions: the wallet helper
// erroring, and GetPositions erroring. Both must abort (return an error) and log
// an abort reason. The wallet-error path must not even reach positions valuation.
func TestAccountValue_AbortPaths(t *testing.T) {
	t.Run("wallet helper error aborts before positions", func(t *testing.T) {
		fake := &fakeTradingClient{
			positions: []polymarket.Position{{ConditionID: "0xc", Asset: "tok", Size: 1}},
		}
		wallet := &fakeWallet{err: errors.New("rpc down")}
		app := snapshotTestApp(fake, wallet)

		var buf bytes.Buffer
		logger := NewLogger(&buf, "run_wallet_err")
		if _, err := app.snapshotAccountValue(context.Background(), logger); err == nil {
			t.Fatal("expected error when wallet helper fails")
		}
		// No positions valuation should have happened.
		if len(fake.callOrder()) != 0 {
			t.Errorf("trading client touched on wallet-error abort: %v", fake.callOrder())
		}
		assertAborted(t, &buf, "wallet_usdc")
	})

	t.Run("unparseable wallet balance aborts", func(t *testing.T) {
		fake := &fakeTradingClient{}
		wallet := &fakeWallet{balance: "not-a-number"}
		app := snapshotTestApp(fake, wallet)

		var buf bytes.Buffer
		logger := NewLogger(&buf, "run_bad_bal")
		if _, err := app.snapshotAccountValue(context.Background(), logger); err == nil {
			t.Fatal("expected error for unparseable wallet balance")
		}
		assertAborted(t, &buf, "wallet_usdc")
	})

	t.Run("positions error aborts", func(t *testing.T) {
		fake := &fakeTradingClient{positionsErr: errors.New("positions down")}
		wallet := &fakeWallet{balance: "50.0"}
		app := snapshotTestApp(fake, wallet)

		var buf bytes.Buffer
		logger := NewLogger(&buf, "run_pos_err")
		if _, err := app.snapshotAccountValue(context.Background(), logger); err == nil {
			t.Fatal("expected error when GetPositions fails")
		}
		assertAborted(t, &buf, "get_positions")
	})
}

func assertAborted(t *testing.T, buf *bytes.Buffer, wantStage string) {
	t.Helper()
	for _, e := range eventsNamed(parseEvents(t, buf.String()), "account_value") {
		if e["status"] == "aborted" {
			if e["stage"] != wantStage {
				t.Errorf("abort stage = %v, want %q", e["stage"], wantStage)
			}
			if e["reason"] == nil || e["reason"] == "" {
				t.Errorf("abort log missing reason: %v", e)
			}
			return
		}
	}
	t.Errorf("expected an aborted account_value log line with stage %q", wantStage)
}

// TestAccountValue_OrderingAfterRedeemAndCancel asserts the snapshot's
// GetPositions read occurs AFTER the redeem (M3) and stale-cancel (M7) passes in
// runOnce, and that cancellation does not mutate the wallet USDC balance.
func TestAccountValue_OrderingAfterRedeemAndCancel(t *testing.T) {
	// One redeemable position drives a redeem call; one stale open order drives a
	// cancel call. The order is on a non-binary market (no clob tokens) so the
	// cancel pass fails closed and skips it — but the GetOrders call still records
	// the cancel pass running before the snapshot.
	fake := &fakeTradingClient{
		positions: []polymarket.Position{
			{ConditionID: "0xredeem", Asset: "rtok", Size: 3, Redeemable: true, Outcome: "No"},
		},
		openOrders: []polymarket.Order{
			{ID: "ord1", Market: "0xmkt", AssetID: "atok", Side: "BUY", Price: "0.50", Expiration: "0"},
		},
		gammaMarkets: map[string]*polymarket.Market{
			// Empty market -> non-binary -> cancel pass fails closed for this market.
			"0xmkt": {ConditionID: "0xmkt"},
		},
		books: map[string]*polymarket.OrderBookDetail{
			"rtok": {Bids: entries("0.95"), Asks: entries("0.97")},
		},
	}
	wallet := &fakeWallet{balance: "200.0"}
	app := snapshotTestApp(fake, wallet)

	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_order")
	app.runOnce(context.Background(), logger)

	order := fake.callOrder()
	redeemIdx := indexOf(order, "RedeemWinnings:")
	ordersIdx := indexOf(order, "GetOrders:")
	if redeemIdx < 0 {
		t.Fatalf("redeem pass did not run: %v", order)
	}
	if ordersIdx < 0 {
		t.Fatalf("stale-cancel pass did not run: %v", order)
	}

	// The snapshot's GetPositions is the LAST GetPositions in the sequence: redeem
	// reads positions first, the snapshot reads them last. It must come after both
	// the redeem call and the stale-cancel GetOrders call.
	snapPosIdx := lastIndexOf(order, "GetPositions")
	if snapPosIdx < redeemIdx {
		t.Errorf("snapshot GetPositions (idx %d) ran before redeem (idx %d): %v", snapPosIdx, redeemIdx, order)
	}
	if snapPosIdx < ordersIdx {
		t.Errorf("snapshot GetPositions (idx %d) ran before stale-cancel GetOrders (idx %d): %v", snapPosIdx, ordersIdx, order)
	}

	// Cancellation does not mutate wallet_usdc: it is read solely from the wallet
	// helper and equals the configured balance regardless of any cancel activity.
	avs := eventsNamed(parseEvents(t, buf.String()), "account_value")
	if len(avs) != 1 || avs[0]["status"] != "ok" {
		t.Fatalf("expected one ok account_value event, got %v", avs)
	}
	if floatField(t, avs[0], "wallet_usdc") != 200.0 {
		t.Errorf("wallet_usdc = %v, want 200.0 (cancellation must not change cash)", avs[0]["wallet_usdc"])
	}
}

func indexOf(s []string, prefix string) int {
	for i, v := range s {
		if v == prefix || hasPrefix(v, prefix) {
			return i
		}
	}
	return -1
}

func lastIndexOf(s []string, want string) int {
	idx := -1
	for i, v := range s {
		if v == want {
			idx = i
		}
	}
	return idx
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
