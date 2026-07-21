package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestHandleCompanyWebhooksCRUDAndEndpoints(t *testing.T) {
	t.Setenv("INGRESS_PUBLIC_URL", "https://edge.example.ngrok.app")

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := svc.CreateAgent(ctx, "fulfill-webhook"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := svc.CreateA2AMethod(ctx, "fulfill_order", "Fulfill order", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if _, err := svc.AddCapability(ctx, "fulfill-webhook", "fulfiller", "fulfill_order"); err != nil {
		t.Fatalf("AddCapability failed: %v", err)
	}

	company, err := svc.CreateCompany(ctx, "Webhook API Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, company.ID, "fulfill-webhook", "fulfiller"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	putBody := map[string]any{
		"provider":      "shopify",
		"event":         "orders/create",
		"event_path":    "orders_create",
		"target_role":   "fulfiller",
		"target_method": "fulfill_order",
		"hmac_secret":   "secret-1",
		"enabled":       true,
	}
	var putBuf bytes.Buffer
	if err := json.NewEncoder(&putBuf).Encode(putBody); err != nil {
		t.Fatalf("encode put body failed: %v", err)
	}
	putReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/webhooks", &putBuf)
	putRec := httptest.NewRecorder()
	h.handleCompany(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected put status %d, got %d body=%s", http.StatusOK, putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/webhooks", nil)
	getRec := httptest.NewRecorder()
	h.handleCompany(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d body=%s", http.StatusOK, getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	key, _ := getResp["webhook_ingress_key"].(string)
	if strings.TrimSpace(key) == "" {
		t.Fatalf("expected webhook ingress key in response")
	}
	webhooks, _ := getResp["webhooks"].([]any)
	if len(webhooks) != 1 {
		t.Fatalf("expected 1 webhook config, got %d", len(webhooks))
	}
	row, _ := webhooks[0].(map[string]any)
	publicURL, _ := row["public_url"].(string)
	if !strings.Contains(publicURL, "/ingress/webhooks/shopify/"+key+"/orders_create") {
		t.Fatalf("unexpected public_url: %q", publicURL)
	}

	pubReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/public-endpoints", nil)
	pubRec := httptest.NewRecorder()
	h.handleCompany(pubRec, pubReq)
	if pubRec.Code != http.StatusOK {
		t.Fatalf("expected public endpoints status %d, got %d body=%s", http.StatusOK, pubRec.Code, pubRec.Body.String())
	}
	var pubResp map[string]any
	if err := json.NewDecoder(pubRec.Body).Decode(&pubResp); err != nil {
		t.Fatalf("decode public endpoints response failed: %v", err)
	}
	if _, ok := pubResp["a2a_callback_url"]; ok {
		t.Fatalf("a2a_callback_url should not be present")
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/webhooks/rotate-key", nil)
	rotateRec := httptest.NewRecorder()
	h.handleCompany(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("expected rotate status %d, got %d body=%s", http.StatusOK, rotateRec.Code, rotateRec.Body.String())
	}
	var rotateResp map[string]any
	if err := json.NewDecoder(rotateRec.Body).Decode(&rotateResp); err != nil {
		t.Fatalf("decode rotate response failed: %v", err)
	}
	newKey, _ := rotateResp["webhook_ingress_key"].(string)
	if strings.TrimSpace(newKey) == "" || newKey == key {
		t.Fatalf("expected rotated webhook key, got old=%q new=%q", key, newKey)
	}

	updatedCompany, err := data.GetCompany(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompany failed: %v", err)
	}
	if strings.TrimSpace(updatedCompany.WebhookIngressKey) != strings.TrimSpace(newKey) {
		t.Fatalf("expected company webhook key %q, got %q", newKey, updatedCompany.WebhookIngressKey)
	}
}
