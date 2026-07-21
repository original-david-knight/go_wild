package gowild_polymarket

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestPrivateKeyToAddress(t *testing.T) {
	key := testKey(t)
	addr := privateKeyToAddress(key)

	if len(addr) != 42 {
		t.Errorf("expected 42-char address, got %d: %s", len(addr), addr)
	}
	if addr[:2] != "0x" {
		t.Errorf("expected 0x prefix, got %s", addr[:2])
	}
}

func TestSignClobAuth(t *testing.T) {
	key := testKey(t)
	addr := privateKeyToAddress(key)

	sig, err := signClobAuth(key, addr, 1700000000, 0, 137)
	if err != nil {
		t.Fatalf("signClobAuth failed: %v", err)
	}

	// Should start with 0x
	if sig[:2] != "0x" {
		t.Errorf("expected 0x prefix, got %s", sig[:2])
	}

	// 0x + 130 hex chars (65 bytes = r(32) + s(32) + v(1))
	if len(sig) != 132 {
		t.Errorf("expected 132-char signature, got %d: %s", len(sig), sig)
	}

	// v should be 27 or 28
	sigBytes, _ := hex.DecodeString(sig[2:])
	v := sigBytes[64]
	if v != 27 && v != 28 {
		t.Errorf("expected v=27 or v=28, got %d", v)
	}
}

func TestSignClobAuth_Deterministic(t *testing.T) {
	// Use a fixed key for reproducibility
	keyBytes, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	key, _ := crypto.ToECDSA(keyBytes)
	addr := privateKeyToAddress(key)

	// Same inputs should produce the same signature
	sig1, err := signClobAuth(key, addr, 1700000000, 0, 137)
	if err != nil {
		t.Fatalf("signClobAuth failed: %v", err)
	}

	sig2, err := signClobAuth(key, addr, 1700000000, 0, 137)
	if err != nil {
		t.Fatalf("signClobAuth failed: %v", err)
	}

	if sig1 != sig2 {
		t.Errorf("expected deterministic signatures, got:\n  %s\n  %s", sig1, sig2)
	}

	// Different timestamp should produce different signature
	sig3, err := signClobAuth(key, addr, 1700000001, 0, 137)
	if err != nil {
		t.Fatalf("signClobAuth failed: %v", err)
	}
	if sig1 == sig3 {
		t.Error("expected different signature for different timestamp")
	}
}

func TestSignClobAuth_RecoverAddress(t *testing.T) {
	// Use a fixed key
	keyBytes, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	key, _ := crypto.ToECDSA(keyBytes)
	addr := privateKeyToAddress(key)

	// Sign
	sig, err := signClobAuth(key, addr, 1700000000, 0, 137)
	if err != nil {
		t.Fatalf("signClobAuth failed: %v", err)
	}

	// Recompute the digest to verify we can recover the address
	domainEncoded, _ := abiPack(
		[]abi.Type{abiBytes32, abiBytes32, abiBytes32, abiUint256},
		[]interface{}{
			clobAuthDomainTypeHash,
			clobAuthDomainName,
			clobAuthDomainVersion,
			big.NewInt(137),
		},
	)
	domainSeparator := crypto.Keccak256Hash(domainEncoded)

	timestampHash := crypto.Keccak256Hash([]byte("1700000000"))
	messageEncoded, _ := abiPack(
		[]abi.Type{abiBytes32, abiAddress, abiBytes32, abiUint256, abiBytes32},
		[]interface{}{
			clobAuthTypeHash,
			common.HexToAddress(addr),
			timestampHash,
			big.NewInt(0),
			clobAuthMessage,
		},
	)
	messageHash := crypto.Keccak256Hash(messageEncoded)

	rawData := make([]byte, 0, 66)
	rawData = append(rawData, 0x19, 0x01)
	rawData = append(rawData, domainSeparator[:]...)
	rawData = append(rawData, messageHash[:]...)
	digest := crypto.Keccak256(rawData)

	// Decode signature and adjust v for ecrecover
	sigBytes, _ := hex.DecodeString(sig[2:])
	sigBytes[64] -= 27

	// Recover public key
	pubKey, err := crypto.Ecrecover(digest, sigBytes)
	if err != nil {
		t.Fatalf("ecrecover failed: %v", err)
	}

	recoveredPub, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		t.Fatalf("unmarshal pubkey failed: %v", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*recoveredPub).Hex()
	if recoveredAddr != addr {
		t.Errorf("address mismatch:\n  expected: %s\n  recovered: %s", addr, recoveredAddr)
	}
}

