package gowild_polymarket

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// EIP-712 ABI types used for manual encoding (matching Polymarket's go-order-utils approach).
var (
	abiBytes32, _ = abi.NewType("bytes32", "", nil)
	abiUint256, _ = abi.NewType("uint256", "", nil)
	abiAddress, _ = abi.NewType("address", "", nil)
	abiUint8, _   = abi.NewType("uint8", "", nil)
)

// Pre-computed type hashes for ClobAuth EIP-712 signing.
var (
	clobAuthDomainTypeHash = crypto.Keccak256Hash(
		[]byte("EIP712Domain(string name,string version,uint256 chainId)"),
	)
	clobAuthTypeHash = crypto.Keccak256Hash(
		[]byte("ClobAuth(address address,string timestamp,uint256 nonce,string message)"),
	)
	clobAuthDomainName    = crypto.Keccak256Hash([]byte("ClobAuthDomain"))
	clobAuthDomainVersion = crypto.Keccak256Hash([]byte("1"))
	clobAuthMessage       = crypto.Keccak256Hash([]byte("This message attests that I control the given wallet"))
)

// Pre-computed type hashes for Order EIP-712 signing.
var (
	orderDomainTypeHash = crypto.Keccak256Hash(
		[]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
	)
	// orderTypeHash must match the on-chain V2 CTF Exchange ORDER_TYPEHASH exactly,
	// or the CLOB rejects orders with "invalid EOA signature". The struct has NO
	// expiration field — GTD expiry is enforced off-chain by the CLOB, not signed.
	// Verified against the deployed exchange (0xE111180000d2663C0091e4f400237545B87B996B):
	// keccak256(...) == 0xbb86318a2138f5fa8ae32fbe8e659f8fcf13cc6ae4014a707893055433818589
	orderTypeHash = crypto.Keccak256Hash(
		[]byte("Order(uint256 salt,address maker,address signer,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side,uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"),
	)
	orderDomainName    = crypto.Keccak256Hash([]byte("Polymarket CTF Exchange"))
	orderDomainVersion = crypto.Keccak256Hash([]byte("2"))
)

const zeroBytes32 = "0x0000000000000000000000000000000000000000000000000000000000000000"

// privateKeyToAddress returns the Ethereum address for a private key.
func privateKeyToAddress(key *ecdsa.PrivateKey) string {
	return crypto.PubkeyToAddress(key.PublicKey).Hex()
}

// abiPack encodes values using ABI encoding (matching Polymarket's go-order-utils Encode function).
func abiPack(types []abi.Type, values []interface{}) ([]byte, error) {
	args := make(abi.Arguments, len(types))
	for i, t := range types {
		args[i] = abi.Argument{Type: t}
	}
	return args.Pack(values...)
}

// createOrDeriveAPICredentials creates CLOB API credentials (or derives existing ones) from an L1 EIP-712 signature.
// It first tries POST /auth/api-key (create), and if that fails, falls back to GET /auth/derive-api-key (derive).
// If httpClient is nil, a default client is used.
func createOrDeriveAPICredentials(privateKey *ecdsa.PrivateKey, chainID int, httpClient *http.Client) (*apiCredentials, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	// Try creating first
	creds, err := callAuthEndpoint(privateKey, chainID, httpClient, http.MethodPost, "/auth/api-key")
	if err == nil {
		return creds, nil
	}

	// Fall back to deriving existing credentials
	return callAuthEndpoint(privateKey, chainID, httpClient, http.MethodGet, "/auth/derive-api-key")
}

// callAuthEndpoint calls a CLOB auth endpoint with L1 EIP-712 headers.
func callAuthEndpoint(privateKey *ecdsa.PrivateKey, chainID int, httpClient *http.Client, method, path string) (*apiCredentials, error) {
	address := privateKeyToAddress(privateKey)
	timestamp := time.Now().Unix()
	var nonce int64 = 0

	sig, err := signClobAuth(privateKey, address, timestamp, nonce, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to sign ClobAuth: %w", err)
	}

	req, err := http.NewRequest(method, clobBaseURL+path, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("POLY_ADDRESS", address)
	req.Header.Set("POLY_SIGNATURE", sig)
	req.Header.Set("POLY_TIMESTAMP", strconv.FormatInt(timestamp, 10))
	req.Header.Set("POLY_NONCE", strconv.FormatInt(nonce, 10))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s request failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s %s failed with status %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	var creds apiCredentials
	if err := decodeJSON(resp.Body, &creds); err != nil {
		return nil, fmt.Errorf("failed to decode API credentials: %w", err)
	}

	return &creds, nil
}

// signClobAuth signs a ClobAuth EIP-712 message using manual ABI encoding
// (matching Polymarket's go-order-utils approach instead of apitypes.TypedData).
func signClobAuth(privateKey *ecdsa.PrivateKey, address string, timestamp, nonce int64, chainID int) (string, error) {
	// Build domain separator (no verifyingContract)
	domainEncoded, err := abiPack(
		[]abi.Type{abiBytes32, abiBytes32, abiBytes32, abiUint256},
		[]interface{}{
			clobAuthDomainTypeHash,
			clobAuthDomainName,
			clobAuthDomainVersion,
			big.NewInt(int64(chainID)),
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to encode domain: %w", err)
	}
	domainSeparator := crypto.Keccak256Hash(domainEncoded)

	// Build message hash
	timestampHash := crypto.Keccak256Hash([]byte(strconv.FormatInt(timestamp, 10)))
	messageEncoded, err := abiPack(
		[]abi.Type{abiBytes32, abiAddress, abiBytes32, abiUint256, abiBytes32},
		[]interface{}{
			clobAuthTypeHash,
			common.HexToAddress(address),
			timestampHash,
			big.NewInt(nonce),
			clobAuthMessage,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to encode message: %w", err)
	}
	messageHash := crypto.Keccak256Hash(messageEncoded)

	// EIP-712: \x19\x01 + domainSeparator + messageHash
	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator[:]), string(messageHash[:])))
	digest := crypto.Keccak256(rawData)

	sig, err := crypto.Sign(digest, privateKey)
	if err != nil {
		return "", err
	}

	// Adjust v value from 0/1 to 27/28
	sig[64] += 27

	return "0x" + hex.EncodeToString(sig), nil
}

