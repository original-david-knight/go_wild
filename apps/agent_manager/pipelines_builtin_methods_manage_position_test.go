package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionLegacyPlace(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-123",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-1",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"token_id": "token-1",
		"price":    0.61,
		"size":     15,
		"side":     "buy",
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(place) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected 1 placed order, got %d", len(client.placedOrders))
	}
	got := client.placedOrders[0]
	if got.TokenID != "token-1" || got.Price != 0.61 || got.Size != 15 {
		t.Fatalf("unexpected place-order request: %+v", got)
	}
	if got.Side != polymarket.Buy {
		t.Fatalf("expected BUY side, got %q", got.Side)
	}
	if got.OrderType != polymarket.GTC {
		t.Fatalf("expected default order type GTC, got %q", got.OrderType)
	}
	if gotStatus, _ := result["status"].(string); gotStatus != "placed" {
		t.Fatalf("expected status placed, got %q", gotStatus)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionLegacyCancel(t *testing.T) {
	client := &testBuiltinPolymarketClient{}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-1",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"action":   "cancel",
		"order_id": "ord-999",
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(cancel) returned error: %v", err)
	}

	if len(client.cancelledOrderIDs) != 1 || client.cancelledOrderIDs[0] != "ord-999" {
		t.Fatalf("expected cancel order_id ord-999, got %#v", client.cancelledOrderIDs)
	}
	if got, _ := result["status"].(string); got != "cancelled" {
		t.Fatalf("expected status cancelled, got %q", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionLegacyViewFilters(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		positions: []polymarket.Position{
			{Asset: "token-1", ConditionID: "cond-1", Title: "Match"},
			{Asset: "token-2", ConditionID: "cond-2", Title: "Ignore"},
		},
		orders: []polymarket.Order{
			{ID: "ord-1", AssetID: "token-1", Market: "cond-1", Side: "BUY"},
			{ID: "ord-2", AssetID: "token-2", Market: "cond-2", Side: "SELL"},
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, nil, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"company_id":   "company-1",
		"condition_id": "cond-1",
		"token_id":     "token-1",
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(update_position legacy view) returned error: %v", err)
	}

	if client.gotOrdersMarket != "cond-1" {
		t.Fatalf("expected GetOrders market filter cond-1, got %q", client.gotOrdersMarket)
	}
	if got, _ := result["positions_found"].(int); got != 1 {
		t.Fatalf("expected positions_found 1, got %v", result["positions_found"])
	}
	if got, _ := result["orders_found"].(int); got != 1 {
		t.Fatalf("expected orders_found 1, got %v", result["orders_found"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionPlacesAggressiveBuy(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-1",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-buy-1",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-2",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-1",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"remaining_capacity":    12.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(manage_position buy) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected 1 placed order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on yes-token, got %+v", order)
	}
	if order.Price != 0.62 || order.Size != 12 {
		t.Fatalf("unexpected order price/size: %+v", order)
	}
	if order.OrderType != polymarket.GTC {
		t.Fatalf("expected GTC order type, got %q", order.OrderType)
	}
	if got, _ := result["status"].(string); got != "placed" {
		t.Fatalf("expected status placed, got %q", got)
	}
	if got, _ := result["side"].(string); got != "yes" {
		t.Fatalf("expected side yes, got %q", got)
	}
	if got, _ := result["execution_tier"].(int); got != 1 {
		t.Fatalf("expected execution_tier 1, got %v", result["execution_tier"])
	}
	if got, _ := result["buy_orders_placed"].(int); got != 1 {
		t.Fatalf("expected buy_orders_placed 1, got %v", result["buy_orders_placed"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionAcceptsLegacyStringTokens(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-legacy-token-strings",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-legacy-token-strings",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-legacy-token-strings",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-legacy-token-strings",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"remaining_capacity":    12.0,
			"question":              "Will test happen?",
			"tokens":                []string{"yes-token", "no-token"},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(manage_position legacy token strings) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected 1 placed order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on yes-token, got %+v", order)
	}
	if got, _ := result["status"].(string); got != "placed" {
		t.Fatalf("expected status placed, got %q", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionRefreshesLiveResolutionDate(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-live",
				Question:        "Will live market happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-live","no-live"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-live", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-live", "sell"): "0.60",
			builtinPolymarketPriceKey("no-live", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-live", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-live-1",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-live-fix",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-live",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"remaining_capacity":    12.0,
			"question":              "Wrong stale question",
			"resolution_date":       "2021-12-04T00:00:00Z",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "wrong-yes"},
				{"outcome": "No", "token_id": "wrong-no"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(refresh live market) returned error: %v", err)
	}

	if len(client.gotMarketIDs) != 1 || client.gotMarketIDs[0] != "cond-live" {
		t.Fatalf("expected GetMarket to refresh cond-live, got %#v", client.gotMarketIDs)
	}
	if got, _ := result["status"].(string); got != "placed" {
		t.Fatalf("expected status placed after live refresh, got %q", got)
	}
	if got, _ := result["question"].(string); got != "Will live market happen?" {
		t.Fatalf("expected refreshed question, got %q", got)
	}
	if len(client.placedOrders) != 1 || client.placedOrders[0].TokenID != "yes-live" {
		t.Fatalf("expected refreshed live token IDs to be used, got %#v", client.placedOrders)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionUsesLivePriceNotCache(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	engine := &PipelineEngine{db: db}

	staleCached := &polymarketCachedMarket{
		ID:              "cond-live-price",
		MarketID:        "market-stale",
		EventID:         "evt-stale",
		EventTitle:      "Stale cached market",
		EventSlug:       "stale-cached-market",
		Question:        "Will cached pricing be ignored?",
		Description:     "Stale cached metadata",
		Slug:            "cached-pricing-ignored",
		EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format("2006-01-02"),
		Volume:          "1000",
		Liquidity:       "500",
		OutcomePrices:   `["0.91","0.09"]`,
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["yes-live","no-live"]`,
		Active:          true,
		Closed:          false,
		AcceptingOrders: true,
		BestBid:         0.89,
		BestAsk:         0.91,
		SearchText:      builtinPolymarketNormalizeSearchText("Will cached pricing be ignored?"),
		SyncedAt:        time.Now().UTC(),
	}
	if err := db.Table(polymarketCachedMarket{}).Insert(ctx, staleCached); err != nil {
		t.Fatalf("insert stale cached market failed: %v", err)
	}
	if err := SetSetting(ctx, db, polymarketMarketCacheLastSyncSetting, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("set cache last sync failed: %v", err)
	}

	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-live-price",
				Question:        "Will cached pricing be ignored?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.91","0.09"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-live","no-live"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-live", "buy"):  "0.02",
			builtinPolymarketPriceKey("yes-live", "sell"): "0.019",
			builtinPolymarketPriceKey("no-live", "buy"):   "0.99",
			builtinPolymarketPriceKey("no-live", "sell"):  "0.98",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-live-price",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(ctx, engine, &data.PipelineRun{
		ID:             "run-live-price",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-live-price",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"aum":                   1000.0,
			"remaining_capacity":    12.0,
			"question":              "Will cached pricing be ignored?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-live"},
				{"outcome": "No", "token_id": "no-live"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(live price over cache) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected 1 placed order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.Price != 0.02 {
		t.Fatalf("expected live ask price 0.02, got %+v", order)
	}
	if got, _ := result["market_price"].(float64); got != 0.02 {
		t.Fatalf("expected result market_price 0.02, got %v", result["market_price"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionFallsBackToLiveMarketQuotesWhenPriceEndpointFails(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-fallback-quotes",
				Question:        "Will fallback quotes be used?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.021","0.979"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-fallback","no-fallback"]`,
				Active:          true,
				AcceptingOrders: true,
				BestBid:         0.02,
				BestAsk:         0.021,
			},
		},
		priceErrs: map[string]error{
			builtinPolymarketPriceKey("yes-fallback", "buy"):  fmt.Errorf("get price failed: API error 404: {\"error\":\"No orderbook exists for the requested token id\"}"),
			builtinPolymarketPriceKey("yes-fallback", "sell"): fmt.Errorf("get price failed: API error 404: {\"error\":\"No orderbook exists for the requested token id\"}"),
			builtinPolymarketPriceKey("no-fallback", "buy"):   fmt.Errorf("get price failed: API error 404: {\"error\":\"No orderbook exists for the requested token id\"}"),
			builtinPolymarketPriceKey("no-fallback", "sell"):  fmt.Errorf("get price failed: API error 404: {\"error\":\"No orderbook exists for the requested token id\"}"),
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-fallback-quotes",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-fallback-quotes",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-fallback-quotes",
			"estimated_probability": 0.8,
			"confidence":            0.9,
			"aum":                   1000.0,
			"remaining_capacity":    12.0,
			"question":              "Will fallback quotes be used?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-fallback"},
				{"outcome": "No", "token_id": "no-fallback"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(fallback quotes) returned error: %v", err)
	}

	if len(client.gotMarketIDs) == 0 || client.gotMarketIDs[len(client.gotMarketIDs)-1] != "cond-fallback-quotes" {
		t.Fatalf("expected GetMarket fallback for cond-fallback-quotes, got %#v", client.gotMarketIDs)
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one placed order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-fallback" || order.Price != 0.021 {
		t.Fatalf("expected fallback YES quote at 0.021, got %+v", order)
	}
	if got, _ := result["status"].(string); got != "placed" {
		t.Fatalf("expected status placed, got %q", got)
	}
	if got, _ := result["market_price"].(float64); got != 0.021 {
		t.Fatalf("expected result market_price 0.021, got %v", result["market_price"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionSkipsUnavailableOrderbook(t *testing.T) {
	noOrderbookErr := fmt.Errorf("get price failed: API error 404: {\"error\":\"No orderbook exists for the requested token id\"}")
	client := &testBuiltinPolymarketClient{
		priceErrs: map[string]error{
			builtinPolymarketPriceKey("yes-stale", "buy"):  noOrderbookErr,
			builtinPolymarketPriceKey("yes-stale", "sell"): noOrderbookErr,
			builtinPolymarketPriceKey("no-stale", "buy"):   noOrderbookErr,
			builtinPolymarketPriceKey("no-stale", "sell"):  noOrderbookErr,
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-stale-orderbook",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-stale-orderbook",
			"estimated_probability": 0.8,
			"confidence":            0.9,
			"aum":                   1000.0,
			"remaining_capacity":    12.0,
			"question":              "Will stale market be skipped?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-stale"},
				{"outcome": "No", "token_id": "no-stale"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(unavailable orderbook) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); got != "neutral" {
		t.Fatalf("expected neutral skip status, got %q", got)
	}
	if got, _ := result["pricing_unavailable"].(bool); !got {
		t.Fatalf("expected pricing_unavailable true, got %#v", result["pricing_unavailable"])
	}
	if strings.HasPrefix(fmt.Sprint(result["status"]), "FAILED") {
		t.Fatalf("expected unavailable orderbook not to fail the branch, got %#v", result["status"])
	}
	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no placed orders, got %#v", client.placedOrders)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionSkipsBuyWhenUSDCBalanceIsZero(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-zero-usdc",
				Question:        "Will zero balance avoid buy?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.02","0.98"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-zero","no-zero"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-zero", "buy"):  "0.02",
			builtinPolymarketPriceKey("yes-zero", "sell"): "0.019",
			builtinPolymarketPriceKey("no-zero", "buy"):   "0.99",
			builtinPolymarketPriceKey("no-zero", "sell"):  "0.98",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 0, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-zero-usdc",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-zero-usdc",
			"estimated_probability": 0.8,
			"confidence":            0.9,
			"aum":                   1000.0,
			"remaining_capacity":    12.0,
			"question":              "Will zero balance avoid buy?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-zero"},
				{"outcome": "No", "token_id": "no-zero"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(zero USDC) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); got != "neutral" {
		t.Fatalf("expected neutral no-action status, got %q", got)
	}
	if got, _ := result["insufficient_usdc_balance"].(bool); !got {
		t.Fatalf("expected insufficient_usdc_balance true, got %#v", result["insufficient_usdc_balance"])
	}
	if got, _ := result["available_usdc_balance"].(float64); got != 0 {
		t.Fatalf("expected available_usdc_balance 0, got %#v", result["available_usdc_balance"])
	}
	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no placed orders when USDC balance is zero, got %#v", client.placedOrders)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionTreatsBuyInsufficientBalanceAsNoAction(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-balance-reject",
				Question:        "Will rejection be no-action?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.02","0.98"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-reject","no-reject"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-reject", "buy"):  "0.02",
			builtinPolymarketPriceKey("yes-reject", "sell"): "0.019",
			builtinPolymarketPriceKey("no-reject", "buy"):   "0.99",
			builtinPolymarketPriceKey("no-reject", "sell"):  "0.98",
		},
		placeOrderErr: fmt.Errorf("place order failed after automatic allowance setup retry: place order failed: API error 400: {\"error\":\"not enough balance / allowance: the balance is not enough -> balance: 0, order amount: 283646100\"}"),
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-balance-reject",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-balance-reject",
			"estimated_probability": 0.8,
			"confidence":            0.9,
			"aum":                   1000.0,
			"remaining_capacity":    12.0,
			"question":              "Will rejection be no-action?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-reject"},
				{"outcome": "No", "token_id": "no-reject"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(balance rejection) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); got != "neutral" {
		t.Fatalf("expected neutral no-action status, got %q", got)
	}
	if got, _ := result["insufficient_usdc_balance"].(bool); !got {
		t.Fatalf("expected insufficient_usdc_balance true, got %#v", result["insufficient_usdc_balance"])
	}
	if strings.HasPrefix(fmt.Sprint(result["status"]), "FAILED") {
		t.Fatalf("expected balance rejection not to fail the branch, got %#v", result["status"])
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one attempted order before rejection, got %d", len(client.placedOrders))
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionResizesOversizedAlignedBuyOrder(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-oversized",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.02","0.98"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		orders: []polymarket.Order{
			{
				ID:           "buy-oversized",
				AssetID:      "yes-token",
				Market:       "cond-oversized",
				Side:         polymarket.Buy,
				Status:       "live",
				Type:         polymarket.GTC,
				Price:        "0.02",
				OriginalSize: "720.5",
				SizeMatched:  "0",
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.02",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.019",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.99",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.98",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-replacement",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-oversized-buy",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-oversized",
			"estimated_probability": 0.8,
			"confidence":            0.9,
			"aum":                   1000.0,
			"remaining_capacity":    720.5,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(oversized buy cleanup) returned error: %v", err)
	}

	if len(client.cancelledOrderIDs) != 1 || client.cancelledOrderIDs[0] != "buy-oversized" {
		t.Fatalf("expected oversized aligned buy order to be cancelled, got %#v", client.cancelledOrderIDs)
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected 1 replacement order, got %d", len(client.placedOrders))
	}
	if got := client.placedOrders[0].Size; got != 50 {
		t.Fatalf("expected replacement order size 50 shares, got %v", got)
	}
	if got, _ := result["max_allowed"].(float64); got != 50 {
		t.Fatalf("expected max_allowed 50, got %v", result["max_allowed"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionTopUpsAlignedBuyOrderWhenAUMGrows(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-topup-aum",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.02","0.98"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		orders: []polymarket.Order{
			{
				ID:           "buy-aligned-small",
				AssetID:      "yes-token",
				Market:       "cond-topup-aum",
				Side:         polymarket.Buy,
				Status:       "live",
				Type:         polymarket.GTC,
				Price:        "0.02",
				OriginalSize: "10",
				SizeMatched:  "0",
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.02",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.019",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.99",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.98",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-topup",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-topup-aum",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-topup-aum",
			"estimated_probability": 0.8,
			"confidence":            0.9,
			"aum":                   1000.0,
			"remaining_capacity":    10.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(top up aligned buy) returned error: %v", err)
	}

	if len(client.cancelledOrderIDs) != 0 {
		t.Fatalf("expected aligned order to be kept, got cancellations %#v", client.cancelledOrderIDs)
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected 1 top-up order, got %d", len(client.placedOrders))
	}
	if got := client.placedOrders[0].Size; got != 40 {
		t.Fatalf("expected top-up order size 40 shares, got %v", got)
	}
	if got, _ := result["status"].(string); got != "updated" {
		t.Fatalf("expected status updated, got %q", got)
	}
	if got, _ := result["max_allowed"].(float64); got != 50 {
		t.Fatalf("expected max_allowed 50, got %v", result["max_allowed"])
	}
	if got, _ := result["remaining_capacity"].(float64); got != 50 {
		t.Fatalf("expected remaining_capacity 50, got %v", result["remaining_capacity"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionKeepsAlignedTier2BuyInTightCapitalBand(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-tier2-aligned-hold",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.25","0.74"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		orders: []polymarket.Order{
			{
				ID:           "buy-tier2-aligned",
				AssetID:      "yes-token",
				Market:       "cond-tier2-aligned-hold",
				Side:         polymarket.Buy,
				Status:       "live",
				Type:         polymarket.GTD,
				Price:        "0.245",
				OriginalSize: "5",
				SizeMatched:  "0",
			},
		},
		orderBooks: map[string]*polymarket.OrderBook{
			"yes-token": {
				Market:  "cond-tier2-aligned-hold",
				AssetID: "yes-token",
				Bids:    []polymarket.OrderBookEntry{{Price: "0.24", Size: "200"}},
				Asks:    []polymarket.OrderBookEntry{{Price: "0.25", Size: "200"}},
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.25",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.24",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.74",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.73",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-tier2-aligned-hold",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-tier2-aligned-hold",
			"estimated_probability": 0.275,
			"confidence":            0.60,
			"aum":                   1000.0,
			"remaining_capacity":    50.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(tier2 aligned hold) returned error: %v", err)
	}

	if len(client.cancelledOrderIDs) != 0 {
		t.Fatalf("expected aligned tier2 buy order to be kept, got cancellations %#v", client.cancelledOrderIDs)
	}
	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no replacement order, got %#v", client.placedOrders)
	}
	if got, _ := result["status"].(string); got != "held" {
		t.Fatalf("expected status held, got %q", got)
	}
	if action := fmt.Sprint(result["action_taken"]); !strings.Contains(action, "Held aligned YES BUY order(s) totaling 5.0000 shares.") {
		t.Fatalf("expected aligned-buy hold note, got %q", action)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionCancelsTargetSellBeforeTier2Buy(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-tier2-sell-cleanup",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.25","0.74"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-tier2-sell-cleanup", Outcome: "Yes", Size: 10},
		},
		orders: []polymarket.Order{
			{
				ID:           "sell-target-stale",
				AssetID:      "yes-token",
				Market:       "cond-tier2-sell-cleanup",
				Side:         polymarket.Sell,
				Status:       "live",
				Type:         polymarket.GTC,
				Price:        "0.24",
				OriginalSize: "5",
				SizeMatched:  "0",
			},
		},
		orderBooks: map[string]*polymarket.OrderBook{
			"yes-token": {
				Market:  "cond-tier2-sell-cleanup",
				AssetID: "yes-token",
				Bids:    []polymarket.OrderBookEntry{{Price: "0.24", Size: "200"}},
				Asks:    []polymarket.OrderBookEntry{{Price: "0.25", Size: "200"}},
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.25",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.24",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.74",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.73",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-tier2-clean-topup",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-tier2-sell-cleanup",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-tier2-sell-cleanup",
			"estimated_probability": 0.275,
			"confidence":            0.60,
			"aum":                   2000.0,
			"remaining_capacity":    90.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(tier2 sell cleanup) returned error: %v", err)
	}

	if len(client.cancelledOrderIDs) != 1 || client.cancelledOrderIDs[0] != "sell-target-stale" {
		t.Fatalf("expected stale target sell order to be cancelled, got %#v", client.cancelledOrderIDs)
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one tier2 top-up order, got %#v", client.placedOrders)
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on yes-token, got %+v", order)
	}
	if order.Price != 0.245 || order.Size != 8 {
		t.Fatalf("unexpected tier2 top-up order: %+v", order)
	}
	if order.OrderType != polymarket.GTD {
		t.Fatalf("expected GTD order type, got %q", order.OrderType)
	}
	if got, _ := result["orders_cancelled"].(int); got != 1 {
		t.Fatalf("expected orders_cancelled 1, got %v", result["orders_cancelled"])
	}
	if got, _ := result["buy_orders_placed"].(int); got != 1 {
		t.Fatalf("expected buy_orders_placed 1, got %v", result["buy_orders_placed"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionDoesNotRatchetModerateConfidencePosition(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-moderate-hold",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-moderate-hold", Outcome: "Yes", Size: 25},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-moderate-hold",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-moderate-hold",
			"estimated_probability": 0.80,
			"confidence":            0.60,
			"aum":                   1000.0,
			"remaining_capacity":    25.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(moderate hold) returned error: %v", err)
	}

	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no additional orders, got %#v", client.placedOrders)
	}
	if got, _ := result["status"].(string); got != "held" {
		t.Fatalf("expected status held, got %q", got)
	}
	if got, _ := result["target_position"].(float64); got != 25 {
		t.Fatalf("expected target_position 25, got %v", result["target_position"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionBackfillsMissingCapacityFromLiveSnapshot(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-live-cap",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-live-cap", Outcome: "Yes", Size: 25, CurrentValue: 200},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.40",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.38",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-live-cap",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)
	installTestBuiltinPolymarketLiquidUSDBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-live-cap",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-live-cap",
			"estimated_probability": 0.80,
			"confidence":            0.95,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(live cap backfill) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one BUY order after live cap backfill, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on yes-token, got %+v", order)
	}
	if order.Size != 35 {
		t.Fatalf("expected top-up size 35 shares from live cap, got %v", order.Size)
	}
	if got, _ := result["sizing_context_source"].(string); got != "live_snapshot" {
		t.Fatalf("expected sizing_context_source live_snapshot, got %v", result["sizing_context_source"])
	}
	if got, _ := result["aum"].(float64); got != 1200 {
		t.Fatalf("expected aum 1200, got %v", result["aum"])
	}
	if got, _ := result["max_allowed"].(float64); got != 60 {
		t.Fatalf("expected max_allowed 60, got %v", result["max_allowed"])
	}
	if got, _ := result["target_position"].(float64); got != 60 {
		t.Fatalf("expected target_position 60, got %v", result["target_position"])
	}
	if got, _ := result["target_gap"].(float64); got != 35 {
		t.Fatalf("expected target_gap 35, got %v", result["target_gap"])
	}
	if client.gotPositionsCalls != 1 {
		t.Fatalf("expected a single GetPositions call, got %d", client.gotPositionsCalls)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionLiveSnapshotIncludesOtherMarketValue(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-target-market",
				Question:        "Will target happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["target-yes-token","target-no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "other-yes-token", ConditionID: "cond-other-market", Outcome: "Yes", Size: 40, CurrentValue: 800},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("target-yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("target-yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("target-no-token", "buy"):   "0.40",
			builtinPolymarketPriceKey("target-no-token", "sell"):  "0.38",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-other-market-value",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)
	installTestBuiltinPolymarketLiquidUSDBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-live-cap-other-market",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-target-market",
			"estimated_probability": 0.80,
			"confidence":            0.95,
			"question":              "Will target happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "target-yes-token"},
				{"outcome": "No", "token_id": "target-no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(other market live cap backfill) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one BUY order after full live cap backfill, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "target-yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on target-yes-token, got %+v", order)
	}
	if order.Size != 90 {
		t.Fatalf("expected top-up size 90 shares from full live cap, got %v", order.Size)
	}
	if got, _ := result["sizing_context_source"].(string); got != "live_snapshot" {
		t.Fatalf("expected sizing_context_source live_snapshot, got %v", result["sizing_context_source"])
	}
	if got, _ := result["current_position"].(float64); got != 0 {
		t.Fatalf("expected current_position 0 on target market, got %v", result["current_position"])
	}
	if got, _ := result["aum"].(float64); got != 1800 {
		t.Fatalf("expected aum 1800 including other market value, got %v", result["aum"])
	}
	if got, _ := result["max_allowed"].(float64); got != 90 {
		t.Fatalf("expected max_allowed 90 from full live portfolio, got %v", result["max_allowed"])
	}
	if got, _ := result["target_position"].(float64); got != 90 {
		t.Fatalf("expected target_position 90, got %v", result["target_position"])
	}
	if got, _ := result["target_gap"].(float64); got != 90 {
		t.Fatalf("expected target_gap 90, got %v", result["target_gap"])
	}
	if client.gotPositionsCalls != 1 {
		t.Fatalf("expected a single GetPositions call, got %d", client.gotPositionsCalls)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionReusesLatestThesisWhenResearchIsMissing(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	ctx := context.Background()

	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-note-seeded",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-note-seeded", Outcome: "Yes", Size: 25, CurrentValue: 200},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.40",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.38",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-note-seeded",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)
	installTestBuiltinPolymarketLiquidUSDBalance(t, 1000, nil)

	probability := 0.80
	confidence := 0.95
	if _, err := data.AddMarketNoteWithMetadata(ctx, db, "company-1", "agent-1", "cond-note-seeded", "Prior thesis", &data.MarketNoteMetadata{
		Kind:                 "builtin_polymarket_manage_position",
		Side:                 "yes",
		Question:             "Will test happen?",
		Reasoning:            "Prior structured thesis.",
		EstimatedProbability: &probability,
		Confidence:           &confidence,
		CapturedAt:           time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddMarketNoteWithMetadata failed: %v", err)
	}

	result, err := executeBuiltinPipelineMethod(ctx, pe, &data.PipelineRun{
		ID:             "run-note-seeded",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-note-seeded",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(note seeded thesis) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one BUY order after note-seeded thesis, got %d", len(client.placedOrders))
	}
	if got, _ := result["thesis_input_source"].(string); got != "latest_note" {
		t.Fatalf("expected thesis_input_source latest_note, got %v", result["thesis_input_source"])
	}
	if got, _ := result["estimated_probability"].(float64); got != 0.8 {
		t.Fatalf("expected estimated_probability 0.8, got %v", result["estimated_probability"])
	}
	if got, _ := result["confidence"].(float64); got != 0.95 {
		t.Fatalf("expected confidence 0.95, got %v", result["confidence"])
	}
	if got, _ := result["target_position"].(float64); got != 60 {
		t.Fatalf("expected target_position 60, got %v", result["target_position"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionSkipsPoisonedZeroConfidenceNoteWhenReusingThesis(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	ctx := context.Background()

	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-note-poisoned",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-note-poisoned", Outcome: "Yes", Size: 25, CurrentValue: 200},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.40",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.38",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-note-poisoned",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)
	installTestBuiltinPolymarketLiquidUSDBalance(t, 1000, nil)

	goodProbability := 0.80
	goodConfidence := 0.95
	if _, err := data.AddMarketNoteWithMetadata(ctx, db, "company-1", "agent-1", "cond-note-poisoned", "Good thesis", &data.MarketNoteMetadata{
		Kind:                 "builtin_polymarket_manage_position",
		Side:                 "yes",
		Question:             "Will test happen?",
		Reasoning:            "Real thesis to reuse.",
		EstimatedProbability: &goodProbability,
		Confidence:           &goodConfidence,
		CapturedAt:           time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("AddMarketNoteWithMetadata good thesis failed: %v", err)
	}
	zeroProbability := 0.0
	zeroConfidence := 0.0
	if _, err := data.AddMarketNoteWithMetadata(ctx, db, "company-1", "agent-1", "cond-note-poisoned", "Operational note", &data.MarketNoteMetadata{
		Kind:                 "builtin_polymarket_manage_position",
		Status:               "FAILED: missing research inputs",
		Action:               "missing research inputs",
		Side:                 "no",
		Question:             "Will test happen?",
		EstimatedProbability: &zeroProbability,
		Confidence:           &zeroConfidence,
		CapturedAt:           time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddMarketNoteWithMetadata poisoned thesis failed: %v", err)
	}

	result, err := executeBuiltinPipelineMethod(ctx, pe, &data.PipelineRun{
		ID:             "run-note-poisoned",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-note-poisoned",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(poisoned note reuse) returned error: %v", err)
	}

	if got, _ := result["thesis_input_source"].(string); got != "latest_note" {
		t.Fatalf("expected thesis_input_source latest_note, got %v", result["thesis_input_source"])
	}
	if got, _ := result["confidence"].(float64); got != 0.95 {
		t.Fatalf("expected confidence 0.95 from older good note, got %v", result["confidence"])
	}
	if got, _ := result["target_position"].(float64); got != 60 {
		t.Fatalf("expected target_position 60, got %v", result["target_position"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionFailsClearlyWhenResearchAndPriorThesisAreMissing(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-missing-thesis",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Volume:          "78000",
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-missing-thesis", Outcome: "Yes", Size: 25, CurrentValue: 200},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.40",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.38",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)
	installTestBuiltinPolymarketLiquidUSDBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), pe, &data.PipelineRun{
		ID:             "run-missing-thesis",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-missing-thesis",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(missing thesis) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); !strings.HasPrefix(got, "FAILED:") {
		t.Fatalf("expected FAILED status, got %q", got)
	}
	if got, _ := result["thesis_input_source"].(string); got != "missing" {
		t.Fatalf("expected thesis_input_source missing, got %v", result["thesis_input_source"])
	}
	if errText := fmt.Sprint(result["error"]); !strings.Contains(errText, "missing research inputs") {
		t.Fatalf("expected clear missing research error, got %q", errText)
	}
	notes, err := data.ListMarketNotes(context.Background(), db, "company-1", "cond-missing-thesis", 1)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	metadata := data.ParseMarketNoteMetadata(notes[0])
	if metadata == nil {
		t.Fatal("expected structured note metadata")
	}
	if metadata.EstimatedProbability != nil {
		t.Fatalf("expected no estimated_probability in missing-thesis note, got %#v", metadata.EstimatedProbability)
	}
	if metadata.Confidence != nil {
		t.Fatalf("expected no confidence in missing-thesis note, got %#v", metadata.Confidence)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionTopUpsModerateConfidencePositionWhenAUMGrows(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-moderate-topup",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-moderate-topup", Outcome: "Yes", Size: 25},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-moderate-topup",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-moderate-topup",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-moderate-topup",
			"estimated_probability": 0.80,
			"confidence":            0.60,
			"aum":                   2000.0,
			"remaining_capacity":    75.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(moderate aum top up) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one top-up order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on yes-token, got %+v", order)
	}
	if order.Size != 25 {
		t.Fatalf("expected top-up order size 25 shares, got %v", order.Size)
	}
	if got, _ := result["target_position"].(float64); got != 50 {
		t.Fatalf("expected target_position 50, got %v", result["target_position"])
	}
	if got, _ := result["target_side_position"].(float64); got != 25 {
		t.Fatalf("expected target_side_position 25, got %v", result["target_side_position"])
	}
	if got, _ := result["target_gap"].(float64); got != 25 {
		t.Fatalf("expected target_gap 25, got %v", result["target_gap"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionHoldsExistingExposureOnThinPositiveEdge(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-thin-topup",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.03","0.97"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "no-token", ConditionID: "cond-thin-topup", Outcome: "No", Size: 10},
		},
		orderBooks: map[string]*polymarket.OrderBook{
			"no-token": {
				Market:  "cond-thin-topup",
				AssetID: "no-token",
				Bids:    []polymarket.OrderBookEntry{{Price: "0.94", Size: "200"}},
				Asks:    []polymarket.OrderBookEntry{{Price: "0.95", Size: "200"}},
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.03",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.02",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.95",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.94",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-thin-topup",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 200, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-thin-topup",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-thin-topup",
			"estimated_probability": 0.03,
			"confidence":            0.95,
			"aum":                   400.0,
			"remaining_capacity":    10.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(thin edge top up) returned error: %v", err)
	}

	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no top-up order, got %d", len(client.placedOrders))
	}
	if got, _ := result["status"].(string); got != "held" {
		t.Fatalf("expected status held, got %v", result["status"])
	}
	if got, _ := result["target_side_position"].(float64); got != 10 {
		t.Fatalf("expected target_side_position 10, got %v", result["target_side_position"])
	}
	if got, _ := result["target_gap"].(float64); got != 10 {
		t.Fatalf("expected target_gap 10, got %v", result["target_gap"])
	}
	if action := fmt.Sprint(result["action_taken"]); !strings.Contains(action, "Target exceeds current shares") {
		t.Fatalf("expected held explanation about blocked add, got %q", action)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionUsesContinuousConfidenceSizing(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-continuous-sizing",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-continuous-sizing", Outcome: "Yes", Size: 25},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-continuous-sizing",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-continuous-sizing",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-continuous-sizing",
			"estimated_probability": 0.80,
			"confidence":            0.70,
			"aum":                   1000.0,
			"remaining_capacity":    25.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(continuous sizing) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one top-up order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Buy {
		t.Fatalf("expected BUY on yes-token, got %+v", order)
	}
	if order.Size != 12.5 {
		t.Fatalf("expected top-up size 12.5 shares, got %v", order.Size)
	}
	if got, _ := result["target_position"].(float64); got != 37.5 {
		t.Fatalf("expected target_position 37.5, got %v", result["target_position"])
	}
	if got, _ := result["position_scale"].(float64); got != 0.75 {
		t.Fatalf("expected position_scale 0.75, got %v", result["position_scale"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionShrinksTargetWhenNetEdgeIsThin(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-net-edge-thin",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.78","0.22"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.78",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.74",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.24",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.20",
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-net-edge-thin",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-net-edge-thin",
			"estimated_probability": 0.82,
			"confidence":            0.85,
			"aum":                   1000.0,
			"remaining_capacity":    50.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(net edge thin) returned error: %v", err)
	}

	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no order because net edge was too thin, got %#v", client.placedOrders)
	}
	if got, _ := result["target_position"].(float64); got != 50 {
		t.Fatalf("expected thesis/confidence target_position 50, got %v", result["target_position"])
	}
	if got, _ := result["signal_scale"].(float64); got != 0.2 {
		t.Fatalf("expected signal_scale 0.2, got %v", result["signal_scale"])
	}
	if got, _ := result["ev_desired_shares"].(float64); got != 10 {
		t.Fatalf("expected ev_desired_shares 10, got %v", result["ev_desired_shares"])
	}
	if got, _ := result["net_edge"].(float64); got != 0.02 {
		t.Fatalf("expected net_edge 0.02, got %v", result["net_edge"])
	}
	if got := fmt.Sprint(result["status"]); got != "neutral" {
		t.Fatalf("expected neutral status, got %q", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionSellsTargetSharesAboveAUMCap(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-aum-downsize",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-aum-downsize", Outcome: "Yes", Size: 60},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-downsize-target",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-aum-downsize",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-aum-downsize",
			"estimated_probability": 0.80,
			"confidence":            0.90,
			"aum":                   1000.0,
			"remaining_capacity":    999.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(target downsize by aum) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one SELL order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Sell {
		t.Fatalf("expected SELL on yes-token, got %+v", order)
	}
	if order.Price != 0.60 || order.Size != 10 {
		t.Fatalf("unexpected sell order price/size: %+v", order)
	}
	if got, _ := result["status"].(string); got != "updated" {
		t.Fatalf("expected status updated, got %q", got)
	}
	if got, _ := result["max_allowed"].(float64); got != 50 {
		t.Fatalf("expected max_allowed 50, got %v", result["max_allowed"])
	}
	if got, _ := result["sell_orders_placed"].(int); got != 1 {
		t.Fatalf("expected sell_orders_placed 1, got %v", result["sell_orders_placed"])
	}
	if got, _ := result["buy_orders_placed"].(int); got != 0 {
		t.Fatalf("expected buy_orders_placed 0, got %v", result["buy_orders_placed"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionSellsDownToConfidenceWeightedTarget(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-moderate-downsize",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-moderate-downsize", Outcome: "Yes", Size: 40},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-moderate-downsize",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-moderate-downsize",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-moderate-downsize",
			"estimated_probability": 0.80,
			"confidence":            0.60,
			"aum":                   1000.0,
			"remaining_capacity":    10.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(confidence-weighted downsize) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one SELL order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Sell {
		t.Fatalf("expected SELL on yes-token, got %+v", order)
	}
	if order.Size != 15 {
		t.Fatalf("expected sell order size 15 shares, got %v", order.Size)
	}
	if got, _ := result["target_position"].(float64); got != 25 {
		t.Fatalf("expected target_position 25, got %v", result["target_position"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionHoldsTinyDownsizeBelowVenueMinimum(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-small-downsize",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.62","0.38"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-small-downsize", Outcome: "Yes", Size: 51.14},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.41",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.39",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-small-downsize",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-small-downsize",
			"estimated_probability": 0.80,
			"confidence":            0.90,
			"aum":                   1000.0,
			"remaining_capacity":    999.0,
			"question":              "Will test happen?",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(small downsize below min) returned error: %v", err)
	}

	if len(client.placedOrders) != 0 {
		t.Fatalf("expected no SELL order below venue minimum, got %#v", client.placedOrders)
	}
	if got, _ := result["status"].(string); got != "held" {
		t.Fatalf("expected status held, got %q", got)
	}
	if blocked, _ := result["min_order_blocked"].(bool); !blocked {
		t.Fatalf("expected min_order_blocked true, got %#v", result["min_order_blocked"])
	}
	if got, _ := result["min_order_blocked_shares"].(float64); got != 1.14 {
		t.Fatalf("expected min_order_blocked_shares 1.14, got %v", result["min_order_blocked_shares"])
	}
	if got, _ := result["target_position"].(float64); got != 50 {
		t.Fatalf("expected target_position 50, got %v", result["target_position"])
	}
	if action := fmt.Sprint(result["action_taken"]); !strings.Contains(action, "venue minimum order size") {
		t.Fatalf("expected venue minimum explanation, got %q", action)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionUsesRecentThesisToAccelerateExit(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	ctx := context.Background()

	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ConditionID:     "cond-thesis-drift",
				Question:        "Will test happen?",
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.68","0.32"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Active:          true,
				AcceptingOrders: true,
			},
		},
		positions: []polymarket.Position{
			{Asset: "yes-token", ConditionID: "cond-thesis-drift", Outcome: "Yes", Size: 20},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.68",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.66",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.35",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.33",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "ord-thesis-drift",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	prevProb := 0.88
	prevConfidence := 0.92
	if _, err := data.AddMarketNoteWithMetadata(ctx, db, "company-1", "agent-1", "cond-thesis-drift", "Recent thesis", &data.MarketNoteMetadata{
		Kind:                 "builtin_polymarket_manage_position",
		Side:                 "yes",
		Question:             "Will test happen?",
		Reasoning:            "Previous research was much stronger.",
		EstimatedProbability: &prevProb,
		Confidence:           &prevConfidence,
		CapturedAt:           time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddMarketNoteWithMetadata failed: %v", err)
	}

	result, err := executeBuiltinPipelineMethod(ctx, pe, &data.PipelineRun{
		ID:             "run-thesis-drift",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-thesis-drift",
			"estimated_probability": 0.68,
			"confidence":            0.55,
			"aum":                   1000.0,
			"remaining_capacity":    30.0,
			"question":              "Will test happen?",
			"reasoning":             "The thesis weakened materially.",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(thesis drift exit) returned error: %v", err)
	}

	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one SELL order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "yes-token" || order.Side != polymarket.Sell {
		t.Fatalf("expected SELL on yes-token, got %+v", order)
	}
	if order.Size != 12.5 {
		t.Fatalf("expected sell size 12.5 shares, got %v", order.Size)
	}
	if got, _ := result["target_position"].(float64); got != 7.5 {
		t.Fatalf("expected thesis-adjusted target_position 7.5, got %v", result["target_position"])
	}
	if got, _ := result["thesis_drift_score"].(float64); got != 1 {
		t.Fatalf("expected thesis_drift_score 1, got %v", result["thesis_drift_score"])
	}
	if got, _ := result["thesis_blocks_adds"].(bool); !got {
		t.Fatalf("expected thesis_blocks_adds true, got %#v", result["thesis_blocks_adds"])
	}
	if got := fmt.Sprint(result["thesis_reason"]); got != "recent thesis weakened materially" {
		t.Fatalf("expected thesis_reason %q, got %q", "recent thesis weakened materially", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionReducesWrongSideExposure(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		positions: []polymarket.Position{
			{Asset: "no-token", ConditionID: "cond-1", Outcome: "No", Size: 5},
		},
		orders: []polymarket.Order{
			{ID: "buy-no-1", AssetID: "no-token", Market: "cond-1", Side: "BUY", OriginalSize: "2", SizeMatched: "0"},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.62",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.60",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.24",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.22",
		},
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success: true,
			OrderID: "sell-no-1",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), nil, &data.PipelineRun{
		ID:             "run-3",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-1",
			"estimated_probability": 0.80,
			"confidence":            0.40,
			"remaining_capacity":    10.0,
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(manage_position sell) returned error: %v", err)
	}

	if len(client.cancelledOrderIDs) != 1 || client.cancelledOrderIDs[0] != "buy-no-1" {
		t.Fatalf("expected conflicting no BUY order to be cancelled, got %#v", client.cancelledOrderIDs)
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one SELL order, got %d", len(client.placedOrders))
	}
	order := client.placedOrders[0]
	if order.TokenID != "no-token" || order.Side != polymarket.Sell {
		t.Fatalf("expected SELL on no-token, got %+v", order)
	}
	if order.Price != 0.22 || order.Size != 5 {
		t.Fatalf("unexpected sell order price/size: %+v", order)
	}
	if got, _ := result["status"].(string); got != "updated" {
		t.Fatalf("expected status updated, got %q", got)
	}
	if got, _ := result["orders_cancelled"].(int); got != 1 {
		t.Fatalf("expected orders_cancelled 1, got %v", result["orders_cancelled"])
	}
	if got, _ := result["sell_orders_placed"].(int); got != 1 {
		t.Fatalf("expected sell_orders_placed 1, got %v", result["sell_orders_placed"])
	}
	if got, _ := result["buy_orders_placed"].(int); got != 0 {
		t.Fatalf("expected buy_orders_placed 0, got %v", result["buy_orders_placed"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionAddsNoteForNeutralOutcome(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	ctx := context.Background()

	client := &testBuiltinPolymarketClient{
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.79",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.77",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.23",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.21",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(ctx, pe, &data.PipelineRun{
		ID:             "run-neutral",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-neutral",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"remaining_capacity":    10.0,
			"question":              "Will test happen?",
			"reasoning":             "The market is close to fair value.",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(manage_position neutral) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); got != "neutral" {
		t.Fatalf("expected neutral status, got %q", got)
	}
	if strings.TrimSpace(fmt.Sprint(result["market_note_id"])) == "" {
		t.Fatalf("expected market_note_id to be set, got %#v", result["market_note_id"])
	}

	notes, err := data.ListMarketNotes(ctx, db, "company-1", "cond-neutral", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Status: neutral") {
		t.Fatalf("expected note to include neutral status, got %q", notes[0].Content)
	}
	if !strings.Contains(notes[0].Content, "No action. Net edge after spread and slippage is too thin to justify new exposure.") {
		t.Fatalf("expected note to include EV-aware no-action reason, got %q", notes[0].Content)
	}
	metadata := data.ParseMarketNoteMetadata(notes[0])
	if metadata == nil {
		t.Fatal("expected structured note metadata")
	}
	if metadata.EstimatedProbability == nil || *metadata.EstimatedProbability != 0.8 {
		t.Fatalf("expected estimated_probability 0.8, got %#v", metadata.EstimatedProbability)
	}
	if metadata.Confidence == nil || *metadata.Confidence != 0.85 {
		t.Fatalf("expected confidence 0.85, got %#v", metadata.Confidence)
	}
	if metadata.ThesisHash == "" {
		t.Fatal("expected thesis hash in note metadata")
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionSkipsNoteForLowVolumeMarket(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	ctx := context.Background()

	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ID:              "market-low-volume-note",
				ConditionID:     "cond-low-volume-note",
				Question:        "Will test happen?",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.79","0.21"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-token","no-token"]`,
				Volume:          "49999.99",
			},
		},
		prices: map[string]string{
			builtinPolymarketPriceKey("yes-token", "buy"):  "0.79",
			builtinPolymarketPriceKey("yes-token", "sell"): "0.77",
			builtinPolymarketPriceKey("no-token", "buy"):   "0.23",
			builtinPolymarketPriceKey("no-token", "sell"):  "0.21",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(ctx, pe, &data.PipelineRun{
		ID:             "run-low-volume-note",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-low-volume-note",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"remaining_capacity":    10.0,
			"question":              "Will test happen?",
			"reasoning":             "The market is close to fair value.",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(manage_position low volume note) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); got != "neutral" {
		t.Fatalf("expected neutral status, got %q", got)
	}
	if _, ok := result["market_note_id"]; ok {
		t.Fatalf("expected no market_note_id for low-volume market, got %#v", result["market_note_id"])
	}
	if got := fmt.Sprint(result["market_note_skipped"]); got != "low_volume" {
		t.Fatalf("expected market_note_skipped low_volume, got %q", got)
	}

	notes, err := data.ListMarketNotes(ctx, db, "company-1", "cond-low-volume-note", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected 0 notes, got %d", len(notes))
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionAddsNoteForFailure(t *testing.T) {
	db := setupManagerTestDB(t)
	pe := &PipelineEngine{db: db}
	ctx := context.Background()

	client := &testBuiltinPolymarketClient{
		getPositionsErr: fmt.Errorf("positions unavailable"),
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(ctx, pe, &data.PipelineRun{
		ID:             "run-failed",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-failed",
			"estimated_probability": 0.80,
			"confidence":            0.85,
			"remaining_capacity":    10.0,
			"question":              "Will failure be noted?",
			"reasoning":             "State loading failed.",
			"tokens": []map[string]any{
				{"outcome": "Yes", "token_id": "yes-token"},
				{"outcome": "No", "token_id": "no-token"},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(manage_position failure) returned error: %v", err)
	}

	if got := fmt.Sprint(result["status"]); !strings.HasPrefix(got, "FAILED: failed to list polymarket positions: positions unavailable") {
		t.Fatalf("expected failed status, got %q", got)
	}
	if strings.TrimSpace(fmt.Sprint(result["market_note_id"])) == "" {
		t.Fatalf("expected market_note_id to be set, got %#v", result["market_note_id"])
	}

	notes, err := data.ListMarketNotes(ctx, db, "company-1", "cond-failed", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Status: FAILED: failed to list polymarket positions: positions unavailable") {
		t.Fatalf("expected note to include failure status, got %q", notes[0].Content)
	}
	if !strings.Contains(notes[0].Content, "Error: failed to list polymarket positions: positions unavailable") {
		t.Fatalf("expected note to include failure error, got %q", notes[0].Content)
	}
}

func TestDeriveBuiltinPolymarketCapacityTreatsAUMAsShareCap(t *testing.T) {
	maxAllowed, remainingCapacity := deriveBuiltinPolymarketCapacity(builtinPolymarketPayload{
		AUM: 1000,
	}, 10)

	if maxAllowed != 50 {
		t.Fatalf("maxAllowed = %v, want 50 shares", maxAllowed)
	}
	if remainingCapacity != 40 {
		t.Fatalf("remainingCapacity = %v, want 40 shares", remainingCapacity)
	}
}

func TestDeriveBuiltinPolymarketCapacityClampsInflatedRemainingCapacity(t *testing.T) {
	maxAllowed, remainingCapacity := deriveBuiltinPolymarketCapacity(builtinPolymarketPayload{
		AUM:               1000,
		RemainingCapacity: 720.5,
	}, 10)

	if maxAllowed != 50 {
		t.Fatalf("maxAllowed = %v, want 50 shares", maxAllowed)
	}
	if remainingCapacity != 40 {
		t.Fatalf("remainingCapacity = %v, want 40 shares after clamp", remainingCapacity)
	}
}

func TestDeriveBuiltinPolymarketCapacityExpandsStaleRemainingCapacityFromAUM(t *testing.T) {
	maxAllowed, remainingCapacity := deriveBuiltinPolymarketCapacity(builtinPolymarketPayload{
		AUM:               1000,
		RemainingCapacity: 10,
	}, 10)

	if maxAllowed != 50 {
		t.Fatalf("maxAllowed = %v, want 50 shares", maxAllowed)
	}
	if remainingCapacity != 40 {
		t.Fatalf("remainingCapacity = %v, want 40 shares after AUM refresh", remainingCapacity)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketManagePositionLegacyTradeRejectsFailedOrder(t *testing.T) {
	client := &testBuiltinPolymarketClient{
		placeOrderResp: &polymarket.PlaceOrderResponse{
			Success:  false,
			ErrorMsg: "not enough balance",
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	_, err := executeBuiltinPipelineMethod(context.Background(), nil, nil, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_manage_position",
	}, map[string]any{
		"company_id": "company-1",
		"token_id":   "token-1",
		"price":      0.42,
		"size":       5,
		"side":       "SELL",
	})
	if err == nil || !strings.Contains(err.Error(), "order rejected") {
		t.Fatalf("expected rejected order error, got %v", err)
	}
}