func TestSignRequestHMAC(t *testing.T) {
	key := testKey(t)
	addr := privateKeyToAddress(key)

	// Create a client with mock credentials (skip API derivation)
	secret := base64.URLEncoding.EncodeToString([]byte("test-secret-key-32-bytes-long!!!"))
	c := &Client{
		privateKey: key,
		address:    addr,
		chainID:    137,
		creds: &apiCredentials{
			APIKey:     "test-api-key",
			Secret:     secret,
			Passphrase: "test-passphrase",
		},
	}

	req, _ := http.NewRequest("GET", "https://clob.polymarket.com/orders", nil)
	err := c.signRequest(req, "")
	if err != nil {
		t.Fatalf("signRequest failed: %v", err)
	}

	// Verify headers are set
	if req.Header.Get("POLY_ADDRESS") != addr {
		t.Errorf("expected POLY_ADDRESS %s, got %s", addr, req.Header.Get("POLY_ADDRESS"))
	}
	if req.Header.Get("POLY_API_KEY") != "test-api-key" {
		t.Errorf("expected POLY_API_KEY test-api-key, got %s", req.Header.Get("POLY_API_KEY"))
	}
	if req.Header.Get("POLY_PASSPHRASE") != "test-passphrase" {
		t.Errorf("expected POLY_PASSPHRASE test-passphrase, got %s", req.Header.Get("POLY_PASSPHRASE"))
	}

	// Verify signature is a valid base64 string
	timestamp := req.Header.Get("POLY_TIMESTAMP")
	if timestamp == "" {
		t.Fatal("expected POLY_TIMESTAMP to be set")
	}

	sigStr := req.Header.Get("POLY_SIGNATURE")
	sigBytes, err := base64.URLEncoding.DecodeString(sigStr)
	if err != nil {
		t.Fatalf("signature is not valid base64: %v", err)
	}
	if len(sigBytes) != 32 {
		t.Errorf("expected 32-byte HMAC-SHA256, got %d bytes", len(sigBytes))
	}

	// Verify the HMAC manually
	message := timestamp + "GET" + "/orders" + ""
	secretBytes, _ := base64.URLEncoding.DecodeString(secret)
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	expected := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	if sigStr != expected {
		t.Errorf("HMAC mismatch:\n  got:  %s\n  want: %s", sigStr, expected)
	}
}

func TestSideToUint8(t *testing.T) {
	if sideToUint8("BUY") != "0" {
		t.Errorf("expected BUY=0, got %s", sideToUint8("BUY"))
	}
	if sideToUint8("buy") != "0" {
		t.Errorf("expected buy=0, got %s", sideToUint8("buy"))
	}
	if sideToUint8("SELL") != "1" {
		t.Errorf("expected SELL=1, got %s", sideToUint8("SELL"))
	}
	if sideToUint8("1") != "1" {
		t.Errorf("expected 1=1, got %s", sideToUint8("1"))
	}
}

func TestParseSideToUint8(t *testing.T) {
	tests := []struct {
		name string
		side string
		want uint8
	}{
		{name: "BUY text", side: "BUY", want: 0},
		{name: "SELL text", side: "SELL", want: 1},
		{name: "BUY numeric", side: "0", want: 0},
		{name: "SELL numeric", side: "1", want: 1},
	}

	for _, tt := range tests {
		if got := parseSideToUint8(tt.side); got != tt.want {
			t.Errorf("%s: parseSideToUint8(%q)=%d, want %d", tt.name, tt.side, got, tt.want)
		}
	}
}

