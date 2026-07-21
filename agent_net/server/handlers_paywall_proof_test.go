package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
)

func TestVerifyBuyerProofPolygon(t *testing.T) {
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate ECDSA key: %v", err)
	}
	buyerAddress := crypto.PubkeyToAddress(privKey.PublicKey).Hex()

	productID := "prod_test123"
	txHash := "0xabc123"
	message := buildBuyerProofMessage(productID, txHash)
	hash := accounts.TextHash([]byte(message))

	sig, err := crypto.Sign(hash, privKey)
	if err != nil {
		t.Fatalf("failed to sign proof message: %v", err)
	}
	wireSig := make([]byte, len(sig))
	copy(wireSig, sig)
	wireSig[64] += 27 // ethers-style recovery ID
	sigHex := "0x" + hex.EncodeToString(wireSig)

	if err := verifyBuyerProof("polygon", productID, txHash, buyerAddress, sigHex); err != nil {
		t.Fatalf("expected polygon buyer proof to verify, got error: %v", err)
	}

	if err := verifyBuyerProof("polygon", productID, txHash, "0x0000000000000000000000000000000000000001", sigHex); err == nil {
		t.Fatalf("expected mismatch buyer address to fail proof verification")
	}
}

func TestVerifyBuyerProofSolana(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate Ed25519 key: %v", err)
	}
	buyerAddress := base58.Encode(pubKey)

	productID := "prod_test456"
	txHash := "5WJQzZt9x9n42vSkY8hSMR8qjvJQwJk7UiS3h6h8zA7R"
	message := buildBuyerProofMessage(productID, txHash)
	sig := ed25519.Sign(privKey, []byte(message))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	if err := verifyBuyerProof("solana", productID, txHash, buyerAddress, sigB64); err != nil {
		t.Fatalf("expected solana buyer proof to verify, got error: %v", err)
	}

	if err := verifyBuyerProof("solana", productID, txHash, buyerAddress, base64.StdEncoding.EncodeToString([]byte("bad"))); err == nil {
		t.Fatalf("expected invalid signature to fail proof verification")
	}
}

func TestBuyerAddressMatchesChainRules(t *testing.T) {
	polygonChecksummed := "0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	polygonLower := "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	if !buyerAddressMatches("polygon", polygonChecksummed, polygonLower) {
		t.Fatalf("expected polygon addresses to match case-insensitively")
	}

	if buyerAddressMatches("solana", "SoLaNaAddreSS", "solanaaddress") {
		t.Fatalf("expected solana addresses to require exact case-sensitive match")
	}
}

func TestValidatePaywallBlockTime(t *testing.T) {
	createdAt := time.Date(2026, time.January, 2, 15, 4, 5, 0, time.UTC)

	if err := validatePaywallBlockTime(createdAt.Add(-6*time.Minute), createdAt); err == nil {
		t.Fatalf("expected transaction older than grace window to fail")
	}
	if err := validatePaywallBlockTime(createdAt.Add(-5*time.Minute), createdAt); err != nil {
		t.Fatalf("expected boundary transaction to pass, got error: %v", err)
	}
	if err := validatePaywallBlockTime(createdAt.Add(1*time.Minute), createdAt); err != nil {
		t.Fatalf("expected newer transaction to pass, got error: %v", err)
	}
}
