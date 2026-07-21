package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestEmailTools(server *httptest.Server) *EmailTools {
	return &EmailTools{
		apiKey:  "test-key",
		inboxID: "inbox-123",
		baseURL: server.URL,
		client:  server.Client(),
	}
}

func TestEmailTools_GetInboxID(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")
	if et.GetInboxID() != "inbox-1" {
		t.Errorf("expected inbox-1, got %s", et.GetInboxID())
	}
}

func TestEmailTools_ListEmails_Messages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("bad auth header: %s", r.Header.Get("Authorization"))
		}
		// Should hit /inboxes/inbox-123/messages
		if r.URL.Path != "/inboxes/inbox-123/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"messages": []any{},
		})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, err := et.ListEmailsTool(context.Background(), ListEmailsInput{View: "messages"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_ListEmails_Inbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123" {
			t.Errorf("expected /inboxes/inbox-123, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "inbox-123",
			"email": "test@example.com",
		})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, err := et.ListEmailsTool(context.Background(), ListEmailsInput{View: "inbox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_ListEmails_Threads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123/threads" {
			t.Errorf("expected threads path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"threads": []any{}})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, err := et.ListEmailsTool(context.Background(), ListEmailsInput{View: "threads"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_ListEmails_InvalidView(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")
	result, err := et.ListEmailsTool(context.Background(), ListEmailsInput{View: "invalid"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for invalid view")
	}
}

func TestEmailTools_ListEmails_QueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "5" {
			t.Errorf("expected limit=5, got %s", q.Get("limit"))
		}
		if q.Get("labels") != "inbox" {
			t.Errorf("expected labels=inbox, got %s", q.Get("labels"))
		}
		if q.Get("ascending") != "true" {
			t.Errorf("expected ascending=true, got %s", q.Get("ascending"))
		}
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	et.ListEmailsTool(context.Background(), ListEmailsInput{
		View:      "messages",
		Limit:     5,
		Labels:    "inbox",
		Ascending: true,
	})
}

func TestEmailTools_ReadEmail_NoID(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")
	result, _ := et.ReadEmailTool(context.Background(), ReadEmailInput{})
	if result.Success {
		t.Error("expected failure when no ID provided")
	}
}

func TestEmailTools_ReadEmail_BothIDs(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")
	result, _ := et.ReadEmailTool(context.Background(), ReadEmailInput{
		MessageID: "msg-1",
		ThreadID:  "thread-1",
	})
	if result.Success {
		t.Error("expected failure when both IDs provided")
	}
}

func TestEmailTools_ReadEmail_Message(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123/messages/msg-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg-1",
			"subject": "Test",
		})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, err := et.ReadEmailTool(context.Background(), ReadEmailInput{MessageID: "msg-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_ReadEmail_Thread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123/threads/thread-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "thread-1"})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, _ := et.ReadEmailTool(context.Background(), ReadEmailInput{ThreadID: "thread-1"})
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_SendEmail_Validation(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")

	// No recipients
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:  "send",
		Subject: "hi",
		Text:    "body",
	})
	if result.Success {
		t.Error("expected failure for no recipients")
	}

	// No subject
	result, _ = et.SendEmailTool(context.Background(), SendEmailInput{
		Action: "send",
		To:     []string{"a@b.com"},
		Text:   "body",
	})
	if result.Success {
		t.Error("expected failure for no subject")
	}

	// No body
	result, _ = et.SendEmailTool(context.Background(), SendEmailInput{
		Action:  "send",
		To:      []string{"a@b.com"},
		Subject: "hi",
	})
	if result.Success {
		t.Error("expected failure for no body")
	}

	// Invalid action
	result, _ = et.SendEmailTool(context.Background(), SendEmailInput{Action: "invalid"})
	if result.Success {
		t.Error("expected failure for invalid action")
	}
}

func TestEmailTools_SendEmail_Reply_Validation(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")

	// Reply without message_id
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action: "reply",
		Text:   "response",
	})
	if result.Success {
		t.Error("expected failure for reply without message_id")
	}

	// Reply without body
	result, _ = et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "reply",
		MessageID: "msg-1",
	})
	if result.Success {
		t.Error("expected failure for reply without body")
	}
}

func TestEmailTools_SendEmail_Forward_Validation(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")

	// Forward without message_id
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action: "forward",
		To:     []string{"a@b.com"},
	})
	if result.Success {
		t.Error("expected failure for forward without message_id")
	}

	// Forward without recipients
	result, _ = et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "forward",
		MessageID: "msg-1",
	})
	if result.Success {
		t.Error("expected failure for forward without recipients")
	}
}

func TestEmailTools_SendEmail_UpdateLabels_Validation(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")

	// Update labels without message_id
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "update_labels",
		AddLabels: []string{"starred"},
	})
	if result.Success {
		t.Error("expected failure for update_labels without message_id")
	}
}

func TestEmailTools_SendEmail_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/inboxes/inbox-123/messages/send" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "msg-new",
			"subject": "Test",
		})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, err := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:  "send",
		To:      []string{"user@example.com"},
		Subject: "Test",
		Text:    "Hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_Reply_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123/messages/msg-1/reply" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "msg-reply"})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "reply",
		MessageID: "msg-1",
		Text:      "Thanks!",
	})
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_ReplyAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123/messages/msg-1/reply-all" {
			t.Errorf("expected reply-all path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "msg-reply"})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "reply",
		MessageID: "msg-1",
		Text:      "Thanks!",
		ReplyAll:  true,
	})
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_Forward_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inboxes/inbox-123/messages/msg-1/forward" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "msg-fwd"})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "forward",
		MessageID: "msg-1",
		To:        []string{"other@example.com"},
	})
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_UpdateLabels_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "msg-1"})
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, _ := et.SendEmailTool(context.Background(), SendEmailInput{
		Action:    "update_labels",
		MessageID: "msg-1",
		AddLabels: []string{"starred"},
	})
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestEmailTools_DescribeTool(t *testing.T) {
	et := NewEmailTools("key", "inbox-1")

	for _, name := range []string{"list_emails", "read_email", "send_email"} {
		desc := et.DescribeTool(name)
		if desc == "" {
			t.Errorf("expected non-empty description for %s", name)
		}
	}
	if et.DescribeTool("unknown") != "" {
		t.Error("expected empty description for unknown tool")
	}
}

func TestEmailTools_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	et := newTestEmailTools(server)
	result, err := et.ListEmailsTool(context.Background(), ListEmailsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for 500 response")
	}
}
