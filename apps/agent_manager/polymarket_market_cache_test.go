package main

import (
	"context"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestMaybeSyncPolymarketMarketCacheHandlesDuplicateConditionIDs(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)

	client := &testBuiltinPolymarketClient{
		events: []polymarket.Event{
			{
				ID:    "evt-1",
				Title: "First event",
				Slug:  "first-event",
				Tags: []polymarket.Tag{
					{ID: "tag-1", Label: "Politics", Slug: "politics"},
				},
				Markets: []polymarket.Market{
					{
						ID:              "market-1",
						ConditionID:     "cond-dup",
						Question:        "Will the duplicate market sync cleanly?",
						Description:     "First version",
						Slug:            "duplicate-market",
						EndDate:         time.Now().UTC().AddDate(0, 1, 0).Format(time.RFC3339),
						Volume:          "10",
						Liquidity:       "5",
						OutcomePrices:   `["0.51","0.49"]`,
						Outcomes:        `["Yes","No"]`,
						ClobTokenIDs:    `["yes-dup","no-dup"]`,
						Active:          true,
						Closed:          false,
						AcceptingOrders: true,
					},
				},
			},
			{
				ID:    "evt-2",
				Title: "Second event",
				Slug:  "second-event",
				Tags: []polymarket.Tag{
					{ID: "tag-2", Label: "Technology", Slug: "technology"},
				},
				Markets: []polymarket.Market{
					{
						ID:              "market-2",
						ConditionID:     "cond-dup",
						Question:        "Will the duplicate market sync cleanly? updated",
						Description:     "Second version",
						Slug:            "duplicate-market-updated",
						EndDate:         time.Now().UTC().AddDate(0, 2, 0).Format(time.RFC3339),
						Volume:          "20",
						Liquidity:       "15",
						OutcomePrices:   `["0.52","0.48"]`,
						Outcomes:        `["Yes","No"]`,
						ClobTokenIDs:    `["yes-dup","no-dup"]`,
						Active:          true,
						Closed:          false,
						AcceptingOrders: true,
					},
				},
			},
		},
	}

	syncPerformed, err := maybeSyncPolymarketMarketCache(ctx, db, client)
	if err != nil {
		t.Fatalf("maybeSyncPolymarketMarketCache returned error: %v", err)
	}
	if !syncPerformed {
		t.Fatalf("expected syncPerformed true")
	}

	rows, err := db.Table(polymarketCachedMarket{}).GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll(polymarketCachedMarket) returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 cached market row, got %d", len(rows))
	}

	item, ok := rows[0].(*polymarketCachedMarket)
	if !ok {
		t.Fatalf("expected polymarketCachedMarket row, got %T", rows[0])
	}
	if item.ID != "cond-dup" {
		t.Fatalf("expected cached condition_id cond-dup, got %q", item.ID)
	}
	if item.Question != "Will the duplicate market sync cleanly? updated" {
		t.Fatalf("expected later duplicate to win update, got %q", item.Question)
	}
	if item.EventID != "evt-2" {
		t.Fatalf("expected updated event metadata from second event, got %q", item.EventID)
	}

	lastError, err := GetSetting(ctx, db, polymarketMarketCacheLastErrorSetting)
	if err != nil {
		t.Fatalf("GetSetting(last sync error) returned error: %v", err)
	}
	if lastError != "" {
		t.Fatalf("expected empty cache sync error, got %q", lastError)
	}
}
