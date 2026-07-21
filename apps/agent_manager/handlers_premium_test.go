package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/data"
)

// setupHandlersWithAgentNetDB creates a Handlers instance with an in-memory
// agent_net database pre-wired (simulating AGENT_NET_DATABASE_URL being set).
func setupHandlersWithAgentNetDB(t *testing.T) (*Handlers, gowild_data.Database, string) {
	t.Helper()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	agent, err := svc.CreateAgent(context.Background(), "premium-test")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	// Create an in-memory SQLite DB to act as the agent_net DB.
	agentNetDB, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create agent_net test db: %v", err)
	}
	if err := agentNetDB.AddTable(gowild_agent_net.PremiumAgent{}); err != nil {
		t.Fatalf("failed to register PremiumAgent table: %v", err)
	}
	t.Cleanup(func() { agentNetDB.Close() })

	h := NewHandlers(svc, nil, nil, nil, nil)
	// Inject the agent_net DB directly, bypassing the lazy-init from env var.
	h.agentNetDB = agentNetDB

	return h, agentNetDB, agent.ID
}

func TestPubKeyFromSeedPhrase(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	agent, err := svc.CreateAgent(context.Background(), "derive-test")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	// Agent should have a wallet seed phrase auto-generated.
	if agent.WalletSeedPhrase == "" {
		t.Fatal("expected agent to have a wallet seed phrase")
	}

	pubKey, err := pubKeyFromSeedPhrase(agent.WalletSeedPhrase)
	if err != nil {
		t.Fatalf("pubKeyFromSeedPhrase failed: %v", err)
	}
	if pubKey == "" {
		t.Fatal("expected non-empty public key")
	}
	// Base64URL encoded Ed25519 key is 43 chars (32 bytes, no padding).
	if len(pubKey) != 43 {
		t.Errorf("expected 43-char base64url pubkey, got %d chars: %s", len(pubKey), pubKey)
	}

	// Calling again should produce the same key (deterministic).
	pubKey2, err := pubKeyFromSeedPhrase(agent.WalletSeedPhrase)
	if err != nil {
		t.Fatalf("second pubKeyFromSeedPhrase failed: %v", err)
	}
	if pubKey != pubKey2 {
		t.Errorf("expected deterministic key derivation, got %s vs %s", pubKey, pubKey2)
	}
}

func TestPubKeyFromSeedPhrase_Empty(t *testing.T) {
	_, err := pubKeyFromSeedPhrase("")
	if err == nil {
		t.Fatal("expected error for empty seed phrase")
	}
}

func TestResolveAgentNetPublicKey_UsesCompanyKey(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	agent, err := svc.CreateAgent(context.Background(), "company-key-test")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	// Derive key without company — should use agent's own seed.
	soloKey, err := resolveAgentNetPublicKey(context.Background(), db, agent)
	if err != nil {
		t.Fatalf("resolveAgentNetPublicKey (solo) failed: %v", err)
	}

	// Create a company and add the agent to it.
	company := &data.Company{
		ID:               "test-company",
		Name:             "Test Co",
		WalletSeedPhrase: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
	}
	if err := db.Table(data.Company{}).Insert(context.Background(), company); err != nil {
		t.Fatalf("Insert company failed: %v", err)
	}
	member := &data.CompanyMember{
		ID:        "member-1",
		CompanyID: company.ID,
		AgentID:   agent.ID,
		Role:      "member",
	}
	if err := db.Table(data.CompanyMember{}).Insert(context.Background(), member); err != nil {
		t.Fatalf("Insert member failed: %v", err)
	}

	// Derive key with company — should use company seed.
	companyKey, err := resolveAgentNetPublicKey(context.Background(), db, agent)
	if err != nil {
		t.Fatalf("resolveAgentNetPublicKey (company) failed: %v", err)
	}

	if soloKey == companyKey {
		t.Error("expected company key to differ from solo key")
	}

	// Company key should match deriving directly from the company seed.
	expectedKey, err := pubKeyFromSeedPhrase(company.WalletSeedPhrase)
	if err != nil {
		t.Fatalf("pubKeyFromSeedPhrase (company) failed: %v", err)
	}
	if companyKey != expectedKey {
		t.Errorf("expected %s, got %s", expectedKey, companyKey)
	}
}

