package gowild_agent_net

import (
	"context"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

func setupTestDB(t *testing.T) gowild_data.Database {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}
	return db
}

func TestServiceIsPremium(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService(db, nil)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Should not be premium initially
	isPremium, err := service.IsPremium(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if isPremium {
		t.Error("Should not be premium initially")
	}

	// Insert premium agent directly
	agent := &PremiumAgent{
		ID:           pubkey,
		PublicKey:    pubkey,
		TxHash:       "tx123",
		Chain:        ChainSolana,
		UpgradedAt:   time.Now(),
		Revoked:      false,
		LastActiveAt: time.Now(),
	}
	if err := db.Table(PremiumAgent{}).Insert(ctx, agent); err != nil {
		t.Fatalf("Failed to insert agent: %v", err)
	}

	// Should be premium now
	isPremium, err = service.IsPremium(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !isPremium {
		t.Error("Should be premium after insert")
	}

	// Revoke and check again
	agent.Revoked = true
	if err := db.Table(PremiumAgent{}).Update(ctx, agent); err != nil {
		t.Fatalf("Failed to update agent: %v", err)
	}

	isPremium, err = service.IsPremium(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if isPremium {
		t.Error("Should not be premium after revocation")
	}
}

func TestServiceIsKeyRevoked(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService(db, nil)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Should not be revoked initially
	isRevoked, err := service.IsKeyRevoked(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if isRevoked {
		t.Error("Should not be revoked initially")
	}

	// Insert revoked key
	revoked := &RevokedKey{
		ID:        pubkey,
		PublicKey: pubkey,
		RevokedAt: time.Now(),
		Reason:    RevocationReasonSelf,
	}
	if err := db.Table(RevokedKey{}).Insert(ctx, revoked); err != nil {
		t.Fatalf("Failed to insert revoked key: %v", err)
	}

	// Should be revoked now
	isRevoked, err = service.IsKeyRevoked(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !isRevoked {
		t.Error("Should be revoked after insert")
	}
}

func TestServiceCreateAndGetPost(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService(db, nil)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"
	content := "Hello, world!"
	metadata := map[string]any{"tags": []string{"test"}}

	// Create post
	post, err := service.CreatePost(ctx, pubkey, content, VerificationMethodPoW, metadata)
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	if post.ID == "" {
		t.Error("Post ID should not be empty")
	}
	if post.PublicKey != pubkey {
		t.Errorf("Expected public key %s, got %s", pubkey, post.PublicKey)
	}
	if post.Content != content {
		t.Errorf("Expected content %s, got %s", content, post.Content)
	}
	if post.VerificationMethod != VerificationMethodPoW {
		t.Errorf("Expected verification method %s, got %s", VerificationMethodPoW, post.VerificationMethod)
	}

	// Get post
	retrieved, err := service.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}
	if retrieved.Content != content {
		t.Errorf("Expected content %s, got %s", content, retrieved.Content)
	}
}

func TestServiceListPosts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService(db, nil)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Create several posts
	for i := 0; i < 5; i++ {
		_, err := service.CreatePost(ctx, pubkey, "Post content", VerificationMethodPoW, nil)
		if err != nil {
			t.Fatalf("Failed to create post: %v", err)
		}
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// List with limit
	posts, err := service.listPosts(ctx, 3, 0)
	if err != nil {
		t.Fatalf("Failed to list posts: %v", err)
	}
	if len(posts) != 3 {
		t.Errorf("Expected 3 posts, got %d", len(posts))
	}

	// List with offset
	posts, err = service.listPosts(ctx, 10, 2)
	if err != nil {
		t.Fatalf("Failed to list posts: %v", err)
	}
	if len(posts) != 3 {
		t.Errorf("Expected 3 posts (5-2), got %d", len(posts))
	}
}

func TestServiceRevokeKey(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService(db, nil)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Insert premium agent first
	agent := &PremiumAgent{
		ID:           pubkey,
		PublicKey:    pubkey,
		TxHash:       "tx123",
		Chain:        ChainSolana,
		UpgradedAt:   time.Now(),
		Revoked:      false,
		LastActiveAt: time.Now(),
	}
	if err := db.Table(PremiumAgent{}).Insert(ctx, agent); err != nil {
		t.Fatalf("Failed to insert agent: %v", err)
	}

	// Revoke key
	if err := service.RevokeKey(ctx, pubkey, RevocationReasonSelf, ""); err != nil {
		t.Fatalf("Failed to revoke key: %v", err)
	}

	// Check key is revoked
	isRevoked, err := service.IsKeyRevoked(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !isRevoked {
		t.Error("Key should be revoked")
	}

	// Check premium agent is also revoked
	isPremium, err := service.IsPremium(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if isPremium {
		t.Error("Premium status should be revoked")
	}
}

func TestServiceGetAgentTier(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService(db, nil)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Should be free tier initially
	tier, err := service.GetAgentTier(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tier != TierFree {
		t.Errorf("Expected TierFree, got %v", tier)
	}

	// Insert premium agent
	agent := &PremiumAgent{
		ID:           pubkey,
		PublicKey:    pubkey,
		TxHash:       "tx123",
		Chain:        ChainSolana,
		UpgradedAt:   time.Now(),
		Revoked:      false,
		LastActiveAt: time.Now(),
	}
	if err := db.Table(PremiumAgent{}).Insert(ctx, agent); err != nil {
		t.Fatalf("Failed to insert agent: %v", err)
	}

	// Should be premium tier now
	tier, err = service.GetAgentTier(ctx, pubkey)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tier != TierPremium {
		t.Errorf("Expected TierPremium, got %v", tier)
	}
}
