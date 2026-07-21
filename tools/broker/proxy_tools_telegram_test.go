package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

// --- TelegramTools ---

func TestTelegramTools_Send(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "message_id": float64(999)})
	}))

	tt := NewTelegramTools(c)
	result, err := tt.TelegramSendTool(context.Background(), tools.TelegramSendInput{
		ChatID: 12345, Text: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/telegram/send" {
		t.Errorf("expected telegram/send path, got %s", gotPath)
	}
}

func TestTelegramTools_GetUpdates(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))

	tt := NewTelegramTools(c)
	result, err := tt.TelegramGetUpdatesTool(context.Background(), tools.TelegramGetUpdatesInput{Clear: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if !strings.Contains(gotPath, "clear=true") {
		t.Errorf("expected clear=true in query, got %s", gotPath)
	}
}

func TestTelegramTools_GetUpdates_NoClear(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))

	tt := NewTelegramTools(c)
	tt.TelegramGetUpdatesTool(context.Background(), tools.TelegramGetUpdatesInput{Clear: false})
	if strings.Contains(gotPath, "clear") {
		t.Errorf("expected no clear param, got %s", gotPath)
	}
}

func TestTelegramTools_GetChats(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"chats": []any{}})
	}))

	tt := NewTelegramTools(c)
	result, err := tt.TelegramGetChatsTool(context.Background(), tools.TelegramGetChatsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/telegram/chats" {
		t.Errorf("expected telegram/chats path, got %s", gotPath)
	}
}

func TestTelegramTools_GetBotInfo(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"username": "testbot", "id": float64(123)})
	}))

	tt := NewTelegramTools(c)
	result, err := tt.TelegramGetBotInfoTool(context.Background(), tools.TelegramGetBotInfoInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTelegramTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "telegram not configured"})
	}))

	tt := NewTelegramTools(c)
	result, err := tt.TelegramSendTool(context.Background(), tools.TelegramSendInput{ChatID: 1, Text: "hi"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestTelegramTools_DescribeTool(t *testing.T) {
	tt := NewTelegramTools(nil)
	if tt.DescribeTool("telegram_send") == "" {
		t.Error("expected non-empty description")
	}
	if tt.DescribeTool("telegram_get_updates") == "" {
		t.Error("expected non-empty description")
	}
	if tt.DescribeTool("nonexistent") != "" {
		t.Error("expected empty for unknown tool")
	}
}
