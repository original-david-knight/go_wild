package gowild_agentic_loop

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"google.golang.org/genai"
)

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Retry on 5xx server errors, rate limits, and transient network errors
	retryablePatterns := []string{
		"500", "INTERNAL",
		"502", "BAD_GATEWAY",
		"503", "UNAVAILABLE",
		"429", "RESOURCE_EXHAUSTED",
		"rate limiter",
		"connection reset",
		"connection refused",
		"timeout",
		"deadline exceeded",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "RESOURCE_EXHAUSTED") || strings.Contains(s, "rate limiter")
}

// addJitter applies ±25% randomization to a duration to prevent thundering herd.
func addJitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.75 + rand.Float64()*0.5))
}

// generateWithRetry wraps GenerateContent with exponential backoff retry.
// Rate limit errors (429) get separate, more patient retry handling than other errors.
func (l *AgenticLoop) generateWithRetry(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, maxRetries int) (*GenerateResponse, error) {
	var lastErr error
	normalAttempts := 0
	rateLimitAttempts := 0
	const maxRateLimitRetries = 6
	const rateLimitBaseDelay = 5 * time.Second

	for {
		// Apply per-request timeout if configured
		reqCtx := ctx
		var cancel context.CancelFunc
		if l.responseTimeout > 0 {
			reqCtx, cancel = context.WithTimeout(ctx, l.responseTimeout)
		}

		resp, err := l.client.GenerateContent(reqCtx, contents, config)

		if cancel != nil {
			cancel()
		}

		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Don't retry if parent context is cancelled
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Don't retry non-retryable errors
		if !isRetryableError(err) {
			return nil, err
		}

		var delay time.Duration
		if isRateLimitError(err) {
			rateLimitAttempts++
			if rateLimitAttempts > maxRateLimitRetries {
				break
			}
			// Rate limit backoff: 5s, 10s, 20s, 40s, 60s, 60s
			delay = rateLimitBaseDelay * time.Duration(1<<(rateLimitAttempts-1))
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
		} else {
			normalAttempts++
			if normalAttempts > maxRetries {
				break
			}
			// Server error backoff: 1s, 2s, 4s, ...
			delay = time.Second * time.Duration(1<<(normalAttempts-1))
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}

		// Add jitter to prevent thundering herd
		delay = addJitter(delay)

		// Wait before retrying
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("failed after retries: %w", lastErr)
}

// generate is every turn's generation step. With token streaming off it is
// generateWithRetry unchanged; with it on, deltas flow into the event channel
// as the client hands them over, and the reported bool tells the caller its
// text has already been emitted (lifedash M17).
func (l *AgenticLoop) generate(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, maxRetries int, events chan<- Event) (*GenerateResponse, bool, error) {
	if !l.streamTokens {
		resp, err := l.generateWithRetry(ctx, contents, config, maxRetries)
		return resp, false, err
	}
	resp, err := l.generateStreamingWithRetry(ctx, contents, config, maxRetries, events)
	return resp, true, err
}

// generateStreamingWithRetry retries a streamed generation only while nothing
// has been emitted: once a delta has reached the caller, a retry would replay
// it, so a mid-stream failure is returned as the error it is — the consumer
// shows the factual failure line and keeps the partial text (the M17 error
// path), rather than reading the same sentence twice.
func (l *AgenticLoop) generateStreamingWithRetry(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, maxRetries int, events chan<- Event) (*GenerateResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(addJitter(time.Duration(attempt) * time.Second)):
			}
		}
		emitted := false
		resp, err := l.client.GenerateContentStreaming(ctx, contents, config, func(delta string) {
			emitted = true
			events <- TextDeltaEvent{Text: delta}
		})
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if emitted || ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, lastErr
}
