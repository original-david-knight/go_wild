package gowild_agent_net

import (
	"encoding/hex"
	"testing"
)

func TestCountLeadingZeroBits(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{"all zeros", []byte{0x00, 0x00, 0x00}, 24},
		{"one byte zero", []byte{0x00, 0x80}, 8},
		{"half byte", []byte{0x0F}, 4},
		{"one bit", []byte{0x7F}, 1},
		{"no zeros", []byte{0xFF}, 0},
		{"mixed", []byte{0x00, 0x00, 0x01}, 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CountLeadingZeroBits(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestCanonicalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			"simple object",
			map[string]any{"b": 1, "a": 2},
			`{"a":2,"b":1}`,
		},
		{
			"nested object",
			map[string]any{"z": map[string]any{"b": 1, "a": 2}, "a": 3},
			`{"a":3,"z":{"a":2,"b":1}}`,
		},
		{
			"array",
			[]any{1, 2, 3},
			`[1,2,3]`,
		},
		{
			"string",
			"hello",
			`"hello"`,
		},
		{
			"null",
			nil,
			`null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CanonicalJSON(tt.input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(result))
			}
		})
	}
}

func TestValidateNonce(t *testing.T) {
	tests := []struct {
		name    string
		nonce   string
		wantErr bool
	}{
		{"valid short", "abcd1234", false},
		{"valid long", "abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJ", false},
		{"too short", "abc1234", true},
		{"too long", "abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZabc1234", true},
		{"invalid chars", "abcd!@#$", true},
		{"spaces", "abcd 1234", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNonce(tt.nonce)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNonce(%q) error = %v, wantErr %v", tt.nonce, err, tt.wantErr)
			}
		})
	}
}

func TestComputePoWChallenge(t *testing.T) {
	payload := []byte(`{"content":"test"}`)
	timestamp := "2024-01-15T10:30:00Z"
	nonce := "abc12345"

	challenge := ComputePoWChallenge(payload, timestamp, nonce)

	// Should be 32 bytes (SHA256)
	if len(challenge) != 32 {
		t.Errorf("Expected challenge length 32, got %d", len(challenge))
	}

	// Same inputs should produce same challenge
	challenge2 := ComputePoWChallenge(payload, timestamp, nonce)
	if hex.EncodeToString(challenge) != hex.EncodeToString(challenge2) {
		t.Error("Same inputs should produce same challenge")
	}

	// Different inputs should produce different challenge
	challenge3 := ComputePoWChallenge(payload, timestamp, "different1")
	if hex.EncodeToString(challenge) == hex.EncodeToString(challenge3) {
		t.Error("Different inputs should produce different challenge")
	}
}

func TestComputePoWHash(t *testing.T) {
	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(i)
	}

	hash := ComputePoWHash(challenge)

	// Should be 32 bytes (Argon2id output)
	if len(hash) != 32 {
		t.Errorf("Expected hash length 32, got %d", len(hash))
	}

	// Same input should produce same hash
	hash2 := ComputePoWHash(challenge)
	if hex.EncodeToString(hash) != hex.EncodeToString(hash2) {
		t.Error("Same input should produce same hash")
	}
}

func TestVerifyPoW(t *testing.T) {
	payload := []byte(`{"content":"test"}`)
	timestamp := "2024-01-15T10:30:00Z"
	nonce := "testnonc"

	// Compute the actual hash
	challenge := ComputePoWChallenge(payload, timestamp, nonce)
	hash := ComputePoWHash(challenge)
	hashHex := hex.EncodeToString(hash)

	// Count leading zeros to determine a valid difficulty
	leadingZeros := CountLeadingZeroBits(hash)

	// Should pass with equal or lower difficulty
	valid, err := VerifyPoW(hashHex, payload, timestamp, nonce, leadingZeros)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !valid {
		t.Error("Hash should meet difficulty requirement")
	}

	// Should pass with lower difficulty
	if leadingZeros > 0 {
		valid, err = VerifyPoW(hashHex, payload, timestamp, nonce, leadingZeros-1)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !valid {
			t.Error("Hash should meet lower difficulty requirement")
		}
	}

	// Should fail with higher difficulty (if we haven't maxed out)
	if leadingZeros < 256 {
		valid, err = VerifyPoW(hashHex, payload, timestamp, nonce, 256)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if valid {
			t.Error("Hash should not meet impossible difficulty requirement")
		}
	}

	// Invalid hex should error
	_, err = VerifyPoW("not-hex", payload, timestamp, nonce, 1)
	if err == nil {
		t.Error("Invalid hex should error")
	}

	// Wrong hash should not verify
	wrongHash := hex.EncodeToString(make([]byte, 32))
	valid, err = VerifyPoW(wrongHash, payload, timestamp, nonce, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if valid {
		t.Error("Wrong hash should not verify")
	}
}
