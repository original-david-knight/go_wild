package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/supplier/providers/cjdropshipping"
	"github.com/original-david-knight/go_wild/tools/amazon"
	"github.com/original-david-knight/go_wild/tools/supplier"
	"github.com/original-david-knight/go_wild/tools/supplier/providers"
)

func companyPolymarketToResponse(conn *data.CompanyPolymarketConnection) map[string]any {
	if conn == nil {
		return nil
	}
	return map[string]any{
		"id":              conn.ID,
		"company_id":      conn.CompanyID,
		"proxy_url":       conn.ProxyURL,
		"onchain_rpc_url": conn.OnchainRPCURL,
		"funder_address":  conn.FunderAddress,
		"signature_type":  conn.SignatureType,
		"chain_id":        conn.ChainID,
		"enabled":         conn.Enabled,
		"created_at":      conn.CreatedAt.Format(time.RFC3339),
		"updated_at":      conn.UpdatedAt.Format(time.RFC3339),
		"last_tested_at":  conn.LastTestedAt.Format(time.RFC3339),
	}
}

func (h *Handlers) getCompanyPolymarket(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyPolymarketConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load polymarket config: "+err.Error())
		return
	}
	if conn == nil {
		writeJSON(w, http.StatusOK, map[string]any{"polymarket": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"polymarket": companyPolymarketToResponse(conn)})
}

func (h *Handlers) putCompanyPolymarket(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		ProxyURL      string `json:"proxy_url"`
		OnchainRPCURL string `json:"onchain_rpc_url"`
		FunderAddress string `json:"funder_address"`
		SignatureType *int   `json:"signature_type,omitempty"`
		ChainID       *int   `json:"chain_id,omitempty"`
		Enabled       *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.service.GetCompanyPolymarketConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load existing polymarket config: "+err.Error())
		return
	}

	conn := &data.CompanyPolymarketConnection{
		CompanyID:     companyID,
		ProxyURL:      strings.TrimSpace(req.ProxyURL),
		OnchainRPCURL: strings.TrimSpace(req.OnchainRPCURL),
		FunderAddress: strings.TrimSpace(req.FunderAddress),
		SignatureType: 0,
		ChainID:       0,
		Enabled:       true,
	}
	if existing != nil {
		conn.ID = existing.ID
		if conn.ProxyURL == "" {
			conn.ProxyURL = existing.ProxyURL
		}
		if conn.OnchainRPCURL == "" {
			conn.OnchainRPCURL = existing.OnchainRPCURL
		}
		if conn.FunderAddress == "" {
			conn.FunderAddress = existing.FunderAddress
		}
		conn.SignatureType = existing.SignatureType
		conn.ChainID = existing.ChainID
		conn.Enabled = existing.Enabled
	}
	if req.SignatureType != nil {
		conn.SignatureType = *req.SignatureType
	}
	if req.ChainID != nil {
		conn.ChainID = *req.ChainID
	}
	if req.Enabled != nil {
		conn.Enabled = *req.Enabled
	}

	if err := h.service.UpsertCompanyPolymarketConnection(r.Context(), conn); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save polymarket config: "+err.Error())
		return
	}
	saved, _ := h.service.GetCompanyPolymarketConnection(r.Context(), companyID)
	writeJSON(w, http.StatusOK, map[string]any{"polymarket": companyPolymarketToResponse(saved)})
}

