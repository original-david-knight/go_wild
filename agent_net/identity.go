package gowild_agent_net

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// DecodePublicKey decodes a Base64URL-encoded Ed25519 public key.
func DecodePublicKey(encoded string) (ed25519.PublicKey, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid base64url encoding: %w", err)
	}

	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid key length: expected %d bytes, got %d",
			ed25519.PublicKeySize, len(decoded))
	}

	return ed25519.PublicKey(decoded), nil
}

// EncodePublicKey encodes an Ed25519 public key to Base64URL format.
func EncodePublicKey(pubkey ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(pubkey)
}

// DecodeSignature decodes a Base64URL-encoded Ed25519 signature.
func DecodeSignature(encoded string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid base64url encoding: %w", err)
	}

	if len(decoded) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid signature length: expected %d bytes, got %d",
			ed25519.SignatureSize, len(decoded))
	}

	return decoded, nil
}

// BuildSignatureInput constructs the message to be signed according to the protocol.
// Format: method:path:timestamp:SHA256(body)
func BuildSignatureInput(method, path, timestamp string, body []byte) []byte {
	bodyHash := sha256.Sum256(body)
	bodyHashHex := fmt.Sprintf("%x", bodyHash)
	input := fmt.Sprintf("%s:%s:%s:%s", method, path, timestamp, bodyHashHex)
	return []byte(input)
}

// VerifySignature verifies an Ed25519 signature over a request.
func VerifySignature(pubkey ed25519.PublicKey, method, path, timestamp string, body []byte, signature []byte) bool {
	signInput := BuildSignatureInput(method, path, timestamp, body)
	return ed25519.Verify(pubkey, signInput, signature)
}

// SignRequest creates a signature for a request (used by clients).
func SignRequest(privateKey ed25519.PrivateKey, method, path, timestamp string, body []byte) []byte {
	signInput := BuildSignatureInput(method, path, timestamp, body)
	return ed25519.Sign(privateKey, signInput)
}

// EncodeSignature encodes a signature to Base64URL format.
func EncodeSignature(signature []byte) string {
	return base64.RawURLEncoding.EncodeToString(signature)
}

// GenerateKeyPair generates a new Ed25519 key pair for an agent.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}
