package gowild_polymarket

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

// TestNewClient_DegradedMode verifies that when the CLOB auth endpoint rejects
// credentials (e.g. "tenant disabled"), NewClient still returns a usable client
// for public API and on-chain operations.
func TestNewClient_DegradedMode(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Simulate a degraded client by creating the struct directly with credsErr set.
	c := &Client{
		privateKey: key,
		address:    "0x1234567890abcdef1234567890abcdef12345678",
		funder:     "0x1234567890abcdef1234567890abcdef12345678",
		chainID:    polygonChainID,
		credsErr:   errors.New("CLOB API authentication failed: POST /auth/api-key failed with status 401: API key disabled, reason: tenant disabled"),
	}

	// Client should exist but have no CLOB auth
	if c.hasCLOBAuth() {
		t.Fatal("expected hasCLOBAuth() = false for degraded client")
	}
	if c.CLOBAuthError() == nil {
		t.Fatal("expected CLOBAuthError() to be non-nil")
	}
	if !strings.Contains(c.CLOBAuthError().Error(), "tenant disabled") {
		t.Fatalf("expected tenant disabled in auth error, got: %v", c.CLOBAuthError())
	}

	// Address should still work
	if c.Address() == "" {
		t.Fatal("expected non-empty address")
	}

	// Trading methods should fail with clear auth error
	_, err = c.PlaceOrder(nil, "token-1", 0.5, 10, "BUY", "", false)
	if err == nil {
		t.Fatal("expected PlaceOrder to fail on degraded client")
	}
	if !strings.Contains(err.Error(), "CLOB API credentials") {
		t.Fatalf("expected CLOB credentials error, got: %v", err)
	}

	_, err = c.buildAndPreviewOrder(nil, "token-1", 0.5, 10, "BUY", "", false)
	if err == nil {
		t.Fatal("expected buildAndPreviewOrder to fail on degraded client")
	}
	if !strings.Contains(err.Error(), "CLOB API credentials") {
		t.Fatalf("expected CLOB credentials error, got: %v", err)
	}

	// signRequest should also fail with the specific auth error
	err = c.signRequest(nil, "")
	if err == nil {
		t.Fatal("expected signRequest to fail on degraded client")
	}
	if !strings.Contains(err.Error(), "tenant disabled") {
		t.Fatalf("expected tenant disabled in signRequest error, got: %v", err)
	}
}
