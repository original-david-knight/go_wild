package gowild_crypto

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// SignMessage signs a message using EIP-191 personal_sign format.
// This is the standard format for "Sign Message" in wallets like MetaMask.
func (w *ethereumWallet) SignMessage(message string) (*SignedMessage, error) {
	// Create the message hash using Ethereum's personal_sign format
	// This prepends "\x19Ethereum Signed Message:\n<length>" to the message
	msgHash := accounts.TextHash([]byte(message))

	// Sign the hash
	signature, err := crypto.Sign(msgHash, w.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}

	// Ethereum signatures use v = 27 or 28, but crypto.Sign returns v = 0 or 1
	// We need to add 27 to the recovery id (last byte)
	if signature[64] < 27 {
		signature[64] += 27
	}

	return &SignedMessage{
		Chain:     ChainEthereum,
		Address:   w.Address(),
		Message:   message,
		Signature: hexutil.Encode(signature),
	}, nil
}

// SignHash signs a raw 32-byte hash (for advanced use cases).
func (w *ethereumWallet) SignHash(hash []byte) ([]byte, error) {
	if len(hash) != 32 {
		return nil, fmt.Errorf("hash must be 32 bytes, got %d", len(hash))
	}

	signature, err := crypto.Sign(hash, w.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign hash: %w", err)
	}

	// Adjust recovery id for Ethereum
	if signature[64] < 27 {
		signature[64] += 27
	}

	return signature, nil
}

// PublicKeyHex returns the public key as a hex string.
func (w *ethereumWallet) PublicKeyHex() string {
	publicKey := w.privateKey.Public()
	publicKeyECDSA := publicKey.(*ecdsa.PublicKey)
	publicKeyBytes := crypto.FromECDSAPub(publicKeyECDSA)
	return hex.EncodeToString(publicKeyBytes)
}
