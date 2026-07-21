package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlePublicEndpoints(t *testing.T) {
	t.Setenv("INGRESS_PUBLIC_URL", "https://edge.example.ngrok.app")

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/public-endpoints", nil)
	rec := httptest.NewRecorder()
	h.handlePublicEndpoints(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if got, _ := resp["ingress_public_url"].(string); got != "https://edge.example.ngrok.app" {
		t.Fatalf("unexpected ingress_public_url: %q", got)
	}
	templates, _ := resp["templates"].(map[string]any)
	if _, ok := templates["a2a_callback"]; ok {
		t.Fatalf("a2a_callback template should not be present")
	}
	webhookTemplate, _ := templates["webhook"].(string)
	if webhookTemplate == "" {
		t.Fatalf("expected webhook template to be present")
	}
}
