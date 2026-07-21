package main

import (
	"context"
	"encoding/base64"
	"testing"
)

func TestLoadOrGenerateBrokerSecret_FromDB(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	// First call: generates and stores
	secret1 := loadOrGenerateBrokerSecret(db)
	if len(secret1) != 32 {
		t.Fatalf("expected 32-byte secret, got %d", len(secret1))
	}

	// Verify it was stored in DB
	val, err := GetSetting(ctx, db, "broker_secret")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if val == "" {
		t.Fatal("expected stored secret in DB")
	}

	// Decode and verify
	decoded, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		t.Fatalf("failed to decode stored secret: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("stored secret wrong length: %d", len(decoded))
	}

	// Second call: should load from DB (same value)
	secret2 := loadOrGenerateBrokerSecret(db)
	if string(secret1) != string(secret2) {
		t.Error("expected same secret on second call (loaded from DB)")
	}
}

func TestLoadOrGenerateBrokerSecret_NilDB(t *testing.T) {
	// With nil DB, should still generate a secret
	secret := loadOrGenerateBrokerSecret(nil)
	if len(secret) != 32 {
		t.Fatalf("expected 32-byte secret, got %d", len(secret))
	}
}

func TestLoadOrGenerateBrokerSecret_DifferentOnNilDB(t *testing.T) {
	// Each call with nil DB generates a new secret
	s1 := loadOrGenerateBrokerSecret(nil)
	s2 := loadOrGenerateBrokerSecret(nil)
	if string(s1) == string(s2) {
		t.Error("expected different secrets from nil DB calls")
	}
}
