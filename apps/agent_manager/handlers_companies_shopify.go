package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/shopify"
)

const (
	defaultCompanyShopifyWebhookProvider  = "shopify"
	defaultCompanyShopifyWebhookEvent     = "orders/create"
	defaultCompanyShopifyWebhookEventPath = "orders_create"
	defaultCompanyShopifyWebhookRole      = "fulfiller"
	defaultCompanyShopifyWebhookMethod    = "fulfill_order"
)

func companyShopifyToResponse(conn *data.CompanyShopifyConnection) map[string]any {
	if conn == nil {
		return nil
	}
	expAt := ""
	if !conn.AccessTokenExpAt.IsZero() {
		expAt = conn.AccessTokenExpAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id":                      conn.ID,
		"company_id":              conn.CompanyID,
		"shop_url":                conn.ShopURL,
		"api_version":             conn.APIVersion,
		"client_id":               conn.ClientID,
		"enabled":                 conn.Enabled,
		"has_client_secret":       strings.TrimSpace(conn.ClientSecretEnc) != "",
		"access_token_expires_at": expAt,
		"created_at":              conn.CreatedAt.Format(time.RFC3339),
		"updated_at":              conn.UpdatedAt.Format(time.RFC3339),
		"last_tested_at":          conn.LastTestedAt.Format(time.RFC3339),
	}
}

func companyShopifyWebhookSecret(conn *data.CompanyShopifyConnection) string {
	if conn == nil {
		return ""
	}
	if secret := strings.TrimSpace(conn.WebhookSecretEnc); secret != "" {
		return secret
	}
	return strings.TrimSpace(conn.ClientSecretEnc)
}

