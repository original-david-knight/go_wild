package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestPolymarketToolsRequireCompanyMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "poly-agent")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}
	h.getOrderBookFn = func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error) {
		return &polymarket.OrderBook{
			Bids: []polymarket.OrderBookEntry{{Price: "0.50", Size: "10"}},
			Asks: []polymarket.OrderBookEntry{{Price: "0.51", Size: "12"}},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "company membership") {
		t.Fatalf("expected company membership error, got %s", rec.Body.String())
	}
}

func TestPolymarketToolsIncludeCompanyScopeMetadata(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "poly-agent-member")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	company, err := service.CreateCompany(ctx, "poly-co", "", "")
	if err != nil {
		t.Fatalf("failed to create company: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agent.ID, "member"); err != nil {
		t.Fatalf("failed to add agent to company: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}
	h.getOrderBookFn = func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error) {
		return &polymarket.OrderBook{
			Bids: []polymarket.OrderBookEntry{{Price: "0.50", Size: "10"}},
			Asks: []polymarket.OrderBookEntry{{Price: "0.51", Size: "12"}},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if got, _ := payload["identity_scope"].(string); got != "company" {
		t.Fatalf("expected identity_scope company, got %q", got)
	}
	if got, _ := payload["company_id"].(string); got != company.ID {
		t.Fatalf("expected company_id %q, got %q", company.ID, got)
	}
}

func TestPolymarketToolsDenyWhenCompanyConnectionDisabled(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "poly-agent-disabled")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	company, err := service.CreateCompany(ctx, "poly-disabled-co", "", "")
	if err != nil {
		t.Fatalf("failed to create company: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agent.ID, "member"); err != nil {
		t.Fatalf("failed to add agent to company: %v", err)
	}
	if err := service.UpsertCompanyPolymarketConnection(ctx, &data.CompanyPolymarketConnection{
		CompanyID: company.ID,
		Enabled:   false,
	}); err != nil {
		t.Fatalf("failed to upsert disabled polymarket connection: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)
	h.getClientFn = func(context.Context, string) (*polymarket.Client, error) {
		return &polymarket.Client{}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/orderbook-depth", strings.NewReader(`{"token_id":"token-1"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	rec := httptest.NewRecorder()

	h.handleOrderBookDepth(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("expected disabled error, got %s", rec.Body.String())
	}
}
