package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLLMRateLimiter_ConcurrencyLimit(t *testing.T) {
	rl := newLLMRateLimiter(2)
	var active atomic.Int32
	var maxActive atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := rl.Acquire(context.Background())
			if err != nil {
				t.Errorf("Acquire failed: %v", err)
				return
			}
			cur := active.Add(1)
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			release()
		}()
	}

	wg.Wait()
	if got := maxActive.Load(); got > 2 {
		t.Fatalf("expected max 2 concurrent, got %d", got)
	}
}

func TestLLMRateLimiter_CooldownAfterRateLimit(t *testing.T) {
	rl := newLLMRateLimiter(5)

	// Record a rate limit — sets cooldown
	rl.RecordRateLimit()

	rl.mu.Lock()
	cooldown := rl.cooldownUntil
	rl.mu.Unlock()

	if time.Until(cooldown) <= 0 {
		t.Fatalf("expected future cooldown, got %v", cooldown)
	}
	// First 429 should set ~3.75-6.25s cooldown (5s ± 25%)
	wait := time.Until(cooldown)
	if wait < 3*time.Second || wait > 7*time.Second {
		t.Fatalf("expected ~5s cooldown, got %.1fs", wait.Seconds())
	}
}

func TestLLMRateLimiter_SuccessResetsCooldown(t *testing.T) {
	rl := newLLMRateLimiter(5)
	rl.RecordRateLimit()
	rl.RecordSuccess()

	rl.mu.Lock()
	count := rl.backoffCount
	rl.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected backoff count 0 after success, got %d", count)
	}
}

func TestLLMRateLimiter_ExponentialBackoff(t *testing.T) {
	rl := newLLMRateLimiter(5)

	// Record multiple rate limits — backoff should increase
	rl.RecordRateLimit() // 5s base
	rl.RecordRateLimit() // 10s base
	rl.RecordRateLimit() // 20s base

	rl.mu.Lock()
	cooldown := rl.cooldownUntil
	rl.mu.Unlock()

	wait := time.Until(cooldown)
	// Third 429: 20s ± 25% = 15-25s
	if wait < 14*time.Second || wait > 26*time.Second {
		t.Fatalf("expected ~20s cooldown after 3 rate limits, got %.1fs", wait.Seconds())
	}
}

func TestLLMRateLimiter_CooldownCapped(t *testing.T) {
	rl := newLLMRateLimiter(5)

	// Many rate limits should cap at 60s
	for i := 0; i < 10; i++ {
		rl.RecordRateLimit()
	}

	rl.mu.Lock()
	cooldown := rl.cooldownUntil
	rl.mu.Unlock()

	wait := time.Until(cooldown)
	// 60s ± 25% = 45-75s
	if wait > 76*time.Second {
		t.Fatalf("expected cooldown capped at ~60s, got %.1fs", wait.Seconds())
	}
}

func TestLLMRateLimiter_AcquireCancelledDuringCooldown(t *testing.T) {
	rl := newLLMRateLimiter(5)

	// Set a long cooldown
	rl.mu.Lock()
	rl.cooldownUntil = time.Now().Add(10 * time.Second)
	rl.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := rl.Acquire(ctx)
	if err == nil {
		t.Fatal("expected context error during cooldown wait")
	}
}

func TestLLMRateLimiter_AcquireWaitsForExtendedCooldown(t *testing.T) {
	rl := newLLMRateLimiter(1)
	rl.mu.Lock()
	rl.cooldownUntil = time.Now().Add(60 * time.Millisecond)
	rl.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		release, err := rl.Acquire(context.Background())
		if err == nil {
			release()
		}
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	rl.mu.Lock()
	rl.cooldownUntil = time.Now().Add(140 * time.Millisecond)
	rl.mu.Unlock()

	select {
	case err := <-done:
		t.Fatalf("Acquire returned before extended cooldown elapsed: %v", err)
	case <-time.After(70 * time.Millisecond):
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Acquire failed after cooldown elapsed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Acquire did not finish after extended cooldown elapsed")
	}
}

func TestLLMRateLimiter_AcquireRechecksCooldownAfterSemaphoreWait(t *testing.T) {
	rl := newLLMRateLimiter(1)

	release, err := rl.Acquire(context.Background())
	if err != nil {
		t.Fatalf("initial Acquire failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		release, err := rl.Acquire(context.Background())
		if err == nil {
			release()
		}
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	rl.mu.Lock()
	rl.cooldownUntil = time.Now().Add(140 * time.Millisecond)
	rl.mu.Unlock()
	release()

	select {
	case err := <-done:
		t.Fatalf("Acquire returned before semaphore-triggered cooldown elapsed: %v", err)
	case <-time.After(70 * time.Millisecond):
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Acquire failed after semaphore-triggered cooldown elapsed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Acquire did not finish after semaphore-triggered cooldown elapsed")
	}
}

func TestIsGeminiRateLimitError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("some random error"), false},
		{fmt.Errorf("429 RESOURCE_EXHAUSTED"), true},
		{fmt.Errorf("status 429: too many requests"), true},
		{fmt.Errorf("RESOURCE_EXHAUSTED: please slow down"), true},
		{fmt.Errorf("502 BAD_GATEWAY"), false},
	}
	for _, tt := range tests {
		if got := isGeminiRateLimitError(tt.err); got != tt.want {
			t.Errorf("isGeminiRateLimitError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
