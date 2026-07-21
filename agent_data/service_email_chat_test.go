package data

import (
	"context"
	"testing"
)

func TestEmailWhitelist(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()
	svc.EnsureAgent(ctx)

	// Empty whitelist
	wl, _ := svc.GetEmailWhitelist(ctx)
	if len(wl) != 0 {
		t.Errorf("expected empty whitelist, got %v", wl)
	}

	// Add entries
	svc.AddEmailWhitelistEntry(ctx, "alice@example.com")
	svc.AddEmailWhitelistEntry(ctx, "bob@example.com")

	// Dedup (case insensitive)
	svc.AddEmailWhitelistEntry(ctx, "ALICE@EXAMPLE.COM")

	wl, _ = svc.GetEmailWhitelist(ctx)
	if len(wl) != 2 {
		t.Errorf("expected 2 entries (deduped), got %d: %v", len(wl), wl)
	}

	// Check whitelisted
	ok, _ := svc.IsEmailWhitelisted(ctx, []string{"alice@example.com"})
	if !ok {
		t.Error("alice should be whitelisted")
	}

	ok, _ = svc.IsEmailWhitelisted(ctx, []string{"alice@example.com", "bob@example.com"})
	if !ok {
		t.Error("alice+bob should be whitelisted")
	}

	ok, _ = svc.IsEmailWhitelisted(ctx, []string{"alice@example.com", "unknown@example.com"})
	if ok {
		t.Error("unknown should not be whitelisted")
	}

	ok, _ = svc.IsEmailWhitelisted(ctx, nil)
	if ok {
		t.Error("empty recipients should not be whitelisted")
	}

	// Remove entry
	svc.RemoveEmailWhitelistEntry(ctx, "alice@example.com")
	wl, _ = svc.GetEmailWhitelist(ctx)
	if len(wl) != 1 {
		t.Errorf("expected 1 entry after remove, got %d", len(wl))
	}
}

func TestPendingEmails(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Add pending email
	pe := &PendingEmail{
		Type:       "send",
		Recipients: "test@example.com",
		Subject:    "Hello",
		Preview:    "Hi there",
	}
	if err := svc.AddPendingEmail(ctx, pe); err != nil {
		t.Fatalf("AddPendingEmail failed: %v", err)
	}
	if pe.ID == "" {
		t.Error("expected ID to be set")
	}

	// Get pending emails
	emails, err := svc.GetPendingEmails(ctx)
	if err != nil {
		t.Fatalf("GetPendingEmails failed: %v", err)
	}
	if len(emails) != 1 {
		t.Fatalf("expected 1 pending email, got %d", len(emails))
	}

	// Get by ID
	got, err := svc.GetPendingEmailByID(ctx, pe.ID)
	if err != nil {
		t.Fatalf("GetPendingEmailByID failed: %v", err)
	}
	if got.Subject != "Hello" {
		t.Errorf("unexpected subject: %q", got.Subject)
	}

	// Update status
	if err := svc.UpdatePendingEmailStatus(ctx, pe.ID, "approved"); err != nil {
		t.Fatalf("UpdatePendingEmailStatus failed: %v", err)
	}

	// Pending list should be empty now (status changed)
	emails, _ = svc.GetPendingEmails(ctx)
	if len(emails) != 0 {
		t.Errorf("expected 0 pending after approval, got %d", len(emails))
	}

	// Delete
	if err := svc.DeletePendingEmail(ctx, pe.ID); err != nil {
		t.Fatalf("DeletePendingEmail failed: %v", err)
	}
}

func TestChatHistory(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Save messages
	svc.SaveChatMessage(ctx, "user", "Hello")
	svc.SaveChatMessage(ctx, "assistant", "Hi there!")
	svc.SaveChatMessage(ctx, "user", "How are you?")

	// Get history - verify count and content exists
	msgs, err := svc.GetChatHistory(ctx, 10)
	if err != nil {
		t.Fatalf("GetChatHistory failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Verify all messages are present
	contents := map[string]bool{}
	for _, m := range msgs {
		contents[m.Content] = true
	}
	for _, expected := range []string{"Hello", "Hi there!", "How are you?"} {
		if !contents[expected] {
			t.Errorf("expected message %q not found", expected)
		}
	}

	// Limit
	msgs, _ = svc.GetChatHistory(ctx, 2)
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages with limit, got %d", len(msgs))
	}

	// Default limit
	msgs, _ = svc.GetChatHistory(ctx, 0)
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages with default limit, got %d", len(msgs))
	}
}
