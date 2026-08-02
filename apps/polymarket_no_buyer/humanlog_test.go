package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestHumanLog_ActionsShownSkipsHiddenByDefault(t *testing.T) {
	skips := []struct {
		name string
		f    map[string]any
	}{
		{"market_eligibility", map[string]any{"status": "rejected", "condition_id": "0xabc", "reason": "liquidity_below_min"}},
		{"reconcile_skip", map[string]any{"condition_id": "0xabc", "reason": "no_midpoint_too_low"}},
		{"sizing", map[string]any{"condition_id": "0xabc"}},
		{"no_midpoint", map[string]any{"status": "skipped", "skip_reason": "no usable two-sided NO order book"}},
		{"stale_order", map[string]any{"status": "kept", "condition_id": "0xabc"}},
	}
	for _, ev := range skips {
		if _, show := renderHuman("run_x", ev.name, ev.f, false); show {
			t.Errorf("%s should be hidden in non-verbose", ev.name)
		}
		if _, show := renderHuman("run_x", ev.name, ev.f, true); !show {
			t.Errorf("%s should be shown in verbose", ev.name)
		}
	}

	actions := []struct {
		name string
		f    map[string]any
	}{
		{"reconcile_order", map[string]any{"status": "placed", "question": "Will X?", "shares": 12.5, "price": 0.91, "notional": 11.38, "expiration": int64(1800000000)}},
		{"redeem_attempt", map[string]any{"status": "succeeded", "title": "Will Y?", "collateral_payout": "1.5"}},
		{"account_value", map[string]any{"status": "ok", "total": 1234.5, "wallet_usdc": 1000.0, "positions_value": 234.5}},
		{"discover_summary", map[string]any{"markets_scanned": 312, "markets_eligible": 5}},
		{"reconcile_order", map[string]any{"status": "canceled", "condition_id": "0xabc", "reason": "diverged"}},
	}
	for _, ev := range actions {
		if _, show := renderHuman("run_x", ev.name, ev.f, false); !show {
			t.Errorf("%s (%v) should always be shown", ev.name, ev.f["status"])
		}
	}
}

func TestHumanLog_PrefersQuestionAndShortensIDs(t *testing.T) {
	longCond := "0x05260e1c44dc94aadd963c29e0005618d4b9975db01f0883e01455c729a04cf1"

	line, _ := renderHuman("run_x", "reconcile_order", map[string]any{
		"status": "placed", "question": "Will it rain?", "condition_id": longCond,
		"shares": 10.0, "price": 0.9, "notional": 9.0, "expiration": int64(1800000000),
	}, false)
	if !strings.Contains(line, "Will it rain?") {
		t.Errorf("expected the question in the line: %q", line)
	}
	if strings.Contains(line, longCond) {
		t.Errorf("the full condition id must never appear: %q", line)
	}

	// No question available -> shortened id, never the full id.
	skip, _ := renderHuman("run_x", "reconcile_skip", map[string]any{"condition_id": longCond, "reason": "liquidity_below_min"}, true)
	if strings.Contains(skip, longCond) {
		t.Errorf("long id must be shortened: %q", skip)
	}
	if !strings.Contains(skip, longCond[:10]) {
		t.Errorf("expected the short id prefix %q in: %q", longCond[:10], skip)
	}
	if !strings.Contains(skip, "liquidity below min") {
		t.Errorf("reason should be humanized (spaces not underscores): %q", skip)
	}
}

func TestHumanLog_FormatsActions(t *testing.T) {
	exp := time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC).Unix()
	placed, _ := renderHuman("run_x", "reconcile_order", map[string]any{
		"status": "placed", "question": "Q", "shares": 12.5, "price": 0.91, "notional": 11.38, "expiration": exp,
	}, false)
	for _, want := range []string{"placed YES buy", "\"Q\"", "12.5", "$0.91", "$11.38", "expires"} {
		if !strings.Contains(placed, want) {
			t.Errorf("placed line missing %q: %q", want, placed)
		}
	}

	acct, _ := renderHuman("run_x", "account_value", map[string]any{"status": "ok", "total": 1234.56, "wallet_usdc": 1000.0, "positions_value": 234.56}, false)
	if !strings.Contains(acct, "$1,234.56") {
		t.Errorf("expected comma-grouped total: %q", acct)
	}
}

// TestHumanLog_PipelineEndToEnd drives the whole pipeline with a human logger over
// the fake client and asserts the output is readable text (no JSON), shows the
// action-level summaries by default, and reveals per-market skips only with verbose.
func TestHumanLog_PipelineEndToEnd(t *testing.T) {
	markets, books := buildEligibleMarkets(2)
	gamma := map[string]*polymarket.Market{}
	for i := range markets {
		gamma[markets[i].ConditionID] = &markets[i]
	}
	fake := &fakeTradingClient{
		markets:      markets,
		books:        books,
		gammaMarkets: gamma,
		positions: []polymarket.Position{
			{ConditionID: "0xresolved", Redeemable: true, Size: 5, Title: "Resolved market", Outcome: "Yes"},
		},
	}
	wallet := &fakeWallet{balance: "1000"}

	run := func(verbose bool) string {
		cfg := defaultConfig()
		app := &App{cfg: &cfg, trading: fake, wallet: wallet, newRunID: seqRunID(), now: func() time.Time { return runNow }}
		var buf bytes.Buffer
		app.runOnce(context.Background(), NewHumanLogger(&buf, "run_00aaaa", verbose))
		return buf.String()
	}

	out := run(false)
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "{") {
			t.Fatalf("found a JSON line in human output:\n%s", out)
		}
	}
	for _, want := range []string{"run 00aaaa", "Account value:", "Scanned", "eligible"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "– skip") {
		t.Errorf("non-verbose output should not contain per-market skip lines:\n%s", out)
	}

	verbose := run(true)
	if !strings.Contains(verbose, "eligible:") && !strings.Contains(verbose, "– skip") {
		t.Errorf("verbose output should include per-market detail (eligible/skip):\n%s", verbose)
	}
	if len(strings.Split(verbose, "\n")) <= len(strings.Split(out, "\n")) {
		t.Errorf("verbose output should have more lines than default (%d vs %d)", len(strings.Split(verbose, "\n")), len(strings.Split(out, "\n")))
	}
}
