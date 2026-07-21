package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestHandleOrderBookDepth_Success(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}
	h.getOrderBookFn = func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error) {
		return &polymarket.OrderBook{
			Market:  "market-1",
			AssetID: "token-1",
			Bids: []polymarket.OrderBookEntry{
				{Price: "0.72", Size: "1000"},
				{Price: "0.71", Size: "5000"},
			},
			Asks: []polymarket.OrderBookEntry{
				{Price: "0.73", Size: "1200"},
				{Price: "0.74", Size: "900"},
			},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1","levels":2}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	depth, ok := payload["depth"].(map[string]any)
	if !ok {
		t.Fatalf("expected depth object, got %#v", payload["depth"])
	}
	if depth["token_id"] != "token-1" {
		t.Fatalf("expected token_id token-1, got %v", depth["token_id"])
	}
	top, ok := depth["top_of_book"].(map[string]any)
	if !ok {
		t.Fatalf("expected top_of_book object, got %#v", depth["top_of_book"])
	}
	if top["spread"] != 0.01 {
		t.Fatalf("expected spread 0.01, got %v", top["spread"])
	}
}

func TestHandleOrderBookDepth_MissingAgentID(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing agent ID") {
		t.Fatalf("expected missing agent ID error, got %s", rec.Body.String())
	}
}

func TestHandleOrderBookDepth_InvalidJSON(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %s", rec.Body.String())
	}
}

func TestHandleOrderBookDepth_ClientError(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return nil, errors.New("client unavailable")
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "client unavailable") {
		t.Fatalf("expected client error message, got %s", rec.Body.String())
	}
}

func TestHandleOrderBookDepth_OrderBookError(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}
	h.getOrderBookFn = func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error) {
		return nil, errors.New("order book unavailable")
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "order book unavailable") {
		t.Fatalf("expected order book error message, got %s", rec.Body.String())
	}
}

func TestHandleOrderBookDepth_InvalidOrderBookData(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}
	h.getOrderBookFn = func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error) {
		return &polymarket.OrderBook{
			Bids: []polymarket.OrderBookEntry{{Price: "bad-price", Size: "100"}},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid bid price") {
		t.Fatalf("expected invalid bid price error, got %s", rec.Body.String())
	}
}

func TestServerRoute_OrderBookDepth(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	secret := []byte("test-secret")

	poly := NewBrokerPolymarketHandler(nil)
	poly.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}
	poly.getOrderBookFn = func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error) {
		return &polymarket.OrderBook{
			Bids: []polymarket.OrderBookEntry{{Price: "0.72", Size: "1000"}},
			Asks: []polymarket.OrderBookEntry{{Price: "0.73", Size: "1200"}},
		}, nil
	}

	server := NewServer(":0", &Handlers{}, &BrokerHandlers{
		auth:       NewBrokerAuthHandler(service, secret),
		llm:        NewBrokerLLMHandler(nil),
		wallet:     NewBrokerWalletHandler(service),
		polymarket: poly,
		email:      NewBrokerEmailHandler(service),
		search:     NewBrokerSearchHandler(),
		telegram:   NewBrokerTelegramHandler(service),
		tools:      nil,
		secret:     secret,
	}, nil)

	token := testSessionTokenForAgent(t, db, secret, "agent-1")
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1","levels":1}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.buildHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	depth, ok := payload["depth"].(map[string]any)
	if !ok {
		t.Fatalf("expected depth object, got %#v", payload["depth"])
	}
	if depth["levels_used"] != float64(1) {
		t.Fatalf("expected levels_used 1, got %v", depth["levels_used"])
	}
}
