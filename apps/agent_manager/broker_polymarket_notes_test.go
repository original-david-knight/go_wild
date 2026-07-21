package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestBrokerPolymarketNotes_AddNote_NoCompany(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "notes-no-co")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)

	body := `{"condition_id":"cond-1","content":"some note"}`
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/market-notes/add", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	rec := httptest.NewRecorder()

	h.handleAddMarketNote(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "company membership") {
		t.Fatalf("expected company membership error, got %s", rec.Body.String())
	}
}

func TestBrokerPolymarketNotes_AddNote_WithCompany(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "notes-with-co")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := service.CreateCompany(ctx, "notes-co", "test company", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agent.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)

	body := `{"condition_id":"cond-1","content":"my market note"}`
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/market-notes/add", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	rec := httptest.NewRecorder()

	h.handleAddMarketNote(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	note, ok := resp["note"].(map[string]any)
	if !ok {
		t.Fatalf("expected note in response, got %v", resp)
	}
	if note["content"] != "my market note" {
		t.Fatalf("expected note content 'my market note', got %v", note["content"])
	}
	if resp["company_id"] != company.ID {
		t.Fatalf("expected company_id %s, got %v", company.ID, resp["company_id"])
	}
}

func TestBrokerPolymarketNotes_ListNotes_NoCompany(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "list-no-co")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)

	body := `{"condition_id":"cond-1","limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/market-notes/list", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	rec := httptest.NewRecorder()

	h.handleListMarketNotes(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "company membership") {
		t.Fatalf("expected company membership error, got %s", rec.Body.String())
	}
}

func TestBrokerPolymarketNotes_AddNote_DisabledByMethodConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	systemSvc := data.NewAgentService(db, "system")
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "notes-method-add")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := service.CreateCompany(ctx, "notes-method-add-co", "test company", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agent.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "custom_market_review", "review market", "", "", "", false, false, false, true, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/market-notes/add", strings.NewReader(`{"condition_id":"cond-1","content":"my market note"}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	req = req.WithContext(context.WithValue(req.Context(), brokerExecutionMethodKey, "custom_market_review"))
	rec := httptest.NewRecorder()

	h.handleAddMarketNote(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "custom_market_review") {
		t.Fatalf("expected method-specific error, got %s", rec.Body.String())
	}
}

func TestBrokerPolymarketNotes_ListNotes_DisabledByMethodConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	systemSvc := data.NewAgentService(db, "system")
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "notes-method-list")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := service.CreateCompany(ctx, "notes-method-list-co", "test company", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agent.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "custom_market_review", "review market", "", "", "", false, false, false, true, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	h := NewBrokerPolymarketHandler(service)
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/market-notes/list", strings.NewReader(`{"condition_id":"cond-1","limit":10}`))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agent.ID))
	req = req.WithContext(context.WithValue(req.Context(), brokerExecutionMethodKey, "custom_market_review"))
	rec := httptest.NewRecorder()

	h.handleListMarketNotes(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "custom_market_review") {
		t.Fatalf("expected method-specific error, got %s", rec.Body.String())
	}
}

func TestBrokerPolymarketExecutionPolicyUsesMethodConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	systemSvc := data.NewAgentService(db, "system")
	ctx := context.Background()

	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "generic_method", "review market", "", "", "", false, false, false, false, true); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}
	h := NewBrokerPolymarketHandler(service)
	reqCtx := context.WithValue(ctx, brokerExecutionMethodKey, "generic_method")

	if method, disabled := h.currentExecutionDisablesMarketNotes(reqCtx); disabled || method != "generic_method" {
		t.Fatalf("expected direct market notes to remain enabled, got method=%q disabled=%v", method, disabled)
	}
	if !h.currentExecutionDisablesPolymarketNoteAugmentation(reqCtx) {
		t.Fatalf("expected note augmentation to be disabled for configured method")
	}
}
