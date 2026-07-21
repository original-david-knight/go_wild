package gowild_agent_net

import (
	"context"
	"testing"
	"time"
)

func TestNonceTrackerCheckAndRecord(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewNonceTracker(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"
	nonce := "testnonce123"
	timestamp := time.Now()

	// First use should succeed
	if err := tracker.CheckAndRecord(ctx, pubkey, nonce, timestamp); err != nil {
		t.Fatalf("First nonce use should succeed: %v", err)
	}

	// Second use should fail (replay attack)
	if err := tracker.CheckAndRecord(ctx, pubkey, nonce, timestamp); err == nil {
		t.Error("Second nonce use should fail (replay attack)")
	}

	// Different nonce should succeed
	if err := tracker.CheckAndRecord(ctx, pubkey, "different1", timestamp); err != nil {
		t.Fatalf("Different nonce should succeed: %v", err)
	}

	// Same nonce with different timestamp should succeed
	differentTime := timestamp.Add(time.Hour)
	if err := tracker.CheckAndRecord(ctx, pubkey, nonce, differentTime); err != nil {
		t.Fatalf("Same nonce with different timestamp should succeed: %v", err)
	}
}

func TestNonceTrackerIsUsed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewNonceTracker(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"
	nonce := "testnonce123"
	timestamp := time.Now()

	// Should not be used initially
	if tracker.isUsed(ctx, pubkey, nonce, timestamp) {
		t.Error("Nonce should not be used initially")
	}

	// Record it
	if err := tracker.CheckAndRecord(ctx, pubkey, nonce, timestamp); err != nil {
		t.Fatalf("Failed to record nonce: %v", err)
	}

	// Should be used now
	if !tracker.isUsed(ctx, pubkey, nonce, timestamp) {
		t.Error("Nonce should be used after recording")
	}
}

func TestNonceTrackerCleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewNonceTracker(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Record a nonce with current time
	currentTime := time.Now()
	if err := tracker.CheckAndRecord(ctx, pubkey, "currentnonce", currentTime); err != nil {
		t.Fatalf("Failed to record nonce: %v", err)
	}

	// Cleanup shouldn't delete fresh nonces
	deleted, err := tracker.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted for fresh nonces, got %d", deleted)
	}

	// Insert an expired nonce directly
	expiredNonce := &UsedNonce{
		ID:        "expirednonce",
		PublicKey: pubkey,
		Nonce:     "expirednonce",
		Timestamp: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(-30 * time.Minute), // Already expired
	}
	if err := db.Table(UsedNonce{}).Insert(ctx, expiredNonce); err != nil {
		t.Fatalf("Failed to insert expired nonce: %v", err)
	}

	// Cleanup should delete the expired nonce
	deleted, err = tracker.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1 deleted for expired nonces, got %d", deleted)
	}
}

func TestNonceTTL(t *testing.T) {
	// Verify the TTL constant is set correctly
	if NonceTTL != 10*time.Minute {
		t.Errorf("Expected NonceTTL to be 10 minutes, got %v", NonceTTL)
	}
}
