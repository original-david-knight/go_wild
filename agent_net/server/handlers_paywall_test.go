package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func setupPaywallTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, db := setupTestServer(t)

	// Register paywall tables (normally registered via agent_data init)
	if err := db.AddTable(data.PaywallProduct{}); err != nil {
		t.Fatalf("Failed to add PaywallProduct table: %v", err)
	}
	if err := db.AddTable(data.PaywallPurchase{}); err != nil {
		t.Fatalf("Failed to add PaywallPurchase table: %v", err)
	}

	// Use temp dir for file storage
	storageDir := t.TempDir()
	srv.paywall.storageDir = storageDir

	return srv, storageDir
}

func makeMultipartPaywallRequest(t *testing.T, makeReq func(string, string, []byte) *http.Request, fields map[string]string, fileName string, fileContent []byte) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("failed to write field %s: %v", k, err)
		}
	}

	if fileName != "" {
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatalf("failed to create file part: %v", err)
		}
		part.Write(fileContent)
	}
	writer.Close()

	body := buf.Bytes()
	req := makeReq("POST", "/api/v1/paywall/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestPaywallCreateSuccess(t *testing.T) {
	srv, storageDir := setupPaywallTestServer(t)

	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	req := makeMultipartPaywallRequest(t, makeReq, map[string]string{
		"title":          "Test Ebook",
		"description":    "A great book",
		"price_usdc":     "4.99",
		"chain":          "polygon",
		"wallet_address": "0x1234567890abcdef",
	}, "ebook.pdf", []byte("fake pdf content"))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	productID, ok := resp["product_id"].(string)
	if !ok || productID == "" {
		t.Fatal("expected product_id in response")
	}
	checkoutURL, _ := resp["checkout_url"].(string)
	if !strings.HasSuffix(checkoutURL, "/paywall/"+productID) {
		t.Errorf("checkout_url = %v, want suffix /paywall/%s", checkoutURL, productID)
	}
	if resp["file_name"] != "ebook.pdf" {
		t.Errorf("file_name = %v, want ebook.pdf", resp["file_name"])
	}

	// Verify file was written to storage
	storagePath := filepath.Join(storageDir, productID, "ebook.pdf")
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Errorf("file not written to storage: %s", storagePath)
	}
}

func TestPaywallCreateValidation(t *testing.T) {
	srv, _ := setupPaywallTestServer(t)
	handler := srv.handler()
	_, makeReq := makePremiumAgent(t, srv)

	tests := []struct {
		name     string
		fields   map[string]string
		wantCode int
	}{
		{
			"missing title",
			map[string]string{"price_usdc": "1.00", "chain": "polygon", "wallet_address": "0x1"},
			http.StatusBadRequest,
		},
		{
			"missing price",
			map[string]string{"title": "T", "chain": "polygon", "wallet_address": "0x1"},
			http.StatusBadRequest,
		},
		{
			"invalid chain",
			map[string]string{"title": "T", "price_usdc": "1.00", "chain": "ethereum", "wallet_address": "0x1"},
			http.StatusBadRequest,
		},
		{
			"missing wallet",
			map[string]string{"title": "T", "price_usdc": "1.00", "chain": "polygon"},
			http.StatusBadRequest,
		},
		{
			"zero price",
			map[string]string{"title": "T", "price_usdc": "0", "chain": "polygon", "wallet_address": "0x1"},
			http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := makeMultipartPaywallRequest(t, makeReq, tc.fields, "test.pdf", []byte("data"))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestPaywallCreateRequiresPremium(t *testing.T) {
	srv, _ := setupPaywallTestServer(t)
	handler := srv.handler()

	// Use a non-premium agent
	body := []byte("dummy")
	req, _, _ := makeSignedRequest(t, "POST", "/api/v1/paywall/create", body)
	req.Header.Set("Content-Type", "multipart/form-data")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should be rejected by PremiumOnlyMiddleware (402)
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPaywallVerifyRequiresBuyerSignature(t *testing.T) {
	srv, _ := setupPaywallTestServer(t)
	handler := srv.handler()

	req := httptest.NewRequest(http.MethodPost, "/paywall/prod_test/verify", strings.NewReader(`{"tx_hash":"0xabc","buyer_address":"0x1234"}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "buyer_signature is required") {
		t.Fatalf("expected missing buyer_signature error, got: %s", w.Body.String())
	}
}
