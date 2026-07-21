package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsFiltersCandidates(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	engine := &PipelineEngine{db: db}

	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ID:              "market-eligible",
				ConditionID:     "cond-eligible",
				Question:        "Will the test market pass?",
				Description:     "Eligible candidate",
				Slug:            "eligible-market",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.61","0.39"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-eligible","no-eligible"]`,
				Volume:          "120000",
				Liquidity:       "4500",
				Volume24hr:      900,
				BestBid:         0.60,
				BestAsk:         0.62,
			},
			{
				ID:              "market-position",
				ConditionID:     "cond-position",
				Question:        "Should be skipped by position",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.52","0.48"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-position","no-position"]`,
			},
			{
				ID:              "market-sports",
				ConditionID:     "cond-sports",
				Question:        "Will the Lakers win the NBA Finals?",
				Description:     "Sports market",
				Slug:            "lakers-nba-finals",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.52","0.48"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-sports","no-sports"]`,
				Tags: []polymarket.Tag{
					{Label: "Sports", Slug: "sports"},
					{Label: "NBA", Slug: "nba"},
				},
			},
			{
				ID:              "market-sports-tagless",
				ConditionID:     "cond-sports-tagless",
				Question:        "Will Inter win the 2025-26 Serie A League?",
				Description:     "This is a polymarket on whether the listed club will win the 2025-26 Serie A. This market will resolve to Yes if the listed club is officially crowned the winner.",
				Slug:            "inter-win-the-202526-serie-a-league",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.94","0.06"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-sports-tagless","no-sports-tagless"]`,
			},
			{
				ID:              "market-crypto",
				ConditionID:     "cond-crypto",
				Question:        "What price will Bitcoin hit in March?",
				Description:     "Crypto market",
				Slug:            "bitcoin-price-march",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.52","0.48"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-crypto","no-crypto"]`,
				Tags: []polymarket.Tag{
					{Label: "Crypto", Slug: "crypto"},
					{Label: "Crypto Prices", Slug: "crypto-prices"},
				},
			},
			{
				ID:              "market-stock",
				ConditionID:     "cond-stock",
				Question:        "Will Apple (AAPL) close at $260-265 in 2025?",
				Description:     "Stock price market",
				Slug:            "apple-aapl-close-260-265",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.52","0.48"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-stock","no-stock"]`,
				Tags: []polymarket.Tag{
					{Label: "Stock Prices", Slug: "stock-prices"},
					{Label: "Stocks", Slug: "stocks"},
					{Label: "Equities", Slug: "equities"},
				},
			},
			{
				ID:              "market-order",
				ConditionID:     "cond-order",
				Question:        "Should be skipped by open order",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.44","0.56"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-order","no-order"]`,
			},
			{
				ID:              "market-note",
				ConditionID:     "cond-note",
				Question:        "Should be skipped by recent note",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 2, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.27","0.73"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-note","no-note"]`,
				Volume:          "71000",
			},
			{
				ID:              "market-far",
				ConditionID:     "cond-far",
				Question:        "Should be skipped by far resolution",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 8, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.33","0.67"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-far","no-far"]`,
				Volume:          "82000",
			},
			{
				ID:              "market-old-note",
				ConditionID:     "cond-old-note",
				Question:        "Should be kept despite older note",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 3, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.49","0.51"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-old","no-old"]`,
				Volume:          "56000",
			},
			{
				ID:              "market-note-changed",
				ConditionID:     "cond-note-changed",
				Question:        "Should be reconsidered after market moved",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         time.Now().UTC().AddDate(0, 2, 14).Format(time.RFC3339),
				OutcomePrices:   `["0.61","0.39"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-note-changed","no-note-changed"]`,
				Volume:          "78000",
			},
		},
		positions: []polymarket.Position{
			{ConditionID: "cond-position", Asset: "yes-position", Outcome: "Yes", Size: 3},
		},
		orders: []polymarket.Order{
			{ID: "ord-1", Market: "cond-order", AssetID: "yes-order", Side: "BUY", Status: "live", OriginalSize: "5", SizeMatched: "0"},
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	if _, err := data.AddMarketNote(ctx, db, "company-1", "agent-1", "cond-note", "recent analysis"); err != nil {
		t.Fatalf("AddMarketNote recent failed: %v", err)
	}
	recentProbability := 0.42
	recentVolume := 51000.0
	recentSpread := 0.01
	recentChangedMetadata := &data.MarketNoteMetadata{
		Kind:              "builtin_polymarket_manage_position",
		Question:          "Should be reconsidered after market moved",
		Reasoning:         "Recent note should not block if the market repriced materially.",
		ResolutionDate:    time.Now().UTC().AddDate(0, 2, 14).Format(time.RFC3339),
		MarketFingerprint: builtinPolymarketMarketFingerprint(client.markets[len(client.markets)-1]),
		MarketProbability: &recentProbability,
		MarketVolume:      &recentVolume,
		Spread:            &recentSpread,
		CapturedAt:        time.Now().UTC(),
	}
	if _, err := data.AddMarketNoteWithMetadata(ctx, db, "company-1", "agent-1", "cond-note-changed", "recent structured analysis", recentChangedMetadata); err != nil {
		t.Fatalf("AddMarketNoteWithMetadata changed failed: %v", err)
	}
	oldNote := &data.MarketNote{
		ID:               "note-old",
		CompanyID:        "company-1",
		ConditionID:      "cond-old-note",
		Content:          "stale analysis",
		CreatedByAgentID: "agent-1",
		CreatedAt:        time.Now().UTC().Add(-10 * 24 * time.Hour),
	}
	if err := db.Table(data.MarketNote{}).Insert(ctx, oldNote); err != nil {
		t.Fatalf("insert old market note failed: %v", err)
	}

	result, err := executeBuiltinPipelineMethod(ctx, engine, &data.PipelineRun{
		ID:             "run-find-1",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_find_markets",
	}, nil)
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets) returned error: %v", err)
	}

	markets, ok := result["markets"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any markets, got %T", result["markets"])
	}
	if len(markets) != 4 {
		t.Fatalf("expected 4 eligible markets, got %d (%#v)", len(markets), markets)
	}
	if got, _ := result["markets_found"].(int); got != 4 {
		t.Fatalf("expected markets_found 4, got %v", result["markets_found"])
	}
	if got, _ := result["skipped_existing_shares"].(int); got != 1 {
		t.Fatalf("expected skipped_existing_shares 1, got %v", result["skipped_existing_shares"])
	}
	if got, _ := result["skipped_sports"].(int); got != 2 {
		t.Fatalf("expected skipped_sports 2, got %v", result["skipped_sports"])
	}
	if got, _ := result["skipped_crypto"].(int); got != 1 {
		t.Fatalf("expected skipped_crypto 1, got %v", result["skipped_crypto"])
	}
	if got, _ := result["skipped_stocks"].(int); got != 1 {
		t.Fatalf("expected skipped_stocks 1, got %v", result["skipped_stocks"])
	}
	if got, _ := result["skipped_existing_orders"].(int); got != 1 {
		t.Fatalf("expected skipped_existing_orders 1, got %v", result["skipped_existing_orders"])
	}
	if got, _ := result["skipped_recent_notes"].(int); got != 0 {
		t.Fatalf("expected skipped_recent_notes 0, got %v", result["skipped_recent_notes"])
	}
	if got, _ := result["skipped_far_resolution"].(int); got != 1 {
		t.Fatalf("expected skipped_far_resolution 1, got %v", result["skipped_far_resolution"])
	}
	if got, _ := result["skipped_low_volume"].(int); got != 0 {
		t.Fatalf("expected skipped_low_volume 0, got %v", result["skipped_low_volume"])
	}

	gotConditionIDs := make([]string, 0, len(markets))
	for _, m := range markets {
		gotConditionIDs = append(gotConditionIDs, fmt.Sprint(m["condition_id"]))
	}
	sort.Strings(gotConditionIDs)
	if strings.Join(gotConditionIDs, ",") != "cond-eligible,cond-note,cond-note-changed,cond-old-note" {
		t.Fatalf("unexpected returned condition IDs: %v", gotConditionIDs)
	}
	if got, _ := markets[0]["remaining_capacity"].(float64); got != 50 {
		t.Fatalf("expected remaining_capacity 50, got %v", markets[0]["remaining_capacity"])
	}
	tokens, ok := markets[0]["tokens"].([]map[string]any)
	if !ok || len(tokens) != 2 {
		t.Fatalf("expected parsed tokens on first market, got %#v", markets[0]["tokens"])
	}
	if got, _ := markets[0]["probability"].(float64); got != 0.61 {
		t.Fatalf("expected probability 0.61, got %v", markets[0]["probability"])
	}
	if len(client.gotSearchQueries) != 0 {
		t.Fatalf("expected no seeded SearchMarkets calls, got %#v", client.gotSearchQueries)
	}
	if len(client.gotListOffsets) == 0 || client.gotListOffsets[0] != 0 {
		t.Fatalf("expected ListMarkets pagination from offset 0, got %#v", client.gotListOffsets)
	}
	if got, _ := result["scan_mode"].(string); got != "all_markets" {
		t.Fatalf("expected scan_mode all_markets, got %q", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsAcceptsQueryAndLimit(t *testing.T) {
	now := time.Now().UTC()
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ID:              "market-1",
				ConditionID:     "cond-1",
				CreatedAt:       now.Add(-48 * time.Hour).Format(time.RFC3339),
				StartDateISO:    now.Add(-48 * time.Hour).Format(time.RFC3339),
				Question:        "Will OpenAI have the best model?",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.45","0.55"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-1","no-1"]`,
				Volume:          "90000",
			},
			{
				ID:              "market-2",
				ConditionID:     "cond-2",
				CreatedAt:       now.Add(-24 * time.Hour).Format(time.RFC3339),
				StartDateISO:    now.Add(-24 * time.Hour).Format(time.RFC3339),
				Question:        "Will another model win?",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.35","0.65"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-2","no-2"]`,
				Volume:          "95000",
			},
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), &PipelineEngine{}, &data.PipelineRun{
		ID:             "run-find-2",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "polymarket_find_markets",
	}, map[string]any{
		"query": "openai",
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets query) returned error: %v", err)
	}

	if len(client.gotSearchQueries) != 1 || client.gotSearchQueries[0] != "openai" {
		t.Fatalf("expected search query openai, got %#v", client.gotSearchQueries)
	}
	if got, _ := result["query"].(string); got != "openai" {
		t.Fatalf("expected result query openai, got %q", got)
	}
	if got, _ := result["markets_found"].(int); got != 1 {
		t.Fatalf("expected markets_found 1, got %v", result["markets_found"])
	}
	markets, ok := result["markets"].([]map[string]any)
	if !ok || len(markets) != 1 {
		t.Fatalf("expected one market, got %#v", result["markets"])
	}
	if got := fmt.Sprint(markets[0]["condition_id"]); got != "cond-2" {
		t.Fatalf("expected newest market cond-2 first, got %q", got)
	}
	if len(client.gotListOffsets) != 0 {
		t.Fatalf("expected query mode to avoid ListMarkets, got offsets %#v", client.gotListOffsets)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsSkipsLowVolumeMarkets(t *testing.T) {
	now := time.Now().UTC()
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ID:              "market-low-volume",
				ConditionID:     "cond-low-volume",
				CreatedAt:       now.Add(-2 * time.Hour).Format(time.RFC3339),
				StartDateISO:    now.Add(-2 * time.Hour).Format(time.RFC3339),
				Question:        "Newest but too small",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.55","0.45"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-low","no-low"]`,
				Volume:          "49999.99",
			},
			{
				ID:              "market-high-volume",
				ConditionID:     "cond-high-volume",
				CreatedAt:       now.Add(-4 * time.Hour).Format(time.RFC3339),
				StartDateISO:    now.Add(-4 * time.Hour).Format(time.RFC3339),
				Question:        "Older but liquid enough",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.42","0.58"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-high","no-high"]`,
				Volume:          "50000",
			},
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), &PipelineEngine{}, &data.PipelineRun{
		ID:             "run-find-low-volume",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "polymarket_find_markets",
	}, map[string]any{
		"query": "market",
		"limit": 2,
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets low volume) returned error: %v", err)
	}

	if got, _ := result["markets_found"].(int); got != 1 {
		t.Fatalf("expected markets_found 1, got %v", result["markets_found"])
	}
	if got, _ := result["skipped_low_volume"].(int); got != 1 {
		t.Fatalf("expected skipped_low_volume 1, got %v", result["skipped_low_volume"])
	}
	markets, ok := result["markets"].([]map[string]any)
	if !ok || len(markets) != 1 {
		t.Fatalf("expected one market, got %#v", result["markets"])
	}
	if got := fmt.Sprint(markets[0]["condition_id"]); got != "cond-high-volume" {
		t.Fatalf("expected high-volume market to remain, got %q", got)
	}
	if got, _ := result["min_volume"].(float64); got != builtinPolymarketFindMarketsMinVolume {
		t.Fatalf("expected min_volume %.2f, got %v", builtinPolymarketFindMarketsMinVolume, result["min_volume"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsListsNewestFirst(t *testing.T) {
	now := time.Now().UTC()
	client := &testBuiltinPolymarketClient{
		markets: []polymarket.Market{
			{
				ID:              "market-older",
				ConditionID:     "cond-older",
				CreatedAt:       now.Add(-72 * time.Hour).Format(time.RFC3339),
				StartDateISO:    now.Add(-72 * time.Hour).Format(time.RFC3339),
				Question:        "Older market",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.45","0.55"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-older","no-older"]`,
				Volume:          "88000",
			},
			{
				ID:              "market-newer",
				ConditionID:     "cond-newer",
				CreatedAt:       now.Add(-12 * time.Hour).Format(time.RFC3339),
				StartDateISO:    now.Add(-12 * time.Hour).Format(time.RFC3339),
				Question:        "Newer market",
				Active:          true,
				AcceptingOrders: true,
				EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
				OutcomePrices:   `["0.35","0.65"]`,
				Outcomes:        `["Yes","No"]`,
				ClobTokenIDs:    `["yes-newer","no-newer"]`,
				Volume:          "91000",
			},
		},
	}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(context.Background(), &PipelineEngine{}, &data.PipelineRun{
		ID:             "run-find-newest-list",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "polymarket_find_markets",
	}, map[string]any{
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets newest list) returned error: %v", err)
	}

	if len(client.gotSearchQueries) != 0 {
		t.Fatalf("expected list mode without SearchMarkets calls, got %#v", client.gotSearchQueries)
	}
	if want := []int{0}; fmt.Sprint(client.gotListOffsets) != fmt.Sprint(want) {
		t.Fatalf("expected ListMarkets offsets %v, got %v", want, client.gotListOffsets)
	}
	markets, ok := result["markets"].([]map[string]any)
	if !ok || len(markets) != 1 {
		t.Fatalf("expected one market, got %#v", result["markets"])
	}
	if got := fmt.Sprint(markets[0]["condition_id"]); got != "cond-newer" {
		t.Fatalf("expected newest listed market cond-newer first, got %q", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsScansUntilEligibleOrExhausted(t *testing.T) {
	ctx := context.Background()
	engine := &PipelineEngine{}
	ineligibleMarkets := make([]polymarket.Market, 0, builtinPolymarketFindMarketsListPageSize+1)
	for i := 0; i < builtinPolymarketFindMarketsListPageSize; i++ {
		ineligibleMarkets = append(ineligibleMarkets, polymarket.Market{
			ID:              fmt.Sprintf("sports-%d", i),
			ConditionID:     fmt.Sprintf("cond-sports-%d", i),
			Question:        "Will the Lakers win tonight?",
			EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
			Active:          true,
			AcceptingOrders: true,
			Tags: []polymarket.Tag{
				{Label: "Sports", Slug: "sports"},
			},
		})
	}
	ineligibleMarkets = append(ineligibleMarkets, polymarket.Market{
		ID:              "eligible-1",
		ConditionID:     "cond-eligible-1",
		Question:        "Will OpenAI ship feature X?",
		EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
		Active:          true,
		AcceptingOrders: true,
		OutcomePrices:   `["0.45","0.55"]`,
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["yes-1","no-1"]`,
		Volume:          "76000",
	})
	client := &testBuiltinPolymarketClient{markets: ineligibleMarkets}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(ctx, engine, &data.PipelineRun{
		ID:             "run-find-3",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_find_markets",
	}, map[string]any{
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets all scan) returned error: %v", err)
	}

	if want := []int{0, builtinPolymarketFindMarketsListPageSize}; fmt.Sprint(client.gotListOffsets) != fmt.Sprint(want) {
		t.Fatalf("expected ListMarkets offsets %v, got %v", want, client.gotListOffsets)
	}
	if got, _ := result["markets_found"].(int); got != 1 {
		t.Fatalf("expected markets_found 1, got %v", result["markets_found"])
	}
	if got, _ := result["candidates_examined"].(int); got != builtinPolymarketFindMarketsListPageSize+1 {
		t.Fatalf("expected candidates_examined %d, got %v", builtinPolymarketFindMarketsListPageSize+1, result["candidates_examined"])
	}
	if got, _ := result["pages_scanned"].(int); got != 2 {
		t.Fatalf("expected pages_scanned 2, got %v", result["pages_scanned"])
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsUsesEventTagCache(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	engine := &PipelineEngine{db: db}

	client := &testBuiltinPolymarketClient{
		events: []polymarket.Event{
			{
				ID:    "evt-sports",
				Title: "Serie A League Winner",
				Slug:  "serie-a-league-winner",
				Tags: []polymarket.Tag{
					{Label: "Sports", Slug: "sports"},
					{Label: "Soccer", Slug: "soccer"},
					{Label: "Serie A", Slug: "serie-a"},
				},
				Markets: []polymarket.Market{
					{
						ID:              "m-sports",
						ConditionID:     "cond-sports-cached",
						Question:        "Will Inter win the 2025-26 Serie A League?",
						Description:     "This is a polymarket on whether the listed club will win the 2025-26 Serie A.",
						Slug:            "inter-win-the-202526-serie-a-league",
						Active:          true,
						AcceptingOrders: true,
						EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
						OutcomePrices:   `["0.94","0.06"]`,
						Outcomes:        `["Yes","No"]`,
						ClobTokenIDs:    `["yes-sports-cached","no-sports-cached"]`,
						Volume:          "250000",
					},
				},
			},
			{
				ID:    "evt-eligible",
				Title: "OpenAI hardware product",
				Slug:  "openai-hardware-product",
				Tags: []polymarket.Tag{
					{Label: "Technology", Slug: "technology"},
				},
				Markets: []polymarket.Market{
					{
						ID:              "m-eligible",
						ConditionID:     "cond-eligible-cached",
						Question:        "Will OpenAI launch a new consumer hardware product by March 31, 2026?",
						Description:     "Eligible cached market",
						Slug:            "openai-hardware-product-2026",
						Active:          true,
						AcceptingOrders: true,
						EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
						OutcomePrices:   `["0.25","0.75"]`,
						Outcomes:        `["Yes","No"]`,
						ClobTokenIDs:    `["yes-eligible-cached","no-eligible-cached"]`,
						Volume:          "150000",
					},
				},
			},
		},
	}
	installTestBuiltinPolymarketClient(t, client)
	installTestBuiltinPolymarketUSDCBalance(t, 1000, nil)

	result, err := executeBuiltinPipelineMethod(ctx, engine, &data.PipelineRun{
		ID:             "run-find-cache",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_find_markets",
	}, map[string]any{
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets cached tags) returned error: %v", err)
	}

	if want := []int{0}; fmt.Sprint(client.gotEventOffsets) != fmt.Sprint(want) {
		t.Fatalf("expected ListEvents offsets %v, got %v", want, client.gotEventOffsets)
	}
	if len(client.gotListOffsets) != 0 {
		t.Fatalf("expected live ListMarkets path to be skipped, got offsets %#v", client.gotListOffsets)
	}
	if got, _ := result["cache_used"].(bool); !got {
		t.Fatalf("expected cache_used true, got %v", result["cache_used"])
	}
	if got, _ := result["scan_mode"].(string); got != "cached_events" {
		t.Fatalf("expected scan_mode cached_events, got %q", got)
	}
	if got, _ := result["skipped_sports"].(int); got != 1 {
		t.Fatalf("expected skipped_sports 1, got %v", result["skipped_sports"])
	}
	markets, ok := result["markets"].([]map[string]any)
	if !ok || len(markets) != 1 {
		t.Fatalf("expected one market, got %#v", result["markets"])
	}
	if got := fmt.Sprint(markets[0]["condition_id"]); got != "cond-eligible-cached" {
		t.Fatalf("expected cached eligible market, got %q", got)
	}
}

func TestExecuteBuiltinPipelineMethodPolymarketFindMarketsCachePrefersNewestMarkets(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	engine := &PipelineEngine{db: db}
	now := time.Now().UTC()

	older := &polymarketCachedMarket{
		ID:              "cond-cache-older",
		MarketID:        "market-cache-older",
		EventID:         "evt-cache",
		EventTitle:      "Cached event",
		EventSlug:       "cached-event",
		Question:        "Older cached market",
		Description:     "Older cached metadata",
		CreatedAt:       now.Add(-72 * time.Hour).Format(time.RFC3339),
		StartDateISO:    now.Add(-72 * time.Hour).Format(time.RFC3339),
		Slug:            "older-cached-market",
		EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
		Volume:          "85000",
		Liquidity:       "3000",
		OutcomePrices:   `["0.60","0.40"]`,
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["yes-cache-older","no-cache-older"]`,
		Active:          true,
		Closed:          false,
		AcceptingOrders: true,
		Volume24hr:      900,
		SearchText:      builtinPolymarketNormalizeSearchText("Older cached market"),
		SyncedAt:        now,
	}
	newer := &polymarketCachedMarket{
		ID:              "cond-cache-newer",
		MarketID:        "market-cache-newer",
		EventID:         "evt-cache",
		EventTitle:      "Cached event",
		EventSlug:       "cached-event",
		Question:        "Newer cached market",
		Description:     "Newer cached metadata",
		CreatedAt:       now.Add(-6 * time.Hour).Format(time.RFC3339),
		StartDateISO:    now.Add(-6 * time.Hour).Format(time.RFC3339),
		Slug:            "newer-cached-market",
		EndDate:         now.AddDate(0, 1, 0).Format(time.RFC3339),
		Volume:          "55000",
		Liquidity:       "50",
		OutcomePrices:   `["0.40","0.60"]`,
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["yes-cache-newer","no-cache-newer"]`,
		Active:          true,
		Closed:          false,
		AcceptingOrders: true,
		Volume24hr:      5,
		SearchText:      builtinPolymarketNormalizeSearchText("Newer cached market"),
		SyncedAt:        now,
	}
	if err := db.Table(polymarketCachedMarket{}).Insert(ctx, older); err != nil {
		t.Fatalf("insert older cached market failed: %v", err)
	}
	if err := db.Table(polymarketCachedMarket{}).Insert(ctx, newer); err != nil {
		t.Fatalf("insert newer cached market failed: %v", err)
	}
	if err := SetSetting(ctx, db, polymarketMarketCacheLastSyncSetting, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("set cache last sync failed: %v", err)
	}

	client := &testBuiltinPolymarketClient{}
	installTestBuiltinPolymarketClient(t, client)

	result, err := executeBuiltinPipelineMethod(ctx, engine, &data.PipelineRun{
		ID:             "run-find-cache-newest",
		ScopeMode:      "company",
		ScopeCompanyID: "company-1",
	}, PipelineStep{
		Runner:     pipelineStepRunnerBuiltin,
		NextMethod: "builtin_polymarket_find_markets",
	}, map[string]any{
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("executeBuiltinPipelineMethod(find_markets cache newest) returned error: %v", err)
	}

	if len(client.gotEventOffsets) != 0 {
		t.Fatalf("expected fresh cache to avoid ListEvents sync, got %#v", client.gotEventOffsets)
	}
	if got, _ := result["cache_used"].(bool); !got {
		t.Fatalf("expected cache_used true, got %v", result["cache_used"])
	}
	markets, ok := result["markets"].([]map[string]any)
	if !ok || len(markets) != 1 {
		t.Fatalf("expected one market, got %#v", result["markets"])
	}
	if got := fmt.Sprint(markets[0]["condition_id"]); got != "cond-cache-newer" {
		t.Fatalf("expected newest cached market cond-cache-newer first, got %q", got)
	}
}
