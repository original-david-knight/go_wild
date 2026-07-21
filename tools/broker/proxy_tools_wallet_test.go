package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

// --- WalletTools ---

func TestWalletTools_GetWalletAddress(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"address": "0xabc123"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.GetWalletAddressTool(context.Background(), tools.GetWalletAddressInput{Chain: "ethereum"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/wallet/address" {
		t.Errorf("expected wallet/address path, got %s", gotPath)
	}
}

func TestWalletTools_GetBalances(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{
			"solana":        map[string]any{"ok": true, "balance": "1.0", "symbol": "SOL"},
			"eth":           map[string]any{"ok": true, "balance": "0.5", "symbol": "ETH"},
			"polygon":       map[string]any{"ok": true, "balance": "10.0", "symbol": "POL"},
			"polygon_usdte": map[string]any{"ok": true, "balance": "25.0", "symbol": "USDT"},
		})
	}))

	wt := NewWalletTools(c)
	result, err := wt.GetBalancesTool(context.Background(), tools.GetBalancesInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/wallet/balances" {
		t.Errorf("expected wallet/balances path, got %s", gotPath)
	}
}

func TestWalletTools_SignMessage(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"signature": "0xsig"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.SignMessageTool(context.Background(), tools.SignMessageInput{
		Chain: "ethereum", Message: "Hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_SendToken(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tx_hash": "0xtx"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.SendTokenTool(context.Background(), tools.SendTokenInput{
		Chain: "ethereum", To: "0xrecipient", Amount: "1.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_SwapToken(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tx_hash": "0xswap"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.SwapTokenTool(context.Background(), tools.SwapTokenInput{
		Chain: "solana", FromToken: "SOL", ToToken: "USDC", Amount: "10",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_ContractCall(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": "0x01"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.ContractCallTool(context.Background(), tools.ContractCallInput{
		Chain: "ethereum", ContractAddress: "0xcontract", Method: "balanceOf",
		Args: []any{"0xowner"}, ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_GetTransactionHistory(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"transactions": []any{}})
	}))

	wt := NewWalletTools(c)
	result, err := wt.GetTransactionHistoryTool(context.Background(), tools.GetTransactionHistoryInput{
		Limit: 10, Chain: "ethereum",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_EncryptMessage(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ciphertext": "enc", "nonce": "n1"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.EncryptMessageTool(context.Background(), tools.EncryptMessageInput{
		Plaintext: "secret", RecipientPublicKey: "pubkey123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_DecryptMessage(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"plaintext": "secret"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.DecryptMessageTool(context.Background(), tools.DecryptMessageInput{
		Ciphertext: "enc", Nonce: "n1", SenderPublicKey: "pubkey456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_GetEd25519PublicKey(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"public_key": "ed25519pubkey"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.GetEd25519PublicKeyTool(context.Background(), tools.GetEd25519PublicKeyInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestWalletTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "wallet not configured"})
	}))

	wt := NewWalletTools(c)
	result, err := wt.GetBalancesTool(context.Background(), tools.GetBalancesInput{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestWalletTools_DescribeTool(t *testing.T) {
	wt := NewWalletTools(nil)
	for _, name := range []string{
		"get_wallet_address", "get_balances", "sign_message", "send_token",
		"swap_token", "contract_call", "get_transaction_history",
		"encrypt_message", "decrypt_message", "get_ed25519_public_key",
	} {
		if wt.DescribeTool(name) == "" {
			t.Errorf("expected non-empty description for %s", name)
		}
	}
}
