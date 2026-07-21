package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

func newTestPaywallDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("failed to add tables: %v", err)
	}
	return db
}

func TestPaywallReplayPrevention(t *testing.T) {
	db := newTestPaywallDB(t)
	ctx := context.Background()

	// Create a purchase with a specific tx_hash
	purchase := &data.PaywallPurchase{
		ID:           "pur_test1",
		ProductID:    "prod_test1",
		TxHash:       "0xabc123",
		Chain:        "polygon",
		PayerAddress: "0x1234",
		AmountUSDC:   "4.990000",
	}
	if err := data.CreatePaywallPurchase(ctx, db, purchase); err != nil {
		t.Fatalf("failed to create purchase: %v", err)
	}

	// Try to find the same tx_hash — should find it (replay detected)
	existing, err := data.GetPaywallPurchaseByTxHash(ctx, db, "0xabc123")
	if err != nil {
		t.Fatalf("failed to check tx_hash: %v", err)
	}
	if existing == nil {
		t.Fatal("expected to find existing purchase for replay prevention")
	}
	if existing.TxHash != "0xabc123" {
		t.Errorf("tx_hash = %q, want %q", existing.TxHash, "0xabc123")
	}

	// Different tx_hash should not be found
	other, err := data.GetPaywallPurchaseByTxHash(ctx, db, "0xdef456")
	if err != nil {
		t.Fatalf("failed to check other tx_hash: %v", err)
	}
	if other != nil {
		t.Fatal("expected no purchase for different tx_hash")
	}
}

func TestPaywallDownloadTokenExpiry(t *testing.T) {
	db := newTestPaywallDB(t)
	ctx := context.Background()

	// Create a purchase with an already-expired token
	purchase := &data.PaywallPurchase{
		ID:             "pur_expired",
		ProductID:      "prod_test1",
		TxHash:         "0xexpired",
		Chain:          "polygon",
		PayerAddress:   "0x1234",
		AmountUSDC:     "4.990000",
		DownloadToken:  "expired_token_abc",
		TokenExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := data.CreatePaywallPurchase(ctx, db, purchase); err != nil {
		t.Fatalf("failed to create purchase: %v", err)
	}

	// Look up purchase by token
	found, err := data.GetPaywallPurchaseByToken(ctx, db, "expired_token_abc")
	if err != nil {
		t.Fatalf("failed to get purchase by token: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find purchase by token")
	}

	// Verify the token is expired
	if !time.Now().After(found.TokenExpiresAt) {
		t.Error("expected token to be expired")
	}
}

func TestPaywallInputValidation(t *testing.T) {
	db := newTestPaywallDB(t)
	handler := &BrokerPaywallHandler{db: db}

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing title", `{"file_path":"/data/f.pdf","price_usdc":"1.00","chain":"polygon","wallet_address":"0x1"}`, http.StatusBadRequest},
		{"missing file_path", `{"title":"T","price_usdc":"1.00","chain":"polygon","wallet_address":"0x1"}`, http.StatusBadRequest},
		{"invalid price", `{"title":"T","file_path":"/data/f.pdf","price_usdc":"free","chain":"polygon","wallet_address":"0x1"}`, http.StatusBadRequest},
		{"zero price", `{"title":"T","file_path":"/data/f.pdf","price_usdc":"0","chain":"polygon","wallet_address":"0x1"}`, http.StatusBadRequest},
		{"invalid chain", `{"title":"T","file_path":"/data/f.pdf","price_usdc":"1.00","chain":"ethereum","wallet_address":"0x1"}`, http.StatusBadRequest},
		{"missing wallet", `{"title":"T","file_path":"/data/f.pdf","price_usdc":"1.00","chain":"polygon"}`, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/broker/v1/paywall/create", strings.NewReader(tc.body))
			req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "test-agent"))
			w := httptest.NewRecorder()
			handler.handlePaywallCreate(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}