func (h *Handlers) deleteCompanyPolymarket(w http.ResponseWriter, r *http.Request, companyID string) {
	if err := h.service.DeleteCompanyPolymarketConnection(r.Context(), companyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete polymarket config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func companyTopDawgToResponse(conn *data.CompanyTopDawgConnection) map[string]any {
	if conn == nil {
		return nil
	}
	return map[string]any{
		"id":             conn.ID,
		"company_id":     conn.CompanyID,
		"supplier_id":    conn.SupplierID,
		"enabled":        conn.Enabled,
		"has_api_key":    strings.TrimSpace(conn.APIKeyEnc) != "",
		"created_at":     conn.CreatedAt.Format(time.RFC3339),
		"updated_at":     conn.UpdatedAt.Format(time.RFC3339),
		"last_tested_at": conn.LastTestedAt.Format(time.RFC3339),
	}
}

func (h *Handlers) getCompanyTopDawg(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyTopDawgConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load topdawg config: "+err.Error())
		return
	}
	if conn == nil {
		writeJSON(w, http.StatusOK, map[string]any{"topdawg": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"topdawg": companyTopDawgToResponse(conn)})
}

func (h *Handlers) putCompanyTopDawg(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		APIKey     string `json:"api_key"`
		SupplierID string `json:"supplier_id"`
		Enabled    *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.service.GetCompanyTopDawgConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load existing topdawg config: "+err.Error())
		return
	}

	conn := &data.CompanyTopDawgConnection{
		CompanyID:  companyID,
		APIKeyEnc:  strings.TrimSpace(req.APIKey),
		SupplierID: strings.TrimSpace(req.SupplierID),
		Enabled:    true,
	}
	if existing != nil {
		conn.ID = existing.ID
		if conn.APIKeyEnc == "" {
			conn.APIKeyEnc = existing.APIKeyEnc
		}
		if conn.SupplierID == "" {
			conn.SupplierID = existing.SupplierID
		}
		conn.Enabled = existing.Enabled
	}
	if req.Enabled != nil {
		conn.Enabled = *req.Enabled
	}

	if err := h.service.UpsertCompanyTopDawgConnection(r.Context(), conn); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save topdawg config: "+err.Error())
		return
	}
	saved, _ := h.service.GetCompanyTopDawgConnection(r.Context(), companyID)
	writeJSON(w, http.StatusOK, map[string]any{"topdawg": companyTopDawgToResponse(saved)})
}

func (h *Handlers) deleteCompanyTopDawg(w http.ResponseWriter, r *http.Request, companyID string) {
	if err := h.service.DeleteCompanyTopDawgConnection(r.Context(), companyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete topdawg config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *Handlers) testCompanyTopDawg(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyTopDawgConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load topdawg config: "+err.Error())
		return
	}
	if conn == nil || !conn.Enabled {
		writeError(w, http.StatusBadRequest, "company topdawg config missing or disabled")
		return
	}
	client := providers.NewTopDawg(strings.TrimSpace(conn.APIKeyEnc), strings.TrimSpace(conn.SupplierID))
	_, err = client.SearchProducts(r.Context(), "test", supplier.SearchOpts{Page: 1})
	if err != nil {
		writeError(w, http.StatusBadRequest, "topdawg test failed: "+err.Error())
		return
	}
	conn.LastTestedAt = time.Now()
	_ = h.service.UpsertCompanyTopDawgConnection(r.Context(), conn)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func companyCJDropshippingToResponse(conn *data.CompanyCJDropshippingConnection) map[string]any {
	if conn == nil {
		return nil
	}
	accessExpAt := ""
	if !conn.AccessTokenExpAt.IsZero() {
		accessExpAt = conn.AccessTokenExpAt.Format(time.RFC3339)
	}
	refreshExpAt := ""
	if !conn.RefreshTokenExpAt.IsZero() {
		refreshExpAt = conn.RefreshTokenExpAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id":                        conn.ID,
		"company_id":                conn.CompanyID,
		"default_from_country_code": conn.DefaultFromCountryCode,
		"enabled":                   conn.Enabled,
		"has_api_key":               strings.TrimSpace(conn.APIKeyEnc) != "",
		"has_access_token":          strings.TrimSpace(conn.AccessTokenEnc) != "",
		"has_refresh_token":         strings.TrimSpace(conn.RefreshTokenEnc) != "",
		"has_platform_token":        strings.TrimSpace(conn.PlatformTokenEnc) != "",
		"access_token_expires_at":   accessExpAt,
		"refresh_token_expires_at":  refreshExpAt,
		"created_at":                conn.CreatedAt.Format(time.RFC3339),
		"updated_at":                conn.UpdatedAt.Format(time.RFC3339),
		"last_tested_at":            conn.LastTestedAt.Format(time.RFC3339),
	}
}

func (h *Handlers) getCompanyCJDropshipping(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyCJDropshippingConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load cjdropshipping config: "+err.Error())
		return
	}
	if conn == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cjdropshipping": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cjdropshipping": companyCJDropshippingToResponse(conn)})
}

