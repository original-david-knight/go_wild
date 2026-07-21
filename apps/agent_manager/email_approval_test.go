package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

// ============================================================
// Service tests
// ============================================================

func TestAgentServiceEmailWhitelist(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "alice")

	// Initially empty
	wl, err := svc.GetEmailWhitelist(ctx, "alice")
	if err != nil {
		t.Fatalf("GetEmailWhitelist failed: %v", err)
	}
	if len(wl) != 0 {
		t.Errorf("expected empty whitelist, got %d entries", len(wl))
	}

	// Add entry
	if err := svc.AddEmailWhitelistEntry(ctx, "alice", "bob@example.com"); err != nil {
		t.Fatalf("AddEmailWhitelistEntry failed: %v", err)
	}

	wl, _ = svc.GetEmailWhitelist(ctx, "alice")
	if len(wl) != 1 || wl[0] != "bob@example.com" {
		t.Errorf("expected [bob@example.com], got %v", wl)
	}

	// Add duplicate (case-insensitive) — should not duplicate
	if err := svc.AddEmailWhitelistEntry(ctx, "alice", "BOB@EXAMPLE.COM"); err != nil {
		t.Fatalf("AddEmailWhitelistEntry (dup) failed: %v", err)
	}
	wl, _ = svc.GetEmailWhitelist(ctx, "alice")
	if len(wl) != 1 {
		t.Errorf("expected 1 entry after duplicate add, got %d", len(wl))
	}

	// Add second entry
	svc.AddEmailWhitelistEntry(ctx, "alice", "carol@example.com")
	wl, _ = svc.GetEmailWhitelist(ctx, "alice")
	if len(wl) != 2 {
		t.Errorf("expected 2 entries, got %d", len(wl))
	}

	// Remove entry
	if err := svc.RemoveEmailWhitelistEntry(ctx, "alice", "bob@example.com"); err != nil {
		t.Fatalf("RemoveEmailWhitelistEntry failed: %v", err)
	}
	wl, _ = svc.GetEmailWhitelist(ctx, "alice")
	if len(wl) != 1 || wl[0] != "carol@example.com" {
		t.Errorf("expected [carol@example.com] after removal, got %v", wl)
	}

	// Remove non-existent — no error
	if err := svc.RemoveEmailWhitelistEntry(ctx, "alice", "nobody@example.com"); err != nil {
		t.Fatalf("RemoveEmailWhitelistEntry (non-existent) failed: %v", err)
	}
}

func TestAgentServicePendingEmails(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	// Initially empty
	emails, err := svc.GetPendingEmails(ctx, "jake")
	if err != nil {
		t.Fatalf("GetPendingEmails failed: %v", err)
	}
	if len(emails) != 0 {
		t.Errorf("expected no pending emails, got %d", len(emails))
	}

	// Insert pending emails via the data layer
	agentSvc := data.NewAgentService(db, "jake")
	agentSvc.AddPendingEmail(ctx, &data.PendingEmail{
		Type:        "send",
		Recipients:  "user@example.com",
		Subject:     "Hello",
		Preview:     "Hello there",
		RequestData: `{"action":"send","to":"user@example.com","subject":"Hello","text":"Hello there"}`,
	})
	agentSvc.AddPendingEmail(ctx, &data.PendingEmail{
		Type:        "reply",
		Recipients:  "other@example.com",
		Subject:     "Re: Meeting",
		Preview:     "Sure, let's do it",
		RequestData: `{"action":"reply","message_id":"msg-123","text":"Sure, let's do it"}`,
	})

	// Now should have 2
	emails, err = svc.GetPendingEmails(ctx, "jake")
	if err != nil {
		t.Fatalf("GetPendingEmails failed: %v", err)
	}
	if len(emails) != 2 {
		t.Fatalf("expected 2 pending emails, got %d", len(emails))
	}

	// Reject one
	pe, err := svc.RejectPendingEmail(ctx, "jake", emails[0].ID)
	if err != nil {
		t.Fatalf("RejectPendingEmail failed: %v", err)
	}
	if pe.ID != emails[0].ID {
		t.Errorf("expected rejected email ID %s, got %s", emails[0].ID, pe.ID)
	}

	// Rejecting again should fail (already rejected)
	_, err = svc.RejectPendingEmail(ctx, "jake", emails[0].ID)
	if err == nil {
		t.Error("expected error rejecting already-rejected email")
	}

	// After rejection, only 1 pending
	remaining, _ := svc.GetPendingEmails(ctx, "jake")
	if len(remaining) != 1 {
		t.Errorf("expected 1 pending email after rejection, got %d", len(remaining))
	}
}

func TestAgentServiceRejectPendingEmail_NotFound(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	_, err := svc.RejectPendingEmail(ctx, "jake", "nonexistent-id")
	if err == nil {
		t.Error("expected error rejecting nonexistent email")
	}
}

