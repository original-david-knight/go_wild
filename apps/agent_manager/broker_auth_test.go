package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentauth "github.com/original-david-knight/go_wild/agent_auth"
	"github.com/original-david-knight/go_wild/agent_data"
	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	gowild_data "github.com/original-david-knight/go_wild/data"
	brokerprotocol "github.com/original-david-knight/go_wild/tools/broker"
)

func TestGenerateAndValidateAgentSessionToken(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long!!!")
	agentID := "jake"
	address := "0x000000000000000000000000000000000000dEaD"
	now := time.Unix(1_700_000_000, 0).UTC()

	token, expiresAt, err := agentauth.GenerateSessionToken(secret, agentID, address, now, time.Hour)
	if err != nil {
		t.Fatalf("GenerateAgentSessionToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if !strings.HasPrefix(token, "ey") || strings.Count(token, ".") != 2 {
		t.Fatalf("expected JWT-shaped token, got %q", token)
	}

	session, err := agentauth.ValidateSessionToken(secret, token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ValidateAgentSessionToken failed: %v", err)
	}
	if session.AgentID != agentID {
		t.Errorf("expected agent ID %q, got %q", agentID, session.AgentID)
	}
	if !agentauth.SameAddress(session.Address, address) {
		t.Errorf("expected address %q, got %q", address, session.Address)
	}
	if !session.ExpiresAt.Equal(expiresAt) {
		t.Errorf("expected expiry %s, got %s", expiresAt, session.ExpiresAt)
	}
}

func TestValidateAgentSessionTokenInvalid(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-long!!!")
	now := time.Unix(1_700_000_000, 0).UTC()
	token, _, err := agentauth.GenerateSessionToken(secret, "jake", "0x000000000000000000000000000000000000dEaD", now, time.Hour)
	if err != nil {
		t.Fatalf("GenerateAgentSessionToken failed: %v", err)
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
			if _, err := agentauth.ValidateSessionToken(secret, tc.token, tc.now); err == nil {
				t.Error("expected invalid token")
			}
		})
	}
}

func TestBrokerAuthChallengeVerifyFlow(t *testing.T) {
	db := setupManagerTestDB(t)
	secret := []byte("test-secret-key-32-bytes-long!!!")
	agentID := "agent-auth-flow"
	_, address, privateKey := setupBrokerAuthTestAgent(t, db, agentID)

	auth := NewBrokerAuthHandler(NewAgentService(db), secret)
	auth.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

	challenge := postAuthChallenge(t, auth, brokerprotocol.AuthChallengeRequest{
		AgentID: agentID,
		Address: address,
	})
	message, err := brokerprotocol.BuildSignInMessage(challenge)
	if err != nil {
		t.Fatalf("BuildSignInMessage failed: %v", err)
	}
	if challenge.Message != message {
		t.Fatalf("challenge message mismatch")
	}

	wallet, err := gowild_crypto.NewWallet(gowild_crypto.WalletConfig{EthPrivateKey: privateKey})
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	signed, err := wallet.SignMessage(gowild_crypto.ChainEthereum, message)
	if err != nil {
		t.Fatalf("SignMessage failed: %v", err)
	}

	verify := postAuthVerify(t, auth, brokerprotocol.AuthVerifyRequest{
		AgentID:   agentID,
		Address:   address,
		Nonce:     challenge.Nonce,
		Message:   message,
		Signature: signed.Signature,
	})
	if verify.TokenType != "Bearer" || verify.SessionToken == "" {
		t.Fatalf("expected bearer session token, got %#v", verify)
	}

	session, err := auth.ValidateSessionToken(t.Context(), verify.SessionToken)
	if err != nil {
		t.Fatalf("ValidateSessionToken failed: %v", err)
	}
	if session.AgentID != agentID || !agentauth.SameAddress(session.Address, address) {
		t.Fatalf("unexpected session: %#v", session)
	}
}

