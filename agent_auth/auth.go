// Package agentauth implements off-chain Ethereum challenge-response
// authentication for agents. It contains protocol and cryptographic logic only;
// callers remain responsible for storage, permission lookup, and HTTP routing.
package agentauth

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	DefaultVersion          = "1"
	DefaultSessionIssuer    = "gowild-agent-manager"
	DefaultSessionClaimType = "agent_session"
	BearerTokenType         = "Bearer"
)

type ChallengeRequest struct {
	AgentID string `json:"agent_id"`
	Address string `json:"address"`
}

type Challenge struct {
	AgentID        string `json:"agent_id"`
	Address        string `json:"address"`
	Domain         string `json:"domain"`
	Statement      string `json:"statement"`
	URI            string `json:"uri"`
	Version        string `json:"version"`
	ChainID        int    `json:"chain_id"`
	Nonce          string `json:"nonce"`
	IssuedAt       string `json:"issued_at"`
	ExpirationTime string `json:"expiration_time"`
	RequestID      string `json:"request_id"`
	Message        string `json:"message"`
}

type ChallengeOptions struct {
	AgentID   string
	Address   string
	Domain    string
	Statement string
	URI       string
	Version   string
	ChainID   int
	Nonce     string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RequestID string
}

type VerifyRequest struct {
	AgentID   string `json:"agent_id"`
	Address   string `json:"address"`
	Nonce     string `json:"nonce"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

type VerifyResponse struct {
	SessionToken string `json:"session_token"`
	TokenType    string `json:"token_type"`
	AgentID      string `json:"agent_id"`
	Address      string `json:"address"`
	ExpiresAt    string `json:"expires_at"`
}

type Session struct {
	AgentID   string
	Address   string
	ExpiresAt time.Time
}

type sessionClaims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Address   string `json:"address"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	TokenType string `json:"typ"`
}

// NewChallenge creates a SIWE-like challenge and signs nothing. If opts.Nonce
// is empty, a cryptographically secure nonce is generated.
func NewChallenge(opts ChallengeOptions) (Challenge, error) {
	agentID, err := NormalizeAgentID(opts.AgentID)
	if err != nil {
		return Challenge{}, err
	}
	address, err := NormalizeAddress(opts.Address)
	if err != nil {
		return Challenge{}, err
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = DefaultVersion
	}
	issuedAt := opts.IssuedAt.UTC().Truncate(time.Second)
	if issuedAt.IsZero() {
		return Challenge{}, fmt.Errorf("issued_at is required")
	}
	expiresAt := opts.ExpiresAt.UTC().Truncate(time.Second)
	if expiresAt.IsZero() {
		return Challenge{}, fmt.Errorf("expiration_time is required")
	}
	if !expiresAt.After(issuedAt) {
		return Challenge{}, fmt.Errorf("expiration_time must be after issued_at")
	}
	nonce := strings.TrimSpace(opts.Nonce)
	if nonce == "" {
		nonce, err = GenerateNonce()
		if err != nil {
			return Challenge{}, err
		}
	}
	requestID := strings.TrimSpace(opts.RequestID)
	if requestID == "" {
		requestID = agentID
	}

	challenge := Challenge{
		AgentID:        agentID,
		Address:        address,
		Domain:         strings.TrimSpace(opts.Domain),
		Statement:      strings.TrimSpace(opts.Statement),
		URI:            strings.TrimSpace(opts.URI),
		Version:        version,
		ChainID:        opts.ChainID,
		Nonce:          nonce,
		IssuedAt:       issuedAt.Format(time.RFC3339),
		ExpirationTime: expiresAt.Format(time.RFC3339),
		RequestID:      requestID,
	}
	message, err := BuildSignInMessage(challenge)
	if err != nil {
		return Challenge{}, err
	}
	challenge.Message = message
	return challenge, nil
}

