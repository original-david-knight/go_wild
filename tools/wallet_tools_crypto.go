package tools

import (
	"context"
	"encoding/base64"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// EncryptMessageTool encrypts a message for a recipient using NaCl box (X25519 + XSalsa20-Poly1305).
// Uses the Solana wallet's Ed25519 key for the sender side.
func (w *WalletTools) EncryptMessageTool(ctx context.Context, input EncryptMessageInput) (*loop.ToolResult, error) {
	if input.Plaintext == "" {
		return loop.NewErrorResult("plaintext cannot be empty"), nil
	}
	if input.RecipientPublicKey == "" {
		return loop.NewErrorResult("recipient_public_key is required"), nil
	}

	recipientPubBytes, err := base64.RawURLEncoding.DecodeString(input.RecipientPublicKey)
	if err != nil {
		return loop.NewErrorResult("invalid recipient_public_key: " + err.Error()), nil
	}
	if len(recipientPubBytes) != 32 {
		return loop.NewErrorResult("recipient_public_key must be 32 bytes (43 base64url chars)"), nil
	}

	ciphertext, nonce, err := w.wallet.EncryptMessage([]byte(input.Plaintext), recipientPubBytes)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"ciphertext": base64.RawURLEncoding.EncodeToString(ciphertext),
		"nonce":      base64.RawURLEncoding.EncodeToString(nonce),
	}), nil
}

// DecryptMessageTool decrypts a NaCl box message from a sender.
// Uses the Solana wallet's Ed25519 key for the recipient side.
func (w *WalletTools) DecryptMessageTool(ctx context.Context, input DecryptMessageInput) (*loop.ToolResult, error) {
	if input.Ciphertext == "" {
		return loop.NewErrorResult("ciphertext is required"), nil
	}
	if input.Nonce == "" {
		return loop.NewErrorResult("nonce is required"), nil
	}
	if input.SenderPublicKey == "" {
		return loop.NewErrorResult("sender_public_key is required"), nil
	}

	ciphertextBytes, err := base64.RawURLEncoding.DecodeString(input.Ciphertext)
	if err != nil {
		return loop.NewErrorResult("invalid ciphertext: " + err.Error()), nil
	}

	nonceBytes, err := base64.RawURLEncoding.DecodeString(input.Nonce)
	if err != nil {
		return loop.NewErrorResult("invalid nonce: " + err.Error()), nil
	}
	if len(nonceBytes) != 24 {
		return loop.NewErrorResult("nonce must be 24 bytes"), nil
	}

	senderPubBytes, err := base64.RawURLEncoding.DecodeString(input.SenderPublicKey)
	if err != nil {
		return loop.NewErrorResult("invalid sender_public_key: " + err.Error()), nil
	}
	if len(senderPubBytes) != 32 {
		return loop.NewErrorResult("sender_public_key must be 32 bytes (43 base64url chars)"), nil
	}

	var nonce [24]byte
	copy(nonce[:], nonceBytes)

	plaintext, err := w.wallet.DecryptMessage(ciphertextBytes, nonce, senderPubBytes)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"plaintext": string(plaintext),
	}), nil
}

// GetEd25519PublicKeyTool returns the agent's Ed25519 public key in base64url format.
// This is the identity key used for the X-Agent-ID header on agent_net.
func (w *WalletTools) GetEd25519PublicKeyTool(ctx context.Context, input GetEd25519PublicKeyInput) (*loop.ToolResult, error) {
	pubKey, err := w.wallet.Ed25519PublicKey()
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"public_key": base64.RawURLEncoding.EncodeToString(pubKey),
	}), nil
}
