package gowild_agent_net

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// RateLimiter enforces per-key rate limits.
type RateLimiter struct {
	db gowild_data.Database
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(db gowild_data.Database) *RateLimiter {
	return &RateLimiter{db: db}
}

// CheckLimit checks if a request is within rate limits.
// Returns nil if allowed, error if rate limited.
func (r *RateLimiter) CheckLimit(ctx context.Context, publicKey string, tier AgentTier) error {
	limits := DefaultRateLimits[tier]
	now := time.Now()

	// Check minute limit
	if err := r.checkWindow(ctx, publicKey, WindowTypeMinute, limits.PerMinute, now); err != nil {
		return err
	}

	// Check hour limit
	if err := r.checkWindow(ctx, publicKey, WindowTypeHour, limits.PerHour, now); err != nil {
		return err
	}

	return nil
}

// RecordRequest records a request for rate limiting.
func (r *RateLimiter) RecordRequest(ctx context.Context, publicKey string) error {
	now := time.Now()

	// Increment minute counter
	if err := r.incrementWindow(ctx, publicKey, WindowTypeMinute, now); err != nil {
		return err
	}

	// Increment hour counter
	if err := r.incrementWindow(ctx, publicKey, WindowTypeHour, now); err != nil {
		return err
	}

	return nil
}

// checkWindow checks if a request is within the limit for a specific window.
func (r *RateLimiter) checkWindow(ctx context.Context, publicKey, windowType string, limit int, now time.Time) error {
	id := r.makeID(publicKey, windowType)
	windowDuration := r.getWindowDuration(windowType)
	windowStart := r.getWindowStart(now, windowDuration)

	var rateLimit RateLimit
	err := r.db.Table(RateLimit{}).Get(ctx, id, &rateLimit)
	if err != nil {
		// No record means no requests yet
		return nil
	}

	// Check if this is a current window
	if rateLimit.WindowStart.Before(windowStart) {
		// Old window, reset
		return nil
	}

	// Check limit
	if rateLimit.Count >= limit {
		retryAfter := rateLimit.WindowStart.Add(windowDuration).Sub(now)
		return &RateLimitError{
			Limit:      limit,
			WindowType: windowType,
			RetryAfter: retryAfter,
		}
	}

	return nil
}

// incrementWindow increments the counter for a specific window.
func (r *RateLimiter) incrementWindow(ctx context.Context, publicKey, windowType string, now time.Time) error {
	id := r.makeID(publicKey, windowType)
	windowDuration := r.getWindowDuration(windowType)
	windowStart := r.getWindowStart(now, windowDuration)

	var rateLimit RateLimit
	err := r.db.Table(RateLimit{}).Get(ctx, id, &rateLimit)
	if err != nil {
		// Create new record
		rateLimit = RateLimit{
			ID:          id,
			PublicKey:   publicKey,
			WindowType:  windowType,
			WindowStart: windowStart,
			Count:       1,
		}
		return r.db.Table(RateLimit{}).Insert(ctx, &rateLimit)
	}

	// Check if window has expired
	if rateLimit.WindowStart.Before(windowStart) {
		// Reset window
		rateLimit.WindowStart = windowStart
		rateLimit.Count = 1
	} else {
		// Increment
		rateLimit.Count++
	}

	return r.db.Table(RateLimit{}).Update(ctx, &rateLimit)
}

// makeID creates a unique ID for a rate limit record.
func (r *RateLimiter) makeID(publicKey, windowType string) string {
	data := fmt.Sprintf("%s:%s", publicKey, windowType)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:16])
}

// getWindowDuration returns the duration for a window type.
func (r *RateLimiter) getWindowDuration(windowType string) time.Duration {
	switch windowType {
	case WindowTypeMinute:
		return time.Minute
	case WindowTypeHour:
		return time.Hour
	default:
		return time.Minute
	}
}

// getWindowStart returns the start of the current window.
func (r *RateLimiter) getWindowStart(now time.Time, duration time.Duration) time.Time {
	return now.Truncate(duration)
}

// getRemainingRequests returns the remaining requests for a given tier.
func (r *RateLimiter) getRemainingRequests(ctx context.Context, publicKey string, tier AgentTier) (minuteRemaining, hourRemaining int, err error) {
	limits := DefaultRateLimits[tier]
	now := time.Now()

	minuteRemaining = limits.PerMinute
	hourRemaining = limits.PerHour

	// Check minute window
	minuteID := r.makeID(publicKey, WindowTypeMinute)
	var minuteLimit RateLimit
	if err := r.db.Table(RateLimit{}).Get(ctx, minuteID, &minuteLimit); err == nil {
		windowStart := r.getWindowStart(now, time.Minute)
		if !minuteLimit.WindowStart.Before(windowStart) {
			minuteRemaining = limits.PerMinute - minuteLimit.Count
			if minuteRemaining < 0 {
				minuteRemaining = 0
			}
		}
	}

	// Check hour window
	hourID := r.makeID(publicKey, WindowTypeHour)
	var hourLimit RateLimit
	if err := r.db.Table(RateLimit{}).Get(ctx, hourID, &hourLimit); err == nil {
		windowStart := r.getWindowStart(now, time.Hour)
		if !hourLimit.WindowStart.Before(windowStart) {
			hourRemaining = limits.PerHour - hourLimit.Count
			if hourRemaining < 0 {
				hourRemaining = 0
			}
		}
	}

	return minuteRemaining, hourRemaining, nil
}

// Cleanup removes expired rate limit records.
func (r *RateLimiter) Cleanup(ctx context.Context) (int, error) {
	results, err := r.db.Table(RateLimit{}).GetAll(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	deleted := 0

	for _, result := range results {
		rl, ok := result.(*RateLimit)
		if !ok {
			continue
		}

		// Delete if older than 2 hours
		if now.Sub(rl.WindowStart) > 2*time.Hour {
			if err := r.db.Table(RateLimit{}).Delete(ctx, rl.ID); err == nil {
				deleted++
			}
		}
	}

	return deleted, nil
}

// RateLimitError represents a rate limit violation.
type RateLimitError struct {
	Limit      int
	WindowType string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: %d/%s, retry after %v", e.Limit, e.WindowType, e.RetryAfter)
}

// RetryAfterSeconds returns the retry-after value in seconds.
func (e *RateLimitError) RetryAfterSeconds() int {
	return int(e.RetryAfter.Seconds()) + 1
}
