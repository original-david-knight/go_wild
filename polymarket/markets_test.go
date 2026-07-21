package gowild_polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClient_GetMarketUsesConditionIDsAndReturnsExactMatch(t *testing.T) {
	const wantedConditionID = "0xb48621f7eba07b0a3eeabc6afb09ae42490239903997b9d412b0f69aeb040c8b"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/markets" {
			t.Fatalf("expected path /markets, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("condition_ids"); got != wantedConditionID {
			t.Fatalf("expected condition_ids=%q, got %q", wantedConditionID, got)
		}
		if got := r.URL.Query().Get("condition_id"); got != "" {
			t.Fatalf("did not expect legacy condition_id query param, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":          "wrong-market",
				"conditionId": "0xdeadbeef",
				"question":    "Wrong market",
				"endDateIso":  "2021-12-04",
			},
			{
				"id":              "531202",
				"conditionId":     wantedConditionID,
				"question":        "BitBoy convicted?",
				"slug":            "bitboy-convicted",
				"endDate":         "2026-03-31T12:00:00Z",
				"endDateIso":      "2026-03-31",
				"active":          true,
				"acceptingOrders": true,
			},
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	c := &Client{
		publicClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
	}

	market, err := c.GetMarket(context.Background(), wantedConditionID)
	if err != nil {
		t.Fatalf("GetMarket returned error: %v", err)
	}
	if market == nil {
		t.Fatal("expected market, got nil")
	}
	if market.ConditionID != wantedConditionID {
		t.Fatalf("expected condition ID %q, got %q", wantedConditionID, market.ConditionID)
	}
	if market.Question != "BitBoy convicted?" {
		t.Fatalf("expected BitBoy market, got %q", market.Question)
	}
	if market.EndDate != "2026-03-31" {
		t.Fatalf("expected 2026 end date, got %q", market.EndDate)
	}
}

func TestClient_ListEventsUsesActiveOpenFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/events" {
			t.Fatalf("expected path /events, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Fatalf("expected limit=2, got %q", got)
		}
		if got := r.URL.Query().Get("offset"); got != "4" {
			t.Fatalf("expected offset=4, got %q", got)
		}
		if got := r.URL.Query().Get("active"); got != "true" {
			t.Fatalf("expected active=true, got %q", got)
		}
		if got := r.URL.Query().Get("closed"); got != "false" {
			t.Fatalf("expected closed=false, got %q", got)
		}
		if got := r.URL.Query().Get("order"); got != "id" {
			t.Fatalf("expected order=id, got %q", got)
		}
		if got := r.URL.Query().Get("ascending"); got != "false" {
			t.Fatalf("expected ascending=false, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":    "evt-1",
				"title": "Serie A Winner",
				"slug":  "serie-a-winner",
				"tags": []map[string]any{
					{"id": "1", "label": "Sports", "slug": "sports"},
					{"id": "2", "label": "Soccer", "slug": "soccer"},
				},
				"markets": []map[string]any{
					{
						"id":              "m-1",
						"conditionId":     "cond-1",
						"createdAt":       "2026-03-10T12:00:00Z",
						"startDateIso":    "2026-03-10T12:00:00Z",
						"question":        "Will Inter win Serie A?",
						"slug":            "inter-serie-a",
						"active":          true,
						"closed":          false,
						"acceptingOrders": true,
					},
				},
			},
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	c := &Client{
		publicClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
	}

	events, err := c.ListEvents(context.Background(), 2, 4)
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Title != "Serie A Winner" {
		t.Fatalf("unexpected event title %q", events[0].Title)
	}
	if len(events[0].Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(events[0].Tags))
	}
	if len(events[0].Markets) != 1 || events[0].Markets[0].ConditionID != "cond-1" {
		t.Fatalf("unexpected nested market payload %#v", events[0].Markets)
	}
	if got := events[0].Markets[0].CreatedAt; got != "2026-03-10T12:00:00Z" {
		t.Fatalf("expected nested createdAt to decode, got %q", got)
	}
	if got := events[0].Markets[0].StartDateISO; got != "2026-03-10T12:00:00Z" {
		t.Fatalf("expected nested startDateIso to decode, got %q", got)
	}
}

