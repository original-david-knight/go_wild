package gowild_agent_net

import (
	"context"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

func setupMessagingTest(t *testing.T) (*Service, gowild_data.Database) {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}
	service := NewService(db, nil)
	return service, db
}

func createPremiumAgent(t *testing.T, service *Service, db gowild_data.Database) string {
	t.Helper()
	pubkey, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}
	agentID := EncodePublicKey(pubkey)

	// Upgrade to premium (no blockchain verifier in tests)
	if err := service.UpgradeAccount(context.Background(), agentID, "tx_"+agentID[:8], ChainSolana); err != nil {
		t.Fatalf("Failed to upgrade agent: %v", err)
	}
	return agentID
}

func TestSendMessage(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	msg, err := service.SendMessage(ctx, sender, recipient, "encrypted_data_here", "nonce24bytes_base64", nil)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	if msg.ID == "" {
		t.Error("Message should have an ID")
	}
	if msg.FromPublicKey != sender {
		t.Errorf("FromPublicKey mismatch: got %s, want %s", msg.FromPublicKey, sender)
	}
	if msg.ToPublicKey != recipient {
		t.Errorf("ToPublicKey mismatch: got %s, want %s", msg.ToPublicKey, recipient)
	}
	if msg.Ciphertext != "encrypted_data_here" {
		t.Error("Ciphertext mismatch")
	}
	if msg.ReadAt != nil {
		t.Error("New message should be unread")
	}
	if msg.ExpiresAt != nil {
		t.Error("Message without expiry should have nil ExpiresAt")
	}
}

func TestSendMessage_WithExpiry(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	expiry := time.Now().Add(1 * time.Hour)
	msg, err := service.SendMessage(ctx, sender, recipient, "ephemeral_msg", "nonce123", &expiry)
	if err != nil {
		t.Fatalf("SendMessage with expiry failed: %v", err)
	}

	if msg.ExpiresAt == nil {
		t.Error("Message should have an expiry")
	}
}

func TestSendMessage_SelfMessage(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	agent := createPremiumAgent(t, service, db)

	_, err := service.SendMessage(ctx, agent, agent, "self_talk", "nonce", nil)
	if err == nil {
		t.Error("Should not be able to send message to self")
	}
}

func TestSendMessage_NonPremiumSender(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	// Free tier sender
	pubkey, _, _ := GenerateKeyPair()
	freeAgent := EncodePublicKey(pubkey)
	recipient := createPremiumAgent(t, service, db)

	_, err := service.SendMessage(ctx, freeAgent, recipient, "data", "nonce", nil)
	if err == nil {
		t.Error("Non-premium sender should be rejected")
	}
}

func TestSendMessage_NonPremiumRecipient(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	pubkey, _, _ := GenerateKeyPair()
	freeAgent := EncodePublicKey(pubkey)

	_, err := service.SendMessage(ctx, sender, freeAgent, "data", "nonce", nil)
	if err == nil {
		t.Error("Non-premium recipient should be rejected")
	}
}

func TestSendMessage_MessageTooLarge(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	largeMsg := make([]byte, MaxMessageSize+1)
	for i := range largeMsg {
		largeMsg[i] = 'A'
	}

	_, err := service.SendMessage(ctx, sender, recipient, string(largeMsg), "nonce", nil)
	if err == nil {
		t.Error("Message exceeding MaxMessageSize should be rejected")
	}
}

func TestSendMessage_EmptyCiphertext(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	_, err := service.SendMessage(ctx, sender, recipient, "", "nonce", nil)
	if err == nil {
		t.Error("Empty ciphertext should be rejected")
	}
}

func TestGetConversation(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	alice := createPremiumAgent(t, service, db)
	bob := createPremiumAgent(t, service, db)

	// Send messages in both directions
	service.SendMessage(ctx, alice, bob, "hello_bob", "n1", nil)
	service.SendMessage(ctx, bob, alice, "hello_alice", "n2", nil)
	service.SendMessage(ctx, alice, bob, "how_are_you", "n3", nil)

	// Get conversation from Alice's perspective
	msgs, err := service.GetConversation(ctx, alice, bob, 50, 0)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}

	if len(msgs) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(msgs))
	}

	// Verify all messages are present (ordering depends on timestamp precision,
	// which differs between SQLite test DB and production PostgreSQL)
	ciphertexts := make(map[string]bool)
	for _, m := range msgs {
		ciphertexts[m.Ciphertext] = true
	}
	for _, expected := range []string{"hello_bob", "hello_alice", "how_are_you"} {
		if !ciphertexts[expected] {
			t.Errorf("Missing message with ciphertext %q", expected)
		}
	}

	// Get conversation from Bob's perspective (should be the same count)
	msgs2, err := service.GetConversation(ctx, bob, alice, 50, 0)
	if err != nil {
		t.Fatalf("GetConversation from Bob failed: %v", err)
	}

	if len(msgs2) != 3 {
		t.Fatalf("Expected 3 messages from Bob's view, got %d", len(msgs2))
	}
}

