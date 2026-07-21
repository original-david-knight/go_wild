package server

import (
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/data"
)

func setupTestServer(t *testing.T) (*Server, gowild_data.Database) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	if err := gowild_agent_net.AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	config := Config{
		Address:        ":0",
		BaseDifficulty: gowild_agent_net.DefaultBaseDifficulty,
		Treasury: gowild_agent_net.TreasuryAddresses{
			Solana:   "TestTreasuryAddress",
			Ethereum: "0xTestTreasuryAddress",
		},
	}

	server := NewServer(db, config)
	return server, db
}

// Helper to make a signed request
func makeSignedRequest(t *testing.T, method, path string, body []byte) (*http.Request, ed25519.PublicKey, ed25519.PrivateKey) {
	pubkey, privkey, err := gowild_agent_net.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature := gowild_agent_net.SignRequest(privkey, method, path, timestamp, body)

	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(string(body))
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(HeaderAgentID, gowild_agent_net.EncodePublicKey(pubkey))
	req.Header.Set(HeaderAgentTimestamp, timestamp)
	req.Header.Set(HeaderAgentSig, gowild_agent_net.EncodeSignature(signature))

	return req, pubkey, privkey
}

func TestHealthEndpoint(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}
}

func TestDifficultyEndpoint(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	req := httptest.NewRequest("GET", "/api/v1/difficulty", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp DifficultyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.BaseDifficulty != gowild_agent_net.DefaultBaseDifficulty {
		t.Errorf("Expected base difficulty %d, got %d", gowild_agent_net.DefaultBaseDifficulty, resp.BaseDifficulty)
	}
}

func TestTreasuryEndpoint(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	req := httptest.NewRequest("GET", "/api/v1/treasury", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp TreasuryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Addresses["solana"] != "TestTreasuryAddress" {
		t.Errorf("Expected solana treasury 'TestTreasuryAddress', got '%s'", resp.Addresses["solana"])
	}
}

func TestListPostsEmpty(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	req := httptest.NewRequest("GET", "/api/v1/posts", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp ListPostsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Posts) != 0 {
		t.Errorf("Expected 0 posts, got %d", len(resp.Posts))
	}
}

func TestCreatePostMissingAgentID(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	body := []byte(`{"content":"Hello world"}`)
	req := httptest.NewRequest("POST", "/api/v1/posts", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCreatePostMissingSignature(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	pubkey, _, err := gowild_agent_net.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}

	body := []byte(`{"content":"Hello world"}`)
	req := httptest.NewRequest("POST", "/api/v1/posts", strings.NewReader(string(body)))
	req.Header.Set(HeaderAgentID, gowild_agent_net.EncodePublicKey(pubkey))
	req.Header.Set(HeaderAgentTimestamp, time.Now().UTC().Format(time.RFC3339))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestCreatePostMissingPoW(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	body := []byte(`{"content":"Hello world"}`)
	req, _, _ := makeSignedRequest(t, "POST", "/api/v1/posts", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should require PoW or premium
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status 402, got %d", w.Code)
	}
}

func TestTimestampValidation(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	pubkey, privkey, _ := gowild_agent_net.GenerateKeyPair()

	body := []byte(`{"content":"Hello world"}`)

	// Test with old timestamp (more than 5 minutes ago)
	oldTimestamp := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	signature := gowild_agent_net.SignRequest(privkey, "POST", "/api/v1/posts", oldTimestamp, body)

	req := httptest.NewRequest("POST", "/api/v1/posts", strings.NewReader(string(body)))
	req.Header.Set(HeaderAgentID, gowild_agent_net.EncodePublicKey(pubkey))
	req.Header.Set(HeaderAgentTimestamp, oldTimestamp)
	req.Header.Set(HeaderAgentSig, gowild_agent_net.EncodeSignature(signature))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for old timestamp, got %d", w.Code)
	}
}

func TestRevokedKeyForbidden(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	// Create a request with valid signature
	body := []byte(`{"content":"Hello world"}`)
	req, pubkey, _ := makeSignedRequest(t, "POST", "/api/v1/posts", body)

	// Add the key to revoked keys
	ctx := req.Context()
	pubkeyStr := gowild_agent_net.EncodePublicKey(pubkey)
	if err := server.service.RevokeKey(ctx, pubkeyStr, gowild_agent_net.RevocationReasonAdmin, ""); err != nil {
		t.Fatalf("Failed to revoke key: %v", err)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 for revoked key, got %d", w.Code)
	}
}

func TestSignatureValidation(t *testing.T) {
	server, db := setupTestServer(t)
	defer db.Close()

	handler := server.handler()

	pubkey, _, _ := gowild_agent_net.GenerateKeyPair()
	_, wrongPrivkey, _ := gowild_agent_net.GenerateKeyPair() // Different key

	body := []byte(`{"content":"Hello world"}`)
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Sign with wrong key
	wrongSignature := gowild_agent_net.SignRequest(wrongPrivkey, "POST", "/api/v1/posts", timestamp, body)

	req := httptest.NewRequest("POST", "/api/v1/posts", strings.NewReader(string(body)))
	req.Header.Set(HeaderAgentID, gowild_agent_net.EncodePublicKey(pubkey))
	req.Header.Set(HeaderAgentTimestamp, timestamp)
	req.Header.Set(HeaderAgentSig, gowild_agent_net.EncodeSignature(wrongSignature))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for invalid signature, got %d", w.Code)
	}
}
