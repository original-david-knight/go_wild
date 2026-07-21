package gowild_crypto

import (
	"testing"
)

// Test Ethereum wallet creation and signing
func TestEthereumWallet(t *testing.T) {
	// Well-known test private key (DO NOT use in production)
	testPrivateKey := "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

	wallet, err := newEthereumWallet(testPrivateKey, "https://eth.llamarpc.com")
	if err != nil {
		t.Fatalf("Failed to create ethereum wallet: %v", err)
	}

	// Expected address for this test private key
	expectedAddress := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	if wallet.Address() != expectedAddress {
		t.Errorf("Expected address %s, got %s", expectedAddress, wallet.Address())
	}

	// Test message signing
	message := "Test message for signing"
	signed, err := wallet.SignMessage(message)
	if err != nil {
		t.Fatalf("Failed to sign message: %v", err)
	}

	if signed.Message != message {
		t.Errorf("Expected message %s, got %s", message, signed.Message)
	}

	if signed.Address != expectedAddress {
		t.Errorf("Expected address %s in signed message, got %s", expectedAddress, signed.Address)
	}

	if signed.Signature == "" {
		t.Error("Signature should not be empty")
	}

	// Verify the signature
	valid, err := verifyEthereumSignature(signed.Address, signed.Message, signed.Signature)
	if err != nil {
		t.Fatalf("Failed to verify signature: %v", err)
	}
	if !valid {
		t.Error("Signature verification failed")
	}

	// Test with wrong address
	valid, err = verifyEthereumSignature("0x0000000000000000000000000000000000000000", signed.Message, signed.Signature)
	if err != nil {
		t.Fatalf("Failed to verify signature: %v", err)
	}
	if valid {
		t.Error("Signature should be invalid for wrong address")
	}
}

// Test Ethereum wallet with 0x prefix
func TestEthereumWalletWithPrefix(t *testing.T) {
	testPrivateKey := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

	wallet, err := newEthereumWallet(testPrivateKey, "https://eth.llamarpc.com")
	if err != nil {
		t.Fatalf("Failed to create ethereum wallet with 0x prefix: %v", err)
	}

	expectedAddress := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	if wallet.Address() != expectedAddress {
		t.Errorf("Expected address %s, got %s", expectedAddress, wallet.Address())
	}
}

// Test Solana wallet creation and signing
func TestSolanaWallet(t *testing.T) {
	// Well-known Phantom test keypair (DO NOT use in production)
	// This is derived from the seed: "test test test test test test test test test test test junk"
	// Address: 7Z36Efbt7a4nLiV7s5bY7J2e4TJ6K9A4A7Nw7L8zVFNd
	testPrivateKey := "5MaiiCavjCmn9Hs1o3eznqDEhRwxo7pXiAYez7keQUviUkauRiTMD8DrESdrNjN8zd9mTmVhRvBJeg5vhyvgrAhG"

	wallet, err := newSolanaWallet(testPrivateKey, "https://api.mainnet-beta.solana.com")
	if err != nil {
		t.Fatalf("Failed to create solana wallet: %v", err)
	}

	address := wallet.Address()
	if address == "" {
		t.Error("Address should not be empty")
	}
	t.Logf("Solana wallet address: %s", address)

	// Test message signing
	message := "Test message for Solana signing"
	signed, err := wallet.SignMessage(message)
	if err != nil {
		t.Fatalf("Failed to sign message: %v", err)
	}

	if signed.Message != message {
		t.Errorf("Expected message %s, got %s", message, signed.Message)
	}

	if signed.Address != address {
		t.Errorf("Expected address %s in signed message, got %s", address, signed.Address)
	}

	if signed.Signature == "" {
		t.Error("Signature should not be empty")
	}

	// Verify the signature
	valid, err := verifySolanaSignature(signed.Address, signed.Message, signed.Signature)
	if err != nil {
		t.Fatalf("Failed to verify signature: %v", err)
	}
	if !valid {
		t.Error("Signature verification failed")
	}
}

// Test Chain validation
func TestChainValidation(t *testing.T) {
	tests := []struct {
		chain    Chain
		expected bool
	}{
		{ChainEthereum, true},
		{ChainSolana, true},
		{"bitcoin", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := tt.chain.IsValid(); got != tt.expected {
			t.Errorf("Chain(%q).IsValid() = %v, want %v", tt.chain, got, tt.expected)
		}
	}
}
