package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

type testCompanyPolymarketClient struct {
	positions    []polymarket.Position
	orders       []polymarket.Order
	markets      map[string]*polymarket.Market
	ordersByID   map[string]*polymarket.Order
	orderBooks   map[string]*polymarket.OrderBook
	placeResp    *polymarket.PlaceOrderResponse
	placeErr     error
	cancelErr    error
	positionsErr error
	ordersErr    error
	marketErr    error
	orderErr     error
	orderBookErr error

	placedOrders []struct {
		TokenID   string
		Price     float64
		Size      float64
		Side      string
		OrderType string
		NegRisk   bool
	}
	cancelledOrderIDs []string
}

func (c *testCompanyPolymarketClient) GetPositions(context.Context) ([]polymarket.Position, error) {
	return append([]polymarket.Position(nil), c.positions...), c.positionsErr
}

func (c *testCompanyPolymarketClient) GetOrders(context.Context, string) ([]polymarket.Order, error) {
	return append([]polymarket.Order(nil), c.orders...), c.ordersErr
}

func (c *testCompanyPolymarketClient) GetMarket(_ context.Context, conditionID string) (*polymarket.Market, error) {
	if c.marketErr != nil {
		return nil, c.marketErr
	}
	if market, ok := c.markets[conditionID]; ok {
		return market, nil
	}
	return nil, nil
}

func (c *testCompanyPolymarketClient) GetOrder(_ context.Context, orderID string) (*polymarket.Order, error) {
	if c.orderErr != nil {
		return nil, c.orderErr
	}
	if order, ok := c.ordersByID[orderID]; ok {
		return order, nil
	}
	return nil, nil
}

func (c *testCompanyPolymarketClient) GetOrderBook(_ context.Context, tokenID string) (*polymarket.OrderBook, error) {
	if c.orderBookErr != nil {
		return nil, c.orderBookErr
	}
	if book, ok := c.orderBooks[tokenID]; ok {
		return book, nil
	}
	return nil, nil
}

func (c *testCompanyPolymarketClient) PlaceOrder(_ context.Context, tokenID string, price, size float64, side, orderType string, negRisk bool) (*polymarket.PlaceOrderResponse, error) {
	c.placedOrders = append(c.placedOrders, struct {
		TokenID   string
		Price     float64
		Size      float64
		Side      string
		OrderType string
		NegRisk   bool
	}{
		TokenID:   tokenID,
		Price:     price,
		Size:      size,
		Side:      side,
		OrderType: orderType,
		NegRisk:   negRisk,
	})
	if c.placeResp != nil || c.placeErr != nil {
		return c.placeResp, c.placeErr
	}
	return &polymarket.PlaceOrderResponse{Success: true, OrderID: "placed-order-1"}, nil
}

func (c *testCompanyPolymarketClient) CancelOrder(_ context.Context, orderID string) error {
	c.cancelledOrderIDs = append(c.cancelledOrderIDs, orderID)
	return c.cancelErr
}

