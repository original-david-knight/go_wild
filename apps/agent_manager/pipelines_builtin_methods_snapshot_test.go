package main

import (
	"context"
	"fmt"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestExecuteBuiltinPipelineMethodPolymarketSnapshotIncludesCapitalContext(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		positions: []polymarket.Position{
			{Asset: "yes-1", ConditionID: "cond-1", Outcome: "Yes", Size: 10, CurrentValue: 100},
			{Asset: "no-2", ConditionID: "cond-2", Outcome: "No", Size: 5, CurrentValue: 50},
		},
		orders: []polymarket.Order{
			{ID: "ord-1", Market: "cond-3", AssetID: "asset-3", Side: polymarket.Buy, Price: "0.40", OriginalSize: "3", SizeMatched: "0"},
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 300, nil)
	installTestBuiltinPolymarketLiquidUSDBalance(t, 450, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-snapshot-context",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_snapshot",
	}, map[string]any{})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(snapshot with capital context) returned error: %v", err)
	}

	if got, _ := result["aum"].(float64); got != 600 {
		t.Fatalf("expected aum 600, got %v", result["aum"])
	}
	if got, _ := result["max_allowed"].(float64); got != 30 {
		t.Fatalf("expected max_allowed 30, got %v", result["max_allowed"])
	}
	if got, _ := result["usdc_balance"].(float64); got != 300 {
		t.Fatalf("expected usdc_balance 300, got %v", result["usdc_balance"])
	}
	if got, _ := result["liquid_usd_balance"].(float64); got != 450 {
		t.Fatalf("expected liquid_usd_balance 450, got %v", result["liquid_usd_balance"])
	}

	items, ok := result["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any items, got %#v", result["items"])
	}
	byCondition := make(map[string]map[string]any, len(items))
	for _, item := range items {
		byCondition[fmt.Sprint(item["condition_id"])] = item
	}

	positionItem := byCondition["cond-1"]
	if positionItem == nil {
		t.Fatalf("expected cond-1 item in %#v", items)
	}
	if got := fmt.Sprint(positionItem["company_id"]); got != "company-1" {
		t.Fatalf("expected company_id company-1, got %q", got)
	}
	if got, _ := positionItem["current_position"].(float64); got != 10 {
		t.Fatalf("expected cond-1 current_position 10, got %v", positionItem["current_position"])
	}
	if got, _ := positionItem["remaining_capacity"].(float64); got != 20 {
		t.Fatalf("expected cond-1 remaining_capacity 20, got %v", positionItem["remaining_capacity"])
	}

	orderItem := byCondition["cond-3"]
	if orderItem == nil {
		t.Fatalf("expected cond-3 item in %#v", items)
	}
	if got, _ := orderItem["current_position"].(float64); got != 0 {
		t.Fatalf("expected cond-3 current_position 0, got %v", orderItem["current_position"])
	}
	if got, _ := orderItem["remaining_capacity"].(float64); got != 30 {
		t.Fatalf("expected cond-3 remaining_capacity 30, got %v", orderItem["remaining_capacity"])
	}
}

func TestHydrateBuiltinPolymarketManageSizingContextUsesShortLivedCache(t *testing.T) {
	resetTestBuiltinPolymarketSizingCache(t)

	liquidCalls := 0
	prevLiquidBalance := getBuiltinPolymarketLiquidUSDBalance
	prevUSDCBalance := getBuiltinPolymarketUSDCBalance
	getBuiltinPolymarketLiquidUSDBalance = func(context.Context, *PipelineEngine, string) (float64, error) {
		liquidCalls++
		return 1000, nil
	}
	getBuiltinPolymarketUSDCBalance = func(context.Context, *PipelineEngine, string) (float64, error) {
		return 1000, nil
	}
	t.Cleanup(func() {
		getBuiltinPolymarketLiquidUSDBalance = prevLiquidBalance
		getBuiltinPolymarketUSDCBalance = prevUSDCBalance
	})

	positions := []polymarket.Position{
		{Asset: "target-yes-token", ConditionID: "cond-target-market", Outcome: "Yes", Size: 20, CurrentValue: 200},
	}
	allPositions := append([]polymarket.Position{}, positions...)
	allPositions = append(allPositions, polymarket.Position{
		Asset:        "other-yes-token",
		ConditionID:  "cond-other-market",
		Outcome:      "Yes",
		Size:         30,
		CurrentValue: 600,
	})

	firstPayload := &builtinPolymarketPayload{ConditionID: "cond-target-market"}
	if got := hydrateBuiltinPolymarketManageSizingContext(context.Background(), nil, "company-1", firstPayload, positions, allPositions); got != "live_snapshot" {
		t.Fatalf("expected first sizing source live_snapshot, got %q", got)
	}
	if liquidCalls != 1 {
		t.Fatalf("expected one liquid balance lookup, got %d", liquidCalls)
	}
	if firstPayload.AUM != 1800 {
		t.Fatalf("expected first AUM 1800, got %v", firstPayload.AUM)
	}
	if firstPayload.MaxAllowed != 90 {
		t.Fatalf("expected first max_allowed 90, got %v", firstPayload.MaxAllowed)
	}
	if firstPayload.RemainingCapacity != 70 {
		t.Fatalf("expected first remaining_capacity 70, got %v", firstPayload.RemainingCapacity)
	}

	getBuiltinPolymarketLiquidUSDBalance = func(context.Context, *PipelineEngine, string) (float64, error) {
		liquidCalls++
		return 0, fmt.Errorf("cache miss unexpectedly hit liquid balance")
	}
	secondPayload := &builtinPolymarketPayload{ConditionID: "cond-target-market"}
	if got := hydrateBuiltinPolymarketManageSizingContext(context.Background(), nil, "company-1", secondPayload, positions, allPositions); got != "live_cache" {
		t.Fatalf("expected second sizing source live_cache, got %q", got)
	}
	if liquidCalls != 1 {
		t.Fatalf("expected cached sizing context to skip liquid balance lookup, got %d calls", liquidCalls)
	}
	if secondPayload.AUM != 1800 {
		t.Fatalf("expected cached AUM 1800, got %v", secondPayload.AUM)
	}
	if secondPayload.MaxAllowed != 90 {
		t.Fatalf("expected cached max_allowed 90, got %v", secondPayload.MaxAllowed)
	}
	if secondPayload.RemainingCapacity != 70 {
		t.Fatalf("expected cached remaining_capacity 70, got %v", secondPayload.RemainingCapacity)
	}
}
