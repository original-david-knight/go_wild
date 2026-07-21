package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestClientCache_ReusesClient(t *testing.T) {
	callCount := 0
	client := &polymarket.Client{}

	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(_ context.Context, _ string) (*polymarket.Client, error) {
		callCount++
		return client, nil
	}

	// First call — should invoke getClientFn
	polymarketToolHandler(h, httptest.NewRecorder(), newPolyReq(t, "agent-1", `{"token_id":"t1"}`), func(c *polymarket.Client, input polymarket.OrderBook) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	if callCount != 1 {
		t.Fatalf("expected 1 client creation, got %d", callCount)
	}

	// Second call — getClientFn uses test seam, always invoked (no caching for test seam path)
	polymarketToolHandler(h, httptest.NewRecorder(), newPolyReq(t, "agent-1", `{"token_id":"t1"}`), func(c *polymarket.Client, input polymarket.OrderBook) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	if callCount != 2 {
		t.Fatalf("test seam path should always call getClientFn, got %d", callCount)
	}
}

func TestGetCachedClient_HitAndExpiry(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)

	client := &polymarket.Client{}

	// No cache yet
	h.mu.Lock()
	_, ok := h.getCachedClient("company-1")
	h.mu.Unlock()
	if ok {
		t.Fatal("expected cache miss")
	}

	// Store a client
	h.mu.Lock()
	h.cacheClient("company-1", client)
	h.mu.Unlock()

	// Cache hit
	h.mu.Lock()
	got, ok := h.getCachedClient("company-1")
	h.mu.Unlock()
	if !ok || got != client {
		t.Fatal("expected cache hit with same client")
	}

	// Simulate expiry by backdating
	h.mu.Lock()
	h.clientCache["company-1"].createdAt = time.Now().Add(-polymarketClientCacheTTL - time.Second)
	_, ok = h.getCachedClient("company-1")
	h.mu.Unlock()
	if ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestAuthErrorCooldown_PreventsRetry(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)

	testErr := errors.New("failed to derive API credentials: POST /auth/api-key failed with status 401: API key disabled, reason: tenant disabled")

	// No error initially
	h.mu.Lock()
	err := h.checkAuthErrorCooldown("company-1")
	h.mu.Unlock()
	if err != nil {
		t.Fatal("expected no cached error initially")
	}

	// Record an auth error
	h.mu.Lock()
	h.recordAuthError("company-1", testErr)
	h.mu.Unlock()

	// Should return cached error
	h.mu.Lock()
	err = h.checkAuthErrorCooldown("company-1")
	h.mu.Unlock()
	if err == nil {
		t.Fatal("expected cached auth error")
	}
	if !strings.Contains(err.Error(), "tenant disabled") {
		t.Fatalf("expected tenant disabled in error, got: %v", err)
	}

	// Simulate cooldown expiry
	h.mu.Lock()
	h.authErrors["company-1"].failedAt = time.Now().Add(-polymarketAuthErrorCooldown - time.Second)
	err = h.checkAuthErrorCooldown("company-1")
	h.mu.Unlock()
	if err != nil {
		t.Fatal("expected cooldown expired, but got error")
	}
}

func TestIsPolymarketAuthError(t *testing.T) {
	tests := []struct {
		err  string
		want bool
	}{
		{"API key disabled, reason: tenant disabled", true},
		{"POST /auth/api-key failed with status 401: forbidden", true},
		{"GET /auth/derive-api-key failed with status 403: access denied", true},
		{"failed to create polymarket client: connection timeout", false},
		{"failed to derive keys: bad mnemonic", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isPolymarketAuthError(errors.New(tt.err))
		if got != tt.want {
			t.Errorf("isPolymarketAuthError(%q) = %v, want %v", tt.err, got, tt.want)
		}
	}

	if isPolymarketAuthError(nil) {
		t.Error("isPolymarketAuthError(nil) should be false")
	}
}

func TestShouldCachePolymarketClient(t *testing.T) {
	tests := []struct {
		name    string
		authErr error
		want    bool
	}{
		{
			name:    "healthy client",
			authErr: nil,
			want:    true,
		},
		{
			name:    "permanent auth degradation",
			authErr: errors.New("CLOB API authentication failed: POST /auth/api-key failed with status 401: API key disabled, reason: tenant disabled"),
			want:    true,
		},
		{
			name:    "transient proxy failure",
			authErr: errors.New("CLOB API authentication failed: GET /auth/derive-api-key request failed: Get \"https://clob.polymarket.com/auth/derive-api-key\": socks connect tcp 127.0.0.1:1080->clob.polymarket.com:443: EOF"),
			want:    false,
		},
	}

	for _, tt := range tests {
		if got := shouldCachePolymarketClient(tt.authErr); got != tt.want {
			t.Errorf("%s: shouldCachePolymarketClient() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPolymarketToolHandler_AuthError_Returns503(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(_ context.Context, _ string) (*polymarket.Client, error) {
		return nil, errors.New("failed to derive API credentials: POST /auth/api-key failed with status 401: API key disabled, reason: tenant disabled")
	}

	rec := httptest.NewRecorder()
	polymarketToolHandler(h, rec, newPolyReq(t, "agent-1", `{"token_id":"t1"}`), func(c *polymarket.Client, input polymarket.OrderBook) (map[string]any, error) {
		return nil, nil
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tenant disabled") {
		t.Fatalf("expected tenant disabled in body, got: %s", rec.Body.String())
	}
}

func TestPolymarketToolHandler_NonAuthError_Returns500(t *testing.T) {
	h := NewBrokerPolymarketHandler(nil)
	h.getClientFn = func(_ context.Context, _ string) (*polymarket.Client, error) {
		return nil, errors.New("connection timeout")
	}

	rec := httptest.NewRecorder()
	polymarketToolHandler(h, rec, newPolyReq(t, "agent-1", `{"token_id":"t1"}`), func(c *polymarket.Client, input polymarket.OrderBook) (map[string]any, error) {
		return nil, nil
	})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

// newPolyReq is a helper to create a POST request with broker agent ID in context.
func newPolyReq(t *testing.T, agentID, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/polymarket/test", strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, agentID))
}
