package main

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// TestStaleCancel_PricesEqualIntegerTickRobust verifies the price comparison is
// robust to float rounding at exact half-ticks: every on-grid cent compares equal
// to itself and unequal to its neighbours on a 0.01 tick.
func TestStaleCancel_PricesEqualIntegerTickRobust(t *testing.T) {
	const tick = 0.01
	for cents := 1; cents < 100; cents++ {
		p := float64(cents) / 100
		if !pricesEqual(p, p, tick) {
			t.Errorf("price %.2f not equal to itself under integer-tick comparison", p)
		}
		if cents > 1 {
			lower := float64(cents-1) / 100
			if pricesEqual(p, lower, tick) {
				t.Errorf("distinct ticks %.2f and %.2f compared equal", p, lower)
			}
		}
	}
	// Undeterminable tick fails closed.
	if pricesEqual(0.91, 0.91, 0) {
		t.Errorf("pricesEqual must return false when tick is undeterminable")
	}
}

// TestStaleCancel_UnknownTickKeepsNotDedup verifies fail-closed behavior: when the
// venue tick is unknown, two same-market NO-buy orders at different prices (both
// with the correct expiration) are KEPT, not canceled as duplicates, because their
// price staleness cannot be proven.
func TestStaleCancel_UnknownTickKeepsNotDedup(t *testing.T) {
	const conditionID = "0xc"
	const noToken = "noTok"
	const yesToken = "yesTok"

	closeAt := time.Unix(0, 0).UTC().Add(72 * time.Hour)
	expiry := closeAt.Add(-24 * time.Hour).Unix()
	expStr := strconv.FormatInt(expiry, 10)

	// Two-sided book (so a midpoint exists / market is eligible) but NO tick size.
	book := &polymarket.OrderBookDetail{
		AssetID: noToken,
		Bids:    []polymarket.OrderBookEntry{{Price: "0.90", Size: "100"}},
		Asks:    []polymarket.OrderBookEntry{{Price: "0.92", Size: "100"}},
	}
	market := &polymarket.Market{
		ConditionID:     conditionID,
		Active:          true,
		AcceptingOrders: true,
		Closed:          false,
		EndDate:         closeAt.Format(time.RFC3339),
		Liquidity:       "10000",
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["` + yesToken + `","` + noToken + `"]`,
	}

	fake := &fakeTradingClient{
		openOrders: []polymarket.Order{
			{ID: "o1", Market: conditionID, AssetID: noToken, Side: "BUY", Price: "0.91", Expiration: expStr, OriginalSize: "10"},
			{ID: "o2", Market: conditionID, AssetID: noToken, Side: "BUY", Price: "0.88", Expiration: expStr, OriginalSize: "10"},
		},
		gammaMarkets: map[string]*polymarket.Market{conditionID: market},
		books:        map[string]*polymarket.OrderBookDetail{noToken: book},
	}

	cfg := defaultConfig()
	app := &App{cfg: &cfg, trading: fake, newRunID: seqRunID(), now: func() time.Time { return time.Unix(0, 0).UTC() }}
	var buf bytes.Buffer
	if err := app.stalePass(context.Background(), NewLogger(&buf, "run_x")); err != nil {
		t.Fatalf("stalePass: %v", err)
	}
	if len(fake.canceledOrders) != 0 {
		t.Fatalf("unknown tick must keep both orders (fail closed), but canceled: %v", fake.canceledOrders)
	}
}
