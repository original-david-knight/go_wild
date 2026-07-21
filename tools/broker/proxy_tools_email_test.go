package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

// --- EmailTools ---

func TestEmailTools_ListEmails(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))

	et := NewEmailTools(c)
	result, err := et.ListEmailsTool(context.Background(), tools.ListEmailsInput{View: "inbox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/email/list" {
		t.Errorf("expected email/list path, got %s", gotPath)
	}
}

func TestEmailTools_ReadEmail(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"subject": "Test", "body": "Hello"})
	}))

	et := NewEmailTools(c)
	result, err := et.ReadEmailTool(context.Background(), tools.ReadEmailInput{MessageID: "msg-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestEmailTools_SendEmail(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"message_id": "new-msg"})
	}))

	et := NewEmailTools(c)
	result, err := et.SendEmailTool(context.Background(), tools.SendEmailInput{
		Action:  "send",
		To:      []string{"test@example.com"},
		Subject: "Hello",
		Text:    "Body text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestEmailTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
	}))

	et := NewEmailTools(c)
	result, err := et.ListEmailsTool(context.Background(), tools.ListEmailsInput{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestEmailTools_DescribeTool(t *testing.T) {
	et := NewEmailTools(nil)
	if et.DescribeTool("list_emails") == "" {
		t.Error("expected non-empty description")
	}
	if et.DescribeTool("read_email") == "" {
		t.Error("expected non-empty description")
	}
	if et.DescribeTool("send_email") == "" {
		t.Error("expected non-empty description")
	}
}
