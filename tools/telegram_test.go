package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTelegramTools(t *testing.T) {
	tt := NewTelegramTools("test-token")
	if tt == nil {
		t.Fatal("expected non-nil TelegramTools")
	}
	if tt.token != "test-token" {
		t.Errorf("expected token 'test-token', got %q", tt.token)
	}
}

func TestTelegramTools_GetBotUsername_NoInfo(t *testing.T) {
	tt := NewTelegramTools("token")
	if tt.GetBotUsername() != "" {
		t.Error("expected empty username before Start")
	}
}

func TestTelegramTools_HasPendingMessages_Empty(t *testing.T) {
	tt := NewTelegramTools("token")
	if tt.HasPendingMessages() {
		t.Error("expected no pending messages initially")
	}
	if tt.PendingMessageCount() != 0 {
		t.Errorf("expected 0, got %d", tt.PendingMessageCount())
	}
}

func TestTelegramTools_MessageBuffering(t *testing.T) {
	tt := NewTelegramTools("token")

	// Manually add messages to buffer (simulating fetchUpdates)
	tt.mu.Lock()
	tt.messages = append(tt.messages, TelegramMessage{
		UpdateID:  1,
		MessageID: 100,
		ChatID:    42,
		Text:      "hello",
		FromName:  "User",
	})
	tt.messages = append(tt.messages, TelegramMessage{
		UpdateID:  2,
		MessageID: 101,
		ChatID:    42,
		Text:      "world",
		FromName:  "User",
	})
	tt.mu.Unlock()

	if !tt.HasPendingMessages() {
		t.Error("expected pending messages")
	}
	if tt.PendingMessageCount() != 2 {
		t.Errorf("expected 2, got %d", tt.PendingMessageCount())
	}
}

func TestTelegramTools_GetUpdates_ClearsBuffer(t *testing.T) {
	tt := NewTelegramTools("token")

	// Add messages
	tt.mu.Lock()
	tt.messages = append(tt.messages, TelegramMessage{Text: "msg1"})
	tt.messages = append(tt.messages, TelegramMessage{Text: "msg2"})
	tt.mu.Unlock()

	result, err := tt.TelegramGetUpdatesTool(context.Background(), TelegramGetUpdatesInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	content := result.Content.(map[string]any)
	if content["count"].(int) != 2 {
		t.Errorf("expected 2 messages, got %v", content["count"])
	}

	// Buffer should be cleared
	if tt.PendingMessageCount() != 0 {
		t.Errorf("expected 0 after clear, got %d", tt.PendingMessageCount())
	}
}

func TestTelegramTools_GetChats(t *testing.T) {
	tt := NewTelegramTools("token")

	// Add known chats
	tt.mu.Lock()
	tt.knownChats[42] = TelegramChat{ChatID: 42, ChatType: "private", FirstName: "Alice"}
	tt.knownChats[99] = TelegramChat{ChatID: 99, ChatType: "group", Title: "Test Group"}
	tt.mu.Unlock()

	result, err := tt.TelegramGetChatsTool(context.Background(), TelegramGetChatsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	content := result.Content.(map[string]any)
	if content["count"].(int) != 2 {
		t.Errorf("expected 2 chats, got %v", content["count"])
	}
}

func TestTelegramTools_GetBotInfo_NotInitialized(t *testing.T) {
	tt := NewTelegramTools("token")

	result, err := tt.TelegramGetBotInfoTool(context.Background(), TelegramGetBotInfoInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure when bot info not available")
	}
}

func TestTelegramTools_GetBotInfo_Success(t *testing.T) {
	tt := NewTelegramTools("token")
	tt.botInfo = &botInfo{ID: 123, FirstName: "TestBot", Username: "testbot"}
	tt.polling = true

	result, err := tt.TelegramGetBotInfoTool(context.Background(), TelegramGetBotInfoInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	content := result.Content.(map[string]any)
	if content["username"] != "testbot" {
		t.Errorf("expected testbot, got %v", content["username"])
	}
	if content["polling"] != true {
		t.Error("expected polling=true")
	}
}

func TestTelegramTools_Send_Validation(t *testing.T) {
	tt := NewTelegramTools("token")

	// No chat_id
	result, _ := tt.TelegramSendTool(context.Background(), TelegramSendInput{Text: "hi"})
	if result.Success {
		t.Error("expected failure for no chat_id")
	}

	// No text
	result, _ = tt.TelegramSendTool(context.Background(), TelegramSendInput{ChatID: 42})
	if result.Success {
		t.Error("expected failure for no text")
	}
}

func TestTelegramTools_Send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 200,
				"chat":       map[string]any{"id": 42},
				"date":       1700000000,
			},
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	result, err := tt.TelegramSendTool(context.Background(), TelegramSendInput{
		ChatID: 42,
		Text:   "Hello!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestTelegramTools_Send_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "Bad Request: chat not found",
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	result, _ := tt.TelegramSendTool(context.Background(), TelegramSendInput{
		ChatID: 42,
		Text:   "Hello!",
	})
	if result.Success {
		t.Error("expected failure for API error")
	}
}

func TestTelegramTools_FetchUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []any{
				map[string]any{
					"update_id": 10,
					"message": map[string]any{
						"message_id": 100,
						"from": map[string]any{
							"id":         999,
							"first_name": "Alice",
							"last_name":  "Smith",
							"username":   "alice",
						},
						"chat": map[string]any{
							"id":   42,
							"type": "private",
						},
						"date": 1700000000,
						"text": "Hello bot",
					},
				},
			},
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	tt.fetchUpdates(context.Background())

	if tt.PendingMessageCount() != 1 {
		t.Fatalf("expected 1 message, got %d", tt.PendingMessageCount())
	}

	tt.mu.Lock()
	msg := tt.messages[0]
	tt.mu.Unlock()

	if msg.Text != "Hello bot" {
		t.Errorf("expected 'Hello bot', got %q", msg.Text)
	}
	if msg.FromName != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %q", msg.FromName)
	}
	if msg.Username != "alice" {
		t.Errorf("expected 'alice', got %q", msg.Username)
	}
	if msg.ChatID != 42 {
		t.Errorf("expected chat 42, got %d", msg.ChatID)
	}
}

func TestTelegramTools_FetchUpdates_TracksChats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []any{
				map[string]any{
					"update_id": 1,
					"message": map[string]any{
						"message_id": 1,
						"from":       map[string]any{"id": 1, "first_name": "Bot"},
						"chat":       map[string]any{"id": 50, "type": "group", "title": "Test Group"},
						"date":       1700000000,
						"text":       "hi",
					},
				},
			},
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	tt.fetchUpdates(context.Background())

	tt.mu.Lock()
	chat, exists := tt.knownChats[50]
	tt.mu.Unlock()

	if !exists {
		t.Fatal("expected chat 50 to be tracked")
	}
	if chat.Title != "Test Group" {
		t.Errorf("expected 'Test Group', got %q", chat.Title)
	}
	if chat.ChatType != "group" {
		t.Errorf("expected 'group', got %q", chat.ChatType)
	}
}

