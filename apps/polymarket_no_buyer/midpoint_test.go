package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func midpointTestApp(fake *fakeTradingClient) *App {
	cfg := defaultConfig()
	return &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func entries(prices ...string) []polymarket.OrderBookEntry {
	out := make([]polymarket.OrderBookEntry, 0, len(prices))
	for _, p := range prices {
		out = append(out, polymarket.OrderBookEntry{Price: p, Size: "100"})
	}
	return out
}

func TestComputeNoMidpoint_TwoSided(t *testing.T) {
	// Best NO bid is the MAX bid (0.90), best NO ask is the MIN ask (0.92), with
	// deliberately unsorted entries to prove ordering is not assumed.
	book := &polymarket.OrderBookDetail{
		Bids: entries("0.88", "0.90", "0.85"),
		Asks: entries("0.95", "0.92", "0.99"),
	}
	mid, reason := computeNoMidpoint(book)
	if reason != "" {
		t.Fatalf("unexpected skip reason %q", reason)
	}
	if mid.BestNoBid != 0.90 {
		t.Errorf("best_no_bid = %v, want 0.90", mid.BestNoBid)
	}
	if mid.BestNoAsk != 0.92 {
		t.Errorf("best_no_ask = %v, want 0.92", mid.BestNoAsk)
	}
	if mid.Midpoint != 0.91 {
		t.Errorf("no_midpoint = %v, want 0.91", mid.Midpoint)
	}
}

func TestComputeNoMidpoint_OneSidedAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		book *polymarket.OrderBookDetail
	}{
		{"nil book", nil},
		{"empty book", &polymarket.OrderBookDetail{}},
		{"bid only", &polymarket.OrderBookDetail{Bids: entries("0.90")}},
		{"ask only", &polymarket.OrderBookDetail{Asks: entries("0.92")}},
		{"unparseable bids", &polymarket.OrderBookDetail{Bids: entries("x"), Asks: entries("0.92")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mid, reason := computeNoMidpoint(tc.book)
			if reason != skipNoTwoSidedBook {
				t.Fatalf("reason = %q, want %q", reason, skipNoTwoSidedBook)
			}
			if (mid != noMidpoint{}) {
				t.Errorf("expected no midpoint produced, got %+v", mid)
			}
		})
	}
}

func TestFetchNoMidpoint_EligibleLogShape(t *testing.T) {
	const noID = "52114"
	fake := &fakeTradingClient{
		books: map[string]*polymarket.OrderBookDetail{
			noID: {Bids: entries("0.90", "0.85"), Asks: entries("0.92", "0.95")},
		},
	}
	app := midpointTestApp(fake)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_mid")

	tokens := binaryTokens{YesTokenID: "71902", NoTokenID: noID}
	mid, reason := app.fetchNoMidpoint(context.Background(), logger, "0xabc", tokens)
	if reason != "" {
		t.Fatalf("unexpected skip reason %q", reason)
	}
	if mid.Midpoint != 0.91 {
		t.Fatalf("no_midpoint = %v, want 0.91", mid.Midpoint)
	}

	logs := eventsNamed(parseEvents(t, buf.String()), "no_midpoint")
	if len(logs) != 1 {
		t.Fatalf("no_midpoint logs = %d, want 1", len(logs))
	}
	e := logs[0]
	if e["condition_id"] != "0xabc" || e["yes_token_id"] != "71902" || e["no_token_id"] != noID {
		t.Errorf("log missing condition/token fields: %v", e)
	}
	if e["best_no_bid"] != 0.90 || e["best_no_ask"] != 0.92 || e["no_midpoint"] != 0.91 {
		t.Errorf("log bid/ask/midpoint = %v/%v/%v, want 0.90/0.92/0.91", e["best_no_bid"], e["best_no_ask"], e["no_midpoint"])
	}
	if e["status"] != "ok" {
		t.Errorf("status = %v, want ok", e["status"])
	}
}

func TestFetchNoMidpoint_SkippedLogShape(t *testing.T) {
	const noID = "52114"
	fake := &fakeTradingClient{
		books: map[string]*polymarket.OrderBookDetail{
			noID: {Bids: entries("0.90")}, // one-sided
		},
	}
	app := midpointTestApp(fake)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_skip")

	tokens := binaryTokens{YesTokenID: "71902", NoTokenID: noID}
	mid, reason := app.fetchNoMidpoint(context.Background(), logger, "0xabc", tokens)
	if reason != skipNoTwoSidedBook {
		t.Fatalf("reason = %q, want %q", reason, skipNoTwoSidedBook)
	}
	if (mid != noMidpoint{}) {
		t.Errorf("expected no midpoint produced, got %+v", mid)
	}

	logs := eventsNamed(parseEvents(t, buf.String()), "no_midpoint")
	if len(logs) != 1 {
		t.Fatalf("no_midpoint logs = %d, want 1", len(logs))
	}
	e := logs[0]
	if e["status"] != "skipped" || e["skip_reason"] != skipNoTwoSidedBook.String() {
		t.Errorf("skipped log = %v, want status=skipped reason=%q", e, skipNoTwoSidedBook.String())
	}
	if _, ok := e["no_midpoint"]; ok {
		t.Errorf("skipped log must not carry a no_midpoint field: %v", e)
	}
}

func TestFetchNoMidpoint_FetchError(t *testing.T) {
	const noID = "52114"
	fake := &fakeTradingClient{
		bookErrByToken: map[string]error{noID: errors.New("boom")},
	}
	app := midpointTestApp(fake)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_err")

	tokens := binaryTokens{YesTokenID: "71902", NoTokenID: noID}
	_, reason := app.fetchNoMidpoint(context.Background(), logger, "0xabc", tokens)
	if reason != skipNoTwoSidedBook {
		t.Fatalf("reason = %q, want %q", reason, skipNoTwoSidedBook)
	}
	logs := eventsNamed(parseEvents(t, buf.String()), "no_midpoint")
	if len(logs) != 1 || logs[0]["fetch_error"] != "boom" {
		t.Fatalf("expected a skipped log carrying fetch_error=boom, got %v", logs)
	}
}
