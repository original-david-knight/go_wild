package gowild_my

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemaphoreBasic(t *testing.T) {
	s := newSemaphore(2)
	ctx := context.Background()

	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	// Third acquire should block — verify with a short timeout.
	tctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := s.Acquire(tctx); err == nil {
		t.Fatal("third acquire should have blocked")
	}

	s.Release()

	// Now a slot is free.
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	s.Release()
	s.Release()
}

func TestSemaphoreConcurrency(t *testing.T) {
	const limit = 3
	s := newSemaphore(limit)
	ctx := context.Background()

	var peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Acquire(ctx); err != nil {
				return
			}
			cur := peak.Add(1)
			if cur > int32(limit) {
				t.Errorf("concurrent holders %d exceeds limit %d", cur, limit)
			}
			time.Sleep(5 * time.Millisecond)
			peak.Add(-1)
			s.Release()
		}()
	}
	wg.Wait()
}

func TestSemaphoreContextCancellation(t *testing.T) {
	s := newSemaphore(1)
	ctx := context.Background()

	if err := s.Acquire(ctx); err != nil {
		t.Fatal(err)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Acquire(cctx); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	s.Release()
}

func TestEnvSemaphore(t *testing.T) {
	// Clear any cached semaphores from prior tests.
	envSemaMu.Lock()
	delete(envSemas, "TEST_SEMA_LIMIT")
	envSemaMu.Unlock()

	t.Setenv("TEST_SEMA_LIMIT", "2")
	s := EnvSemaphore("TEST_SEMA_LIMIT", 10)
	ctx := context.Background()

	if err := s.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := s.Acquire(tctx); err == nil {
		t.Fatal("should have blocked at capacity 2")
	}
	s.Release()
	s.Release()

	// Same key returns same instance.
	s2 := EnvSemaphore("TEST_SEMA_LIMIT", 10)
	if s != s2 {
		t.Fatal("expected cached instance")
	}
}

func TestEnvSemaphoreDefault(t *testing.T) {
	envSemaMu.Lock()
	delete(envSemas, "TEST_SEMA_UNSET")
	envSemaMu.Unlock()

	t.Setenv("TEST_SEMA_UNSET", "")
	s := EnvSemaphore("TEST_SEMA_UNSET", 1)
	ctx := context.Background()

	if err := s.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := s.Acquire(tctx); err == nil {
		t.Fatal("should have blocked at default capacity 1")
	}
	s.Release()
}