func TestOrderTypeHashMatchesOnchainV2(t *testing.T) {
	// Pinned to the deployed V2 CTF Exchange's ORDER_TYPEHASH constant
	// (0xE111180000d2663C0091e4f400237545B87B996B on Polygon). A mismatch here means
	// the CLOB will reject every order with "invalid EOA signature".
	const onchain = "0xbb86318a2138f5fa8ae32fbe8e659f8fcf13cc6ae4014a707893055433818589"
	if got := orderTypeHash.Hex(); got != onchain {
		t.Fatalf("orderTypeHash drifted from on-chain V2 ORDER_TYPEHASH:\n got  %s\n want %s", got, onchain)
	}
}

func TestHashEIP712Order_UsesCLOBV2Fields(t *testing.T) {
	keyBytes, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	key, _ := crypto.ToECDSA(keyBytes)
	order, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.55, 10, Buy, privateKeyToAddress(key), SigTypeEOA, 137, false, 0.01)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}

	baseHash, err := hashEIP712Order(order, 137, false)
	if err != nil {
		t.Fatalf("hashEIP712Order failed: %v", err)
	}

	// taker, nonce, and feeRateBps remain V1-only fields that are excluded from
	// the V2 hash; mutating them must not change the digest.
	legacyOnly := *order
	legacyOnly.Taker = "0x1111111111111111111111111111111111111111"
	legacyOnly.Nonce = "7"
	legacyOnly.FeeRateBps = "123"
	legacyHash, err := hashEIP712Order(&legacyOnly, 137, false)
	if err != nil {
		t.Fatalf("hashEIP712Order legacy mutation failed: %v", err)
	}
	if legacyHash != baseHash {
		t.Fatalf("expected legacy V1-only fields (taker/nonce/feeRateBps) to be excluded from V2 hash")
	}

	// The on-chain V2 Order struct has NO expiration field — GTD expiry is enforced
	// off-chain by the CLOB, not committed to the signature. Mutating Expiration must
	// therefore NOT change the digest (signing it would break "invalid EOA signature").
	expirationChanged := *order
	expirationChanged.Expiration = "9999999999"
	expirationHash, err := hashEIP712Order(&expirationChanged, 137, false)
	if err != nil {
		t.Fatalf("hashEIP712Order expiration mutation failed: %v", err)
	}
	if expirationHash != baseHash {
		t.Fatalf("expiration must be excluded from the V2 hash (not part of on-chain ORDER_TYPEHASH)")
	}

	timestampChanged := *order
	ts, ok := new(big.Int).SetString(order.Timestamp, 10)
	if !ok {
		t.Fatalf("invalid timestamp %q", order.Timestamp)
	}
	timestampChanged.Timestamp = new(big.Int).Add(ts, big.NewInt(1)).String()
	timestampHash, err := hashEIP712Order(&timestampChanged, 137, false)
	if err != nil {
		t.Fatalf("hashEIP712Order timestamp mutation failed: %v", err)
	}
	if timestampHash == baseHash {
		t.Fatalf("expected timestamp to be included in V2 hash")
	}
}

func TestSignOrder_CLOBV2RecoverAddress(t *testing.T) {
	keyBytes, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	key, _ := crypto.ToECDSA(keyBytes)
	addr := privateKeyToAddress(key)
	order, err := buildOrder(key, "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.55, 10, Buy, addr, SigTypeEOA, 137, false, 0.01)
	if err != nil {
		t.Fatalf("buildOrder failed: %v", err)
	}

	digest, err := hashEIP712Order(order, 137, false)
	if err != nil {
		t.Fatalf("hashEIP712Order failed: %v", err)
	}

	sigBytes, err := hex.DecodeString(order.Signature[2:])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	sigBytes[64] -= 27

	pubKey, err := crypto.Ecrecover(digest.Bytes(), sigBytes)
	if err != nil {
		t.Fatalf("ecrecover failed: %v", err)
	}
	recoveredPub, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		t.Fatalf("unmarshal pubkey failed: %v", err)
	}
	recoveredAddr := crypto.PubkeyToAddress(*recoveredPub).Hex()
	if recoveredAddr != addr {
		t.Fatalf("address mismatch: expected %s recovered %s", addr, recoveredAddr)
	}
}
