package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAgentNetDBNoEnvReturnsNil(t *testing.T) {
	t.Setenv("AGENT_NET_DATABASE_URL", "")
	h := &Handlers{}

	db, err := h.getAgentNetDB()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if db != nil {
		t.Fatalf("expected nil DB when AGENT_NET_DATABASE_URL is unset")
	}
}

func TestGetAgentNetDBUsesInjectedDB(t *testing.T) {
	db := setupManagerTestDB(t)
	t.Setenv("AGENT_NET_DATABASE_URL", "postgres://invalid:invalid@localhost:1/invalid")

	h := &Handlers{agentNetDB: db}
	got, err := h.getAgentNetDB()
	if err != nil {
		t.Fatalf("unexpected error for injected DB: %v", err)
	}
	if got != db {
		t.Fatalf("expected injected DB to be returned")
	}
}

func TestHandleSpendNotConfigured(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/api/agents/a-1/spend", nil)
	rec := httptest.NewRecorder()

	h.handleSpend(rec, req, "a-1", "spend")

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected status %d, got %d", http.StatusNotImplemented, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spend handler not configured") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestHandleSpendUnknownAction(t *testing.T) {
	db := setupManagerTestDB(t)
	h := &Handlers{spendHandler: NewSpendHandler(db)}
	req := httptest.NewRequest(http.MethodGet, "/api/agents/a-1/spend/unknown", nil)
	rec := httptest.NewRecorder()

	h.handleSpend(rec, req, "a-1", "spend/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown spend action") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestHandleSpendDispatchesToSpendEndpoints(t *testing.T) {
	db := setupManagerTestDB(t)
	h := &Handlers{spendHandler: NewSpendHandler(db)}

	getReq := httptest.NewRequest(http.MethodGet, "/api/agents/a-1/spend", nil)
	getRec := httptest.NewRecorder()
	h.handleSpend(getRec, getReq, "a-1", "spend")
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected spend GET status %d, got %d body=%s", http.StatusOK, getRec.Code, getRec.Body.String())
	}

	limitsReq := httptest.NewRequest(http.MethodGet, "/api/agents/a-1/spend/limits", nil)
	limitsRec := httptest.NewRecorder()
	h.handleSpend(limitsRec, limitsReq, "a-1", "spend/limits")
	if limitsRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected spend/limits wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, limitsRec.Code, limitsRec.Body.String())
	}
}
