package gowild_crypto

import (
	"testing"
)

const (
	testEthKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	testSolKey = "5MaiiCavjCmn9Hs1o3eznqDEhRwxo7pXiAYez7keQUviUkauRiTMD8DrESdrNjN8zd9mTmVhRvBJeg5vhyvgrAhG"
)

func TestNewWallet_EthOnly(t *testing.T) {
	w, err := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	if !w.hasChain(ChainEthereum) {
		t.Error("expected ETH chain configured")
	}
	if w.hasChain(ChainSolana) {
		t.Error("expected SOL chain not configured")
	}
}

func TestNewWallet_SolOnly(t *testing.T) {
	w, err := NewWallet(WalletConfig{
		SolPrivateKey: testSolKey,
		SolRPCURL:     "https://api.mainnet-beta.solana.com",
	})
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	if w.hasChain(ChainEthereum) {
		t.Error("expected ETH chain not configured")
	}
	if !w.hasChain(ChainSolana) {
		t.Error("expected SOL chain configured")
	}
}

func TestNewWallet_NoKeys(t *testing.T) {
	_, err := NewWallet(WalletConfig{})
	if err == nil {
		t.Error("expected error when no keys provided")
	}
}

func TestNewWallet_InvalidEthKey(t *testing.T) {
	_, err := NewWallet(WalletConfig{
		EthPrivateKey: "invalid",
		EthRPCURL:     "https://eth.drpc.org",
	})
	if err == nil {
		t.Error("expected error for invalid ETH key")
	}
}

func TestWallet_AvailableChains(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		SolPrivateKey: testSolKey,
		EthRPCURL:     "https://eth.drpc.org",
		SolRPCURL:     "https://api.mainnet-beta.solana.com",
	})

	chains := w.availableChains()
	if len(chains) != 2 {
		t.Errorf("expected 2 chains, got %d", len(chains))
	}
}

func TestWallet_GetAddress(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	// ETH address
	info, err := w.GetAddress(ChainEthereum)
	if err != nil {
		t.Fatalf("GetAddress(eth) failed: %v", err)
	}
	if info.Chain != ChainEthereum {
		t.Errorf("expected chain ethereum, got %s", info.Chain)
	}
	if info.Address == "" {
		t.Error("expected non-empty address")
	}

	// SOL not configured
	_, err = w.GetAddress(ChainSolana)
	if err == nil {
		t.Error("expected error for unconfigured SOL chain")
	}

	// Invalid chain
	_, err = w.GetAddress("bitcoin")
	if err == nil {
		t.Error("expected error for unsupported chain")
	}
}

func TestWallet_GetAddress_CaseInsensitive(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	info, err := w.GetAddress(Chain("ETHEREUM"))
	if err != nil {
		t.Fatalf("GetAddress(ETHEREUM) failed: %v", err)
	}
	if info.Chain != ChainEthereum {
		t.Errorf("expected chain ethereum, got %s", info.Chain)
	}
}

func TestWallet_HasChain_CaseInsensitive(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	if !w.hasChain(Chain("ETHEREUM")) {
		t.Error("HasChain should be case-insensitive")
	}
	if w.hasChain(Chain("unknown")) {
		t.Error("HasChain should return false for unknown chain")
	}
}

func TestWallet_SignMessage(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	signed, err := w.SignMessage(ChainEthereum, "test message")
	if err != nil {
		t.Fatalf("SignMessage failed: %v", err)
	}
	if signed.Message != "test message" {
		t.Errorf("expected message 'test message', got %q", signed.Message)
	}
	if signed.Signature == "" {
		t.Error("expected non-empty signature")
	}

	// Verify the signature
	valid, err := verifyEthereumSignature(signed.Address, signed.Message, signed.Signature)
	if err != nil {
		t.Fatalf("verifyEthereumSignature failed: %v", err)
	}
	if !valid {
		t.Error("signature verification failed")
	}
}

func TestWallet_SignMessage_UnconfiguredChain(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	_, err := w.SignMessage(ChainSolana, "test")
	if err == nil {
		t.Error("expected error for unconfigured chain")
	}
}

func TestWallet_SignMessage_UnsupportedChain(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	_, err := w.SignMessage("bitcoin", "test")
	if err == nil {
		t.Error("expected error for unsupported chain")
	}
}

func TestWallet_Ed25519PublicKey(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		SolPrivateKey: testSolKey,
		SolRPCURL:     "https://api.mainnet-beta.solana.com",
	})

	pubKey, err := w.Ed25519PublicKey()
	if err != nil {
		t.Fatalf("Ed25519PublicKey failed: %v", err)
	}
	if len(pubKey) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(pubKey))
	}
}

func TestWallet_Ed25519PublicKey_NoSolWallet(t *testing.T) {
	w, _ := NewWallet(WalletConfig{
		EthPrivateKey: testEthKey,
		EthRPCURL:     "https://eth.drpc.org",
	})

	_, err := w.Ed25519PublicKey()
	if err == nil {
		t.Error("expected error when no SOL wallet")
	}
}

func TestEthereumWallet_PublicKeyHex(t *testing.T) {
	wallet, _ := newEthereumWallet(testEthKey, "https://eth.drpc.org")
	pubHex := wallet.PublicKeyHex()
	if pubHex == "" {
		t.Error("expected non-empty public key hex")
	}
	if len(pubHex) < 64 {
		t.Errorf("public key hex too short: %d", len(pubHex))
	}
}

func TestEthereumWallet_SignHash(t *testing.T) {
	wallet, _ := newEthereumWallet(testEthKey, "https://eth.drpc.org")

	// Sign a 32-byte hash
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}

	sig, err := wallet.SignHash(hash)
	if err != nil {
		t.Fatalf("SignHash failed: %v", err)
	}
	if len(sig) != 65 { // Ethereum signatures are 65 bytes (r, s, v)
		t.Errorf("expected 65-byte signature, got %d", len(sig))
	}
}

func TestSolanaWallet_SignBytes(t *testing.T) {
	wallet, _ := newSolanaWallet(testSolKey, "https://api.mainnet-beta.solana.com")

	data := []byte("some data to sign")
	sig, err := wallet.SignBytes(data)
	if err != nil {
		t.Fatalf("SignBytes failed: %v", err)
	}
	if len(sig) != 64 { // Ed25519 signatures are 64 bytes
		t.Errorf("expected 64-byte signature, got %d", len(sig))
	}
}
