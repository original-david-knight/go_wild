package gowild_agent_net

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters as specified in the protocol.
const (
	Argon2Memory      = 64 * 1024 // 64 MB in KiB
	Argon2Iterations  = 2
	Argon2Parallelism = 1
	Argon2HashLength  = 32 // 32 bytes output
)

// ComputePoWChallenge computes the challenge input for Argon2id.
// challenge = SHA256(canonical_json:timestamp:nonce)
func ComputePoWChallenge(payloadJSON []byte, timestamp, nonce string) []byte {
	input := fmt.Sprintf("%s:%s:%s", string(payloadJSON), timestamp, nonce)
	hash := sha256.Sum256([]byte(input))
	return hash[:]
}

// ComputePoWHash computes the Argon2id hash for PoW verification.
func ComputePoWHash(challenge []byte) []byte {
	// Use challenge as both password and salt (as per spec, salt can be empty or derived)
	// For this implementation, we use first 16 bytes of challenge as salt
	salt := challenge[:16]
	return argon2.IDKey(challenge, salt, Argon2Iterations, Argon2Memory, Argon2Parallelism, Argon2HashLength)
}

// VerifyPoW verifies a Proof of Work hash meets the difficulty requirement.
// Returns true if the hash has at least `difficulty` leading zero bits.
func VerifyPoW(powHashHex string, payloadJSON []byte, timestamp, nonce string, difficulty int) (bool, error) {
	// Decode the provided hash
	providedHash, err := hex.DecodeString(powHashHex)
	if err != nil {
		return false, fmt.Errorf("invalid PoW hash encoding: %w", err)
	}

	if len(providedHash) != Argon2HashLength {
		return false, fmt.Errorf("invalid PoW hash length: expected %d, got %d", Argon2HashLength, len(providedHash))
	}

	// Recompute the hash
	challenge := ComputePoWChallenge(payloadJSON, timestamp, nonce)
	expectedHash := ComputePoWHash(challenge)

	// Verify the hash matches
	if !equalBytes(providedHash, expectedHash) {
		return false, nil
	}

	// Check difficulty (leading zero bits)
	return CountLeadingZeroBits(expectedHash) >= difficulty, nil
}

// CountLeadingZeroBits counts the number of leading zero bits in a byte slice.
func CountLeadingZeroBits(data []byte) int {
	count := 0
	for _, b := range data {
		if b == 0 {
			count += 8
			continue
		}
		// Count leading zeros in this byte
		for i := 7; i >= 0; i-- {
			if (b>>i)&1 == 0 {
				count++
			} else {
				return count
			}
		}
	}
	return count
}

// CanonicalJSON produces canonical JSON with sorted keys and no extra whitespace.
func CanonicalJSON(data any) ([]byte, error) {
	// Marshal to JSON first
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Unmarshal to interface{} to normalize
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}

	// Marshal with sorted keys
	return marshalCanonical(obj)
}

// marshalCanonical recursively marshals JSON with sorted keys.
func marshalCanonical(v any) ([]byte, error) {
	switch val := v.(type) {
	case map[string]any:
		// Sort keys
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var sb strings.Builder
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(k)
			sb.Write(keyBytes)
			sb.WriteByte(':')
			valBytes, err := marshalCanonical(val[k])
			if err != nil {
				return nil, err
			}
			sb.Write(valBytes)
		}
		sb.WriteByte('}')
		return []byte(sb.String()), nil

	case []any:
		var sb strings.Builder
		sb.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				sb.WriteByte(',')
			}
			itemBytes, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			sb.Write(itemBytes)
		}
		sb.WriteByte(']')
		return []byte(sb.String()), nil

	default:
		// For primitives (string, number, bool, null), use standard marshaling
		return json.Marshal(v)
	}
}

// equalBytes compares two byte slices in constant time.
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	result := 0
	for i := range a {
		result |= int(a[i] ^ b[i])
	}
	return result == 0
}

// ValidateNonce checks if a nonce meets the format requirements.
// Nonce must be 8-64 characters, alphanumeric.
func ValidateNonce(nonce string) error {
	if len(nonce) < 8 || len(nonce) > 64 {
		return fmt.Errorf("nonce must be 8-64 characters, got %d", len(nonce))
	}
	for _, c := range nonce {
		if !isAlphanumeric(c) {
			return fmt.Errorf("nonce must be alphanumeric, found '%c'", c)
		}
	}
	return nil
}

func isAlphanumeric(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