func TestGetConversation_Pagination(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	alice := createPremiumAgent(t, service, db)
	bob := createPremiumAgent(t, service, db)

	for i := 0; i < 5; i++ {
		service.SendMessage(ctx, alice, bob, "msg", "n", nil)
		time.Sleep(1 * time.Millisecond)
	}

	// Page 1
	msgs, err := service.GetConversation(ctx, alice, bob, 2, 0)
	if err != nil {
		t.Fatalf("GetConversation page 1 failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(msgs))
	}

	// Page 2
	msgs, err = service.GetConversation(ctx, alice, bob, 2, 2)
	if err != nil {
		t.Fatalf("GetConversation page 2 failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(msgs))
	}

	// Page 3 (last)
	msgs, err = service.GetConversation(ctx, alice, bob, 2, 4)
	if err != nil {
		t.Fatalf("GetConversation page 3 failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message, got %d", len(msgs))
	}
}

func TestListConversations(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	alice := createPremiumAgent(t, service, db)
	bob := createPremiumAgent(t, service, db)
	charlie := createPremiumAgent(t, service, db)

	// Alice talks with Bob and Charlie
	service.SendMessage(ctx, alice, bob, "hi_bob", "n1", nil)
	service.SendMessage(ctx, alice, charlie, "hi_charlie", "n2", nil)
	service.SendMessage(ctx, bob, alice, "reply_from_bob", "n3", nil)

	// List Alice's conversations
	convs, err := service.ListConversations(ctx, alice, 50, 0)
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}

	if len(convs) != 2 {
		t.Fatalf("Expected 2 conversations, got %d", len(convs))
	}

	// Find Bob and Charlie conversations (ordering may vary with SQLite second-precision timestamps)
	var bobConv, charlieConv *ConversationSummary
	for i := range convs {
		switch convs[i].PeerPublicKey {
		case bob:
			bobConv = &convs[i]
		case charlie:
			charlieConv = &convs[i]
		}
	}

	if bobConv == nil {
		t.Fatal("Expected conversation with Bob")
	}
	if charlieConv == nil {
		t.Fatal("Expected conversation with Charlie")
	}

	// Alice has 1 unread from Bob
	if bobConv.UnreadCount != 1 {
		t.Errorf("Expected 1 unread from Bob, got %d", bobConv.UnreadCount)
	}

	// No unreads from Charlie (Alice only sent, didn't receive)
	if charlieConv.UnreadCount != 0 {
		t.Errorf("Expected 0 unreads from Charlie, got %d", charlieConv.UnreadCount)
	}
}

func TestMarkMessageRead(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	msg, _ := service.SendMessage(ctx, sender, recipient, "read_me", "n1", nil)

	// Recipient marks as read
	if err := service.MarkMessageRead(ctx, recipient, msg.ID); err != nil {
		t.Fatalf("MarkMessageRead failed: %v", err)
	}

	// Verify it's read
	msgs, _ := service.GetConversation(ctx, sender, recipient, 50, 0)
	if len(msgs) != 1 {
		t.Fatal("Expected 1 message")
	}
	if msgs[0].ReadAt == nil {
		t.Error("Message should be marked as read")
	}
}

func TestMarkMessageRead_NotRecipient(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	msg, _ := service.SendMessage(ctx, sender, recipient, "data", "n1", nil)

	// Sender tries to mark as read (should fail)
	if err := service.MarkMessageRead(ctx, sender, msg.ID); err == nil {
		t.Error("Only recipient should be able to mark as read")
	}
}

func TestDeleteMessage(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	msg, _ := service.SendMessage(ctx, sender, recipient, "delete_me", "n1", nil)

	// Sender deletes
	if err := service.DeleteMessage(ctx, sender, msg.ID); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	// Verify it's gone
	msgs, _ := service.GetConversation(ctx, sender, recipient, 50, 0)
	if len(msgs) != 0 {
		t.Errorf("Expected 0 messages after delete, got %d", len(msgs))
	}
}

func TestDeleteMessage_NotSender(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	msg, _ := service.SendMessage(ctx, sender, recipient, "data", "n1", nil)

	// Recipient tries to delete (should fail)
	if err := service.DeleteMessage(ctx, recipient, msg.ID); err == nil {
		t.Error("Only sender should be able to delete")
	}
}

func TestCleanupExpiredMessages(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	// Send a message that's already expired
	pastExpiry := time.Now().Add(-1 * time.Hour)
	service.SendMessage(ctx, sender, recipient, "expired", "n1", &pastExpiry)

	// Send a message that hasn't expired
	futureExpiry := time.Now().Add(1 * time.Hour)
	service.SendMessage(ctx, sender, recipient, "still_valid", "n2", &futureExpiry)

	// Send a permanent message
	service.SendMessage(ctx, sender, recipient, "permanent", "n3", nil)

	deleted, err := service.CleanupExpiredMessages(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredMessages failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("Expected 1 deleted message, got %d", deleted)
	}

	// Verify remaining messages
	msgs, _ := service.GetConversation(ctx, sender, recipient, 50, 0)
	if len(msgs) != 2 {
		t.Errorf("Expected 2 remaining messages, got %d", len(msgs))
	}
}

func TestSendMessage_RevokedSender(t *testing.T) {
	service, db := setupMessagingTest(t)
	defer db.Close()
	ctx := context.Background()

	sender := createPremiumAgent(t, service, db)
	recipient := createPremiumAgent(t, service, db)

	// Revoke sender's key
	service.RevokeKey(ctx, sender, RevocationReasonSelf, "")

	_, err := service.SendMessage(ctx, sender, recipient, "data", "nonce", nil)
	if err == nil {
		t.Error("Revoked sender should be rejected")
	}
}