// ============================================================
// Handler tests
// ============================================================

func TestListPendingEmails_Empty(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	svc.CreateAgent(context.Background(), "jake")

	req := httptest.NewRequest("GET", "/api/agents/jake/pending-emails", nil)
	rec := httptest.NewRecorder()
	h.listPendingEmails(rec, req, "jake")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)

	count := int(result["count"].(float64))
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
	emails := result["emails"].([]any)
	if len(emails) != 0 {
		t.Errorf("expected empty emails, got %d", len(emails))
	}
}

func TestListPendingEmails_WithEmails(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")
	agentSvc := data.NewAgentService(db, "jake")
	agentSvc.AddPendingEmail(ctx, &data.PendingEmail{
		Type:        "send",
		Recipients:  "bob@test.com",
		Subject:     "Test Subject",
		Preview:     "Test body",
		RequestData: `{"action":"send"}`,
	})

	req := httptest.NewRequest("GET", "/api/agents/jake/pending-emails", nil)
	rec := httptest.NewRecorder()
	h.listPendingEmails(rec, req, "jake")

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)

	count := int(result["count"].(float64))
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	emails := result["emails"].([]any)
	email := emails[0].(map[string]any)
	if email["type"] != "send" {
		t.Errorf("expected type 'send', got %v", email["type"])
	}
	if email["recipients"] != "bob@test.com" {
		t.Errorf("expected recipients 'bob@test.com', got %v", email["recipients"])
	}
	if email["subject"] != "Test Subject" {
		t.Errorf("expected subject 'Test Subject', got %v", email["subject"])
	}
}

func TestRejectPendingEmails_Single(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")
	agentSvc := data.NewAgentService(db, "jake")
	agentSvc.AddPendingEmail(ctx, &data.PendingEmail{
		Type:        "send",
		Recipients:  "bob@test.com",
		Subject:     "Hello",
		Preview:     "Hi Bob",
		RequestData: `{"action":"send"}`,
	})

	emails, _ := svc.GetPendingEmails(ctx, "jake")
	emailID := emails[0].ID

	body := `{"id":"` + emailID + `"}`
	req := httptest.NewRequest("POST", "/api/agents/jake/pending-emails/reject", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.rejectPendingEmails(rec, req, "jake")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "rejected" {
		t.Errorf("expected status 'rejected', got %v", result["status"])
	}

	// Verify no more pending
	remaining, _ := svc.GetPendingEmails(ctx, "jake")
	if len(remaining) != 0 {
		t.Errorf("expected 0 pending after reject, got %d", len(remaining))
	}
}

func TestRejectPendingEmails_All(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")
	agentSvc := data.NewAgentService(db, "jake")
	for i := 0; i < 3; i++ {
		agentSvc.AddPendingEmail(ctx, &data.PendingEmail{
			Type:        "send",
			Recipients:  "user@test.com",
			Subject:     "Mail",
			Preview:     "Content",
			RequestData: `{"action":"send"}`,
		})
	}

	req := httptest.NewRequest("POST", "/api/agents/jake/pending-emails/reject", strings.NewReader(`{"all":true}`))
	rec := httptest.NewRecorder()
	h.rejectPendingEmails(rec, req, "jake")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	rejected := int(result["rejected"].(float64))
	if rejected != 3 {
		t.Errorf("expected 3 rejected, got %d", rejected)
	}

	remaining, _ := svc.GetPendingEmails(ctx, "jake")
	if len(remaining) != 0 {
		t.Errorf("expected 0 pending after reject all, got %d", len(remaining))
	}
}

func TestRejectPendingEmails_MissingID(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/agents/jake/pending-emails/reject", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.rejectPendingEmails(rec, req, "jake")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRejectPendingEmails_InvalidJSON(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/agents/jake/pending-emails/reject", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.rejectPendingEmails(rec, req, "jake")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandlePendingEmails_Routing(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	// GET /pending-emails → list
	req := httptest.NewRequest("GET", "/api/agents/jake/pending-emails", nil)
	rec := httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "jake")
	if rec.Code != http.StatusOK {
		t.Errorf("GET pending-emails: expected 200, got %d", rec.Code)
	}

	// Unknown sub-action → 404
	req = httptest.NewRequest("GET", "/api/agents/jake/pending-emails/unknown", nil)
	rec = httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "jake")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET pending-emails/unknown: expected 404, got %d", rec.Code)
	}

	// POST approve without body → 400
	req = httptest.NewRequest("POST", "/api/agents/jake/pending-emails/approve", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "jake")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST approve empty: expected 400, got %d", rec.Code)
	}
}

