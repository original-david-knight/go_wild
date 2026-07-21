package data

import (
	"context"
	"testing"
	"time"
)

func TestChatHistoryRetention(t *testing.T) {
	t.Setenv("GOWILD_CHAT_RETENTION_DAYS", "1")

	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	dao := db.Table(ChatMessage{})
	old := &ChatMessage{
		ID:        newID(),
		AgentID:   "test-agent",
		Role:      "user",
		Content:   "old message",
		CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	newer := &ChatMessage{
		ID:        newID(),
		AgentID:   "test-agent",
		Role:      "assistant",
		Content:   "new message",
		CreatedAt: time.Now(),
	}
	if err := dao.Insert(ctx, old); err != nil {
		t.Fatalf("Insert old chat message failed: %v", err)
	}
	if err := dao.Insert(ctx, newer); err != nil {
		t.Fatalf("Insert new chat message failed: %v", err)
	}

	msgs, err := svc.GetChatHistory(ctx, 10)
	if err != nil {
		t.Fatalf("GetChatHistory failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 chat message after prune, got %d", len(msgs))
	}
	if msgs[0].Content != "new message" {
		t.Errorf("unexpected message content: %q", msgs[0].Content)
	}
}
