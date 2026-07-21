package gowild_agent_net

import (
	"context"
	"testing"
)

func TestRateLimiterCheckAndRecord(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	limiter := NewRateLimiter(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// First request should pass
	if err := limiter.CheckLimit(ctx, pubkey, TierFree); err != nil {
		t.Errorf("First request should pass: %v", err)
	}

	// Record it
	if err := limiter.RecordRequest(ctx, pubkey); err != nil {
		t.Fatalf("Failed to record request: %v", err)
	}

	// Second request should fail for free tier (1/min limit)
	if err := limiter.CheckLimit(ctx, pubkey, TierFree); err == nil {
		t.Error("Second request should fail for free tier within same minute")
	}

	// Premium tier should still pass
	if err := limiter.CheckLimit(ctx, pubkey, TierPremium); err != nil {
		t.Errorf("Premium tier should allow more requests: %v", err)
	}
}

func TestRateLimiterPremiumLimits(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	limiter := NewRateLimiter(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Record many requests (within premium limits)
	for i := 0; i < 50; i++ {
		if err := limiter.RecordRequest(ctx, pubkey); err != nil {
			t.Fatalf("Failed to record request: %v", err)
		}
	}

	// Should still pass for premium
	if err := limiter.CheckLimit(ctx, pubkey, TierPremium); err != nil {
		t.Errorf("Premium tier should allow 50 requests: %v", err)
	}
}

func TestRateLimitError(t *testing.T) {
	err := &RateLimitError{
		Limit:      1,
		WindowType: WindowTypeMinute,
		RetryAfter: 45_000_000_000, // 45 seconds in nanoseconds
	}

	if err.RetryAfterSeconds() != 46 { // 45 + 1
		t.Errorf("Expected RetryAfterSeconds 46, got %d", err.RetryAfterSeconds())
	}

	if err.Error() == "" {
		t.Error("Error message should not be empty")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	limiter := NewRateLimiter(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Record a request to create rate limit records
	if err := limiter.RecordRequest(ctx, pubkey); err != nil {
		t.Fatalf("Failed to record request: %v", err)
	}

	// Cleanup should run without error (though won't delete anything fresh)
	deleted, err := limiter.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Fresh records shouldn't be deleted
	if deleted != 0 {
		t.Errorf("Expected 0 deleted, got %d", deleted)
	}
}

func TestRateLimiterGetRemainingRequests(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	limiter := NewRateLimiter(db)
	ctx := context.Background()

	pubkey := "testpubkey123456789012345678901234567890123"

	// Initially should have full limits
	minuteRemaining, hourRemaining, err := limiter.getRemainingRequests(ctx, pubkey, TierFree)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if minuteRemaining != 1 {
		t.Errorf("Expected 1 minute remaining for free tier, got %d", minuteRemaining)
	}
	if hourRemaining != 10 {
		t.Errorf("Expected 10 hour remaining for free tier, got %d", hourRemaining)
	}

	// Record a request
	if err := limiter.RecordRequest(ctx, pubkey); err != nil {
		t.Fatalf("Failed to record request: %v", err)
	}

	// Should have fewer remaining
	minuteRemaining, hourRemaining, err = limiter.getRemainingRequests(ctx, pubkey, TierFree)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if minuteRemaining != 0 {
		t.Errorf("Expected 0 minute remaining after 1 request, got %d", minuteRemaining)
	}
	if hourRemaining != 9 {
		t.Errorf("Expected 9 hour remaining after 1 request, got %d", hourRemaining)
	}
}
