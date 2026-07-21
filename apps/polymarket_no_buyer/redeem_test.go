package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func redeemTestApp(fake *fakeTradingClient, dryRun bool) *App {
	cfg := defaultConfig()
	cfg.DryRun = dryRun
	return &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func TestRedeem_SelectsRedeemableSubset(t *testing.T) {
	positions := []polymarket.Position{
		{ConditionID: "0xopen", Redeemable: false, Size: 10},
		{ConditionID: "0xredeem1", Redeemable: true, Size: 5, Outcome: "Yes"},
		{ConditionID: "0xzero", Redeemable: true, Size: 0},
		{ConditionID: "0xredeem2", Redeemable: true, Size: 3, Outcome: "No"},
		{ConditionID: "0xredeem1", Redeemable: true, Size: 2, Outcome: "Yes"}, // dup condition
	}
	got := selectRedeemableConditions(positions)
	if len(got) != 2 {
		t.Fatalf("got %d conditions, want 2: %+v", len(got), got)
	}
	if got[0].ConditionID != "0xredeem1" || got[1].ConditionID != "0xredeem2" {
		t.Fatalf("unexpected/unsorted conditions: %+v", got)
	}
	if got[0].Size != 7 { // 5 + 2 aggregated
		t.Errorf("aggregated size = %v, want 7", got[0].Size)
	}
}

func TestRedeem_PerMarketFailureIsolation(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{
			{ConditionID: "0xa", Redeemable: true, Size: 1},
			{ConditionID: "0xb", Redeemable: true, Size: 1},
			{ConditionID: "0xc", Redeemable: true, Size: 1},
		},
		redeemErrByCondition: map[string]error{"0xa": errors.New("boom")},
	}
	app := redeemTestApp(fake, false)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_x")

	if err := app.redeemPass(context.Background(), logger); err != nil {
		t.Fatalf("redeemPass returned error %v, want nil (per-market failure must be isolated)", err)
	}
	// Live redemption is a single redeem-all call; per-condition isolation happens
	// inside it and surfaces as individual failed/succeeded transactions.
	if len(fake.redeemCalls) != 1 || fake.redeemCalls[0] != "" {
		t.Fatalf("redeem calls = %v, want exactly one redeem-all call (empty conditionID)", fake.redeemCalls)
	}
	status := map[string]any{}
	for _, e := range eventsNamed(parseEvents(t, buf.String()), "redeem_attempt") {
		cond, _ := e["condition_id"].(string)
		status[cond] = e["status"]
		if cond == "0xa" && (e["reason"] == "" || e["reason"] == nil) {
			t.Errorf("failing condition log = %v, want a non-empty reason", e)
		}
	}
	// The failing condition is logged failed; the healthy ones still succeed.
	if status["0xa"] != "failed" {
		t.Errorf("0xa status = %v, want failed", status["0xa"])
	}
	if status["0xb"] != "succeeded" || status["0xc"] != "succeeded" {
		t.Errorf("healthy conditions status = %v/%v, want succeeded (failure on 0xa must not block them)", status["0xb"], status["0xc"])
	}
}

func TestRedeem_OrderingBeforeOtherPasses(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{{ConditionID: "0xa", Redeemable: true, Size: 1}},
	}
	app := redeemTestApp(fake, false)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_x")
	app.runOnce(context.Background(), logger)

	order := fake.callOrder()
	if len(order) < 2 || order[0] != "GetPositions" || order[1] != "RedeemWinnings:" {
		t.Fatalf("redeem must run first; got call order %v", order)
	}
}

func TestRedeem_DryRunNoSubmit(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{
			{ConditionID: "0xa", Redeemable: true, Size: 1},
			{ConditionID: "0xb", Redeemable: true, Size: 1},
		},
	}
	app := redeemTestApp(fake, true) // dry-run
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_x")
	if err := app.redeemPass(context.Background(), logger); err != nil {
		t.Fatalf("redeemPass: %v", err)
	}
	if len(fake.redeemCalls) != 0 {
		t.Fatalf("dry-run made %d redeem calls, want 0", len(fake.redeemCalls))
	}
	would := 0
	for _, e := range eventsNamed(parseEvents(t, buf.String()), "redeem_attempt") {
		if e["status"] == "would_redeem" {
			would++
		}
	}
	if would != 2 {
		t.Errorf("would_redeem count = %d, want 2", would)
	}
}

func TestRedeem_LogShape(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{{ConditionID: "0xa", Redeemable: true, Size: 1, Outcome: "No"}},
	}
	app := redeemTestApp(fake, false)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_shape")
	if err := app.redeemPass(context.Background(), logger); err != nil {
		t.Fatalf("redeemPass: %v", err)
	}
	attempts := eventsNamed(parseEvents(t, buf.String()), "redeem_attempt")
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	e := attempts[0]
	if e["run_id"] != "run_shape" || e["condition_id"] != "0xa" || e["status"] == nil {
		t.Errorf("redeem_attempt missing required fields: %v", e)
	}
}
