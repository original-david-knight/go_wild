package agentauth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestChallengeMessageAndVerify(t *testing.T) {
	key, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe512961708279cf5f913e9bdf931318")
	if err != nil {
		t.Fatalf("HexToECDSA failed: %v", err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	now := time.Unix(1_700_000_000, 0).UTC()

	challenge, err := NewChallenge(ChallengeOptions{
		AgentID:   "agent-1",
		Address:   address,
		Domain:    "gowild-agent-manager",
		Statement: "Authenticate this agent with the Gowild broker.",
		URI:       "gowild://broker",
		ChainID:   1,
		Nonce:     "nonce-123",
		IssuedAt:  now,
		ExpiresAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("NewChallenge failed: %v", err)
	}
	if challenge.Message == "" {
		t.Fatal("expected challenge message")
	}
	if !strings.Contains(challenge.Message, "Nonce: nonce-123") {
		t.Fatalf("message missing nonce: %q", challenge.Message)
	}

	sig, err := crypto.Sign(accounts.TextHash([]byte(challenge.Message)), key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	sig[64] += 27

	err = VerifyChallenge(challenge, VerifyRequest{
		AgentID:   challenge.AgentID,
		Address:   address,
		Nonce:     challenge.Nonce,
		Message:   challenge.Message,
		Signature: hexutil.Encode(sig),
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("VerifyChallenge failed: %v", err)
	}
}

func TestVerifyChallengeRejectsExpiredNonce(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	challenge, err := NewChallenge(ChallengeOptions{
		AgentID:   "agent-1",
		Address:   "0x000000000000000000000000000000000000dEaD",
		Domain:    "gowild-agent-manager",
		Statement: "Authenticate this agent with the Gowild broker.",
		URI:       "gowild://broker",
		ChainID:   1,
		Nonce:     "nonce-123",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("NewChallenge failed: %v", err)
	}

	err = VerifyChallenge(challenge, VerifyRequest{
		AgentID: challenge.AgentID,
		Address: challenge.Address,
		Nonce:   challenge.Nonce,
		Message: challenge.Message,
	}, now.Add(2*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired nonce error, got %v", err)
	}
}

func TestGenerateNonce(t *testing.T) {
	nonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce failed: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil {
		t.Fatalf("nonce is not base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("expected 32-byte nonce, got %d", len(decoded))
	}
}

func TestSessionTokenRoundTrip(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long!!!")
	agentID := "agent-1"
	address := "0x000000000000000000000000000000000000dEaD"
	now := time.Unix(1_700_000_000, 0).UTC()

	token, expiresAt, err := GenerateSessionToken(secret, agentID, address, now, time.Hour)
	if err != nil {
		t.Fatalf("GenerateSessionToken failed: %v", err)
	}
	if token == "" || strings.Count(token, ".") != 2 {
		t.Fatalf("expected JWT-shaped token, got %q", token)
	}

	session, err := ValidateSessionToken(secret, token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ValidateSessionToken failed: %v", err)
	}
	if session.AgentID != agentID {
		t.Fatalf("expected agent %q, got %q", agentID, session.AgentID)
	}
	if !SameAddress(session.Address, address) {
		t.Fatalf("expected address %q, got %q", address, session.Address)
	}
	if !session.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected expiry %s, got %s", expiresAt, session.ExpiresAt)
	}
}

func TestValidateSessionTokenRejectsTamperAndExpiry(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long!!!")
	now := time.Unix(1_700_000_000, 0).UTC()
	token, _, err := GenerateSessionToken(secret, "agent-1", "0x000000000000000000000000000000000000dEaD", now, time.Hour)
	if err != nil {
		t.Fatalf("GenerateSessionToken failed: %v", err)
	}

	tests := []struct {
		name  string
		token string
		now   time.Time
	}{
		{"empty", "", now},
		{"malformed", "not-a-jwt", now},
		{"tampered", token[:len(token)-1] + "x", now},
		{"expired", token, now.Add(2 * time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateSessionToken(secret, tc.token, tc.now); err == nil {
				t.Fatal("expected invalid token")
			}
		})
	}
}