func TestHandlePendingEmailsWrongMethodReturnsNotFound(t *testing.T) {
	h := &Handlers{}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/jake/pending-emails/approve", nil)
	rec := httptest.NewRecorder()
	h.handlePendingEmails(rec, req, "jake")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestPendingEmailRouteRecognitionHelpers(t *testing.T) {
	if !isPendingEmailAction("") || !isPendingEmailAction("approve") || !isPendingEmailAction("reject") {
		t.Fatalf("expected base/approve/reject actions to be recognized")
	}
	if isPendingEmailAction("unknown") {
		t.Fatalf("expected unknown action to be rejected")
	}
}

func TestParsePendingEmailAction(t *testing.T) {
	if got := parsePendingEmailAction("/api/agents/jake/pending-emails", "jake"); got != "" {
		t.Fatalf("expected base route to parse empty action, got %q", got)
	}
	if got := parsePendingEmailAction("/api/agents/jake/pending-emails/approve", "jake"); got != "approve" {
		t.Fatalf("expected approve action, got %q", got)
	}
	if got := parsePendingEmailAction("/api/agents/jake/pending-emails/reject", "jake"); got != "reject" {
		t.Fatalf("expected reject action, got %q", got)
	}
	if got := parsePendingEmailAction("/api/agents/jake/pending-emails/approve/extra", "jake"); got != "approve/extra" {
		t.Fatalf("expected nested action suffix to remain for unknown-route handling, got %q", got)
	}
}

func TestGetEmailWhitelist_Empty(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	svc.CreateAgent(context.Background(), "jake")

	req := httptest.NewRequest("GET", "/api/agents/jake/email-whitelist", nil)
	rec := httptest.NewRecorder()
	h.getEmailWhitelist(rec, req, "jake")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	emails := result["emails"].([]any)
	if len(emails) != 0 {
		t.Errorf("expected empty whitelist, got %d entries", len(emails))
	}
}

func TestAddEmailWhitelistEntry(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	req := httptest.NewRequest("POST", "/api/agents/jake/email-whitelist",
		strings.NewReader(`{"email":"trusted@example.com"}`))
	rec := httptest.NewRecorder()
	h.addEmailWhitelistEntry(rec, req, "jake")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "added" {
		t.Errorf("expected status 'added', got %q", result["status"])
	}

	// Verify via GET
	req = httptest.NewRequest("GET", "/api/agents/jake/email-whitelist", nil)
	rec = httptest.NewRecorder()
	h.getEmailWhitelist(rec, req, "jake")

	var listResult map[string]any
	json.NewDecoder(rec.Body).Decode(&listResult)
	emails := listResult["emails"].([]any)
	if len(emails) != 1 {
		t.Errorf("expected 1 whitelist entry, got %d", len(emails))
	}
}

func TestAddEmailWhitelistEntry_MissingEmail(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	svc.CreateAgent(context.Background(), "jake")

	req := httptest.NewRequest("POST", "/api/agents/jake/email-whitelist",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.addEmailWhitelistEntry(rec, req, "jake")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRemoveEmailWhitelistEntry(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")
	svc.AddEmailWhitelistEntry(ctx, "jake", "user1@test.com")
	svc.AddEmailWhitelistEntry(ctx, "jake", "user2@test.com")

	// Remove one
	req := httptest.NewRequest("DELETE", "/api/agents/jake/email-whitelist",
		strings.NewReader(`{"email":"user1@test.com"}`))
	rec := httptest.NewRecorder()
	h.removeEmailWhitelistEntry(rec, req, "jake")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "removed" {
		t.Errorf("expected status 'removed', got %q", result["status"])
	}

	// Verify only user2 remains
	wl, _ := svc.GetEmailWhitelist(ctx, "jake")
	if len(wl) != 1 || wl[0] != "user2@test.com" {
		t.Errorf("expected [user2@test.com], got %v", wl)
	}
}

func TestRemoveEmailWhitelistEntry_MissingEmail(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/api/agents/jake/email-whitelist",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.removeEmailWhitelistEntry(rec, req, "jake")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleEmailWhitelist_MethodNotAllowed(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("PATCH", "/api/agents/jake/email-whitelist", nil)
	rec := httptest.NewRecorder()
	h.handleEmailWhitelist(rec, req, "jake")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestEmailWhitelistMethodRecognitionHelper(t *testing.T) {
	if !isEmailWhitelistMethod(http.MethodGet) || !isEmailWhitelistMethod(http.MethodPost) || !isEmailWhitelistMethod(http.MethodDelete) {
		t.Fatalf("expected GET/POST/DELETE to be recognized whitelist methods")
	}
	if isEmailWhitelistMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected whitelist method")
	}
}

func TestApprovePendingEmails_MissingID(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/agents/jake/pending-emails/approve",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.approvePendingEmails(rec, req, "jake")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestApprovePendingEmails_InvalidJSON(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/agents/jake/pending-emails/approve",
		strings.NewReader(`invalid`))
	rec := httptest.NewRecorder()
	h.approvePendingEmails(rec, req, "jake")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
