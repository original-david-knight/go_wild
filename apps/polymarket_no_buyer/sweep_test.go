package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func sweepTestApp(fake *fakeTradingClient, wallet *fakeWallet, dryRun bool) *App {
	cfg := defaultConfig()
	cfg.DryRun = dryRun
	app := &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
	if wallet != nil {
		app.wallet = wallet
	}
	return app
}

func TestSweep_LiveWrapsAndLogs(t *testing.T) {
	fake := &fakeTradingClient{
		sweepResult: &polymarket.CollateralSweepResult{
			Swept: []polymarket.SweptCollateral{
				{TokenAddress: polymarket.USDCAddress, Symbol: "USDC.e", Amount: 9.08, TxHash: "0xaaa"},
				{TokenAddress: polymarket.NativeUSDCAddress, Symbol: "USDC", Amount: 50, TxHash: "0xbbb"},
			},
			TotalSwept: 59.08,
		},
	}
	app := sweepTestApp(fake, nil, false)
	var buf bytes.Buffer

	app.sweepPass(context.Background(), NewLogger(&buf, "run_x"))

	if fake.sweepCalls != 1 {
		t.Fatalf("sweep calls = %d, want 1", fake.sweepCalls)
	}
	events := parseEvents(t, buf.String())
	wrapped := eventsNamed(events, "sweep")
	if len(wrapped) != 2 {
		t.Fatalf("got %d sweep events, want 2: %v", len(wrapped), wrapped)
	}
	for _, e := range wrapped {
		if e["status"] != "wrapped" {
			t.Errorf("sweep status = %v, want wrapped", e["status"])
		}
	}
	results := eventsNamed(events, "sweep_result")
	if len(results) != 1 {
		t.Fatalf("got %d sweep_result events, want 1", len(results))
	}
	if got := results[0]["total_usdc_wrapped"].(float64); got != 59.08 {
		t.Errorf("total_usdc_wrapped = %v, want 59.08", got)
	}
}

func TestSweep_DryRunDoesNotCallClient(t *testing.T) {
	fake := &fakeTradingClient{}
	wallet := &fakeWallet{balance: "12.5"}
	app := sweepTestApp(fake, wallet, true)
	var buf bytes.Buffer

	app.sweepPass(context.Background(), NewLogger(&buf, "run_x"))

	if fake.sweepCalls != 0 {
		t.Fatalf("dry-run must not call SweepCollateralToPUSD; called %d time(s)", fake.sweepCalls)
	}

	// The scan reads both sweepable assets through the wallet helper.
	var scanned []string
	for _, c := range wallet.balanceCalls() {
		scanned = append(scanned, c.tokenAddress)
	}
	for _, want := range []string{polymarket.USDCAddress, polymarket.NativeUSDCAddress} {
		if !slices.Contains(scanned, want) {
			t.Errorf("dry-run scan did not read balance of %s; read %v", want, scanned)
		}
	}

	events := parseEvents(t, buf.String())
	would := eventsNamed(events, "sweep")
	if len(would) != 2 {
		t.Fatalf("got %d sweep events, want 2 (one per asset): %v", len(would), would)
	}
	for _, e := range would {
		if e["status"] != "would_wrap" {
			t.Errorf("dry-run sweep status = %v, want would_wrap", e["status"])
		}
		if got := e["amount"].(float64); got != 12.5 {
			t.Errorf("would_wrap amount = %v, want 12.5", got)
		}
	}
}

func TestSweep_DryRunSkipsDustBalances(t *testing.T) {
	fake := &fakeTradingClient{}
	wallet := &fakeWallet{balance: "0.005"} // below the one-cent dust threshold
	app := sweepTestApp(fake, wallet, true)
	var buf bytes.Buffer

	app.sweepPass(context.Background(), NewLogger(&buf, "run_x"))

	events := parseEvents(t, buf.String())
	if would := eventsNamed(events, "sweep"); len(would) != 0 {
		t.Fatalf("dust balances must not be reported as would_wrap: %v", would)
	}
	results := eventsNamed(events, "sweep_result")
	if len(results) != 1 || results[0]["assets_swept"].(float64) != 0 {
		t.Fatalf("sweep_result = %v, want one event with assets_swept 0", results)
	}
}

func TestSweep_ErrorDoesNotAbortRun(t *testing.T) {
	fake := &fakeTradingClient{sweepErr: errors.New("rpc down")}
	app := sweepTestApp(fake, nil, false)
	var buf bytes.Buffer

	app.runOnce(context.Background(), NewLogger(&buf, "run_x"))

	events := parseEvents(t, buf.String())
	if errs := eventsNamed(events, "sweep_error"); len(errs) != 1 {
		t.Fatalf("got %d sweep_error events, want 1", len(errs))
	}
	// The pipeline continues past the failed sweep: the stale pass still runs and
	// the run completes.
	if !slices.Contains(fake.callOrder(), "GetOrders:") {
		t.Errorf("stale pass skipped after sweep error; calls: %v", fake.callOrder())
	}
	if done := eventsNamed(events, "run_done"); len(done) != 1 {
		t.Errorf("run did not complete after sweep error")
	}
}

func TestSweep_OrderingAfterRedeemBeforeStale(t *testing.T) {
	fake := &fakeTradingClient{
		positions: []polymarket.Position{{ConditionID: "0xa", Redeemable: true, Size: 1}},
	}
	app := sweepTestApp(fake, nil, false)
	var buf bytes.Buffer

	app.runOnce(context.Background(), NewLogger(&buf, "run_x"))

	order := fake.callOrder()
	redeem := slices.Index(order, "RedeemWinnings:")
	sweep := slices.Index(order, "SweepCollateralToPUSD")
	stale := slices.Index(order, "GetOrders:")
	if redeem < 0 || sweep < 0 || stale < 0 || !(redeem < sweep && sweep < stale) {
		t.Fatalf("want redeem < sweep < stale-cancel; got call order %v", order)
	}
}