func TestClient_ListMarketsUsesNewestFirstOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/markets" {
			t.Fatalf("expected path /markets, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "3" {
			t.Fatalf("expected limit=3, got %q", got)
		}
		if got := r.URL.Query().Get("offset"); got != "7" {
			t.Fatalf("expected offset=7, got %q", got)
		}
		if got := r.URL.Query().Get("active"); got != "true" {
			t.Fatalf("expected active=true, got %q", got)
		}
		if got := r.URL.Query().Get("closed"); got != "false" {
			t.Fatalf("expected closed=false, got %q", got)
		}
		if got := r.URL.Query().Get("order"); got != "id" {
			t.Fatalf("expected order=id, got %q", got)
		}
		if got := r.URL.Query().Get("ascending"); got != "false" {
			t.Fatalf("expected ascending=false, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":              "market-9",
				"conditionId":     "cond-9",
				"createdAt":       "2026-03-11T08:00:00Z",
				"startDateIso":    "2026-03-11T08:00:00Z",
				"question":        "Newest market",
				"active":          true,
				"closed":          false,
				"acceptingOrders": true,
			},
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	c := &Client{
		publicClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
	}

	markets, err := c.ListMarkets(context.Background(), 3, 7)
	if err != nil {
		t.Fatalf("ListMarkets returned error: %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("expected 1 market, got %d", len(markets))
	}
	if got := markets[0].CreatedAt; got != "2026-03-11T08:00:00Z" {
		t.Fatalf("expected createdAt to decode, got %q", got)
	}
	if got := markets[0].StartDateISO; got != "2026-03-11T08:00:00Z" {
		t.Fatalf("expected startDateIso to decode, got %q", got)
	}
}

func TestClient_ListMarketsClosingBetweenSetsWindowAndLiquidityParams(t *testing.T) {
	minClose := time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC)
	maxClose := time.Date(2026, 6, 22, 7, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		checks := map[string]string{
			"active":            "true",
			"closed":            "false",
			"order":             "endDate",
			"ascending":         "true",
			"end_date_min":      minClose.Format(time.RFC3339),
			"end_date_max":      maxClose.Format(time.RFC3339),
			"liquidity_num_min": "5000",
		}
		for k, want := range checks {
			if got := q.Get(k); got != want {
				t.Fatalf("param %s = %q, want %q", k, got, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "1", "conditionId": "0xabc", "question": "Q", "endDate": "2026-06-11T00:00:00Z", "liquidity": "9000", "active": true, "acceptingOrders": true},
		})
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	c := &Client{publicClient: &http.Client{Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport}}}

	markets, err := c.ListMarketsClosingBetween(context.Background(), minClose, maxClose, 5000, 100, 0)
	if err != nil {
		t.Fatalf("ListMarketsClosingBetween: %v", err)
	}
	if len(markets) != 1 || markets[0].ConditionID != "0xabc" {
		t.Fatalf("unexpected markets: %+v", markets)
	}
}

func TestClient_ListMarketsClosingBetweenKeysetUsesCursorAndFilters(t *testing.T) {
	minClose := time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC)
	maxClose := time.Date(2026, 6, 22, 7, 0, 0, 0, time.UTC)
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/markets/keyset" {
			t.Fatalf("expected path /markets/keyset, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		checks := map[string]string{
			"limit":             "100",
			"active":            "true",
			"closed":            "false",
			"order":             "endDate",
			"ascending":         "true",
			"end_date_min":      minClose.Format(time.RFC3339),
			"end_date_max":      maxClose.Format(time.RFC3339),
			"liquidity_num_min": "5000",
		}
		for k, want := range checks {
			if got := q.Get(k); got != want {
				t.Fatalf("request %d param %s = %q, want %q", requests, k, got, want)
			}
		}
		if got := q.Get("offset"); got != "" {
			t.Fatalf("request %d should not send offset, got %q", requests, got)
		}

		w.Header().Set("Content-Type", "application/json")
		switch requests {
		case 1:
			if got := q.Get("after_cursor"); got != "" {
				t.Fatalf("first request after_cursor = %q, want empty", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"markets": []map[string]any{
					{"id": "1", "conditionId": "0xabc", "question": "Q", "endDate": "2026-06-11T00:00:00Z", "liquidity": 9000, "active": true, "acceptingOrders": true},
				},
				"next_cursor": "cursor-2",
			})
		case 2:
			if got := q.Get("after_cursor"); got != "cursor-2" {
				t.Fatalf("second request after_cursor = %q, want cursor-2", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"markets": []map[string]any{
					{"id": "2", "conditionId": "0xdef", "question": "Q2", "endDateIso": "2026-06-12T00:00:00Z", "liquidity": "10000", "active": true, "acceptingOrders": true},
				},
			})
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	c := &Client{publicClient: &http.Client{Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport}}}

	first, err := c.ListMarketsClosingBetweenKeyset(context.Background(), minClose, maxClose, 5000, 200, "")
	if err != nil {
		t.Fatalf("first ListMarketsClosingBetweenKeyset: %v", err)
	}
	if first.NextCursor != "cursor-2" || len(first.Markets) != 1 || first.Markets[0].ConditionID != "0xabc" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	if first.Markets[0].EndDate != "2026-06-11T00:00:00Z" {
		t.Fatalf("expected endDate fallback to decode, got %q", first.Markets[0].EndDate)
	}

	second, err := c.ListMarketsClosingBetweenKeyset(context.Background(), minClose, maxClose, 5000, 100, first.NextCursor)
	if err != nil {
		t.Fatalf("second ListMarketsClosingBetweenKeyset: %v", err)
	}
	if second.NextCursor != "" || len(second.Markets) != 1 || second.Markets[0].ConditionID != "0xdef" {
		t.Fatalf("unexpected second page: %+v", second)
	}
}