func TestHandleCompanyPolymarketPortfolioSnapshot(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	h := NewHandlers(service, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := service.CreateCompany(ctx, "Portfolio Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if _, err := data.AddMarketNote(ctx, db, company.ID, "agent-1", "cond-1", "Already reduced once."); err != nil {
		t.Fatalf("AddMarketNote failed: %v", err)
	}

	client := &testCompanyPolymarketClient{
		positions: []polymarket.Position{
			{
				Asset:        "token-yes",
				ConditionID:  "cond-1",
				Title:        "Will it rain?",
				Outcome:      "YES",
				Size:         40,
				AvgPrice:     0.42,
				CurPrice:     0.55,
				InitialValue: 16.8,
				CurrentValue: 22,
				CashPnl:      5.2,
				PercentPnl:   0.3095,
				RealizedPnl:  0,
				TotalBought:  40,
			},
		},
		orders: []polymarket.Order{
			{
				ID:           "order-open-1",
				Market:       "cond-1",
				AssetID:      "token-yes",
				Side:         polymarket.Sell,
				OriginalSize: "10",
				SizeMatched:  "2",
				Price:        "0.60",
				Status:       "live",
				Outcome:      "YES",
				Type:         polymarket.GTC,
			},
			{
				ID:           "order-filled-1",
				Market:       "cond-1",
				AssetID:      "token-yes",
				Side:         polymarket.Sell,
				OriginalSize: "5",
				SizeMatched:  "5",
				Price:        "0.59",
				Status:       "filled",
				Outcome:      "YES",
				Type:         polymarket.GTC,
			},
		},
		markets: map[string]*polymarket.Market{
			"cond-1": {
				ConditionID:     "cond-1",
				Question:        "Will it rain?",
				Slug:            "will-it-rain",
				Image:           "https://example.com/rain.png",
				Icon:            "https://example.com/rain-icon.png",
				OutcomePrices:   `["0.55","0.45"]`,
				Outcomes:        `["YES","NO"]`,
				ClobTokenIDs:    `["token-yes","token-no"]`,
				BestBid:         0.54,
				BestAsk:         0.56,
				AcceptingOrders: true,
				Active:          true,
			},
		},
	}
	h.companyPolymarketClientFactory = func(context.Context, string) (companyPolymarketClient, error) {
		return client, nil
	}
	h.companyWalletBalancesLoader = func(context.Context, string) (map[string]any, error) {
		return map[string]any{
			"polygon_usdce": map[string]any{"ok": true, "balance": "15.50"},
			"polygon_usdte": map[string]any{"ok": true, "balance": "4.50"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/polymarket/portfolio", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	summary, ok := payload["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary object, got %T", payload["summary"])
	}
	if got := summary["usd_assets"]; got != 20.0 {
		t.Fatalf("expected usd_assets 20.0, got %v", got)
	}
	if got := summary["polymarket_assets"]; got != 22.0 {
		t.Fatalf("expected polymarket_assets 22.0, got %v", got)
	}
	if got := summary["total_assets"]; got != 42.0 {
		t.Fatalf("expected total_assets 42.0, got %v", got)
	}

	positions, ok := payload["positions"].([]any)
	if !ok || len(positions) != 1 {
		t.Fatalf("expected one position, got %#v", payload["positions"])
	}
	position, ok := positions[0].(map[string]any)
	if !ok {
		t.Fatalf("expected position object, got %T", positions[0])
	}
	if got := position["note_count"]; got != float64(1) {
		t.Fatalf("expected note_count 1, got %v", got)
	}

	orders, ok := payload["orders"].([]any)
	if !ok || len(orders) != 1 {
		t.Fatalf("expected one open order, got %#v", payload["orders"])
	}
	order, ok := orders[0].(map[string]any)
	if !ok {
		t.Fatalf("expected order object, got %T", orders[0])
	}
	if got := order["remaining_size"]; got != 8.0 {
		t.Fatalf("expected remaining_size 8.0, got %v", got)
	}
	markets, ok := payload["markets"].(map[string]any)
	if !ok {
		t.Fatalf("expected markets object, got %T", payload["markets"])
	}
	market, ok := markets["cond-1"].(map[string]any)
	if !ok {
		t.Fatalf("expected cond-1 market entry, got %#v", markets["cond-1"])
	}
	if got := market["image"]; got != "https://example.com/rain.png" {
		t.Fatalf("expected market image, got %v", got)
	}
}

func TestHandleCompanyPolymarketNotes(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	h := NewHandlers(service, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := service.CreateCompany(ctx, "Notes Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if _, err := data.AddMarketNote(ctx, db, company.ID, "agent-1", "cond-notes", "First note"); err != nil {
		t.Fatalf("AddMarketNote first failed: %v", err)
	}
	if _, err := data.AddMarketNote(ctx, db, company.ID, "agent-2", "cond-notes", "Second note"); err != nil {
		t.Fatalf("AddMarketNote second failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/polymarket/notes?condition_id=cond-notes&limit=10", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if got := payload["company_id"]; got != company.ID {
		t.Fatalf("expected company_id %q, got %v", company.ID, got)
	}
	if got := payload["condition_id"]; got != "cond-notes" {
		t.Fatalf("expected condition_id cond-notes, got %v", got)
	}
	if got := payload["count"]; got != float64(2) {
		t.Fatalf("expected count 2, got %v", got)
	}
	notes, ok := payload["notes"].([]any)
	if !ok || len(notes) != 2 {
		t.Fatalf("expected two notes, got %#v", payload["notes"])
	}
}

func TestHandleCompanyPolymarketSellAddsNote(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	h := NewHandlers(service, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := service.CreateCompany(ctx, "Sell Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	client := &testCompanyPolymarketClient{
		markets: map[string]*polymarket.Market{
			"cond-sell": {
				ConditionID: "cond-sell",
				NegRisk:     true,
			},
		},
		orderBooks: map[string]*polymarket.OrderBook{
			"token-yes": {
				Bids: []polymarket.OrderBookEntry{
					{Price: "0.61", Size: "100"},
				},
			},
		},
		placeResp: &polymarket.PlaceOrderResponse{Success: true, OrderID: "sell-order-1"},
	}
	h.companyPolymarketClientFactory = func(context.Context, string) (companyPolymarketClient, error) {
		return client, nil
	}

	body := `{"asset":"token-yes","condition_id":"cond-sell","outcome":"YES","size":12}`
	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/polymarket/sell", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one placed order, got %d", len(client.placedOrders))
	}
	if client.placedOrders[0].Price != 0.61 {
		t.Fatalf("expected best bid price 0.61, got %v", client.placedOrders[0].Price)
	}
	if !client.placedOrders[0].NegRisk {
		t.Fatalf("expected sell order to forward negRisk=true")
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-sell", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected one market note, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Do not buy this market automatically") {
		t.Fatalf("expected do-not-buy note, got %q", notes[0].Content)
	}
}

func TestHandleCompanyPolymarketCancelAddsNote(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	h := NewHandlers(service, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := service.CreateCompany(ctx, "Cancel Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	client := &testCompanyPolymarketClient{
		ordersByID: map[string]*polymarket.Order{
			"order-cancel-1": {
				ID:           "order-cancel-1",
				Market:       "cond-cancel",
				AssetID:      "token-no",
				Side:         polymarket.Buy,
				OriginalSize: "30",
				SizeMatched:  "5",
				Price:        "0.33",
				Status:       "live",
				Outcome:      "NO",
				Type:         polymarket.GTC,
			},
		},
	}
	h.companyPolymarketClientFactory = func(context.Context, string) (companyPolymarketClient, error) {
		return client, nil
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{"order_id": "order-cancel-1"}); err != nil {
		t.Fatalf("encode request failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/polymarket/cancel", &body)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(client.cancelledOrderIDs) != 1 || client.cancelledOrderIDs[0] != "order-cancel-1" {
		t.Fatalf("expected cancelled order-cancel-1, got %#v", client.cancelledOrderIDs)
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-cancel", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected one market note, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Action: CANCEL") {
		t.Fatalf("expected cancel note, got %q", notes[0].Content)
	}
}

func TestHandleCompanyPolymarketExitCancelsOrdersAndSellsAll(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	h := NewHandlers(service, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := service.CreateCompany(ctx, "Exit Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	client := &testCompanyPolymarketClient{
		positions: []polymarket.Position{
			{
				Asset:        "token-exit-yes",
				ConditionID:  "cond-exit",
				Outcome:      "YES",
				Size:         198,
				CurPrice:     0.77,
				NegativeRisk: true,
			},
		},
		orders: []polymarket.Order{
			{
				ID:           "order-exit-1",
				Market:       "cond-exit",
				AssetID:      "token-exit-yes",
				Side:         polymarket.Sell,
				OriginalSize: "25",
				SizeMatched:  "0",
				Price:        "0.78",
				Status:       "live",
				Outcome:      "YES",
			},
			{
				ID:           "order-exit-2",
				Market:       "cond-exit",
				AssetID:      "token-exit-no",
				Side:         polymarket.Buy,
				OriginalSize: "10",
				SizeMatched:  "0",
				Price:        "0.22",
				Status:       "live",
				Outcome:      "NO",
			},
		},
		orderBooks: map[string]*polymarket.OrderBook{
			"token-exit-yes": {
				Bids: []polymarket.OrderBookEntry{
					{Price: "0.775", Size: "50"},
				},
			},
		},
		placeResp: &polymarket.PlaceOrderResponse{Success: true, OrderID: "exit-sell-order-1"},
	}
	h.companyPolymarketClientFactory = func(context.Context, string) (companyPolymarketClient, error) {
		return client, nil
	}

	body := `{"asset":"token-exit-yes","condition_id":"cond-exit","outcome":"YES"}`
	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/polymarket/exit", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(client.cancelledOrderIDs) != 2 {
		t.Fatalf("expected 2 cancelled orders, got %#v", client.cancelledOrderIDs)
	}
	if len(client.placedOrders) != 1 {
		t.Fatalf("expected one sell order, got %d", len(client.placedOrders))
	}
	placed := client.placedOrders[0]
	if placed.Price != 0.775 {
		t.Fatalf("expected sell price 0.775, got %v", placed.Price)
	}
	if placed.Size != 198 {
		t.Fatalf("expected sell size 198, got %v", placed.Size)
	}
	if placed.Side != polymarket.Sell {
		t.Fatalf("expected SELL side, got %s", placed.Side)
	}
	if !placed.NegRisk {
		t.Fatalf("expected exit sell order to forward negRisk=true")
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-exit", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected one market note, got %d", len(notes))
	}
	if !strings.Contains(notes[0].Content, "Action: EXIT_POSITION") {
		t.Fatalf("expected exit note, got %q", notes[0].Content)
	}
	if !strings.Contains(notes[0].Content, "Cancelled Orders: 2") {
		t.Fatalf("expected cancelled order count in note, got %q", notes[0].Content)
	}
}
