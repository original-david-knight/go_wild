package gowild_polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetPublic(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Query().Get("token_id") != "test-token" {
			t.Errorf("expected token_id=test-token, got %s", r.URL.Query().Get("token_id"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"price": "0.55"})
	}))
	defer server.Close()

	key := testKey(t)
	c := &Client{
		httpClient:   server.Client(),
		publicClient: server.Client(),
		privateKey:   key,
		address:      privateKeyToAddress(key),
		chainID:      137,
		creds: &apiCredentials{
			APIKey:     "test",
			Secret:     "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxvbmchISE=", // base64
			Passphrase: "test",
		},
	}

	data, err := c.getPublic(context.Background(), server.URL, "/price", map[string][]string{
		"token_id": {"test-token"},
	})
	if err != nil {
		t.Fatalf("getPublic failed: %v", err)
	}

	var result struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if result.Price != "0.55" {
		t.Errorf("expected price 0.55, got %s", result.Price)
	}
}

func TestClient_DoRequest_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request"}`))
	}))
	defer server.Close()

	key := testKey(t)
	c := &Client{
		httpClient: server.Client(),
		privateKey: key,
		address:    privateKeyToAddress(key),
		chainID:    137,
	}

	req, _ := http.NewRequest("GET", server.URL+"/test", nil)
	_, err := c.doRequest(req)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}

	apiErr, ok := err.(*apiError)
	if !ok {
		t.Fatalf("expected *apiError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", apiErr.StatusCode)
	}
}

func TestClient_PostAuthenticated_SetsHeaders(t *testing.T) {
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	key := testKey(t)
	addr := privateKeyToAddress(key)
	c := &Client{
		httpClient: server.Client(),
		privateKey: key,
		address:    addr,
		chainID:    137,
		creds: &apiCredentials{
			APIKey:     "my-api-key",
			Secret:     "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxvbmchISE=",
			Passphrase: "my-passphrase",
		},
	}

	// Override the clobBaseURL to use the test server
	body := map[string]string{"test": "data"}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL+"/order", nil)
	req.Header.Set("Content-Type", "application/json")
	_ = c.signRequest(req, string(data))

	// Manually do the request to the test server
	resp, err := c.httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Verify auth headers were set
	if capturedHeaders.Get("POLY_ADDRESS") != addr {
		t.Errorf("expected POLY_ADDRESS %s, got %s", addr, capturedHeaders.Get("POLY_ADDRESS"))
	}
	if capturedHeaders.Get("POLY_API_KEY") != "my-api-key" {
		t.Errorf("expected POLY_API_KEY my-api-key, got %s", capturedHeaders.Get("POLY_API_KEY"))
	}
	if capturedHeaders.Get("POLY_PASSPHRASE") != "my-passphrase" {
		t.Errorf("expected POLY_PASSPHRASE my-passphrase, got %s", capturedHeaders.Get("POLY_PASSPHRASE"))
	}
	if capturedHeaders.Get("POLY_SIGNATURE") == "" {
		t.Error("expected POLY_SIGNATURE to be set")
	}
	if capturedHeaders.Get("POLY_TIMESTAMP") == "" {
		t.Error("expected POLY_TIMESTAMP to be set")
	}
}

func TestAPIError(t *testing.T) {
	err := &apiError{StatusCode: 429, Message: "rate limited"}
	if err.Error() != "rate limited" {
		t.Errorf("expected 'rate limited', got '%s'", err.Error())
	}
}

func TestWithChainID(t *testing.T) {
	c := &Client{chainID: 137}
	WithChainID(80001)(c)
	if c.chainID != 80001 {
		t.Errorf("expected chainID 80001, got %d", c.chainID)
	}
}