func (h *Handlers) ensureDefaultCompanyShopifyWebhook(ctx context.Context, conn *data.CompanyShopifyConnection) (*data.WebhookConfig, error) {
	if conn == nil {
		return nil, fmt.Errorf("company shopify config is missing")
	}
	companyID := strings.TrimSpace(conn.CompanyID)
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}
	eventPath := normalizeWebhookEventPath(defaultCompanyShopifyWebhookEventPath)
	if eventPath == "" {
		eventPath = normalizeWebhookEventPath(defaultCompanyShopifyWebhookEvent)
	}
	if eventPath == "" {
		return nil, fmt.Errorf("default shopify webhook event path is invalid")
	}

	existing, err := h.service.GetCompanyWebhookConfigByPath(ctx, companyID, defaultCompanyShopifyWebhookProvider, eventPath)
	if err != nil {
		return nil, err
	}

	hmacSecret := companyShopifyWebhookSecret(conn)
	if hmacSecret == "" && existing != nil {
		hmacSecret = strings.TrimSpace(existing.HMACSecret)
	}
	if hmacSecret == "" {
		if !conn.Enabled {
			// Allow disabling Shopify config without forcing webhook secret creation.
			return nil, nil
		}
		return nil, fmt.Errorf("shopify webhook secret is missing; set client_secret or webhook_secret")
	}

	cfg := &data.WebhookConfig{
		CompanyID:    companyID,
		Source:       defaultCompanyShopifyWebhookProvider,
		Event:        defaultCompanyShopifyWebhookEvent,
		EventPath:    eventPath,
		TargetRole:   defaultCompanyShopifyWebhookRole,
		TargetMethod: defaultCompanyShopifyWebhookMethod,
		HMACSecret:   hmacSecret,
		Enabled:      conn.Enabled,
	}
	if existing != nil {
		cfg.ID = existing.ID
		if v := strings.TrimSpace(existing.Event); v != "" {
			cfg.Event = v
		}
		if v := strings.TrimSpace(existing.TargetRole); v != "" {
			cfg.TargetRole = v
		}
		if v := strings.TrimSpace(existing.TargetMethod); v != "" {
			cfg.TargetMethod = v
		}
	}
	if err := h.service.UpsertCompanyWebhookConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (h *Handlers) getCompanyShopify(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyShopifyConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shopify config: "+err.Error())
		return
	}
	if conn == nil {
		writeJSON(w, http.StatusOK, map[string]any{"shopify": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shopify": companyShopifyToResponse(conn)})
}

type putCompanyShopifyRequest struct {
	ShopURL       string `json:"shop_url"`
	APIVersion    string `json:"api_version"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

func decodePutCompanyShopifyRequest(r *http.Request) (putCompanyShopifyRequest, error) {
	var req putCompanyShopifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return putCompanyShopifyRequest{}, err
	}
	return req, nil
}

func buildCompanyShopifyConnection(companyID string, req putCompanyShopifyRequest, existing *data.CompanyShopifyConnection) *data.CompanyShopifyConnection {
	conn := &data.CompanyShopifyConnection{
		CompanyID:        companyID,
		ShopURL:          strings.TrimSpace(req.ShopURL),
		APIVersion:       strings.TrimSpace(req.APIVersion),
		ClientID:         strings.TrimSpace(req.ClientID),
		ClientSecretEnc:  strings.TrimSpace(req.ClientSecret),
		WebhookSecretEnc: strings.TrimSpace(req.WebhookSecret),
		Enabled:          true,
	}
	if existing != nil {
		mergeCompanyShopifyConnectionFromExisting(conn, existing)
	}

	conn.ShopURL = normalizeShopifyShopURL(conn.ShopURL)
	conn.APIVersion = normalizeShopifyAPIVersion(conn.APIVersion)
	if shopifyConnectionIdentityChanged(existing, conn) {
		// Force refresh on next use when app identity changes.
		conn.AccessTokenEnc = ""
		conn.AccessTokenExpAt = time.Time{}
	}
	if strings.TrimSpace(conn.ClientID) == "" || strings.TrimSpace(conn.ClientSecretEnc) == "" {
		conn.AccessTokenEnc = ""
		conn.AccessTokenExpAt = time.Time{}
	}
	if req.Enabled != nil {
		conn.Enabled = *req.Enabled
	}
	return conn
}

func mergeCompanyShopifyConnectionFromExisting(conn, existing *data.CompanyShopifyConnection) {
	if conn == nil || existing == nil {
		return
	}
	conn.ID = existing.ID
	if conn.ShopURL == "" {
		conn.ShopURL = existing.ShopURL
	}
	if conn.APIVersion == "" {
		conn.APIVersion = existing.APIVersion
	}
	if conn.ClientID == "" {
		conn.ClientID = existing.ClientID
	}
	if conn.ClientSecretEnc == "" {
		conn.ClientSecretEnc = existing.ClientSecretEnc
	}
	conn.AccessTokenEnc = existing.AccessTokenEnc
	conn.AccessTokenExpAt = existing.AccessTokenExpAt
	if conn.WebhookSecretEnc == "" {
		conn.WebhookSecretEnc = existing.WebhookSecretEnc
	}
	conn.Enabled = existing.Enabled
}

func shopifyConnectionIdentityChanged(existing, conn *data.CompanyShopifyConnection) bool {
	if existing == nil || conn == nil {
		return false
	}
	return conn.ShopURL != normalizeShopifyShopURL(existing.ShopURL) ||
		conn.ClientID != strings.TrimSpace(existing.ClientID) ||
		conn.ClientSecretEnc != strings.TrimSpace(existing.ClientSecretEnc)
}

func (h *Handlers) buildCompanyShopifyUpsertResponse(ctx context.Context, companyID string, saved *data.CompanyShopifyConnection) map[string]any {
	resp := map[string]any{"shopify": companyShopifyToResponse(saved)}
	if saved == nil {
		return resp
	}
	webhookCfg, webhookErr := h.ensureDefaultCompanyShopifyWebhook(ctx, saved)
	if webhookErr != nil {
		resp["webhook_warning"] = "default Shopify webhook was not configured: " + webhookErr.Error()
	}
	ingressKey, ingressKeyErr := h.service.EnsureCompanyWebhookIngressKey(ctx, companyID)
	if ingressKeyErr == nil && strings.TrimSpace(ingressKey) != "" {
		resp["webhook_ingress_key"] = ingressKey
	} else if ingressKeyErr != nil && webhookErr == nil {
		resp["webhook_warning"] = "default Shopify webhook was configured, but ingress key lookup failed: " + ingressKeyErr.Error()
	}
	if webhookCfg != nil {
		ingressBaseURL, _ := ingressPublicBaseURLWithError()
		resp["webhook"] = companyWebhookConfigToResponse(webhookCfg, ingressBaseURL, ingressKey)
	}
	return resp
}

func (h *Handlers) putCompanyShopify(w http.ResponseWriter, r *http.Request, companyID string) {
	req, err := decodePutCompanyShopifyRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.service.GetCompanyShopifyConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load existing shopify config: "+err.Error())
		return
	}

	conn := buildCompanyShopifyConnection(companyID, req, existing)

	if err := h.service.UpsertCompanyShopifyConnection(r.Context(), conn); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save shopify config: "+err.Error())
		return
	}

	saved, _ := h.service.GetCompanyShopifyConnection(r.Context(), companyID)
	writeJSON(w, http.StatusOK, h.buildCompanyShopifyUpsertResponse(r.Context(), companyID, saved))
}

func (h *Handlers) deleteCompanyShopify(w http.ResponseWriter, r *http.Request, companyID string) {
	if err := h.service.DeleteCompanyShopifyConnection(r.Context(), companyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete shopify config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *Handlers) testCompanyShopify(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyShopifyConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shopify config: "+err.Error())
		return
	}
	if conn == nil || !conn.Enabled {
		writeError(w, http.StatusBadRequest, "company shopify config missing or disabled")
		return
	}
	token, err := resolveCompanyShopifyAccessToken(r.Context(), h.service.db, conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "shopify auth failed: "+err.Error())
		return
	}
	client := shopify.NewShopifyClient(normalizeShopifyShopURL(conn.ShopURL), normalizeShopifyAPIVersion(conn.APIVersion), token)
	_, err = client.ListProducts(r.Context(), shopify.ListProductsInput{Limit: 1})
	if err != nil {
		writeError(w, http.StatusBadRequest, "shopify test failed: "+err.Error())
		return
	}
	conn.LastTestedAt = time.Now()
	_ = h.service.UpsertCompanyShopifyConnection(r.Context(), conn)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
