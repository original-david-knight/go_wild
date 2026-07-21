package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestHandleCompanyShopifyCRUD(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Shop Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	putBody := map[string]any{
		"shop_url":      "demo.myshopify.com",
		"api_version":   "2025-01",
		"client_id":     "client-abc",
		"client_secret": "secret-abc",
		"enabled":       true,
	}
	var putBuf bytes.Buffer
	if err := json.NewEncoder(&putBuf).Encode(putBody); err != nil {
		t.Fatalf("encode put body failed: %v", err)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/shopify", &putBuf)
	putRec := httptest.NewRecorder()
	h.handleCompany(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected put status %d, got %d body=%s", http.StatusOK, putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/shopify", nil)
	getRec := httptest.NewRecorder()
	h.handleCompany(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d body=%s", http.StatusOK, getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	shopifyResp, ok := getResp["shopify"].(map[string]any)
	if !ok {
		t.Fatalf("expected shopify object, got %T", getResp["shopify"])
	}
	if got, _ := shopifyResp["shop_url"].(string); got != "demo.myshopify.com" {
		t.Fatalf("expected shop_url demo.myshopify.com, got %q", got)
	}
	if got, _ := shopifyResp["client_id"].(string); got != "client-abc" {
		t.Fatalf("expected client_id client-abc, got %q", got)
	}
	if _, ok := shopifyResp["has_webhook_secret"]; ok {
		t.Fatalf("did not expect deprecated has_webhook_secret in Shopify response")
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/shopify/test", nil)
	testRec := httptest.NewRecorder()
	h.handleCompany(testRec, testReq)
	// In tests we don't mock Shopify HTTP; the call should fail validation/connectivity, not auth.
	if testRec.Code != http.StatusBadRequest {
		t.Fatalf("expected test status %d, got %d body=%s", http.StatusBadRequest, testRec.Code, testRec.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/companies/"+company.ID+"/shopify", nil)
	delRec := httptest.NewRecorder()
	h.handleCompany(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected delete status %d, got %d body=%s", http.StatusOK, delRec.Code, delRec.Body.String())
	}

	getReq2 := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/shopify", nil)
	getRec2 := httptest.NewRecorder()
	h.handleCompany(getRec2, getReq2)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("expected get-after-delete status %d, got %d body=%s", http.StatusOK, getRec2.Code, getRec2.Body.String())
	}
	var getResp2 map[string]any
	if err := json.NewDecoder(getRec2.Body).Decode(&getResp2); err != nil {
		t.Fatalf("decode get-after-delete response failed: %v", err)
	}
	if got := getResp2["shopify"]; got != nil {
		t.Fatalf("expected nil shopify after delete, got %#v", got)
	}
}

func TestHandleCompanyShopifyAutoConfiguresDefaultWebhook(t *testing.T) {
	t.Setenv("INGRESS_PUBLIC_URL", "https://edge.example.ngrok.app")

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Shop Auto Webhook Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	putBody := map[string]any{
		"shop_url":      "auto-demo.myshopify.com",
		"api_version":   "2025-01",
		"client_id":     "auto-client",
		"client_secret": "auto-secret",
		"enabled":       true,
	}
	var putBuf bytes.Buffer
	if err := json.NewEncoder(&putBuf).Encode(putBody); err != nil {
		t.Fatalf("encode put body failed: %v", err)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/shopify", &putBuf)
	putRec := httptest.NewRecorder()
	h.handleCompany(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected put status %d, got %d body=%s", http.StatusOK, putRec.Code, putRec.Body.String())
	}

	var putResp map[string]any
	if err := json.NewDecoder(putRec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode put response failed: %v", err)
	}
	if warning, _ := putResp["webhook_warning"].(string); warning != "" {
		t.Fatalf("unexpected webhook warning: %q", warning)
	}

	webhookResp, ok := putResp["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("expected webhook object in response, got %T", putResp["webhook"])
	}
	if got, _ := webhookResp["provider"].(string); got != "shopify" {
		t.Fatalf("expected provider shopify, got %q", got)
	}
	if got, _ := webhookResp["event"].(string); got != "orders/create" {
		t.Fatalf("expected event orders/create, got %q", got)
	}
	if got, _ := webhookResp["event_path"].(string); got != "orders_create" {
		t.Fatalf("expected event_path orders_create, got %q", got)
	}
	publicURL, _ := webhookResp["public_url"].(string)
	if !strings.Contains(publicURL, "/ingress/webhooks/shopify/") || !strings.Contains(publicURL, "/orders_create") {
		t.Fatalf("unexpected public_url: %q", publicURL)
	}

	configs, err := svc.ListCompanyWebhookConfigs(ctx, company.ID)
	if err != nil {
		t.Fatalf("ListCompanyWebhookConfigs failed: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 webhook config, got %d", len(configs))
	}
	if configs[0].Source != "shopify" || configs[0].Event != "orders/create" || configs[0].EventPath != "orders_create" {
		t.Fatalf("unexpected webhook config: %+v", configs[0])
	}
	if configs[0].TargetRole != "fulfiller" || configs[0].TargetMethod != "fulfill_order" {
		t.Fatalf("unexpected webhook target: role=%q method=%q", configs[0].TargetRole, configs[0].TargetMethod)
	}
	if strings.TrimSpace(configs[0].HMACSecret) != "auto-secret" {
		t.Fatalf("expected webhook hmac from client secret, got %q", configs[0].HMACSecret)
	}
}

func TestBuildCompanyShopifyConnection_MergesExistingAndPreservesTokenWhenIdentityUnchanged(t *testing.T) {
	exp := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	existing := &data.CompanyShopifyConnection{
		ID:               "shop-1",
		CompanyID:        "co-1",
		ShopURL:          "https://Demo.MyShopify.com/",
		APIVersion:       "2024-10",
		ClientID:         "client-old",
		ClientSecretEnc:  "secret-old",
		AccessTokenEnc:   "token-old",
		AccessTokenExpAt: exp,
		WebhookSecretEnc: "webhook-old",
		Enabled:          false,
	}

	conn := buildCompanyShopifyConnection("co-1", putCompanyShopifyRequest{}, existing)
	if conn.ID != "shop-1" {
		t.Fatalf("expected connection ID to be preserved, got %q", conn.ID)
	}
	if conn.ShopURL != normalizeShopifyShopURL(existing.ShopURL) {
		t.Fatalf("expected normalized shop URL %q, got %q", normalizeShopifyShopURL(existing.ShopURL), conn.ShopURL)
	}
	if conn.APIVersion != normalizeShopifyAPIVersion(existing.APIVersion) {
		t.Fatalf("expected API version %q, got %q", normalizeShopifyAPIVersion(existing.APIVersion), conn.APIVersion)
	}
	if conn.ClientID != "client-old" || conn.ClientSecretEnc != "secret-old" {
		t.Fatalf("expected existing credentials to be preserved, got id=%q secret=%q", conn.ClientID, conn.ClientSecretEnc)
	}
	if conn.AccessTokenEnc != "token-old" || !conn.AccessTokenExpAt.Equal(exp) {
		t.Fatalf("expected access token and expiry to be preserved")
	}
	if conn.WebhookSecretEnc != "webhook-old" {
		t.Fatalf("expected existing webhook secret to be preserved, got %q", conn.WebhookSecretEnc)
	}
	if conn.Enabled {
		t.Fatalf("expected existing enabled=false to be preserved")
	}
}

func TestBuildCompanyShopifyConnection_ClearsTokenOnIdentityChange(t *testing.T) {
	existing := &data.CompanyShopifyConnection{
		ID:               "shop-1",
		CompanyID:        "co-1",
		ShopURL:          "demo.myshopify.com",
		APIVersion:       "2024-10",
		ClientID:         "client-old",
		ClientSecretEnc:  "secret-old",
		AccessTokenEnc:   "token-old",
		AccessTokenExpAt: time.Now().Add(2 * time.Hour).UTC(),
		Enabled:          true,
	}

	conn := buildCompanyShopifyConnection("co-1", putCompanyShopifyRequest{
		ClientID: "client-new",
	}, existing)
	if conn.AccessTokenEnc != "" {
		t.Fatalf("expected token to be cleared on identity change, got %q", conn.AccessTokenEnc)
	}
	if !conn.AccessTokenExpAt.IsZero() {
		t.Fatalf("expected token expiry to be reset on identity change")
	}
}

func TestBuildCompanyShopifyConnection_ClearsTokenWhenCredentialsMissingAndHonorsEnabledOverride(t *testing.T) {
	existing := &data.CompanyShopifyConnection{
		ID:               "shop-1",
		CompanyID:        "co-1",
		ShopURL:          "demo.myshopify.com",
		APIVersion:       "2024-10",
		ClientID:         "client-old",
		ClientSecretEnc:  "",
		AccessTokenEnc:   "token-old",
		AccessTokenExpAt: time.Now().Add(2 * time.Hour).UTC(),
		Enabled:          false,
	}
	enable := true
	conn := buildCompanyShopifyConnection("co-1", putCompanyShopifyRequest{
		Enabled: &enable,
	}, existing)
	if conn.AccessTokenEnc != "" || !conn.AccessTokenExpAt.IsZero() {
		t.Fatalf("expected token to be cleared when credentials are incomplete")
	}
	if !conn.Enabled {
		t.Fatalf("expected enabled override to set connection to enabled")
	}
}
