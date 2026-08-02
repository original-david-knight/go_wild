package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/polymarket"
	"github.com/original-david-knight/go_wild/tools"
)

func TestGetWalletTools_RequiresCompanyMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "wallet-agent")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	handler := NewBrokerWalletHandler(service)
	if _, _, err := handler.getWalletTools(ctx, agent.ID); err == nil {
		t.Fatalf("expected company membership error")
	} else if !strings.Contains(err.Error(), "company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetWalletTools_UsesCompanyWalletForAllMembers(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agentA, err := service.CreateAgent(ctx, "wallet-agent-a")
	if err != nil {
		t.Fatalf("failed to create agent A: %v", err)
	}
	agentB, err := service.CreateAgent(ctx, "wallet-agent-b")
	if err != nil {
		t.Fatalf("failed to create agent B: %v", err)
	}

	company, err := service.CreateCompany(ctx, "wallet-co", "", "")
	if err != nil {
		t.Fatalf("failed to create company: %v", err)
	}
	// Deterministic test seed phrase.
	company.WalletSeedPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if err := service.UpdateCompany(ctx, company); err != nil {
		t.Fatalf("failed to update company seed phrase: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agentA.ID, "member"); err != nil {
		t.Fatalf("failed to add agent A to company: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agentB.ID, "member"); err != nil {
		t.Fatalf("failed to add agent B to company: %v", err)
	}

	handler := NewBrokerWalletHandler(service)
	wtA, companyIDA, err := handler.getWalletTools(ctx, agentA.ID)
	if err != nil {
		t.Fatalf("getWalletTools for agent A failed: %v", err)
	}
	wtB, companyIDB, err := handler.getWalletTools(ctx, agentB.ID)
	if err != nil {
		t.Fatalf("getWalletTools for agent B failed: %v", err)
	}
	if companyIDA != company.ID || companyIDB != company.ID {
		t.Fatalf("expected company id %q, got %q and %q", company.ID, companyIDA, companyIDB)
	}

	resA, err := wtA.GetWalletAddressTool(ctx, tools.GetWalletAddressInput{Chain: "ethereum"})
	if err != nil {
		t.Fatalf("GetWalletAddressTool agent A failed: %v", err)
	}
	resB, err := wtB.GetWalletAddressTool(ctx, tools.GetWalletAddressInput{Chain: "ethereum"})
	if err != nil {
		t.Fatalf("GetWalletAddressTool agent B failed: %v", err)
	}
	if !resA.Success || !resB.Success {
		t.Fatalf("expected successful address lookups, got A=%v B=%v", resA.Success, resB.Success)
	}

	contentA, ok := resA.Content.(map[string]any)
	if !ok {
		t.Fatalf("unexpected content type for A: %T", resA.Content)
	}
	contentB, ok := resB.Content.(map[string]any)
	if !ok {
		t.Fatalf("unexpected content type for B: %T", resB.Content)
	}
	addrA, _ := contentA["address"].(string)
	addrB, _ := contentB["address"].(string)
	if addrA == "" || addrB == "" {
		t.Fatalf("expected non-empty addresses, got %q and %q", addrA, addrB)
	}
	if addrA != addrB {
		t.Fatalf("expected shared company address, got %q and %q", addrA, addrB)
	}
}

func setupWalletCompanyAgent(t *testing.T) (context.Context, *AgentService, *BrokerWalletHandler, string, string) {
	t.Helper()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	ctx := context.Background()

	agent, err := service.CreateAgent(ctx, "wallet-company-agent")
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	company, err := service.CreateCompany(ctx, "wallet-company", "", "")
	if err != nil {
		t.Fatalf("failed to create company: %v", err)
	}
	company.WalletSeedPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if err := service.UpdateCompany(ctx, company); err != nil {
		t.Fatalf("failed to set company seed phrase: %v", err)
	}
	if err := service.AddAgentToCompany(ctx, company.ID, agent.ID, "member"); err != nil {
		t.Fatalf("failed to add agent to company: %v", err)
	}

	return ctx, service, NewBrokerWalletHandler(service), agent.ID, company.ID
}

func TestResolvePolygonRPCURL(t *testing.T) {
	t.Run("wallet_env_override", func(t *testing.T) {
		ctx, _, h, _, companyID := setupWalletCompanyAgent(t)
		t.Setenv("WALLET_POLYGON_RPC_URL", "https://wallet-polygon.example")
		t.Setenv("POLYMARKET_RPC_URL", "https://polymarket-fallback.example")
		if got := h.resolvePolygonRPCURL(ctx, companyID); got != "https://wallet-polygon.example" {
			t.Fatalf("resolvePolygonRPCURL = %q, want wallet env override", got)
		}
	})

	t.Run("company_polymarket_connection", func(t *testing.T) {
		ctx, service, h, _, companyID := setupWalletCompanyAgent(t)
		t.Setenv("WALLET_POLYGON_RPC_URL", "")
		t.Setenv("POLYMARKET_RPC_URL", "https://polymarket-fallback.example")
		if err := service.UpsertCompanyPolymarketConnection(ctx, &data.CompanyPolymarketConnection{
			CompanyID:     companyID,
			OnchainRPCURL: "https://company-polygon.example",
			Enabled:       true,
		}); err != nil {
			t.Fatalf("failed to upsert company polymarket connection: %v", err)
		}
		if got := h.resolvePolygonRPCURL(ctx, companyID); got != "https://company-polygon.example" {
			t.Fatalf("resolvePolygonRPCURL = %q, want company polymarket rpc", got)
		}
	})

	t.Run("global_fallback", func(t *testing.T) {
		ctx, _, h, _, companyID := setupWalletCompanyAgent(t)
		t.Setenv("WALLET_POLYGON_RPC_URL", "")
		t.Setenv("POLYMARKET_RPC_URL", "https://polymarket-fallback.example")
		if got := h.resolvePolygonRPCURL(ctx, companyID); got != "https://polymarket-fallback.example" {
			t.Fatalf("resolvePolygonRPCURL = %q, want global fallback", got)
		}
	})

	t.Run("default_fallback_when_unset", func(t *testing.T) {
		ctx, _, h, _, companyID := setupWalletCompanyAgent(t)
		t.Setenv("WALLET_POLYGON_RPC_URL", "")
		t.Setenv("POLYMARKET_RPC_URL", "")
		if got := h.resolvePolygonRPCURL(ctx, companyID); got != gowild_polymarket.PolygonRPCURL {
			t.Fatalf("resolvePolygonRPCURL = %q, want default fallback %q", got, gowild_polymarket.PolygonRPCURL)
		}
	})
}

func TestNormalizeGetBalanceInput(t *testing.T) {
	in := tools.GetBalanceInput{
		Chain:        "  Polygon ",
		TokenAddress: " 0xabc ",
	}
	got := normalizeGetBalanceInput(in)
	if got.Chain != "ethereum" {
		t.Fatalf("normalizeGetBalanceInput chain = %q, want %q", got.Chain, "ethereum")
	}
	if got.TokenAddress != "0xabc" {
		t.Fatalf("normalizeGetBalanceInput token_address = %q, want %q", got.TokenAddress, "0xabc")
	}
}

func TestShouldUsePolygonRPCForBalance(t *testing.T) {
	t.Setenv("WALLET_POLYGON_USDTE_TOKEN_ADDRESS", "")

	if !shouldUsePolygonRPCForBalance(tools.GetBalanceInput{Chain: "polygon"}) {
		t.Fatalf("expected polygon chain to use Polygon RPC")
	}
	if !shouldUsePolygonRPCForBalance(tools.GetBalanceInput{
		Chain:        "ethereum",
		TokenAddress: defaultPolygonUSDTeTokenAddress,
	}) {
		t.Fatalf("expected Polygon USDT.e token to use Polygon RPC")
	}
	if !shouldUsePolygonRPCForBalance(tools.GetBalanceInput{
		Chain:        "ethereum",
		TokenAddress: gowild_polymarket.USDCAddress,
	}) {
		t.Fatalf("expected Polygon USDC.e token to use Polygon RPC")
	}
	if shouldUsePolygonRPCForBalance(tools.GetBalanceInput{Chain: "ethereum"}) {
		t.Fatalf("did not expect native ethereum balance to force Polygon RPC")
	}
	if shouldUsePolygonRPCForBalance(tools.GetBalanceInput{Chain: "solana", TokenAddress: "anything"}) {
		t.Fatalf("did not expect solana balance to use Polygon RPC")
	}
}

func TestWalletToolHandler_RequiresAgentID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/broker/v1/wallet/get_address", nil)
	called := false
	walletToolHandler(NewBrokerWalletHandler(nil), rec, req, false, func(_ *tools.WalletTools, _ struct{}) (map[string]any, error) {
		called = true
		return map[string]any{"ok": true}, nil
	})
	if called {
		t.Fatalf("expected tool fn not to be called without agent id")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalletToolHandler_InvalidJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/wallet/get_address", strings.NewReader("{"))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	called := false
	walletToolHandler(NewBrokerWalletHandler(nil), rec, req, false, func(_ *tools.WalletTools, _ struct{}) (map[string]any, error) {
		called = true
		return map[string]any{"ok": true}, nil
	})
	if called {
		t.Fatalf("expected tool fn not to be called for invalid json")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalletToolHandler_PropagatesWalletConfigError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/broker/v1/wallet/get_address", nil)
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	called := false
	walletToolHandler(NewBrokerWalletHandler(nil), rec, req, false, func(_ *tools.WalletTools, _ struct{}) (map[string]any, error) {
		called = true
		return map[string]any{"ok": true}, nil
	})
	if called {
		t.Fatalf("expected tool fn not to be called when wallet config fails")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalletToolHandler_WriteRateLimitExceeded(t *testing.T) {
	ctx, _, h, agentID, companyID := setupWalletCompanyAgent(t)
	now := time.Now()

	h.mu.Lock()
	h.writeCounts["company:"+companyID] = []time.Time{
		now, now, now, now, now,
		now, now, now, now, now,
	}
	h.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/broker/v1/wallet/send", nil)
	req = req.WithContext(context.WithValue(ctx, brokerAgentIDKey, agentID))

	called := false
	walletToolHandler(h, rec, req, true, func(_ *tools.WalletTools, _ struct{}) (map[string]any, error) {
		called = true
		return map[string]any{"ok": true}, nil
	})
	if called {
		t.Fatalf("expected tool fn not to be called when rate limit blocks request")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWalletToolHandler_SuccessAddsCompanyScope(t *testing.T) {
	type input struct {
		Message string `json:"message"`
	}

	ctx, _, h, agentID, companyID := setupWalletCompanyAgent(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/wallet/echo", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(context.WithValue(ctx, brokerAgentIDKey, agentID))

	walletToolHandler(h, rec, req, false, func(_ *tools.WalletTools, in input) (map[string]any, error) {
		return map[string]any{"echo": in.Message}, nil
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got, _ := payload["echo"].(string); got != "hello" {
		t.Fatalf("echo = %q, want %q", got, "hello")
	}
	if got, _ := payload["identity_scope"].(string); got != "company" {
		t.Fatalf("identity_scope = %q, want %q", got, "company")
	}
	if got, _ := payload["company_id"].(string); got != companyID {
		t.Fatalf("company_id = %q, want %q", got, companyID)
	}
}

func TestWalletToolHandler_NilResultStillReturnsScope(t *testing.T) {
	ctx, _, h, agentID, companyID := setupWalletCompanyAgent(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/broker/v1/wallet/empty", nil)
	req = req.WithContext(context.WithValue(ctx, brokerAgentIDKey, agentID))

	walletToolHandler(h, rec, req, false, func(_ *tools.WalletTools, _ struct{}) (map[string]any, error) {
		return nil, nil
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got, _ := payload["identity_scope"].(string); got != "company" {
		t.Fatalf("identity_scope = %q, want %q", got, "company")
	}
	if got, _ := payload["company_id"].(string); got != companyID {
		t.Fatalf("company_id = %q, want %q", got, companyID)
	}
}
