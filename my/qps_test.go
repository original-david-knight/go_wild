package gowild_my

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestQPSLimiterEnforcesRate(t *testing.T) {
	limiter := NewQPSLimiter(10) // 10 QPS = 100ms between calls
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := limiter.Wait(ctx); err != nil {
			t.Fatalf("Wait returned error: %v", err)
		}
	}
	elapsed := time.Since(start)

	// 5 calls at 10 QPS: first is immediate, 4 gaps of 100ms = 400ms minimum
	if elapsed < 350*time.Millisecond {
		t.Fatalf("expected at least 350ms for 5 calls at 10 QPS, got %v", elapsed)
	}
}

func TestQPSLimiterConcurrent(t *testing.T) {
	limiter := NewQPSLimiter(10) // 10 QPS
	ctx := context.Background()

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Wait(ctx); err != nil {
				t.Errorf("Wait returned error: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 5 concurrent calls should still be serialized to ~400ms
	if elapsed < 350*time.Millisecond {
		t.Fatalf("concurrent calls should be rate-limited, got %v", elapsed)
	}
}

func TestQPSLimiterContextCancel(t *testing.T) {
	limiter := NewQPSLimiter(1) // 1 QPS
	ctx := context.Background()

	// First call goes through immediately.
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("first Wait failed: %v", err)
	}

	// Cancel context before second call completes.
	ctx2, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.Wait(ctx2); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
