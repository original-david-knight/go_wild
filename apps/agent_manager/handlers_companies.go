package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/crypto"
)

func companyToResponse(c *data.Company) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"id":                      c.ID,
		"name":                    c.Name,
		"description":             c.Description,
		"ceo_agent_id":            c.CEOAgentID,
		"has_webhook_ingress_key": strings.TrimSpace(c.WebhookIngressKey) != "",
		"created_at":              c.CreatedAt.Format(time.RFC3339),
		"updated_at":              c.UpdatedAt.Format(time.RFC3339),
	}
}

func addCompanyWalletFields(item map[string]any, seedPhrase string) {
	if item == nil {
		return
	}
	seedPhrase = strings.TrimSpace(seedPhrase)
	item["wallet_seed_phrase"] = seedPhrase
	if seedPhrase == "" {
		item["wallet_public_keys"] = map[string]any{}
		return
	}

	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		item["wallet_public_keys"] = map[string]any{}
		item["wallet_public_keys_error"] = "failed to derive wallet public keys: " + err.Error()
		return
	}

	item["wallet_public_keys"] = map[string]any{
		"ethereum": derived.EthAddress,
		"solana":   derived.SolAddress,
	}
}

func (h *Handlers) addCompanyWalletFieldsFromService(ctx context.Context, item map[string]any, companyID string, fallbackSeedPhrase string) {
	seedPhrase := strings.TrimSpace(fallbackSeedPhrase)
	if seedPhrase == "" {
		ensuredSeedPhrase, err := h.service.EnsureCompanyWalletSeedPhrase(ctx, companyID)
		if err != nil {
			item["wallet_seed_phrase"] = ""
			item["wallet_seed_phrase_error"] = "failed to load company wallet seed phrase: " + err.Error()
			item["wallet_public_keys"] = map[string]any{}
			return
		}
		seedPhrase = strings.TrimSpace(ensuredSeedPhrase)
	}
	addCompanyWalletFields(item, seedPhrase)
}

func companyMemberToResponse(m data.CompanyMember) map[string]any {
	return map[string]any{
		"id":         m.ID,
		"company_id": m.CompanyID,
		"agent_id":   m.AgentID,
		"role":       m.Role,
		"created_at": m.CreatedAt.Format(time.RFC3339),
	}
}

func companyKnowledgeToResponse(entry *data.CompanyKnowledgeEntry) map[string]any {
	if entry == nil {
		return nil
	}
	var tags []string
	if strings.TrimSpace(entry.TagsJSON) != "" {
		_ = json.Unmarshal([]byte(entry.TagsJSON), &tags)
	}
	metadata := map[string]any{}
	if strings.TrimSpace(entry.MetadataJSON) != "" {
		_ = json.Unmarshal([]byte(entry.MetadataJSON), &metadata)
	}
	return map[string]any{
		"id":                  entry.ID,
		"company_id":          entry.CompanyID,
		"kind":                entry.Kind,
		"title":               entry.Title,
		"content":             entry.Content,
		"tags":                tags,
		"metadata":            metadata,
		"created_by_agent_id": entry.CreatedByAgentID,
		"created_at":          entry.CreatedAt.Format(time.RFC3339),
		"updated_at":          entry.UpdatedAt.Format(time.RFC3339),
	}
}

func (h *Handlers) listCompanies(w http.ResponseWriter, r *http.Request) {
	companies, err := h.service.ListCompanies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list companies: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(companies))
	for i := range companies {
		item := companyToResponse(&companies[i])
		h.addCompanyWalletFieldsFromService(r.Context(), item, companies[i].ID, companies[i].WalletSeedPhrase)
		members, err := h.service.ListCompanyMembers(r.Context(), companies[i].ID)
		if err == nil {
			memberList := make([]map[string]any, len(members))
			for j, m := range members {
				memberList[j] = companyMemberToResponse(m)
			}
			item["members"] = memberList
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"companies": out})
}

