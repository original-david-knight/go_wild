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

	"github.com/ethereum/go-ethereum/crypto"
	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func (h *BrokerPolymarketHandler) buildClient(ctx context.Context, agentID string) (*polymarket.Client, string, error) {
	if h.getClientFn != nil {
		companyID := ""
		if h.service != nil {
			var err error
			companyID, _, err = h.resolveCompanyWalletSeedPhrase(ctx, agentID)
			if err != nil {
				return nil, "", err
			}
			conn, err := h.service.GetCompanyPolymarketConnection(ctx, companyID)
			if err != nil {
				return nil, "", fmt.Errorf("failed to load company polymarket connection: %w", err)
			}
			if conn != nil && !conn.Enabled {
				return nil, "", fmt.Errorf("company polymarket connection is disabled")
			}
		}
		client, err := h.getClientFn(ctx, agentID)
		return client, companyID, err
	}
	return h.getClient(ctx, agentID)
}

func (h *BrokerPolymarketHandler) getOrderBook(ctx context.Context, client *polymarket.Client, tokenID string) (*polymarket.OrderBook, error) {
	if h.getOrderBookFn != nil {
		return h.getOrderBookFn(ctx, client, tokenID)
	}
	if client == nil {
		return nil, fmt.Errorf("polymarket client is nil")
	}
	return client.GetOrderBook(ctx, tokenID)
}

// resolveCompanyWalletSeedPhrase resolves the caller's company and ensures a wallet seed phrase exists.
func (h *BrokerPolymarketHandler) resolveCompanyWalletSeedPhrase(ctx context.Context, agentID string) (string, string, error) {
	if h.service == nil {
		return "", "", fmt.Errorf("polymarket service is not configured")
	}
	member, err := h.service.GetCompanyMemberForAgent(ctx, agentID)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve company membership: %w", err)
	}
	if member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return "", "", fmt.Errorf("polymarket tools require company membership")
	}
	companyID := strings.TrimSpace(member.CompanyID)
	seedPhrase, err := h.service.EnsureCompanyWalletSeedPhrase(ctx, companyID)
	if err != nil {
		return "", "", fmt.Errorf("failed to ensure company wallet seed phrase: %w", err)
	}
	return companyID, seedPhrase, nil
}

// getClientForCompany creates a company-scoped Polymarket client using the
// company's wallet seed phrase directly (no agent context required).
func (h *BrokerPolymarketHandler) getClientForCompany(ctx context.Context, companyID string) (*polymarket.Client, string, error) {
	if h.service == nil {
		return nil, "", fmt.Errorf("polymarket service is not configured")
	}
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return nil, "", fmt.Errorf("company_id is required")
	}

	seedPhrase, err := h.service.EnsureCompanyWalletSeedPhrase(ctx, companyID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to ensure company wallet seed phrase: %w", err)
	}
	return h.getClientForCompanySeed(ctx, companyID, seedPhrase)
}

// getCachedClient returns a cached client for the company if one exists and is still valid.
// Must be called with h.mu held.
func (h *BrokerPolymarketHandler) getCachedClient(companyID string) (*polymarket.Client, bool) {
	cached, ok := h.clientCache[companyID]
	if !ok {
		return nil, false
	}
	if time.Since(cached.createdAt) > polymarketClientCacheTTL {
		delete(h.clientCache, companyID)
		return nil, false
	}
	return cached.client, true
}

// cacheClient stores a client in the cache for the given company.
// Must be called with h.mu held.
func (h *BrokerPolymarketHandler) cacheClient(companyID string, client *polymarket.Client) {
	h.clientCache[companyID] = &cachedPolymarketClient{
		client:    client,
		companyID: companyID,
		createdAt: time.Now(),
	}
}

// checkAuthErrorCooldown returns a cached auth error if one occurred recently for the company.
// Must be called with h.mu held.
func (h *BrokerPolymarketHandler) checkAuthErrorCooldown(companyID string) error {
	ae, ok := h.authErrors[companyID]
	if !ok {
		return nil
	}
	if time.Since(ae.failedAt) > polymarketAuthErrorCooldown {
		delete(h.authErrors, companyID)
		return nil
	}
	return ae.err
}

// recordAuthError records an auth failure so subsequent calls can skip retrying.
// Must be called with h.mu held.
func (h *BrokerPolymarketHandler) recordAuthError(companyID string, err error) {
	h.authErrors[companyID] = &polymarketAuthError{
		err:      err,
		failedAt: time.Now(),
	}
}

// getClient creates a company-scoped Polymarket client for the given agent.
// Derives the ETH private key from the company seed phrase (same key works on Polygon).
// Uses a per-company cache to avoid re-deriving API credentials on every tool call.
func (h *BrokerPolymarketHandler) getClient(ctx context.Context, agentID string) (*polymarket.Client, string, error) {
	companyID, seedPhrase, err := h.resolveCompanyWalletSeedPhrase(ctx, agentID)
	if err != nil {
		return nil, "", err
	}
	return h.getClientForCompanySeed(ctx, companyID, seedPhrase)
}

