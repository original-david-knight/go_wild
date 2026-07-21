package gowild_my

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Semaphore limits concurrent access to a shared resource.
// It is safe for concurrent use. Callers call Acquire before starting work
// and Release when done. Acquire respects context cancellation.
type Semaphore struct {
	ch chan struct{}
}

// newSemaphore returns a semaphore that allows at most n concurrent holders.
func newSemaphore(n int) *Semaphore {
	if n <= 0 {
		n = 1
	}
	return &Semaphore{ch: make(chan struct{}, n)}
}

// Acquire blocks until a slot is available or the context is cancelled.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a slot. Must be called once for each successful Acquire.
func (s *Semaphore) Release() {
	<-s.ch
}

// EnvSemaphore creates a named semaphore whose capacity is read from an
// environment variable. If the env var is unset or invalid, defaultN is used.
// The semaphore is created once and cached; subsequent calls with the same
// name return the same instance.
func EnvSemaphore(envKey string, defaultN int) *Semaphore {
	envSemaMu.Lock()
	defer envSemaMu.Unlock()

	if s, ok := envSemas[envKey]; ok {
		return s
	}

	n := defaultN
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	s := newSemaphore(n)
	envSemas[envKey] = s
	return s
}

var (
	envSemaMu sync.Mutex
	envSemas  = map[string]*Semaphore{}
)
