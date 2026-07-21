package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrokerEmailHandler_NoAPIKey(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")

	handler := NewBrokerEmailHandler(svc)
	_, err := handler.getEmailTools(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Fatalf("expected API key error, got: %v", err)
	}
}

func TestBrokerEmailHandler_NoInboxID(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")

	// Set API key but no inbox ID
	agent, _ := svc.GetAgent(ctx, "agent-1")
	agent.AgentMailAPIKey = "test-key"
	svc.UpdateAgent(ctx, agent)

	handler := NewBrokerEmailHandler(svc)
	_, err := handler.getEmailTools(ctx, "agent-1")
	if err == nil {
		t.Fatal("expected error for missing inbox ID")
	}
	if !strings.Contains(err.Error(), "inbox") {
		t.Fatalf("expected inbox error, got: %v", err)
	}
}

func TestBrokerEmailHandler_HandleList_MissingConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")

	handler := NewBrokerEmailHandler(svc)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/email/list", body)
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))

	rec := httptest.NewRecorder()
	handler.handleList(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = ctx // used above
}
