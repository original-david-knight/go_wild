package deepresearch

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	maxRetries       = 5
	retryBaseDelay   = 3 * time.Second
	retryMaxDelay    = 60 * time.Second
	retryJitterRatio = 0.3
)

type generateContentFn func(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)

// retryOnRateLimit wraps a Gemini generateContent call with
// retry + exponential backoff on 429 / RESOURCE_EXHAUSTED errors.
func retryOnRateLimit(fn generateContentFn) generateContentFn {
	return func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
		resp, err := fn(ctx, model, contents, cfg)
		if err == nil || !isRateLimitError(err) {
			return resp, err
		}

		delay := retryBaseDelay
		for attempt := 1; attempt <= maxRetries; attempt++ {
			jitter := time.Duration(float64(delay) * retryJitterRatio * rand.Float64())
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay + jitter):
			}

			resp, err = fn(ctx, model, contents, cfg)
			if err == nil || !isRateLimitError(err) {
				return resp, err
			}
			delay *= 2
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
		}
		return resp, err
	}
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "quota")
}