func TestGrantPremium_Success(t *testing.T) {
	h, agentNetDB, agentID := setupHandlersWithAgentNetDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/premium", nil)
	rec := httptest.NewRecorder()
	h.grantPremium(rec, req, agentID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["premium"] != true {
		t.Errorf("expected premium=true, got %v", resp["premium"])
	}
	if resp["public_key"] == nil || resp["public_key"] == "" {
		t.Error("expected non-empty public_key")
	}

	// Verify record exists in the agent_net DB.
	results, err := agentNetDB.Table(gowild_agent_net.PremiumAgent{}).Query(context.Background(), gowild_data.QueryOpts{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 premium record, got %d", len(results))
	}
	pa := results[0].(*gowild_agent_net.PremiumAgent)
	if pa.Chain != "admin" {
		t.Errorf("expected chain=admin, got %s", pa.Chain)
	}
	if pa.TxHash != "admin_grant_"+agentID {
		t.Errorf("expected tx_hash=admin_grant_%s, got %s", agentID, pa.TxHash)
	}
}

func TestGrantPremium_AlreadyPremium(t *testing.T) {
	h, _, agentID := setupHandlersWithAgentNetDB(t)

	// First call — grants premium.
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/premium", nil)
	rec := httptest.NewRecorder()
	h.grantPremium(rec, req, agentID)
	if rec.Code != http.StatusOK {
		t.Fatalf("first grant: expected 200, got %d", rec.Code)
	}

	// Second call — should detect already premium.
	req2 := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/premium", nil)
	rec2 := httptest.NewRecorder()
	h.grantPremium(rec2, req2, agentID)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second grant: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["already"] != true {
		t.Errorf("expected already=true, got %v", resp["already"])
	}
}

func TestGrantPremium_MethodNotAllowed(t *testing.T) {
	h, _, agentID := setupHandlersWithAgentNetDB(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/premium", nil)
	rec := httptest.NewRecorder()
	h.grantPremium(rec, req, agentID)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestGrantPremium_AgentNotFound(t *testing.T) {
	h, _, _ := setupHandlersWithAgentNetDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/nonexistent/premium", nil)
	rec := httptest.NewRecorder()
	h.grantPremium(rec, req, "nonexistent")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestGrantPremium_NoAgentNetDB(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	agent, err := svc.CreateAgent(context.Background(), "no-db-test")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	// Unset the env var so the lazy init doesn't connect to a real DB.
	orig := os.Getenv("AGENT_NET_DATABASE_URL")
	os.Unsetenv("AGENT_NET_DATABASE_URL")
	t.Cleanup(func() {
		if orig != "" {
			os.Setenv("AGENT_NET_DATABASE_URL", orig)
		}
	})

	h := NewHandlers(svc, nil, nil, nil, nil)
	// agentNetDB is nil — simulates AGENT_NET_DATABASE_URL not set.

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agent.ID+"/premium", nil)
	rec := httptest.NewRecorder()
	h.grantPremium(rec, req, agent.ID)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestApplyAgentNetStatus_WithPremium(t *testing.T) {
	h, agentNetDB, agentID := setupHandlersWithAgentNetDB(t)

	agent, err := h.service.GetAgent(context.Background(), agentID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}

	pubKey, err := resolveAgentNetPublicKey(context.Background(), h.service.db, agent)
	if err != nil {
		t.Fatalf("resolveAgentNetPublicKey failed: %v", err)
	}

	// Insert premium record directly.
	record := &gowild_agent_net.PremiumAgent{
		ID:        pubKey,
		PublicKey: pubKey,
		TxHash:    "test_tx",
		Chain:     "admin",
	}
	if err := agentNetDB.Table(gowild_agent_net.PremiumAgent{}).Insert(context.Background(), record); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	resp := buildAgentResponse(agent, "stopped")
	h.applyAgentNetStatus(context.Background(), agent, &resp)

	if resp.AgentNetPublicKey != pubKey {
		t.Errorf("expected pubkey %s, got %s", pubKey, resp.AgentNetPublicKey)
	}
	if !resp.AgentNetPremium {
		t.Error("expected AgentNetPremium=true")
	}
}

func TestApplyAgentNetStatus_NotPremium(t *testing.T) {
	h, _, agentID := setupHandlersWithAgentNetDB(t)

	agent, err := h.service.GetAgent(context.Background(), agentID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}

	resp := buildAgentResponse(agent, "stopped")
	h.applyAgentNetStatus(context.Background(), agent, &resp)

	if resp.AgentNetPublicKey == "" {
		t.Error("expected non-empty AgentNetPublicKey")
	}
	if resp.AgentNetPremium {
		t.Error("expected AgentNetPremium=false")
	}
}

func TestApplyAgentNetStatus_NoSeedPhrase(t *testing.T) {
	h, _, _ := setupHandlersWithAgentNetDB(t)

	agent := &data.Agent{ID: "no-seed", Name: "No Seed"}
	resp := buildAgentResponse(agent, "stopped")
	h.applyAgentNetStatus(context.Background(), agent, &resp)

	if resp.AgentNetPublicKey != "" {
		t.Errorf("expected empty pubkey for agent without seed, got %s", resp.AgentNetPublicKey)
	}
	if resp.AgentNetPremium {
		t.Error("expected AgentNetPremium=false for agent without seed")
	}
}

func TestGrantPremium_ViaRouting(t *testing.T) {
	h, _, agentID := setupHandlersWithAgentNetDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/premium", nil)
	rec := httptest.NewRecorder()
	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["premium"] != true {
		t.Errorf("expected premium=true, got %v", resp["premium"])
	}
}
