package main

import (
	"testing"
	"time"
)

func TestCheckWriteRateLimit_AllowsUnderLimit(t *testing.T) {
	h := NewBrokerWalletHandler(nil)

	for i := 0; i < 10; i++ {
		if !h.checkWriteRateLimit("agent1") {
			t.Fatalf("expected rate limit to allow request %d", i+1)
		}
	}
}

func TestCheckWriteRateLimit_BlocksOverLimit(t *testing.T) {
	h := NewBrokerWalletHandler(nil)

	// Fill up the limit
	for i := 0; i < 10; i++ {
		h.checkWriteRateLimit("agent1")
	}

	// 11th request should be blocked
	if h.checkWriteRateLimit("agent1") {
		t.Error("expected rate limit to block 11th request")
	}
}

func TestCheckWriteRateLimit_PerAgent(t *testing.T) {
	h := NewBrokerWalletHandler(nil)

	// Fill up agent1
	for i := 0; i < 10; i++ {
		h.checkWriteRateLimit("agent1")
	}

	// agent2 should still be allowed
	if !h.checkWriteRateLimit("agent2") {
		t.Error("expected rate limit to allow different agent")
	}
}

func TestCheckWriteRateLimit_PrunesOldEntries(t *testing.T) {
	h := NewBrokerWalletHandler(nil)

	// Manually insert old timestamps (older than 1 hour)
	h.mu.Lock()
	oldTime := time.Now().Add(-2 * time.Hour)
	h.writeCounts["agent1"] = []time.Time{
		oldTime, oldTime, oldTime, oldTime, oldTime,
		oldTime, oldTime, oldTime, oldTime, oldTime,
	}
	h.mu.Unlock()

	// Should be allowed since old entries get pruned
	if !h.checkWriteRateLimit("agent1") {
		t.Error("expected rate limit to allow after pruning old entries")
	}

	// Verify old entries were pruned
	h.mu.Lock()
	count := len(h.writeCounts["agent1"])
	h.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 entry after pruning, got %d", count)
	}
}

func TestCheckWriteRateLimit_MixedOldAndNew(t *testing.T) {
	h := NewBrokerWalletHandler(nil)

	h.mu.Lock()
	now := time.Now()
	oldTime := now.Add(-2 * time.Hour)
	// 5 old + 5 recent = 10 total, but only 5 should count
	h.writeCounts["agent1"] = []time.Time{
		oldTime, oldTime, oldTime, oldTime, oldTime,
		now.Add(-30 * time.Minute), now.Add(-20 * time.Minute),
		now.Add(-10 * time.Minute), now.Add(-5 * time.Minute),
		now.Add(-1 * time.Minute),
	}
	h.mu.Unlock()

	// Should be allowed since only 5 recent entries exist
	if !h.checkWriteRateLimit("agent1") {
		t.Error("expected rate limit to allow when only 5 recent entries exist")
	}
}