func BuildSignInMessage(ch Challenge) (string, error) {
	fields := []struct {
		name  string
		value string
	}{
		{"agent_id", ch.AgentID},
		{"address", ch.Address},
		{"domain", ch.Domain},
		{"statement", ch.Statement},
		{"uri", ch.URI},
		{"version", ch.Version},
		{"nonce", ch.Nonce},
		{"issued_at", ch.IssuedAt},
		{"expiration_time", ch.ExpirationTime},
		{"request_id", ch.RequestID},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return "", fmt.Errorf("%s is required", field.name)
		}
		if strings.ContainsAny(field.value, "\r\n") {
			return "", fmt.Errorf("%s cannot contain newlines", field.name)
		}
	}
	if ch.ChainID <= 0 {
		return "", fmt.Errorf("chain_id is required")
	}
	address, err := NormalizeAddress(ch.Address)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`%s wants you to sign in with your Ethereum account:
%s

%s

URI: %s
Version: %s
Chain ID: %d
Nonce: %s
Issued At: %s
Expiration Time: %s
Request ID: %s`,
		ch.Domain,
		address,
		ch.Statement,
		ch.URI,
		ch.Version,
		ch.ChainID,
		ch.Nonce,
		ch.IssuedAt,
		ch.ExpirationTime,
		ch.RequestID,
	), nil
}

// VerifyChallenge validates the challenge fields, expiration, message, and
// Ethereum personal_sign signature. Nonce replay prevention is intentionally
// left to the caller's challenge storage.
func VerifyChallenge(ch Challenge, req VerifyRequest, now time.Time) error {
	if strings.TrimSpace(req.Nonce) == "" {
		return fmt.Errorf("nonce is required")
	}
	if strings.TrimSpace(req.Nonce) != strings.TrimSpace(ch.Nonce) {
		return fmt.Errorf("nonce does not match challenge")
	}
	expiresAt, err := ParseChallengeExpiration(ch)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !now.UTC().Before(expiresAt) {
		return fmt.Errorf("invalid or expired nonce")
	}

	agentID, err := NormalizeAgentID(req.AgentID)
	if err != nil {
		return err
	}
	if agentID != strings.TrimSpace(ch.AgentID) {
		return fmt.Errorf("agent_id does not match challenge")
	}
	if !SameAddress(req.Address, ch.Address) {
		return fmt.Errorf("address does not match challenge")
	}

	message, err := canonicalChallengeMessage(ch)
	if err != nil {
		return err
	}
	if req.Message != message {
		return fmt.Errorf("message does not match challenge")
	}

	valid, err := VerifyEthereumSignature(ch.Address, message, req.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}
	if !valid {
		return fmt.Errorf("signature does not recover registered agent address")
	}
	return nil
}

func ParseChallengeExpiration(ch Challenge) (time.Time, error) {
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(ch.ExpirationTime))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid challenge expiration_time")
	}
	return expiresAt.UTC(), nil
}

func GenerateSessionToken(secret []byte, agentID, address string, now time.Time, ttl time.Duration) (string, time.Time, error) {
	if len(secret) == 0 {
		return "", time.Time{}, fmt.Errorf("session signing secret is empty")
	}
	agentID, err := NormalizeAgentID(agentID)
	if err != nil {
		return "", time.Time{}, err
	}
	address, err = NormalizeAddress(address)
	if err != nil {
		return "", time.Time{}, err
	}
	if ttl <= 0 {
		return "", time.Time{}, fmt.Errorf("session ttl must be positive")
	}

	now = now.UTC().Truncate(time.Second)
	expiresAt := now.Add(ttl).UTC()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := sessionClaims{
		Issuer:    DefaultSessionIssuer,
		Subject:   agentID,
		Address:   address,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
		TokenType: DefaultSessionClaimType,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}

	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := signSessionToken(secret, unsigned)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), expiresAt, nil
}

