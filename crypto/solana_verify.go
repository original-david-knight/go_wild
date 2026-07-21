package gowild_crypto

import (
	"encoding/base64"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// verifySolanaSignature verifies that a signature was created by a given public key.
// This is a static method that doesn't require wallet access.
func verifySolanaSignature(publicKeyBase58 string, message string, signatureBase64 string) (bool, error) {
	// Decode public key
	publicKey, err := solana.PublicKeyFromBase58(publicKeyBase58)
	if err != nil {
		return false, fmt.Errorf("invalid public key: %w", err)
	}

	// Decode signature
	sigBytes, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, fmt.Errorf("invalid signature format: %w", err)
	}

	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length: expected 64 bytes, got %d", len(sigBytes))
	}

	var signature solana.Signature
	copy(signature[:], sigBytes)

	// Verify
	messageBytes := []byte(message)
	return publicKey.Verify(messageBytes, signature), nil
}
