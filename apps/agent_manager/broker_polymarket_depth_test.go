package main

import (
	"math"
	"strings"
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestNormalizeOrderBookDepthLevels(t *testing.T) {
	if got := normalizeOrderBookDepthLevels(0); got != defaultOrderBookDepthLevels {
		t.Fatalf("expected default levels %d, got %d", defaultOrderBookDepthLevels, got)
	}
	if got := normalizeOrderBookDepthLevels(-5); got != defaultOrderBookDepthLevels {
		t.Fatalf("expected default levels %d, got %d", defaultOrderBookDepthLevels, got)
	}
	if got := normalizeOrderBookDepthLevels(7); got != 7 {
		t.Fatalf("expected levels 7, got %d", got)
	}
	if got := normalizeOrderBookDepthLevels(999); got != maxOrderBookDepthLevels {
		t.Fatalf("expected capped levels %d, got %d", maxOrderBookDepthLevels, got)
	}
}

func TestBuildOrderBookDepth_SortsAndComputesTotals(t *testing.T) {
	book := &polymarket.OrderBook{
		Market:  "market-1",
		AssetID: "token-1",
		Hash:    "0xabc",
		Bids: []polymarket.OrderBookEntry{
			{Price: "0.71", Size: "5000"},
			{Price: "0.72", Size: "1000"},
			{Price: "0.70", Size: "200"},
		},
		Asks: []polymarket.OrderBookEntry{
			{Price: "0.75", Size: "1200"},
			{Price: "0.73", Size: "1500"},
			{Price: "0.74", Size: "800"},
		},
	}

	depth, err := buildOrderBookDepth(book, "token-1", 2)
	if err != nil {
		t.Fatalf("buildOrderBookDepth failed: %v", err)
	}

	if depth.LevelsRequested != 2 {
		t.Fatalf("expected levels_requested=2, got %d", depth.LevelsRequested)
	}
	if depth.LevelsUsed != 2 {
		t.Fatalf("expected levels_used=2, got %d", depth.LevelsUsed)
	}
	if depth.LevelsReturned.Bids != 2 || depth.LevelsReturned.Asks != 2 {
		t.Fatalf("expected 2 bid levels and 2 ask levels, got %+v", depth.LevelsReturned)
	}
	if len(depth.Bids) != 2 || len(depth.Asks) != 2 {
		t.Fatalf("expected 2 bid levels and 2 ask levels, got %d bids and %d asks", len(depth.Bids), len(depth.Asks))
	}

	if !almostEqual(depth.Bids[0].Price, 0.72) {
		t.Fatalf("expected best bid price 0.72, got %f", depth.Bids[0].Price)
	}
	if !almostEqual(depth.Bids[0].Size, 1000) {
		t.Fatalf("expected best bid size 1000, got %f", depth.Bids[0].Size)
	}
	if !almostEqual(depth.Bids[1].Price, 0.71) {
		t.Fatalf("expected second bid price 0.71, got %f", depth.Bids[1].Price)
	}
	if !almostEqual(depth.Bids[1].CumulativeSize, 6000) {
		t.Fatalf("expected second bid cumulative size 6000, got %f", depth.Bids[1].CumulativeSize)
	}

	if !almostEqual(depth.Asks[0].Price, 0.73) {
		t.Fatalf("expected best ask price 0.73, got %f", depth.Asks[0].Price)
	}
	if !almostEqual(depth.Asks[1].Price, 0.74) {
		t.Fatalf("expected second ask price 0.74, got %f", depth.Asks[1].Price)
	}
	if !almostEqual(depth.Asks[1].CumulativeSize, 2300) {
		t.Fatalf("expected second ask cumulative size 2300, got %f", depth.Asks[1].CumulativeSize)
	}

	if !almostEqual(depth.BidDepthTotalShares, 6000) {
		t.Fatalf("expected bid total shares 6000, got %f", depth.BidDepthTotalShares)
	}
	if !almostEqual(depth.BidDepthTotalNotionalUSD, 4270) {
		t.Fatalf("expected bid total notional 4270, got %f", depth.BidDepthTotalNotionalUSD)
	}
	if !almostEqual(depth.AskDepthTotalShares, 2300) {
		t.Fatalf("expected ask total shares 2300, got %f", depth.AskDepthTotalShares)
	}
	if !almostEqual(depth.AskDepthTotalNotionalUSD, 1687) {
		t.Fatalf("expected ask total notional 1687, got %f", depth.AskDepthTotalNotionalUSD)
	}

	if !almostEqual(depth.TopOfBook.BestBid, 0.72) {
		t.Fatalf("expected best_bid 0.72, got %f", depth.TopOfBook.BestBid)
	}
	if !almostEqual(depth.TopOfBook.BestAsk, 0.73) {
		t.Fatalf("expected best_ask 0.73, got %f", depth.TopOfBook.BestAsk)
	}
	if !almostEqual(depth.TopOfBook.Spread, 0.01) {
		t.Fatalf("expected spread 0.01, got %f", depth.TopOfBook.Spread)
	}
}

func TestBuildOrderBookDepth_InvalidLevel(t *testing.T) {
	book := &polymarket.OrderBook{
		Bids: []polymarket.OrderBookEntry{
			{Price: "not-a-number", Size: "10"},
		},
	}

	_, err := buildOrderBookDepth(book, "token-1", 5)
	if err == nil {
		t.Fatal("expected error for invalid bid level")
	}
	if !strings.Contains(err.Error(), "invalid bid price") {
		t.Fatalf("expected invalid bid price error, got %v", err)
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