func ValidateSessionToken(secret []byte, token string, now time.Time) (*Session, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("session signing secret is empty")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed session token")
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("malformed session signature")
	}
	if !hmac.Equal(sig, signSessionToken(secret, signingInput)) {
		return nil, fmt.Errorf("session signature mismatch")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("malformed session header")
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("malformed session header")
	}
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		return nil, fmt.Errorf("unsupported session token header")
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("malformed session claims")
	}
	var claims sessionClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, fmt.Errorf("malformed session claims")
	}
	if claims.Issuer != DefaultSessionIssuer || claims.TokenType != DefaultSessionClaimType {
		return nil, fmt.Errorf("invalid session issuer")
	}
	agentID, err := NormalizeAgentID(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("session subject is empty")
	}
	address, err := NormalizeAddress(claims.Address)
	if err != nil {
		return nil, fmt.Errorf("session address is invalid")
	}

	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	if !now.UTC().Before(expiresAt) {
		return nil, fmt.Errorf("session token expired")
	}

	return &Session{
		AgentID:   agentID,
		Address:   address,
		ExpiresAt: expiresAt,
	}, nil
}

func VerifyEthereumSignature(address string, message string, signatureHex string) (bool, error) {
	if _, err := NormalizeAddress(address); err != nil {
		return false, err
	}
	recovered, err := RecoverEthereumAddress(message, signatureHex)
	if err != nil {
		return false, err
	}
	return SameAddress(recovered, address), nil
}

// RecoverEthereumAddress performs the ecrecover step for an Ethereum
// personal_sign message. accounts.TextHash applies the EIP-191 prefix and
// Keccak-256 hash before public key recovery.
func RecoverEthereumAddress(message string, signatureHex string) (string, error) {
	signature, err := hexutil.Decode(strings.TrimSpace(signatureHex))
	if err != nil {
		return "", fmt.Errorf("invalid signature format: %w", err)
	}
	if len(signature) != 65 {
		return "", fmt.Errorf("invalid signature length: expected 65 bytes, got %d", len(signature))
	}
	if signature[64] >= 27 {
		signature[64] -= 27
	}

	msgHash := accounts.TextHash([]byte(message))
	publicKey, err := crypto.SigToPub(msgHash, signature)
	if err != nil {
		return "", fmt.Errorf("failed to recover public key: %w", err)
	}
	return crypto.PubkeyToAddress(*publicKey).Hex(), nil
}

func AddressFromPublicKey(publicKey *ecdsa.PublicKey) (string, error) {
	if publicKey == nil {
		return "", fmt.Errorf("public key is required")
	}
	return crypto.PubkeyToAddress(*publicKey).Hex(), nil
}

func AddressFromPublicKeyBytes(publicKeyBytes []byte) (string, error) {
	publicKey, err := crypto.UnmarshalPubkey(publicKeyBytes)
	if err != nil {
		publicKey, err = crypto.DecompressPubkey(publicKeyBytes)
		if err != nil {
			return "", fmt.Errorf("invalid secp256k1 public key: %w", err)
		}
	}
	return AddressFromPublicKey(publicKey)
}

func GenerateNonce() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func NormalizeAgentID(agentID string) (string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	if strings.ContainsAny(agentID, "\r\n") {
		return "", fmt.Errorf("agent_id cannot contain newlines")
	}
	return agentID, nil
}

func NormalizeAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if !common.IsHexAddress(address) {
		return "", fmt.Errorf("valid ethereum address is required")
	}
	return common.HexToAddress(address).Hex(), nil
}

func SameAddress(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if !common.IsHexAddress(a) || !common.IsHexAddress(b) {
		return false
	}
	return common.HexToAddress(a) == common.HexToAddress(b)
}

func canonicalChallengeMessage(ch Challenge) (string, error) {
	message, err := BuildSignInMessage(ch)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ch.Message) != "" && ch.Message != message {
		return "", fmt.Errorf("challenge message is invalid")
	}
	return message, nil
}

func signSessionToken(secret []byte, signingInput string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}