func (h *Handlers) putCompanyCJDropshipping(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		APIKey                 string `json:"api_key"`
		AccessToken            string `json:"access_token"`
		RefreshToken           string `json:"refresh_token"`
		PlatformToken          string `json:"platform_token"`
		DefaultFromCountryCode string `json:"default_from_country_code"`
		Enabled                *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.service.GetCompanyCJDropshippingConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load existing cjdropshipping config: "+err.Error())
		return
	}

	incomingAPIKey := strings.TrimSpace(req.APIKey)
	incomingAccessToken := strings.TrimSpace(req.AccessToken)
	incomingRefreshToken := strings.TrimSpace(req.RefreshToken)

	conn := &data.CompanyCJDropshippingConnection{
		CompanyID:              companyID,
		APIKeyEnc:              incomingAPIKey,
		AccessTokenEnc:         incomingAccessToken,
		RefreshTokenEnc:        incomingRefreshToken,
		PlatformTokenEnc:       strings.TrimSpace(req.PlatformToken),
		DefaultFromCountryCode: strings.ToUpper(strings.TrimSpace(req.DefaultFromCountryCode)),
		Enabled:                true,
	}
	if existing != nil {
		conn.ID = existing.ID
		if conn.APIKeyEnc == "" {
			conn.APIKeyEnc = existing.APIKeyEnc
		}
		if conn.AccessTokenEnc == "" {
			conn.AccessTokenEnc = existing.AccessTokenEnc
			conn.AccessTokenExpAt = existing.AccessTokenExpAt
		}
		if conn.RefreshTokenEnc == "" {
			conn.RefreshTokenEnc = existing.RefreshTokenEnc
			conn.RefreshTokenExpAt = existing.RefreshTokenExpAt
		}
		if conn.PlatformTokenEnc == "" {
			conn.PlatformTokenEnc = existing.PlatformTokenEnc
		}
		if conn.DefaultFromCountryCode == "" {
			conn.DefaultFromCountryCode = existing.DefaultFromCountryCode
		}
		conn.Enabled = existing.Enabled

		if incomingAPIKey != "" && incomingAPIKey != strings.TrimSpace(existing.APIKeyEnc) {
			// API key changed, so stale provider-issued tokens should not be reused unless explicitly provided.
			if incomingAccessToken == "" {
				conn.AccessTokenEnc = ""
				conn.AccessTokenExpAt = time.Time{}
			}
			if incomingRefreshToken == "" {
				conn.RefreshTokenEnc = ""
				conn.RefreshTokenExpAt = time.Time{}
			}
		}
	}
	if incomingAccessToken != "" {
		conn.AccessTokenExpAt = time.Time{}
	}
	if incomingRefreshToken != "" {
		conn.RefreshTokenExpAt = time.Time{}
	}
	if req.Enabled != nil {
		conn.Enabled = *req.Enabled
	}

	if err := h.service.UpsertCompanyCJDropshippingConnection(r.Context(), conn); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save cjdropshipping config: "+err.Error())
		return
	}
	saved, _ := h.service.GetCompanyCJDropshippingConnection(r.Context(), companyID)
	writeJSON(w, http.StatusOK, map[string]any{"cjdropshipping": companyCJDropshippingToResponse(saved)})
}