func TestTelegramTools_FetchUpdates_UpdatesLastID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []any{
				map[string]any{
					"update_id": 100,
					"message": map[string]any{
						"message_id": 1,
						"from":       map[string]any{"id": 1, "first_name": "User"},
						"chat":       map[string]any{"id": 1, "type": "private"},
						"date":       1700000000,
						"text":       "msg",
					},
				},
			},
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	tt.fetchUpdates(context.Background())

	tt.mu.Lock()
	lastID := tt.lastUpdateID
	tt.mu.Unlock()

	if lastID != 100 {
		t.Errorf("expected lastUpdateID=100, got %d", lastID)
	}
}

func TestTelegramTools_FetchUpdates_SkipsEmptyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []any{
				map[string]any{
					"update_id": 1,
					"message": map[string]any{
						"message_id": 1,
						"from":       map[string]any{"id": 1, "first_name": "User"},
						"chat":       map[string]any{"id": 1, "type": "private"},
						"date":       1700000000,
						"text":       "", // Empty text should be skipped
					},
				},
			},
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	tt.fetchUpdates(context.Background())

	if tt.PendingMessageCount() != 0 {
		t.Errorf("expected 0 messages (empty text skipped), got %d", tt.PendingMessageCount())
	}
}

func TestTelegramTools_Stop(t *testing.T) {
	tt := NewTelegramTools("token")
	tt.pollCtx, tt.pollCancel = context.WithCancel(context.Background())
	tt.polling = true

	tt.Stop()

	if tt.polling {
		t.Error("expected polling=false after Stop")
	}
}

func TestTelegramTools_SetOnNewMessages(t *testing.T) {
	tt := NewTelegramTools("token")

	called := false
	tt.SetOnNewMessages(func(msgs []TelegramMessage) {
		called = true
	})

	if tt.onNewMessages == nil {
		t.Error("expected non-nil callback")
	}
	_ = called // callback tested via fetchUpdates integration
}

func TestTelegramTools_DescribeTool(t *testing.T) {
	tt := NewTelegramTools("token")
	for _, name := range []string{"telegram_send", "telegram_get_updates", "telegram_get_chats", "telegram_get_bot_info"} {
		desc := tt.DescribeTool(name)
		if desc == "" {
			t.Errorf("expected non-empty description for %s", name)
		}
	}
	if tt.DescribeTool("unknown") != "" {
		t.Error("expected empty for unknown tool")
	}
}

func TestTelegramTools_Start_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getMe" {
			json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         12345,
					"first_name": "TestBot",
					"username":   "test_bot",
				},
			})
			return
		}
		// getUpdates polling
		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": []any{},
		})
	}))
	defer server.Close()

	tt := NewTelegramTools("token")
	tt.baseURL = server.URL

	err := tt.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer tt.Stop()

	if tt.GetBotUsername() != "test_bot" {
		t.Errorf("expected username 'test_bot', got %q", tt.GetBotUsername())
	}
	if !tt.polling {
		t.Error("expected polling=true after Start")
	}
}