// signRequest signs an HTTP request with L2 HMAC-SHA256 credentials.
func (c *Client) signRequest(req *http.Request, body string) error {
	if c.creds == nil {
		if c.credsErr != nil {
			return c.credsErr
		}
		return fmt.Errorf("API credentials not initialized")
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	method := req.Method
	path := req.URL.Path

	// Build the message to sign: timestamp + method + path + body
	message := timestamp + method + path + body

	// HMAC-SHA256 with base64url-decoded secret (Polymarket uses URL-safe base64)
	secretBytes, err := base64.URLEncoding.DecodeString(c.creds.Secret)
	if err != nil {
		return fmt.Errorf("failed to decode secret: %w", err)
	}
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	signature := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("POLY_ADDRESS", c.address)
	req.Header.Set("POLY_SIGNATURE", signature)
	req.Header.Set("POLY_TIMESTAMP", timestamp)
	req.Header.Set("POLY_API_KEY", c.creds.APIKey)
	req.Header.Set("POLY_PASSPHRASE", c.creds.Passphrase)

	return nil
}

// hashEIP712Order hashes an order using EIP-712 for signing, using manual ABI encoding
// (matching Polymarket's go-order-utils approach).
func hashEIP712Order(order *signedOrder, chainID int, negRisk bool) (common.Hash, error) {
	exchangeAddr := ctfExchangeAddress
	if negRisk {
		exchangeAddr = negRiskCTFExchangeAddress
	}

	// Build domain separator (with verifyingContract)
	domainEncoded, err := abiPack(
		[]abi.Type{abiBytes32, abiBytes32, abiBytes32, abiUint256, abiAddress},
		[]interface{}{
			orderDomainTypeHash,
			orderDomainName,
			orderDomainVersion,
			big.NewInt(int64(chainID)),
			common.HexToAddress(exchangeAddr),
		},
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to encode domain: %w", err)
	}
	domainSeparator := crypto.Keccak256Hash(domainEncoded)

	// Parse order fields to big.Int
	tokenId, ok := new(big.Int).SetString(order.TokenID, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid tokenId: %s", order.TokenID)
	}
	makerAmount, ok := new(big.Int).SetString(order.MakerAmount, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid makerAmount: %s", order.MakerAmount)
	}
	takerAmount, ok := new(big.Int).SetString(order.TakerAmount, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid takerAmount: %s", order.TakerAmount)
	}
	timestamp, ok := new(big.Int).SetString(order.Timestamp, 10)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid timestamp: %s", order.Timestamp)
	}
	metadata, err := parseBytes32(order.Metadata)
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid metadata: %w", err)
	}
	builder, err := parseBytes32(order.Builder)
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid builder: %w", err)
	}

	// Build message hash
	messageEncoded, err := abiPack(
		[]abi.Type{abiBytes32, abiUint256, abiAddress, abiAddress, abiUint256, abiUint256, abiUint256, abiUint8, abiUint8, abiUint256, abiBytes32, abiBytes32},
		[]interface{}{
			orderTypeHash,
			order.Salt,
			common.HexToAddress(order.Maker),
			common.HexToAddress(order.Signer),
			tokenId,
			makerAmount,
			takerAmount,
			parseSideToUint8(order.Side),
			uint8(order.SignatureType),
			timestamp,
			metadata,
			builder,
		},
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to encode message: %w", err)
	}

	// EIP-712: \x19\x01 + domainSeparator + keccak256(messageEncoded)
	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator[:]), string(crypto.Keccak256Hash(messageEncoded).Bytes())))
	return crypto.Keccak256Hash(rawData), nil
}

func parseBytes32(value string) (common.Hash, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = zeroBytes32
	}
	if !strings.HasPrefix(value, "0x") {
		return common.Hash{}, fmt.Errorf("missing 0x prefix")
	}
	if len(value) != 66 {
		return common.Hash{}, fmt.Errorf("expected 32-byte hex string, got %d chars", len(value)-2)
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(raw), nil
}

func sideToUint8(side string) string {
	if side == "BUY" || side == "buy" || side == "0" {
		return "0"
	}
	return "1"
}

// parseSideToUint8 converts side string ("BUY"/"SELL" or "0"/"1") to uint8 for EIP-712 encoding.
func parseSideToUint8(side string) uint8 {
	if side == "1" || side == "SELL" || side == "sell" {
		return 1
	}
	return 0
}

// signOrder signs an order with the private key using EIP-712.
func signOrder(privateKey *ecdsa.PrivateKey, order *signedOrder, chainID int, negRisk bool) (string, error) {
	digest, err := hashEIP712Order(order, chainID, negRisk)
	if err != nil {
		return "", err
	}

	sig, err := crypto.Sign(digest.Bytes(), privateKey)
	if err != nil {
		return "", err
	}

	sig[64] += 27

	return "0x" + hex.EncodeToString(sig), nil
}

// generateSalt generates a random salt for order signing.
func generateSalt() *big.Int {
	return big.NewInt(time.Now().UnixNano())
}
