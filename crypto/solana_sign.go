package gowild_crypto

import (
	"encoding/base64"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// SignMessage signs a message and returns the signature.
// Solana uses ed25519 signatures on the raw message bytes.
func (w *solanaWallet) SignMessage(message string) (*SignedMessage, error) {
	messageBytes := []byte(message)

	// Sign using ed25519
	signature, err := w.keypair.Sign(messageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}

	return &SignedMessage{
		Chain:     ChainSolana,
		Address:   w.Address(),
		Message:   message,
		Signature: base64.StdEncoding.EncodeToString(signature[:]),
	}, nil
}

// SignBytes signs raw bytes and returns the signature.
func (w *solanaWallet) SignBytes(data []byte) (solana.Signature, error) {
	return w.keypair.Sign(data)
}