func (h *Handlers) createCompany(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		CEOAgentID  string `json:"ceo_agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	company, err := h.service.CreateCompany(r.Context(), req.Name, req.Description, req.CEOAgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create company: "+err.Error())
		return
	}
	resp := companyToResponse(company)
	h.addCompanyWalletFieldsFromService(r.Context(), resp, company.ID, company.WalletSeedPhrase)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) getCompany(w http.ResponseWriter, r *http.Request, companyID string) {
	company, err := h.service.GetCompany(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	resp := companyToResponse(company)
	h.addCompanyWalletFieldsFromService(r.Context(), resp, company.ID, company.WalletSeedPhrase)
	members, err := h.service.ListCompanyMembers(r.Context(), companyID)
	if err == nil {
		memberList := make([]map[string]any, len(members))
		for i, m := range members {
			memberList[i] = companyMemberToResponse(m)
		}
		resp["members"] = memberList
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) updateCompany(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
		CEOAgentID  *string `json:"ceo_agent_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	company, err := h.service.GetCompany(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	if req.Name != nil {
		company.Name = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		company.Description = strings.TrimSpace(*req.Description)
	}
	if err := h.service.UpdateCompany(r.Context(), company); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update company: "+err.Error())
		return
	}
	if req.CEOAgentID != nil {
		if err := h.service.SetCompanyCEO(r.Context(), companyID, strings.TrimSpace(*req.CEOAgentID)); err != nil {
			writeError(w, http.StatusBadRequest, "failed to set ceo: "+err.Error())
			return
		}
		company.CEOAgentID = strings.TrimSpace(*req.CEOAgentID)
	}
	resp := companyToResponse(company)
	h.addCompanyWalletFieldsFromService(r.Context(), resp, company.ID, company.WalletSeedPhrase)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) deleteCompany(w http.ResponseWriter, r *http.Request, companyID string) {
	if err := h.service.DeleteCompany(r.Context(), companyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete company: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *Handlers) listCompanyMembers(w http.ResponseWriter, r *http.Request, companyID string) {
	members, err := h.service.ListCompanyMembers(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members: "+err.Error())
		return
	}
	out := make([]map[string]any, len(members))
	for i, m := range members {
		out[i] = companyMemberToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

func (h *Handlers) addCompanyMember(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		AgentID string `json:"agent_id"`
		Role    string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if err := h.service.AddAgentToCompany(r.Context(), companyID, req.AgentID, req.Role); err != nil {
		if errors.Is(err, data.ErrCompanyMembershipConflict) {
			writeError(w, http.StatusConflict, "failed to add member: "+err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "failed to add member: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "added"})
}

func (h *Handlers) updateCompanyMember(w http.ResponseWriter, r *http.Request, companyID, agentID string) {
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Role = strings.TrimSpace(req.Role)
	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}
	if err := h.service.AddAgentToCompany(r.Context(), companyID, agentID, req.Role); err != nil {
		if errors.Is(err, data.ErrCompanyMembershipConflict) {
			writeError(w, http.StatusConflict, "failed to update member: "+err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "failed to update member: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "updated",
		"agent_id": agentID,
		"role":     req.Role,
	})
}

func (h *Handlers) removeCompanyMember(w http.ResponseWriter, r *http.Request, companyID, agentID string) {
	if err := h.service.RemoveAgentFromCompany(r.Context(), companyID, agentID); err != nil {
		writeError(w, http.StatusBadRequest, "failed to remove member: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "removed"})
}

func (h *Handlers) setCompanyCEO(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if err := h.service.SetCompanyCEO(r.Context(), companyID, req.AgentID); err != nil {
		writeError(w, http.StatusBadRequest, "failed to set ceo: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func companyWebhookConfigToResponse(cfg *data.WebhookConfig, ingressBaseURL, ingressKey string) map[string]any {
	if cfg == nil {
		return nil
	}
	out := map[string]any{
		"id":            cfg.ID,
		"company_id":    cfg.CompanyID,
		"provider":      cfg.Source,
		"event":         cfg.Event,
		"event_path":    cfg.EventPath,
		"target_role":   cfg.TargetRole,
		"target_method": cfg.TargetMethod,
		"enabled":       cfg.Enabled,
		"has_hmac":      strings.TrimSpace(cfg.HMACSecret) != "",
	}
	if ingressBaseURL != "" && ingressKey != "" && cfg.EventPath != "" && cfg.Source != "" {
		out["public_url"] = strings.TrimRight(ingressBaseURL, "/") + "/ingress/webhooks/" + cfg.Source + "/" + ingressKey + "/" + cfg.EventPath
	}
	return out
}

func (h *Handlers) getCompanyWebhooks(w http.ResponseWriter, r *http.Request, companyID string) {
	company, err := h.service.GetCompany(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	ingressKey, err := h.service.EnsureCompanyWebhookIngressKey(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to ensure webhook ingress key: "+err.Error())
		return
	}
	configs, err := h.service.ListCompanyWebhookConfigs(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhook configs: "+err.Error())
		return
	}
	ingressBaseURL, _ := ingressPublicBaseURLWithError()
	items := make([]map[string]any, 0, len(configs))
	for i := range configs {
		items = append(items, companyWebhookConfigToResponse(&configs[i], ingressBaseURL, ingressKey))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"company_id":          companyID,
		"company_name":        company.Name,
		"webhook_ingress_key": ingressKey,
		"webhooks":            items,
	})
}

func (h *Handlers) putCompanyWebhook(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		Provider     string `json:"provider"`
		Event        string `json:"event"`
		EventPath    string `json:"event_path"`
		TargetRole   string `json:"target_role"`
		TargetMethod string `json:"target_method"`
		HMACSecret   string `json:"hmac_secret"`
		Enabled      *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	provider := strings.TrimSpace(strings.ToLower(req.Provider))
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	event := strings.TrimSpace(req.Event)
	if event == "" {
		writeError(w, http.StatusBadRequest, "event is required")
		return
	}
	eventPath := normalizeWebhookEventPath(req.EventPath)
	if eventPath == "" {
		eventPath = normalizeWebhookEventPath(event)
	}
	if eventPath == "" {
		writeError(w, http.StatusBadRequest, "event_path is required")
		return
	}

	cfg := &data.WebhookConfig{
		CompanyID:    companyID,
		Source:       provider,
		Event:        event,
		EventPath:    eventPath,
		TargetRole:   strings.TrimSpace(req.TargetRole),
		TargetMethod: strings.TrimSpace(req.TargetMethod),
		HMACSecret:   strings.TrimSpace(req.HMACSecret),
		Enabled:      true,
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}

	existing, err := h.service.GetCompanyWebhookConfigByPath(r.Context(), companyID, provider, eventPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load existing webhook config: "+err.Error())
		return
	}
	if existing != nil {
		cfg.ID = existing.ID
		if cfg.HMACSecret == "" {
			cfg.HMACSecret = existing.HMACSecret
		}
	}

	if err := h.service.UpsertCompanyWebhookConfig(r.Context(), cfg); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save webhook config: "+err.Error())
		return
	}
	ingressKey, _ := h.service.EnsureCompanyWebhookIngressKey(r.Context(), companyID)
	ingressBaseURL, _ := ingressPublicBaseURLWithError()
	writeJSON(w, http.StatusOK, map[string]any{
		"webhook": companyWebhookConfigToResponse(cfg, ingressBaseURL, ingressKey),
	})
}

func (h *Handlers) rotateCompanyWebhookKey(w http.ResponseWriter, r *http.Request, companyID string) {
	key, err := h.service.RotateCompanyWebhookIngressKey(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to rotate webhook ingress key: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":              "rotated",
		"webhook_ingress_key": key,
	})
}

func (h *Handlers) getCompanyPublicEndpoints(w http.ResponseWriter, r *http.Request, companyID string) {
	company, err := h.service.GetCompany(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusNotFound, "company not found")
		return
	}
	ingressKey, err := h.service.EnsureCompanyWebhookIngressKey(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to ensure webhook ingress key: "+err.Error())
		return
	}
	ingressBaseURL, err := ingressPublicBaseURLWithError()
	if err != nil {
		writeError(w, http.StatusBadRequest, "ingress public url invalid: "+err.Error())
		return
	}
	configs, _ := h.service.ListCompanyWebhookConfigs(r.Context(), companyID)
	webhookURLs := make([]map[string]any, 0, len(configs))
	for i := range configs {
		url := ""
		if ingressBaseURL != "" {
			url = strings.TrimRight(ingressBaseURL, "/") + "/ingress/webhooks/" + configs[i].Source + "/" + ingressKey + "/" + configs[i].EventPath
		}
		webhookURLs = append(webhookURLs, map[string]any{
			"provider":   configs[i].Source,
			"event":      configs[i].Event,
			"event_path": configs[i].EventPath,
			"url":        url,
			"enabled":    configs[i].Enabled,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"company_id":          companyID,
		"company_name":        company.Name,
		"ingress_public_url":  ingressBaseURL,
		"webhook_ingress_key": ingressKey,
		"webhooks":            webhookURLs,
	})
}

func (h *Handlers) listCompanyKnowledge(w http.ResponseWriter, r *http.Request, companyID string) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := h.service.ListCompanyKnowledgeEntries(r.Context(), companyID, query, kind, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list company knowledge: "+err.Error())
		return
	}
	out := make([]map[string]any, len(entries))
	for i := range entries {
		out[i] = companyKnowledgeToResponse(&entries[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (h *Handlers) addCompanyKnowledge(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		Kind             string         `json:"kind"`
		Title            string         `json:"title"`
		Content          string         `json:"content"`
		Tags             []string       `json:"tags"`
		Metadata         map[string]any `json:"metadata"`
		CreatedByAgentID string         `json:"created_by_agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	entry, err := h.service.AddCompanyKnowledgeEntry(r.Context(), companyID, req.CreatedByAgentID, req.Kind, req.Title, req.Content, req.Tags, req.Metadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to add company knowledge: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, companyKnowledgeToResponse(entry))
}

func (h *Handlers) getCompanyKnowledge(w http.ResponseWriter, r *http.Request, companyID, entryID string) {
	entry, err := h.service.GetCompanyKnowledgeEntry(r.Context(), companyID, entryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "company knowledge entry not found")
		return
	}
	writeJSON(w, http.StatusOK, companyKnowledgeToResponse(entry))
}

func (h *Handlers) updateCompanyKnowledge(w http.ResponseWriter, r *http.Request, companyID, entryID string) {
	var req struct {
		Kind     string         `json:"kind"`
		Title    string         `json:"title"`
		Content  string         `json:"content"`
		Tags     []string       `json:"tags"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	entry, err := h.service.UpdateCompanyKnowledgeEntry(r.Context(), companyID, entryID, req.Kind, req.Title, req.Content, req.Tags, req.Metadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to update company knowledge: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, companyKnowledgeToResponse(entry))
}

func (h *Handlers) deleteCompanyKnowledge(w http.ResponseWriter, r *http.Request, companyID, entryID string) {
	if err := h.service.DeleteCompanyKnowledgeEntry(r.Context(), companyID, entryID); err != nil {
		writeError(w, http.StatusBadRequest, "failed to delete company knowledge: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *Handlers) getAgentCompany(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	company, err := h.service.GetCompanyForAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve company: "+err.Error())
		return
	}
	if company == nil {
		writeJSON(w, http.StatusOK, map[string]any{"company": nil})
		return
	}
	member, _ := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
	resp := map[string]any{"company": companyToResponse(company)}
	if member != nil {
		resp["member"] = companyMemberToResponse(*member)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) getAgentCompanyMethodTools(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	specs, err := listCompanyMethodTools(r.Context(), h.service.db, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve company method tools: "+err.Error())
		return
	}

	tools := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, spec.asMap())
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}
