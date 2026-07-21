package gowild_agent_net

import (
	"crypto/ed25519"
	"testing"
)

func TestKeyEncodeDecode(t *testing.T) {
	// Generate a key pair
	pubkey, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	// Encode
	encoded := EncodePublicKey(pubkey)

	// Verify length (32 bytes -> 43 chars in Base64URL without padding)
	if len(encoded) != 43 {
		t.Errorf("Expected encoded length 43, got %d", len(encoded))
	}

	// Decode
	decoded, err := DecodePublicKey(encoded)
	if err != nil {
		t.Fatalf("Failed to decode public key: %v", err)
	}

	// Verify they match
	if !pubkey.Equal(decoded) {
		t.Error("Decoded key does not match original")
	}
}

func TestDecodePublicKeyInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"too short", "abc123"},
		{"invalid base64", "!!!invalid!!!"},
		{"wrong length bytes", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}, // 30 bytes (40 chars base64url = 30 bytes)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodePublicKey(tt.input)
			if err == nil {
				t.Error("Expected error for invalid input")
			}
		})
	}
}

func TestSignatureVerification(t *testing.T) {
	// Generate key pair
	pubkey, privkey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	method := "POST"
	path := "/api/v1/posts"
	timestamp := "2024-01-15T10:30:00Z"
	body := []byte(`{"content":"Hello, world!"}`)

	// Sign the request
	signature := SignRequest(privkey, method, path, timestamp, body)

	// Verify the signature
	if !VerifySignature(pubkey, method, path, timestamp, body, signature) {
		t.Error("Valid signature failed verification")
	}

	// Verify with wrong data fails
	if VerifySignature(pubkey, "GET", path, timestamp, body, signature) {
		t.Error("Signature should not verify with wrong method")
	}

	if VerifySignature(pubkey, method, "/wrong/path", timestamp, body, signature) {
		t.Error("Signature should not verify with wrong path")
	}

	if VerifySignature(pubkey, method, path, "2024-01-15T11:00:00Z", body, signature) {
		t.Error("Signature should not verify with wrong timestamp")
	}

	if VerifySignature(pubkey, method, path, timestamp, []byte("wrong body"), signature) {
		t.Error("Signature should not verify with wrong body")
	}
}

func TestSignatureEncodeDecode(t *testing.T) {
	// Generate key pair
	_, privkey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	// Create a signature
	message := []byte("test message")
	signature := ed25519.Sign(privkey, message)

	// Encode
	encoded := EncodeSignature(signature)

	// Verify length (64 bytes -> 86 chars in Base64URL without padding)
	if len(encoded) != 86 {
		t.Errorf("Expected encoded length 86, got %d", len(encoded))
	}

	// Decode
	decoded, err := DecodeSignature(encoded)
	if err != nil {
		t.Fatalf("Failed to decode signature: %v", err)
	}

	// Verify they match
	if len(decoded) != len(signature) {
		t.Errorf("Decoded signature length mismatch")
	}
	for i := range signature {
		if signature[i] != decoded[i] {
			t.Error("Decoded signature does not match original")
			break
		}
	}
}

func TestBuildSignatureInput(t *testing.T) {
	method := "POST"
	path := "/api/v1/posts"
	timestamp := "2024-01-15T10:30:00Z"
	body := []byte(`{"content":"test"}`)

	input := BuildSignatureInput(method, path, timestamp, body)

	// Should be in format: method:path:timestamp:sha256(body)
	expected := "POST:/api/v1/posts:2024-01-15T10:30:00Z:"
	if string(input[:len(expected)]) != expected {
		t.Errorf("Signature input prefix mismatch")
	}

	// Empty body should still work
	inputEmpty := BuildSignatureInput(method, path, timestamp, []byte{})
	if len(inputEmpty) == 0 {
		t.Error("Empty body should still produce signature input")
	}
}