func (h *Handlers) deleteCompanyCJDropshipping(w http.ResponseWriter, r *http.Request, companyID string) {
	if err := h.service.DeleteCompanyCJDropshippingConnection(r.Context(), companyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cjdropshipping config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *Handlers) testCompanyCJDropshipping(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyCJDropshippingConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load cjdropshipping config: "+err.Error())
		return
	}
	if conn == nil || !conn.Enabled {
		writeError(w, http.StatusBadRequest, "company cjdropshipping config missing or disabled")
		return
	}

	accessToken, err := resolveCompanyCJDropshippingAccessToken(r.Context(), h.service.db, conn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cjdropshipping auth failed: "+err.Error())
		return
	}
	client := newCJDropshippingAPIClient(accessToken, conn.PlatformTokenEnc)
	_, err = client.ListProductsV2(r.Context(), cjdropshipping.ProductListV2Params{
		Page:    1,
		Size:    1,
		KeyWord: "test",
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "cjdropshipping test failed: "+err.Error())
		return
	}

	conn.LastTestedAt = time.Now()
	_ = h.service.UpsertCompanyCJDropshippingConnection(r.Context(), conn)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func companyAmazonToResponse(conn *data.CompanyAmazonConnection) map[string]any {
	if conn == nil {
		return nil
	}
	return map[string]any{
		"id":             conn.ID,
		"company_id":     conn.CompanyID,
		"partner_tag":    conn.PartnerTag,
		"marketplace":    conn.Marketplace,
		"enabled":        conn.Enabled,
		"has_access_key": strings.TrimSpace(conn.AccessKeyEnc) != "",
		"has_secret_key": strings.TrimSpace(conn.SecretKeyEnc) != "",
		"created_at":     conn.CreatedAt.Format(time.RFC3339),
		"updated_at":     conn.UpdatedAt.Format(time.RFC3339),
		"last_tested_at": conn.LastTestedAt.Format(time.RFC3339),
	}
}

func (h *Handlers) getCompanyAmazon(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyAmazonConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load amazon config: "+err.Error())
		return
	}
	if conn == nil {
		writeJSON(w, http.StatusOK, map[string]any{"amazon": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"amazon": companyAmazonToResponse(conn)})
}

func (h *Handlers) putCompanyAmazon(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		AccessKey   string `json:"access_key"`
		SecretKey   string `json:"secret_key"`
		PartnerTag  string `json:"partner_tag"`
		Marketplace string `json:"marketplace"`
		Enabled     *bool  `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := h.service.GetCompanyAmazonConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load existing amazon config: "+err.Error())
		return
	}

	conn := &data.CompanyAmazonConnection{
		CompanyID:    companyID,
		AccessKeyEnc: strings.TrimSpace(req.AccessKey),
		SecretKeyEnc: strings.TrimSpace(req.SecretKey),
		PartnerTag:   strings.TrimSpace(req.PartnerTag),
		Marketplace:  strings.TrimSpace(req.Marketplace),
		Enabled:      true,
	}
	if existing != nil {
		conn.ID = existing.ID
		if conn.AccessKeyEnc == "" {
			conn.AccessKeyEnc = existing.AccessKeyEnc
		}
		if conn.SecretKeyEnc == "" {
			conn.SecretKeyEnc = existing.SecretKeyEnc
		}
		if conn.PartnerTag == "" {
			conn.PartnerTag = existing.PartnerTag
		}
		if conn.Marketplace == "" {
			conn.Marketplace = existing.Marketplace
		}
		conn.Enabled = existing.Enabled
	}
	if req.Enabled != nil {
		conn.Enabled = *req.Enabled
	}

	if err := h.service.UpsertCompanyAmazonConnection(r.Context(), conn); err != nil {
		writeError(w, http.StatusBadRequest, "failed to save amazon config: "+err.Error())
		return
	}
	saved, _ := h.service.GetCompanyAmazonConnection(r.Context(), companyID)
	writeJSON(w, http.StatusOK, map[string]any{"amazon": companyAmazonToResponse(saved)})
}

func (h *Handlers) deleteCompanyAmazon(w http.ResponseWriter, r *http.Request, companyID string) {
	if err := h.service.DeleteCompanyAmazonConnection(r.Context(), companyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete amazon config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

func (h *Handlers) testCompanyAmazon(w http.ResponseWriter, r *http.Request, companyID string) {
	conn, err := h.service.GetCompanyAmazonConnection(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load amazon config: "+err.Error())
		return
	}
	if conn == nil || !conn.Enabled {
		writeError(w, http.StatusBadRequest, "company amazon config missing or disabled")
		return
	}
	client := amazon.NewPAAClient(strings.TrimSpace(conn.AccessKeyEnc), strings.TrimSpace(conn.SecretKeyEnc), strings.TrimSpace(conn.PartnerTag), conn.Marketplace)
	_, err = client.SearchItems(r.Context(), amazon.SearchInput{Keywords: "test", Limit: 1})
	if err != nil {
		writeError(w, http.StatusBadRequest, "amazon test failed: "+err.Error())
		return
	}
	conn.LastTestedAt = time.Now()
	_ = h.service.UpsertCompanyAmazonConnection(r.Context(), conn)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
