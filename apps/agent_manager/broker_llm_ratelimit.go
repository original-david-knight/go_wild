package main

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// llmRateLimiter provides shared rate limiting across all agents for Gemini API calls.
// It uses a concurrency semaphore to limit parallel requests and an adaptive backoff
// that activates when Gemini returns 429/RESOURCE_EXHAUSTED errors.
type llmRateLimiter struct {
	mu            sync.Mutex
	sem           chan struct{} // concurrency semaphore
	cooldownUntil time.Time     // global cooldown after 429
	backoffCount  int           // consecutive 429s for exponential backoff
}

func newLLMRateLimiter(maxConcurrent int) *llmRateLimiter {
	return &llmRateLimiter{
		sem: make(chan struct{}, maxConcurrent),
	}
}

// Acquire blocks until a concurrency slot is available and any 429 cooldown has passed.
// Returns a release function that must be called when the request completes.
func (rl *llmRateLimiter) Acquire(ctx context.Context) (release func(), err error) {
	for {
		if err := rl.waitForCooldown(ctx); err != nil {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case rl.sem <- struct{}{}:
		}

		rl.mu.Lock()
		cooldown := rl.cooldownUntil
		rl.mu.Unlock()
		if time.Until(cooldown) <= 0 {
			return func() { <-rl.sem }, nil
		}

		<-rl.sem
	}
}

func (rl *llmRateLimiter) waitForCooldown(ctx context.Context) error {
	for {
		rl.mu.Lock()
		cooldown := rl.cooldownUntil
		rl.mu.Unlock()

		wait := time.Until(cooldown)
		if wait <= 0 {
			return nil
		}

		log.Printf("LLM rate limiter: waiting %.1fs for cooldown", wait.Seconds())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// RecordSuccess resets the consecutive backoff counter.
func (rl *llmRateLimiter) RecordSuccess() {
	rl.mu.Lock()
	rl.backoffCount = 0
	rl.mu.Unlock()
}

// RecordRateLimit sets a global cooldown that affects all agents.
// Uses exponential backoff with jitter: 5s, 10s, 20s, 40s, 60s (capped).
func (rl *llmRateLimiter) RecordRateLimit() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.backoffCount++
	delay := time.Duration(5*(1<<(rl.backoffCount-1))) * time.Second
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	// Add jitter ±25%
	jittered := time.Duration(float64(delay) * (0.75 + rand.Float64()*0.5))
	rl.cooldownUntil = time.Now().Add(jittered)
	log.Printf("LLM rate limiter: 429 received (count=%d), cooldown %.1fs", rl.backoffCount, jittered.Seconds())
}

func isLLMRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") ||
		strings.Contains(s, "resource_exhausted") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit")
}

func isGeminiRateLimitError(err error) bool {
	return isLLMRateLimitError(err)
}