// getClientForCompanySeed creates a company-scoped Polymarket client with an
// already-resolved company seed phrase.
func (h *BrokerPolymarketHandler) getClientForCompanySeed(ctx context.Context, companyID, seedPhrase string) (*polymarket.Client, string, error) {
	conn, err := h.service.GetCompanyPolymarketConnection(ctx, companyID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load company polymarket connection: %w", err)
	}
	if conn != nil && !conn.Enabled {
		return nil, "", fmt.Errorf("company polymarket connection is disabled")
	}

	// Check cache and auth error cooldown under the same lock.
	h.mu.Lock()
	if cachedErr := h.checkAuthErrorCooldown(companyID); cachedErr != nil {
		h.mu.Unlock()
		return nil, "", cachedErr
	}
	if client, ok := h.getCachedClient(companyID); ok {
		h.mu.Unlock()
		return client, companyID, nil
	}
	h.mu.Unlock()

	// Cache miss — create a new client (this hits Polymarket auth endpoints).
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return nil, "", fmt.Errorf("failed to derive keys: %w", err)
	}

	// Parse the hex ETH private key to an ECDSA key
	privateKey, err := crypto.HexToECDSA(derived.EthPrivateKey[2:]) // strip "0x" prefix
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse ETH private key: %w", err)
	}

	// Build client options
	var opts []polymarket.Option
	if conn != nil && conn.ChainID > 0 {
		opts = append(opts, polymarket.WithChainID(conn.ChainID))
	}

	proxyURL := ""
	if conn != nil {
		proxyURL = strings.TrimSpace(conn.ProxyURL)
	}
	if proxyURL == "" {
		proxyURL = h.proxyURL
	}
	if proxyURL == "" {
		proxyURL = strings.TrimSpace(os.Getenv("POLYMARKET_PROXY_URL"))
	}
	if proxyURL != "" {
		opts = append(opts, polymarket.WithProxy(proxyURL))
	}

	rpcURL := ""
	if conn != nil {
		rpcURL = strings.TrimSpace(conn.OnchainRPCURL)
	}
	if rpcURL == "" {
		rpcURL = strings.TrimSpace(os.Getenv("POLYMARKET_RPC_URL"))
	}
	if rpcURL == "" {
		rpcURL = strings.TrimSpace(os.Getenv("WALLET_ETH_RPC_URL"))
	}
	if rpcURL != "" {
		opts = append(opts, polymarket.WithOnchainRPC(rpcURL))
	}

	var client *polymarket.Client
	if conn != nil && strings.TrimSpace(conn.FunderAddress) != "" {
		sigType := conn.SignatureType
		if sigType < polymarket.SigTypeEOA || sigType > polymarket.SigTypePolyGnosisSafe {
			return nil, "", fmt.Errorf("invalid company polymarket signature type: %d", sigType)
		}
		client, err = polymarket.NewClientWithFunder(privateKey, strings.TrimSpace(conn.FunderAddress), sigType, opts...)
	} else {
		client, err = polymarket.NewClient(privateKey, opts...)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to create polymarket client: %w", err)
	}

	// Log CLOB auth degradation but don't block — public/on-chain ops still work.
	if client.CLOBAuthError() != nil {
		log.Printf("Polymarket client for company %s created in degraded mode (no CLOB auth): %v", companyID, client.CLOBAuthError())
		if !shouldCachePolymarketClient(client.CLOBAuthError()) {
			return client, companyID, nil
		}
	}

	// Cache the client (even if degraded — avoids re-hitting auth endpoints).
	h.mu.Lock()
	h.cacheClient(companyID, client)
	// Clear any stale auth error on successful client creation.
	delete(h.authErrors, companyID)
	h.mu.Unlock()

	return client, companyID, nil
}

// isPolymarketAuthError checks whether an error from client creation indicates
// a permanent auth problem (tenant disabled, API key revoked, etc.).
func isPolymarketAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tenant disabled") ||
		strings.Contains(msg, "api key disabled") ||
		strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "status 403")
}

func shouldCachePolymarketClient(authErr error) bool {
	return authErr == nil || isPolymarketAuthError(authErr)
}

// polymarketToolHandler is a generic handler that decodes input, creates a Polymarket client, and calls the handler function.
func polymarketToolHandler[T any](h *BrokerPolymarketHandler, w http.ResponseWriter, r *http.Request, fn func(*polymarket.Client, T) (map[string]any, error)) {
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

	client, companyID, err := h.buildClient(r.Context(), agentID)
	if err != nil {
		log.Printf("Broker polymarket error for agent %s: %v", agentID, err)
		// Return 503 for auth/tenant errors so the agent knows it's a configuration
		// issue, not a transient failure worth retrying.
		if isPolymarketAuthError(err) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	result, err := fn(client, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result == nil {
		result = map[string]any{}
	}
	if companyID != "" {
		result["identity_scope"] = "company"
		result["company_id"] = companyID
	}

	writeJSON(w, http.StatusOK, result)
}

type brokerPolymarketExecutionPolicy struct {
	disableMarketNotes                bool
	disablePolymarketNoteAugmentation bool
}

func (h *BrokerPolymarketHandler) currentExecutionPolymarketPolicy(ctx context.Context) (string, brokerPolymarketExecutionPolicy) {
	method := strings.TrimSpace(BrokerExecutionMethod(ctx))
	if method == "" || h == nil || h.service == nil || h.service.db == nil {
		return method, brokerPolymarketExecutionPolicy{}
	}

	methodDef, err := data.NewAgentService(h.service.db, "system").GetA2AMethod(ctx, method)
	if err != nil || methodDef == nil {
		return method, brokerPolymarketExecutionPolicy{}
	}

	return method, brokerPolymarketExecutionPolicy{
		disableMarketNotes:                methodDef.MarketNotesDisabled(),
		disablePolymarketNoteAugmentation: methodDef.PolymarketNoteAugmentationDisabled(),
	}
}

func (h *BrokerPolymarketHandler) currentExecutionDisablesMarketNotes(ctx context.Context) (string, bool) {
	method, policy := h.currentExecutionPolymarketPolicy(ctx)
	return method, policy.disableMarketNotes
}

func (h *BrokerPolymarketHandler) currentExecutionDisablesPolymarketNoteAugmentation(ctx context.Context) bool {
	_, policy := h.currentExecutionPolymarketPolicy(ctx)
	return policy.disablePolymarketNoteAugmentation
}
