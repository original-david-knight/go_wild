package server

import (
	"encoding/json"
	"net/http"

	"github.com/original-david-knight/go_wild/agent_net"
)

// UpgradeRequest represents the request body for account upgrade.
type UpgradeRequest struct {
	TxSignature string `json:"tx_signature"`
	Chain       string `json:"chain"`
}

// UpgradeResponse represents the response for account upgrade.
type UpgradeResponse struct {
	Success    bool   `json:"success"`
	PublicKey  string `json:"public_key"`
	Tier       string `json:"tier"`
	UpgradedAt string `json:"upgraded_at"`
}

// HandleUpgrade handles POST /api/v1/account/upgrade.
func (h *Handlers) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}

	var req UpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.TxSignature == "" {
		writeBadRequest(w, "tx_signature is required")
		return
	}

	if req.Chain == "" {
		writeBadRequest(w, "chain is required")
		return
	}

	// Validate chain
	switch req.Chain {
	case gowild_agent_net.ChainSolana, gowild_agent_net.ChainEthereum, gowild_agent_net.ChainBase:
		// Valid
	default:
		writeBadRequest(w, "Invalid chain: must be solana, ethereum, or base")
		return
	}

	agentID := GetAgentID(r.Context())

	if err := h.service.UpgradeAccount(r.Context(), agentID, req.TxSignature, req.Chain); err != nil {
		// Check for specific error types
		errMsg := err.Error()
		if errMsg == "account already premium" {
			writeBadRequest(w, errMsg)
			return
		}
		if errMsg == "key has been revoked" {
			writeForbidden(w, errMsg)
			return
		}
		if errMsg == "transaction already used for upgrade" {
			writeBadRequest(w, errMsg)
			return
		}
		writeUpgradeFailed(w, http.StatusBadRequest, "Upgrade failed: "+errMsg)
		return
	}

	// Get upgraded agent details
	agent, _ := h.service.GetPremiumAgent(r.Context(), agentID)
	upgradedAt := ""
	if agent != nil {
		upgradedAt = agent.UpgradedAt.Format("2006-01-02T15:04:05Z")
	}

	writeJSON(w, http.StatusOK, UpgradeResponse{
		Success:    true,
		PublicKey:  agentID,
		Tier:       "premium",
		UpgradedAt: upgradedAt,
	})
}

// HandleDeleteAccount handles DELETE /api/v1/account (self-revocation).
func (h *Handlers) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	tier := GetAgentTier(r.Context())

	// Only premium agents can self-revoke
	if tier != gowild_agent_net.TierPremium {
		writeForbidden(w, "Only premium agents can self-revoke. Use burn-to-revoke for free tier.")
		return
	}

	if err := h.service.RevokeKey(r.Context(), agentID, gowild_agent_net.RevocationReasonSelf, ""); err != nil {
		writeInternalError(w, "Failed to revoke key: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "Key successfully revoked. Generate a new keypair to continue participating.",
		"publicKey": agentID,
	})
}
