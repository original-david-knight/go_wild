package gowild_crypto

import (
	"strings"
	"testing"
)

// Well-known test mnemonic (BIP39 test vector)
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestGenerateMnemonic(t *testing.T) {
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic failed: %v", err)
	}

	words := strings.Fields(mnemonic)
	if len(words) != 24 {
		t.Errorf("expected 24 words, got %d", len(words))
	}

	if !ValidateMnemonic(mnemonic) {
		t.Error("generated mnemonic should be valid")
	}
}

func TestGenerateMnemonic_Unique(t *testing.T) {
	m1, _ := GenerateMnemonic()
	m2, _ := GenerateMnemonic()
	if m1 == m2 {
		t.Error("two generated mnemonics should not be equal")
	}
}

func TestValidateMnemonic(t *testing.T) {
	tests := []struct {
		mnemonic string
		valid    bool
	}{
		{testMnemonic, true},
		{"not a valid mnemonic", false},
		{"", false},
		{"abandon abandon abandon", false}, // too short
	}

	for _, tc := range tests {
		if got := ValidateMnemonic(tc.mnemonic); got != tc.valid {
			t.Errorf("ValidateMnemonic(%q) = %v, want %v", tc.mnemonic[:min(len(tc.mnemonic), 30)], got, tc.valid)
		}
	}
}

func TestDeriveKeysFromMnemonic(t *testing.T) {
	keys, err := DeriveKeysFromMnemonic(testMnemonic, 0)
	if err != nil {
		t.Fatalf("DeriveKeysFromMnemonic failed: %v", err)
	}

	// ETH key should be hex with 0x prefix
	if !strings.HasPrefix(keys.EthPrivateKey, "0x") {
		t.Errorf("ETH key should start with 0x, got %q", keys.EthPrivateKey[:10])
	}

	// ETH address should be hex with 0x prefix
	if !strings.HasPrefix(keys.EthAddress, "0x") || len(keys.EthAddress) != 42 {
		t.Errorf("unexpected ETH address format: %q", keys.EthAddress)
	}

	// SOL key should be base58 encoded (no prefix)
	if keys.SolPrivateKey == "" {
		t.Error("SOL private key should not be empty")
	}

	// SOL address should be base58 (32-44 chars)
	if len(keys.SolAddress) < 32 || len(keys.SolAddress) > 44 {
		t.Errorf("unexpected SOL address length: %d", len(keys.SolAddress))
	}
}

func TestDeriveKeysFromMnemonic_Deterministic(t *testing.T) {
	keys1, _ := DeriveKeysFromMnemonic(testMnemonic, 0)
	keys2, _ := DeriveKeysFromMnemonic(testMnemonic, 0)

	if keys1.EthPrivateKey != keys2.EthPrivateKey {
		t.Error("ETH keys should be deterministic")
	}
	if keys1.EthAddress != keys2.EthAddress {
		t.Error("ETH addresses should be deterministic")
	}
	if keys1.SolPrivateKey != keys2.SolPrivateKey {
		t.Error("SOL keys should be deterministic")
	}
	if keys1.SolAddress != keys2.SolAddress {
		t.Error("SOL addresses should be deterministic")
	}
}

func TestDeriveKeysFromMnemonic_DifferentAccounts(t *testing.T) {
	keys0, _ := DeriveKeysFromMnemonic(testMnemonic, 0)
	keys1, _ := DeriveKeysFromMnemonic(testMnemonic, 1)

	if keys0.EthPrivateKey == keys1.EthPrivateKey {
		t.Error("different accounts should produce different ETH keys")
	}
	if keys0.SolPrivateKey == keys1.SolPrivateKey {
		t.Error("different accounts should produce different SOL keys")
	}
}

func TestDeriveKeysFromMnemonic_InvalidMnemonic(t *testing.T) {
	_, err := DeriveKeysFromMnemonic("invalid mnemonic phrase", 0)
	if err == nil {
		t.Error("expected error for invalid mnemonic")
	}
}

func TestDeriveKeysFromMnemonic_CanCreateWallets(t *testing.T) {
	keys, err := DeriveKeysFromMnemonic(testMnemonic, 0)
	if err != nil {
		t.Fatalf("DeriveKeysFromMnemonic failed: %v", err)
	}

	// Should be able to create ETH wallet from derived key
	ethWallet, err := newEthereumWallet(keys.EthPrivateKey, "https://eth.drpc.org")
	if err != nil {
		t.Fatalf("failed to create ETH wallet from derived key: %v", err)
	}
	if ethWallet.Address() != keys.EthAddress {
		t.Errorf("ETH address mismatch: wallet=%s derived=%s", ethWallet.Address(), keys.EthAddress)
	}

	// Should be able to create SOL wallet from derived key
	solWallet, err := newSolanaWallet(keys.SolPrivateKey, "https://api.mainnet-beta.solana.com")
	if err != nil {
		t.Fatalf("failed to create SOL wallet from derived key: %v", err)
	}
	if solWallet.Address() != keys.SolAddress {
		t.Errorf("SOL address mismatch: wallet=%s derived=%s", solWallet.Address(), keys.SolAddress)
	}
}
