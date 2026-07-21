package data

import (
	"context"
	"testing"
)

func TestWalletTransactions(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Log a transaction
	tx := &WalletTransaction{
		Chain:           "ethereum",
		Type:            "send_token",
		FromAddress:     "0xabc",
		ToAddress:       "0xdef",
		Amount:          "1.5",
		TransactionHash: "0xhash123",
		Status:          "pending",
	}
	if err := svc.LogWalletTransaction(ctx, tx); err != nil {
		t.Fatalf("LogWalletTransaction failed: %v", err)
	}

	// Get transactions
	txs, err := svc.GetWalletTransactions(ctx, 10)
	if err != nil {
		t.Fatalf("GetWalletTransactions failed: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txs))
	}
	if txs[0].Chain != "ethereum" {
		t.Errorf("unexpected chain: %q", txs[0].Chain)
	}

	// Get by hash
	got, err := svc.GetWalletTransactionByHash(ctx, "0xhash123")
	if err != nil {
		t.Fatalf("GetWalletTransactionByHash failed: %v", err)
	}
	if got.Amount != "1.5" {
		t.Errorf("unexpected amount: %q", got.Amount)
	}

	// Get by non-existent hash
	got, _ = svc.GetWalletTransactionByHash(ctx, "0xnonexistent")
	if got != nil {
		t.Error("expected nil for non-existent hash")
	}

	// Update status
	if err := svc.UpdateWalletTransactionStatus(ctx, "0xhash123", "confirmed", ""); err != nil {
		t.Fatalf("UpdateWalletTransactionStatus failed: %v", err)
	}

	got, _ = svc.GetWalletTransactionByHash(ctx, "0xhash123")
	if got.Status != "confirmed" {
		t.Errorf("expected confirmed, got %q", got.Status)
	}

	// Update non-existent
	err = svc.UpdateWalletTransactionStatus(ctx, "0xnonexistent", "failed", "error")
	if err == nil {
		t.Error("expected error updating non-existent transaction")
	}

	// Default limit
	txs, _ = svc.GetWalletTransactions(ctx, 0)
	if len(txs) != 1 {
		t.Errorf("expected 1 with default limit, got %d", len(txs))
	}
}
