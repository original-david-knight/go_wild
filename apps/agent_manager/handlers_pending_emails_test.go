package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPendingEmails_ListEmpty(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-1/pending-emails", nil)
	rec := httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "agent-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	emails, ok := resp["emails"].([]any)
	if !ok {
		t.Fatalf("expected emails array, got %T", resp["emails"])
	}
	if len(emails) != 0 {
		t.Errorf("expected empty emails list, got %d", len(emails))
	}
}

func TestPendingEmails_ApproveRequiresIDOrAll(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")
	h := NewHandlers(svc, nil, nil, nil, nil)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-1/pending-emails/approve", body)
	rec := httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "agent-1")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "id or all required") {
		t.Fatalf("expected 'id or all required' error, got: %s", rec.Body.String())
	}
}

func TestPendingEmails_RejectRequiresIDOrAll(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")
	h := NewHandlers(svc, nil, nil, nil, nil)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-1/pending-emails/reject", body)
	rec := httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "agent-1")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "id or all required") {
		t.Fatalf("expected 'id or all required' error, got: %s", rec.Body.String())
	}
}

func TestParsePendingEmailAction_Basic(t *testing.T) {
	tests := []struct {
		path     string
		agentID  string
		expected string
	}{
		{"/api/agents/x/pending-emails", "x", ""},
		{"/api/agents/x/pending-emails/approve", "x", "approve"},
		{"/api/agents/x/pending-emails/reject", "x", "reject"},
	}
	for _, tt := range tests {
		got := parsePendingEmailAction(tt.path, tt.agentID)
		if got != tt.expected {
			t.Errorf("parsePendingEmailAction(%q, %q) = %q, want %q", tt.path, tt.agentID, got, tt.expected)
		}
	}
}
