package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mr-tron/base58"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/crypto"
	"github.com/original-david-knight/go_wild/data"
)

// handleToolGroups returns the canonical list of tool groups.
func (h *Handlers) handleToolGroups(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, data.AllToolGroups)
}

// listAgents returns all agents with Docker container status.
func (h *Handlers) listAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agents, err := h.service.ListAgents(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]AgentResponse, 0, len(agents))
	for _, a := range agents {
		status := h.docker.ContainerStatus(ctx, a.ID)
		resp := buildAgentResponse(a, status)
		h.applyImageStatus(ctx, a.ID, &resp)
		h.applyAgentNetStatus(ctx, a, &resp)
		responses = append(responses, resp)
	}

	writeJSON(w, http.StatusOK, responses)
}

func (h *Handlers) createAgent(w http.ResponseWriter, r *http.Request) {
	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	id := strings.ToLower(strings.ReplaceAll(name, " ", "-"))

	agent, err := h.service.CreateAgent(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if err := h.docker.EnsureVolume(r.Context(), agent.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent volume: "+err.Error())
		return
	}

	status := h.docker.ContainerStatus(r.Context(), agent.ID)
	resp := buildAgentResponse(agent, status)
	h.applyImageStatus(r.Context(), agent.ID, &resp)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) cloneAgent(w http.ResponseWriter, r *http.Request, sourceID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	newID := strings.ToLower(strings.ReplaceAll(name, " ", "-"))

	clone, err := h.service.CloneAgent(r.Context(), sourceID, newID)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, err.Error())
		} else if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	if err := h.docker.EnsureVolume(r.Context(), clone.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent volume: "+err.Error())
		return
	}

	status := h.docker.ContainerStatus(r.Context(), clone.ID)
	resp := buildAgentResponse(clone, status)
	h.applyImageStatus(r.Context(), clone.ID, &resp)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) getAgent(w http.ResponseWriter, r *http.Request, id string) {
	agent, err := h.service.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	status := h.docker.ContainerStatus(r.Context(), agent.ID)
	resp := buildAgentResponse(agent, status)
	h.applyImageStatus(r.Context(), agent.ID, &resp)
	h.applyAgentNetStatus(r.Context(), agent, &resp)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) updateAgent(w http.ResponseWriter, r *http.Request, id string) {
	var req UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Get existing agent
	agent, err := h.service.GetAgent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Apply updates
	if strings.TrimSpace(req.ModelProvider) != "" {
		agent.ModelProvider = data.NormalizeLLMProvider(req.ModelProvider)
	}
	if strings.TrimSpace(req.OpenAIAuthMode) != "" {
		agent.OpenAIAuthMode = data.NormalizeOpenAIAuthMode(req.OpenAIAuthMode)
	}
	agent.Model = req.Model
	agent.SmartModel = req.SmartModel
	agent.SmartDefault = req.SmartDefault
	agent.MaxTurns = req.MaxTurns
	agent.Heartbeat = normalizeDuration(req.Heartbeat)
	agent.WorkTasksTimeout = normalizeDuration(req.WorkTasksTimeout)
	if req.ExtraFlags != nil {
		_, _, cleanedFlags := parseRuntimeFlags(*req.ExtraFlags)
		agent.ExtraFlags = strings.Join(cleanedFlags, " ")
	}
	// Worker runtime mode has been removed; force interactive runtime config.
	agent.Mode = "interactive"
	agent.WorkerContextMode = "stateless"
	agent.MemoryLimit = req.MemoryLimit
	agent.CPULimit = req.CPULimit
	agent.AutoStart = req.AutoStart
	agent.SystemPrompt = req.SystemPrompt
	switch req.TelegramBotToken {
	case "":
		// Empty means "don't change" — keep existing token
	case "CLEAR":
		agent.TelegramBotToken = ""
	default:
		agent.TelegramBotToken = req.TelegramBotToken
	}
	agent.SetEnvVars(req.EnvVars)
	if req.EnabledTools != nil {
		agent.SetEnabledTools(*req.EnabledTools)
	}

	if err := h.service.UpdateAgent(r.Context(), agent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := h.docker.ContainerStatus(r.Context(), id)
	if status == "running" && h.workerManager != nil {
		// Keep background workers aligned with the latest runtime config.
		go h.workerManager.StartAgent(id)
	}
	resp := buildAgentResponse(agent, status)
	h.applyImageStatus(r.Context(), agent.ID, &resp)
	h.applyAgentNetStatus(r.Context(), agent, &resp)
	writeJSON(w, http.StatusOK, resp)
}

// pubKeyFromSeedPhrase derives the agent_net Ed25519 public key from a seed phrase.
func pubKeyFromSeedPhrase(seedPhrase string) (string, error) {
	if strings.TrimSpace(seedPhrase) == "" {
		return "", fmt.Errorf("seed phrase is empty")
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return "", fmt.Errorf("failed to derive keys: %w", err)
	}
	keypair, err := base58.Decode(derived.SolPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode solana keypair: %w", err)
	}
	if len(keypair) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("invalid keypair length: %d", len(keypair))
	}
	privKey := ed25519.PrivateKey(keypair)
	pubKey := privKey.Public().(ed25519.PublicKey)
	return gowild_agent_net.EncodePublicKey(pubKey), nil
}

