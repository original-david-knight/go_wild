package gowild_my

import (
	"context"
	"sync"
	"time"
)

// QPSLimiter enforces a maximum queries-per-second rate.
// It is safe for concurrent use. Callers call Wait before making a request;
// Wait sleeps only if the minimum interval since the last request hasn't elapsed.
type QPSLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	last     time.Time
}

// NewQPSLimiter returns a limiter that allows at most qps requests per second.
// For example, NewQPSLimiter(1) allows 1 request per second.
func NewQPSLimiter(qps float64) *QPSLimiter {
	if qps <= 0 {
		qps = 1
	}
	return &QPSLimiter{
		interval: time.Duration(float64(time.Second) / qps),
	}
}

// Wait blocks until the rate limit allows the next request.
// It respects context cancellation and returns ctx.Err() if the
// context is cancelled while waiting.
func (l *QPSLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	next := l.last.Add(l.interval)
	if now.Before(next) {
		delay := next.Sub(now)
		l.last = next
		l.mu.Unlock()

		select {
		case <-time.After(delay):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.last = now
	l.mu.Unlock()
	return nil
}
