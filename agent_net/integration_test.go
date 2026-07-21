package gowild_agent_net

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// TestFullPostingFlow tests the complete flow of an agent posting with PoW.
func TestFullPostingFlow(t *testing.T) {
	// Setup
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Step 1: Generate agent keypair
	pubkey, privkey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}
	agentID := EncodePublicKey(pubkey)

	// Step 2: Verify agent is free tier (not premium)
	tier, _ := service.GetAgentTier(ctx, agentID)
	if tier != TierFree {
		t.Errorf("New agent should be free tier")
	}

	// Step 3: Prepare post content
	content := "Hello from AI agent!"
	timestamp := time.Now().UTC().Format(time.RFC3339)
	body := []byte(fmt.Sprintf(`{"content":"%s"}`, content))

	// Step 4: Create signature
	signature := SignRequest(privkey, "POST", "/api/v1/posts", timestamp, body)
	sigEncoded := EncodeSignature(signature)

	// Verify signature works
	if !VerifySignature(pubkey, "POST", "/api/v1/posts", timestamp, body, signature) {
		t.Fatal("Signature verification failed")
	}

	// Step 5: Check nonce tracking
	nonce := fmt.Sprintf("%016x", rand.Int63())
	ts, _ := time.Parse(time.RFC3339, timestamp)
	if err := service.CheckNonce(ctx, agentID, nonce, ts); err != nil {
		t.Fatalf("First nonce check should succeed: %v", err)
	}

	// Second use of same nonce should fail
	if err := service.CheckNonce(ctx, agentID, nonce, ts); err == nil {
		t.Error("Replay nonce should fail")
	}

	// Step 6: Check rate limiting
	if err := service.CheckRateLimit(ctx, agentID, TierFree); err != nil {
		t.Fatalf("First request should pass rate limit: %v", err)
	}

	// Record request
	if err := service.RecordRequest(ctx, agentID); err != nil {
		t.Fatalf("Failed to record request: %v", err)
	}

	// Second request should be rate limited
	if err := service.CheckRateLimit(ctx, agentID, TierFree); err == nil {
		t.Error("Second request should be rate limited for free tier")
	}

	// Step 7: Create post
	post, err := service.CreatePost(ctx, agentID, content, VerificationMethodPoW, nil)
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	if post.VerificationMethod != VerificationMethodPoW {
		t.Errorf("Expected PoW verification, got %s", post.VerificationMethod)
	}

	// Step 8: Retrieve post
	retrieved, err := service.GetPost(ctx, post.ID)
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	if retrieved.Content != content {
		t.Errorf("Content mismatch: expected %s, got %s", content, retrieved.Content)
	}

	// Verify we haven't used the signature/encoded values to avoid compiler warnings
	if len(sigEncoded) != 86 {
		t.Errorf("Unexpected signature length: %d", len(sigEncoded))
	}
}

// TestPoWMining tests the PoW mining and verification process.
func TestPoWMining(t *testing.T) {
	content := `{"content":"Test post"}`
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Get canonical JSON
	contentJSON, err := CanonicalJSON(content)
	if err != nil {
		t.Fatalf("Failed to canonicalize JSON: %v", err)
	}

	// Test mining with low difficulty (1 bit) for fast test
	difficulty := 1
	found := false

	for i := 0; i < 100; i++ {
		nonce := fmt.Sprintf("%016x", rand.Int63())

		// Compute challenge
		challenge := ComputePoWChallenge(contentJSON, timestamp, nonce)
		hash := ComputePoWHash(challenge)

		if CountLeadingZeroBits(hash) >= difficulty {
			// Verify the PoW
			powHashHex := hex.EncodeToString(hash)
			valid, err := VerifyPoW(powHashHex, contentJSON, timestamp, nonce, difficulty)
			if err != nil {
				t.Fatalf("PoW verification error: %v", err)
			}
			if !valid {
				t.Error("Valid PoW should verify")
			}
			found = true
			break
		}
	}

	if !found {
		t.Error("Failed to find valid PoW in 100 attempts (difficulty 1)")
	}
}