func TestBrokerAuthRejectsReplay(t *testing.T) {
	db := setupManagerTestDB(t)
	secret := []byte("test-secret-key-32-bytes-long!!!")
	agentID := "agent-auth-replay"
	_, address, privateKey := setupBrokerAuthTestAgent(t, db, agentID)
	auth := NewBrokerAuthHandler(NewAgentService(db), secret)

	challenge := postAuthChallenge(t, auth, brokerprotocol.AuthChallengeRequest{AgentID: agentID, Address: address})
	message, err := brokerprotocol.BuildSignInMessage(challenge)
	if err != nil {
		t.Fatalf("BuildSignInMessage failed: %v", err)
	}
	wallet, err := gowild_crypto.NewWallet(gowild_crypto.WalletConfig{EthPrivateKey: privateKey})
	if err != nil {
		t.Fatalf("NewWallet failed: %v", err)
	}
	signed, err := wallet.SignMessage(gowild_crypto.ChainEthereum, message)
	if err != nil {
		t.Fatalf("SignMessage failed: %v", err)
	}

	req := brokerprotocol.AuthVerifyRequest{
		AgentID:   agentID,
		Address:   address,
		Nonce:     challenge.Nonce,
		Message:   message,
		Signature: signed.Signature,
	}
	postAuthVerify(t, auth, req)

	rec := httptest.NewRecorder()
	auth.handleVerify(rec, jsonRequest(t, req))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected replay to be rejected with 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBrokerSessionAuthMiddleware(t *testing.T) {
	db := setupManagerTestDB(t)
	secret := []byte("test-secret-key-32-bytes-long!!!")
	agentID := "agent-middleware"
	_, address, _ := setupBrokerAuthTestAgent(t, db, agentID)
	token, _, err := agentauth.GenerateSessionToken(secret, agentID, address, time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("GenerateAgentSessionToken failed: %v", err)
	}

	auth := NewBrokerAuthHandler(NewAgentService(db), secret)
	var gotAgentID, gotAddress string
	handler := brokerSessionAuthMiddleware(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgentID = BrokerAgentID(r.Context())
		gotAddress = BrokerAgentAddress(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/broker/v1/tools/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAgentID != agentID {
		t.Fatalf("expected agent ID %q, got %q", agentID, gotAgentID)
	}
	if !agentauth.SameAddress(gotAddress, address) {
		t.Fatalf("expected address %q, got %q", address, gotAddress)
	}

	req = httptest.NewRequest(http.MethodGet, "/broker/v1/tools/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing auth, got %d", rec.Code)
	}
}

func setupBrokerAuthTestAgent(t *testing.T, db gowild_data.Database, agentID string) (*data.Agent, string, string) {
	t.Helper()
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(t.Context())
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(agent.WalletSeedPhrase, agentAuthDerivationIndex)
	if err != nil {
		t.Fatalf("DeriveKeysFromMnemonic failed: %v", err)
	}
	return agent, derived.EthAddress, derived.EthPrivateKey
}

func testSessionTokenForAgent(t *testing.T, db gowild_data.Database, secret []byte, agentID string) string {
	t.Helper()
	_, address, _ := setupBrokerAuthTestAgent(t, db, agentID)
	token, _, err := agentauth.GenerateSessionToken(secret, agentID, address, time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("GenerateAgentSessionToken failed: %v", err)
	}
	return token
}

func testBrokerAuth(t *testing.T, db gowild_data.Database, secret []byte) *BrokerAuthHandler {
	t.Helper()
	return NewBrokerAuthHandler(NewAgentService(db), secret)
}

func postAuthChallenge(t *testing.T, auth *BrokerAuthHandler, req brokerprotocol.AuthChallengeRequest) brokerprotocol.AuthChallengeResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	auth.handleChallenge(rec, jsonRequest(t, req))
	if rec.Code != http.StatusOK {
		t.Fatalf("challenge failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp brokerprotocol.AuthChallengeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	return resp
}

func postAuthVerify(t *testing.T, auth *BrokerAuthHandler, req brokerprotocol.AuthVerifyRequest) brokerprotocol.AuthVerifyResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	auth.handleVerify(rec, jsonRequest(t, req))
	if rec.Code != http.StatusOK {
		t.Fatalf("verify failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp brokerprotocol.AuthVerifyResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	return resp
}

func jsonRequest(t *testing.T, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}