// resolveAgentNetPublicKey returns the agent_net public key for an agent,
// using the company's seed phrase if the agent belongs to a company.
func resolveAgentNetPublicKey(ctx context.Context, db gowild_data.Database, agent *data.Agent) (string, error) {
	seedPhrase, err := resolveAgentSeedPhrase(ctx, db, agent)
	if err != nil {
		return "", err
	}
	return pubKeyFromSeedPhrase(seedPhrase)
}

// resolveAgentSeedPhrase returns the seed phrase that determines an agent's
// agent_net identity. Company agents use the company seed phrase; solo agents
// use their own. Returns an error if the agent is in a company but the company
// seed phrase is unavailable.
func resolveAgentSeedPhrase(ctx context.Context, db gowild_data.Database, agent *data.Agent) (string, error) {
	member, err := data.GetCompanyMemberForAgent(ctx, db, agent.ID)
	if err != nil {
		return "", fmt.Errorf("failed to check company membership: %w", err)
	}
	if member != nil && strings.TrimSpace(member.CompanyID) != "" {
		phrase, err := data.EnsureCompanyWalletSeedPhrase(ctx, db, member.CompanyID)
		if err != nil {
			return "", fmt.Errorf("agent is in company %s but company seed phrase is unavailable: %w", member.CompanyID, err)
		}
		if strings.TrimSpace(phrase) == "" {
			return "", fmt.Errorf("agent is in company %s but company seed phrase is empty", member.CompanyID)
		}
		return phrase, nil
	}

	// Agent is not in a company — use their own seed phrase.
	if strings.TrimSpace(agent.WalletSeedPhrase) == "" {
		return "", fmt.Errorf("agent has no wallet seed phrase")
	}
	return agent.WalletSeedPhrase, nil
}

// applyAgentNetStatus enriches an AgentResponse with agent_net public key and premium status.
func (h *Handlers) applyAgentNetStatus(ctx context.Context, agent *data.Agent, resp *AgentResponse) {
	pubKey, err := resolveAgentNetPublicKey(ctx, h.service.db, agent)
	if err != nil {
		return
	}
	resp.AgentNetPublicKey = pubKey

	db, err := h.getAgentNetDB()
	if err != nil || db == nil {
		return
	}
	results, err := db.Table(gowild_agent_net.PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": pubKey},
		Limit: 1,
	})
	if err == nil && len(results) > 0 {
		resp.AgentNetPremium = true
	}
}

// grantPremium inserts a premium_agents record into the agent_net production database.
func (h *Handlers) grantPremium(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	db, err := h.getAgentNetDB()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if db == nil {
		writeError(w, http.StatusNotImplemented, "AGENT_NET_DATABASE_URL is not configured")
		return
	}

	agent, err := h.service.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	pubKey, err := resolveAgentNetPublicKey(r.Context(), h.service.db, agent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check if already premium
	results, err := db.Table(gowild_agent_net.PremiumAgent{}).Query(r.Context(), gowild_data.QueryOpts{
		Where: map[string]any{"public_key": pubKey},
		Limit: 1,
	})
	if err == nil && len(results) > 0 {
		resp := map[string]any{
			"public_key": pubKey,
			"premium":    true,
			"already":    true,
		}
		if pa, ok := results[0].(*gowild_agent_net.PremiumAgent); ok {
			resp["upgraded_at"] = pa.UpgradedAt
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	now := time.Now().UTC()
	record := &gowild_agent_net.PremiumAgent{
		ID:           pubKey,
		PublicKey:    pubKey,
		TxHash:       "admin_grant_" + agentID,
		Chain:        "admin",
		UpgradedAt:   now,
		LastActiveAt: now,
	}
	if err := db.Table(gowild_agent_net.PremiumAgent{}).Insert(r.Context(), record); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert premium record: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"public_key":  pubKey,
		"premium":     true,
		"upgraded_at": now,
	})
}
