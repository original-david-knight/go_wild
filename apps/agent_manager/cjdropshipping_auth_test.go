package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestResolveCompanyCJDropshippingAccessTokenUsesCachedToken(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	svc := NewAgentService(db)

	company, err := svc.CreateCompany(ctx, "CJ Cached Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = data.UpsertCompanyCJDropshippingConnection(ctx, db, &data.CompanyCJDropshippingConnection{
		CompanyID:        company.ID,
		APIKeyEnc:        "cj-api-key",
		AccessTokenEnc:   "cached-token",
		AccessTokenExpAt: time.Now().Add(2 * time.Hour),
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyCJDropshippingConnection failed: %v", err)
	}

	conn, err := data.GetCompanyCJDropshippingConnection(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompanyCJDropshippingConnection failed: %v", err)
	}

	token, err := resolveCompanyCJDropshippingAccessToken(ctx, db, conn)
	if err != nil {
		t.Fatalf("resolveCompanyCJDropshippingAccessToken failed: %v", err)
	}
	if token != "cached-token" {
		t.Fatalf("expected cached token, got %q", token)
	}
}

func TestResolveCompanyCJDropshippingAccessTokenRefreshesAndPersists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2.0/v1/authentication/refreshAccessToken" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if got, _ := payload["refreshToken"].(string); got != "refresh-old" {
			t.Fatalf("unexpected refresh token payload: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"result": true,
			"message": "ok",
			"data": {
				"accessToken": "access-new",
				"accessTokenExpiryDate": "2030-01-02 03:04:05",
				"refreshToken": "refresh-new",
				"refreshTokenExpiryDate": "2030-07-02 03:04:05"
			}
		}`))
	}))
	defer server.Close()
	t.Setenv("CJDROPSHIPPING_BASE_URL", strings.TrimRight(server.URL, "/")+"/api2.0/v1")

	db := setupManagerTestDB(t)
	ctx := context.Background()
	svc := NewAgentService(db)

	company, err := svc.CreateCompany(ctx, "CJ Refresh Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = data.UpsertCompanyCJDropshippingConnection(ctx, db, &data.CompanyCJDropshippingConnection{
		CompanyID:         company.ID,
		APIKeyEnc:         "cj-api-key",
		AccessTokenEnc:    "access-old",
		AccessTokenExpAt:  time.Now().Add(-1 * time.Hour),
		RefreshTokenEnc:   "refresh-old",
		RefreshTokenExpAt: time.Now().Add(24 * time.Hour),
		Enabled:           true,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyCJDropshippingConnection failed: %v", err)
	}

	conn, err := data.GetCompanyCJDropshippingConnection(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompanyCJDropshippingConnection failed: %v", err)
	}

	token, err := resolveCompanyCJDropshippingAccessToken(ctx, db, conn)
	if err != nil {
		t.Fatalf("resolveCompanyCJDropshippingAccessToken failed: %v", err)
	}
	if token != "access-new" {
		t.Fatalf("expected refreshed token, got %q", token)
	}

	updated, err := data.GetCompanyCJDropshippingConnection(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompanyCJDropshippingConnection(updated) failed: %v", err)
	}
	if strings.TrimSpace(updated.AccessTokenEnc) != "access-new" {
		t.Fatalf("expected persisted access token, got %q", updated.AccessTokenEnc)
	}
	if strings.TrimSpace(updated.RefreshTokenEnc) != "refresh-new" {
		t.Fatalf("expected persisted refresh token, got %q", updated.RefreshTokenEnc)
	}
	if updated.AccessTokenExpAt.IsZero() {
		t.Fatalf("expected persisted access token expiry")
	}
}
