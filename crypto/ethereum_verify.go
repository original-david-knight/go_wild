package gowild_crypto

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// VerifyEthereumSignature verifies that a signature was created by a given address.
// It recovers the signing public key from an EIP-191 personal_sign message hash.
func VerifyEthereumSignature(address string, message string, signatureHex string) (bool, error) {
	// Decode signature
	signature, err := hexutil.Decode(signatureHex)
	if err != nil {
		return false, fmt.Errorf("invalid signature format: %w", err)
	}

	if len(signature) != 65 {
		return false, fmt.Errorf("invalid signature length: expected 65 bytes, got %d", len(signature))
	}

	// Adjust recovery id back for verification
	if signature[64] >= 27 {
		signature[64] -= 27
	}

	// Hash the message the same way it was signed
	msgHash := accounts.TextHash([]byte(message))

	// Recover the public key from the signature
	publicKey, err := crypto.SigToPub(msgHash, signature)
	if err != nil {
		return false, fmt.Errorf("failed to recover public key: %w", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*publicKey)
	expectedAddr := common.HexToAddress(address)

	return recoveredAddr == expectedAddr, nil
}

// verifyEthereumSignature is kept for package-local callers and older tests.
func verifyEthereumSignature(address string, message string, signatureHex string) (bool, error) {
	return VerifyEthereumSignature(address, message, signatureHex)
}
