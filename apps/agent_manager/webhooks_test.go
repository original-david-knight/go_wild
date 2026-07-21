package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestHandleIngressWebhookMethodGuard(t *testing.T) {
	wr := NewWebhookRouter(nil)
	req := httptest.NewRequest(http.MethodGet, "/ingress/webhooks/shopify/company-key/orders_create", nil)
	rec := httptest.NewRecorder()

	wr.HandleIngressWebhook(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d, got %d body=%s", http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "POST required") {
		t.Fatalf("expected POST required error, got %s", rec.Body.String())
	}
}

func TestHandleIngressWebhookPathValidation(t *testing.T) {
	wr := NewWebhookRouter(nil)

	shortReq := httptest.NewRequest(http.MethodPost, "/ingress/webhooks/shopify/company-key", nil)
	shortRec := httptest.NewRecorder()
	wr.HandleIngressWebhook(shortRec, shortReq)
	if shortRec.Code != http.StatusBadRequest {
		t.Fatalf("expected short path status %d, got %d body=%s", http.StatusBadRequest, shortRec.Code, shortRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodPost, "/ingress/webhooks//company-key/orders_create", nil)
	missingRec := httptest.NewRecorder()
	wr.HandleIngressWebhook(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing-field status %d, got %d body=%s", http.StatusBadRequest, missingRec.Code, missingRec.Body.String())
	}
}

func TestHandleIngressWebhookUnsupportedProvider(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	svc := NewAgentService(db)
	company, err := svc.CreateCompany(ctx, "Unsupported Provider Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if strings.TrimSpace(company.WebhookIngressKey) == "" {
		t.Fatalf("expected company ingress key")
	}

	cfg := &data.WebhookConfig{
		ID:        "cfg-unsupported-provider",
		CompanyID: company.ID,
		Source:    "stripe",
		EventPath: "invoice_paid",
		Enabled:   true,
	}
	if err := db.Table(data.WebhookConfig{}).Insert(ctx, cfg); err != nil {
		t.Fatalf("insert webhook config failed: %v", err)
	}

	wr := NewWebhookRouter(db)
	req := httptest.NewRequest(
		http.MethodPost,
		"/ingress/webhooks/stripe/"+company.WebhookIngressKey+"/invoice_paid",
		nil,
	)
	rec := httptest.NewRecorder()
	wr.HandleIngressWebhook(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported provider") {
		t.Fatalf("expected unsupported provider error, got %s", rec.Body.String())
	}
}

func TestParseIngressWebhookRoute(t *testing.T) {
	route, err := parseIngressWebhookRoute("/ingress/webhooks/shopify/company-key/orders_create")
	if err != nil {
		t.Fatalf("expected parse success, got %v", err)
	}
	if route.provider != "shopify" || route.companyKey != "company-key" || route.eventPath != "orders_create" {
		t.Fatalf("unexpected route parse result: %#v", route)
	}

	if _, err := parseIngressWebhookRoute("/ingress/webhooks/shopify/company-key"); err == nil {
		t.Fatalf("expected short ingress path to fail")
	}
	if _, err := parseIngressWebhookRoute("/ingress/webhooks//company-key/orders_create"); err == nil {
		t.Fatalf("expected missing provider to fail")
	}
}

func TestWebhookProviderRecognition(t *testing.T) {
	if !isWebhookProvider("shopify") {
		t.Fatalf("expected shopify provider to be recognized")
	}
	if isWebhookProvider("stripe") {
		t.Fatalf("expected unsupported provider to be rejected")
	}
}
