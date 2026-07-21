package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/crypto"
	"github.com/original-david-knight/go_wild/polymarket"
	"github.com/original-david-knight/go_wild/tools"
)

// checkWriteRateLimit returns true if the identity is within the rate limit (10 writes/hour).
func (h *BrokerWalletHandler) checkWriteRateLimit(identityKey string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)

	// Prune old entries
	timestamps := h.writeCounts[identityKey]
	pruned := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	h.writeCounts[identityKey] = pruned

	if len(pruned) >= 10 {
		return false
	}

	h.writeCounts[identityKey] = append(pruned, now)
	return true
}

// getWalletTools creates a company-scoped WalletTools instance for the given agent.
func (h *BrokerWalletHandler) getWalletTools(ctx context.Context, agentID string) (*tools.WalletTools, string, error) {
	config, companyID, err := h.getWalletConfig(ctx, agentID)
	if err != nil {
		return nil, "", err
	}

	walletTools, err := tools.NewWalletToolsWithConfig(config)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create wallet tools: %w", err)
	}

	return walletTools, companyID, nil
}

// getWalletConfig resolves company-scoped keys and default RPC settings.
func (h *BrokerWalletHandler) getWalletConfig(ctx context.Context, agentID string) (gowild_crypto.WalletConfig, string, error) {
	if h.service == nil {
		return gowild_crypto.WalletConfig{}, "", fmt.Errorf("wallet service is not configured")
	}

	member, err := h.service.GetCompanyMemberForAgent(ctx, agentID)
	if err != nil {
		return gowild_crypto.WalletConfig{}, "", fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return gowild_crypto.WalletConfig{}, "", fmt.Errorf("wallet tools require company membership")
	}
	companyID := strings.TrimSpace(member.CompanyID)

	config, err := h.getWalletConfigForCompany(ctx, companyID)
	if err != nil {
		return gowild_crypto.WalletConfig{}, "", err
	}
	return config, companyID, nil
}

func (h *BrokerWalletHandler) getWalletConfigForCompany(ctx context.Context, companyID string) (gowild_crypto.WalletConfig, error) {
	if h.service == nil {
		return gowild_crypto.WalletConfig{}, fmt.Errorf("wallet service is not configured")
	}
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return gowild_crypto.WalletConfig{}, fmt.Errorf("company_id is required")
	}

	seedPhrase, err := h.service.EnsureCompanyWalletSeedPhrase(ctx, companyID)
	if err != nil {
		return gowild_crypto.WalletConfig{}, fmt.Errorf("failed to ensure company wallet seed phrase: %w", err)
	}

	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return gowild_crypto.WalletConfig{}, fmt.Errorf("failed to derive keys: %w", err)
	}

	config := gowild_crypto.WalletConfig{
		EthPrivateKey: derived.EthPrivateKey,
		SolPrivateKey: derived.SolPrivateKey,
	}
	if rpcURL := strings.TrimSpace(os.Getenv("WALLET_ETH_RPC_URL")); rpcURL != "" {
		config.EthRPCURL = rpcURL
	}
	if rpcURL := strings.TrimSpace(os.Getenv("WALLET_SOL_RPC_URL")); rpcURL != "" {
		config.SolRPCURL = rpcURL
	}

	return config, nil
}

// resolvePolygonRPCURL returns the RPC URL used for Polygon balance checks.
// Priority: WALLET_POLYGON_RPC_URL -> company polymarket onchain_rpc_url -> POLYMARKET_RPC_URL -> default Polygon RPC.
func (h *BrokerWalletHandler) resolvePolygonRPCURL(ctx context.Context, companyID string) string {
	if rpcURL := strings.TrimSpace(os.Getenv("WALLET_POLYGON_RPC_URL")); rpcURL != "" {
		return rpcURL
	}

	if h.service != nil {
		conn, err := h.service.GetCompanyPolymarketConnection(ctx, strings.TrimSpace(companyID))
		if err == nil && conn != nil {
			if rpcURL := strings.TrimSpace(conn.OnchainRPCURL); rpcURL != "" {
				return rpcURL
			}
		}
	}

	if rpcURL := strings.TrimSpace(os.Getenv("POLYMARKET_RPC_URL")); rpcURL != "" {
		return rpcURL
	}

	return gowild_polymarket.PolygonRPCURL
}

// polygonUSDTeTokenAddress returns the Polygon USDT.e token address used by fixed balance snapshots.
func polygonUSDTeTokenAddress() string {
	tokenAddress := strings.TrimSpace(os.Getenv("WALLET_POLYGON_USDTE_TOKEN_ADDRESS"))
	if tokenAddress == "" {
		return defaultPolygonUSDTeTokenAddress
	}
	return tokenAddress
}

// polygonUSDCeTokenAddress returns the Polygon USDC.e token address used by fixed balance snapshots.
func polygonUSDCeTokenAddress() string {
	tokenAddress := strings.TrimSpace(os.Getenv("WALLET_POLYGON_USDCE_TOKEN_ADDRESS"))
	if tokenAddress == "" {
		return defaultPolygonUSDCeTokenAddress
	}
	return tokenAddress
}

// normalizeGetBalanceInput canonicalizes chain and token fields for wallet balance handling.
func normalizeGetBalanceInput(input tools.GetBalanceInput) tools.GetBalanceInput {
	input.Chain = strings.ToLower(strings.TrimSpace(input.Chain))
	input.TokenAddress = strings.TrimSpace(input.TokenAddress)
	if input.Chain == "polygon" {
		// Wallet internals use ethereum for all EVM RPC calls.
		input.Chain = "ethereum"
	}
	return input
}

// shouldUsePolygonRPCForBalance returns true when a GetBalance request should use Polygon RPC.
func shouldUsePolygonRPCForBalance(input tools.GetBalanceInput) bool {
	chain := strings.ToLower(strings.TrimSpace(input.Chain))
	if chain == "polygon" {
		return true
	}
	if chain != "ethereum" {
		return false
	}

	tokenAddress := strings.TrimSpace(input.TokenAddress)
	if tokenAddress == "" {
		return false
	}
	if strings.EqualFold(tokenAddress, polygonUSDTeTokenAddress()) {
		return true
	}
	if strings.EqualFold(tokenAddress, gowild_polymarket.USDCAddress) || strings.EqualFold(tokenAddress, gowild_polymarket.NativeUSDCAddress) {
		return true
	}
	return false
}

// walletToolHandler is a generic handler that decodes input, calls a wallet tool method, and returns the result.
func walletToolHandler[T any](h *BrokerWalletHandler, w http.ResponseWriter, r *http.Request, isWrite bool, fn func(*tools.WalletTools, T) (map[string]any, error)) {
	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "missing agent ID")
		return
	}

	var input T
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}

	walletTools, companyID, err := h.getWalletTools(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker wallet error for agent %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if isWrite && !h.checkWriteRateLimit("company:"+companyID) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded: max 10 write operations per hour")
		return
	}

	result, err := fn(walletTools, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		result = map[string]any{}
	}
	result["identity_scope"] = "company"
	result["company_id"] = companyID

	writeJSON(w, http.StatusOK, result)
}
