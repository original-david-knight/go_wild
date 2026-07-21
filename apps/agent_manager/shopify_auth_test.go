package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func useShopifyTokenHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()
	prev := shopifyTokenHTTPClient
	shopifyTokenHTTPClient = client
	t.Cleanup(func() {
		shopifyTokenHTTPClient = prev
	})
}

func TestNormalizeShopifyShopURL(t *testing.T) {
	got := normalizeShopifyShopURL(" https://My-Store.myshopify.com/admin ")
	if got != "my-store.myshopify.com" {
		t.Fatalf("expected normalized host, got %q", got)
	}
}

func TestExchangeShopifyClientCredentialsToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/admin/oauth/access_token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm failed: %v", err)
		}
		if got := r.FormValue("grant_type"); got != "client_credentials" {
			t.Fatalf("unexpected grant_type: %q", got)
		}
		if got := r.FormValue("client_id"); got != "client-1" {
			t.Fatalf("unexpected client_id: %q", got)
		}
		if got := r.FormValue("client_secret"); got != "secret-1" {
			t.Fatalf("unexpected client_secret: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-abc","expires_in":3600}`))
	}))
	defer server.Close()
	useShopifyTokenHTTPClient(t, server.Client())

	token, expAt, err := exchangeShopifyClientCredentialsToken(context.Background(), server.URL, "client-1", "secret-1")
	if err != nil {
		t.Fatalf("exchangeShopifyClientCredentialsToken failed: %v", err)
	}
	if token != "token-abc" {
		t.Fatalf("expected token token-abc, got %q", token)
	}
	if expAt.IsZero() {
		t.Fatalf("expected non-zero expiry")
	}
}

func TestResolveCompanyShopifyAccessTokenRefreshesAndPersists(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-fresh","expires_in":1200}`))
	}))
	defer server.Close()
	useShopifyTokenHTTPClient(t, server.Client())

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Shop Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.UpsertCompanyShopifyConnection(ctx, db, &data.CompanyShopifyConnection{
		CompanyID:        company.ID,
		ShopURL:          server.URL,
		APIVersion:       "2025-01",
		ClientID:         "client-1",
		ClientSecretEnc:  "secret-1",
		AccessTokenEnc:   "token-old",
		AccessTokenExpAt: time.Now().Add(-1 * time.Minute),
		Enabled:          true,
	}); err != nil {
		t.Fatalf("UpsertCompanyShopifyConnection failed: %v", err)
	}

	conn, err := data.GetCompanyShopifyConnection(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompanyShopifyConnection failed: %v", err)
	}
	token, err := resolveCompanyShopifyAccessToken(ctx, db, conn)
	if err != nil {
		t.Fatalf("resolveCompanyShopifyAccessToken failed: %v", err)
	}
	if token != "token-fresh" {
		t.Fatalf("expected refreshed token, got %q", token)
	}

	updated, err := data.GetCompanyShopifyConnection(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompanyShopifyConnection(updated) failed: %v", err)
	}
	if strings.TrimSpace(updated.AccessTokenEnc) != "token-fresh" {
		t.Fatalf("expected persisted fresh token, got %q", updated.AccessTokenEnc)
	}
	if updated.AccessTokenExpAt.IsZero() {
		t.Fatalf("expected persisted token expiry")
	}
}