// TestPremiumUpgradeFlow tests the premium upgrade flow (without actual blockchain).
func TestPremiumUpgradeFlow(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	// Service without blockchain verifier (will skip verification)
	service := NewService(db, nil)
	ctx := context.Background()

	// Generate agent
	pubkey, _, _ := GenerateKeyPair()
	agentID := EncodePublicKey(pubkey)

	// Verify free tier
	tier, _ := service.GetAgentTier(ctx, agentID)
	if tier != TierFree {
		t.Error("Should start as free tier")
	}

	// Upgrade (no blockchain verification in test)
	txHash := "test_tx_hash_12345"
	if err := service.UpgradeAccount(ctx, agentID, txHash, ChainSolana); err != nil {
		t.Fatalf("Upgrade failed: %v", err)
	}

	// Verify premium tier
	tier, _ = service.GetAgentTier(ctx, agentID)
	if tier != TierPremium {
		t.Error("Should be premium after upgrade")
	}

	// Verify can't upgrade again
	if err := service.UpgradeAccount(ctx, agentID, "new_tx", ChainSolana); err == nil {
		t.Error("Should not be able to upgrade again")
	}

	// Verify can't reuse tx_hash
	pubkey2, _, _ := GenerateKeyPair()
	agentID2 := EncodePublicKey(pubkey2)
	if err := service.UpgradeAccount(ctx, agentID2, txHash, ChainSolana); err == nil {
		t.Error("Should not be able to reuse tx_hash")
	}

	// Test premium rate limits (higher)
	for i := 0; i < 50; i++ {
		if err := service.RecordRequest(ctx, agentID); err != nil {
			t.Fatalf("Failed to record request: %v", err)
		}
	}
	if err := service.CheckRateLimit(ctx, agentID, TierPremium); err != nil {
		t.Error("Premium should allow 50+ requests per minute")
	}
}

// TestKeyRevocationFlow tests the key revocation process.
func TestKeyRevocationFlow(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Generate and upgrade agent
	pubkey, _, _ := GenerateKeyPair()
	agentID := EncodePublicKey(pubkey)

	service.UpgradeAccount(ctx, agentID, "tx123", ChainSolana)

	// Verify premium
	tier, _ := service.GetAgentTier(ctx, agentID)
	if tier != TierPremium {
		t.Error("Should be premium")
	}

	// Revoke key
	if err := service.RevokeKey(ctx, agentID, RevocationReasonSelf, ""); err != nil {
		t.Fatalf("Revocation failed: %v", err)
	}

	// Verify key is revoked
	isRevoked, _ := service.IsKeyRevoked(ctx, agentID)
	if !isRevoked {
		t.Error("Key should be revoked")
	}

	// Verify no longer premium
	isPremium, _ := service.IsPremium(ctx, agentID)
	if isPremium {
		t.Error("Should not be premium after revocation")
	}

	// Verify can't upgrade revoked key
	if err := service.UpgradeAccount(ctx, agentID, "newtx", ChainSolana); err == nil {
		t.Error("Should not be able to upgrade revoked key")
	}
}

// TestDifficultyAdjustment tests dynamic difficulty based on load.
func TestDifficultyAdjustment(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Get initial difficulty
	diff, err := service.GetCurrentDifficulty(ctx)
	if err != nil {
		t.Fatalf("Failed to get difficulty: %v", err)
	}

	if diff.BaseDifficulty != DefaultBaseDifficulty {
		t.Errorf("Expected base difficulty %d, got %d", DefaultBaseDifficulty, diff.BaseDifficulty)
	}

	// With no posts, current should equal base
	if diff.CurrentDifficulty != diff.BaseDifficulty {
		t.Errorf("With no posts, current should equal base difficulty")
	}

	if diff.PostsLastHour != 0 {
		t.Errorf("Expected 0 posts, got %d", diff.PostsLastHour)
	}
}

// TestCleanup tests the cleanup functionality.
func TestCleanup(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Run cleanup (should not fail even with empty database)
	noncesDeleted, rateLimitsDeleted, err := service.cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	if noncesDeleted != 0 || rateLimitsDeleted != 0 {
		t.Errorf("Fresh database should have nothing to clean up")
	}
}
