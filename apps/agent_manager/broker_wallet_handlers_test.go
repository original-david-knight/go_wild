package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestWalletHandlers_RequireAgentID(t *testing.T) {
	h := NewBrokerWalletHandler(nil)

	cases := []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{name: "get_address", handle: h.handleGetAddress},
		{name: "get_balance", handle: h.handleGetBalance},
		{name: "get_balances", handle: h.handleGetBalances},
		{name: "sign", handle: h.handleSign},
		{name: "send", handle: h.handleSend},
		{name: "swap", handle: h.handleSwap},
		{name: "contract", handle: h.handleContract},
		{name: "history", handle: h.handleHistory},
		{name: "encrypt", handle: h.handleEncrypt},
		{name: "decrypt", handle: h.handleDecrypt},
		{name: "pubkey", handle: h.handlePubKey},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/broker/v1/wallet/"+tc.name, nil)
			tc.handle(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleGetBalances_PropagatesWalletConfigError(t *testing.T) {
	h := NewBrokerWalletHandler(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/broker/v1/wallet/get_balances", nil)
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))

	h.handleGetBalances(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCompactBalanceSnapshot(t *testing.T) {
	t.Run("call_error", func(t *testing.T) {
		out := compactBalanceSnapshot(nil, errors.New("rpc failed"))
		if ok, _ := out["ok"].(bool); ok {
			t.Fatalf("expected ok=false, got %v", out)
		}
		if got, _ := out["error"].(string); got != "rpc failed" {
			t.Fatalf("error = %q, want %q", got, "rpc failed")
		}
	})

	t.Run("nil_result", func(t *testing.T) {
		out := compactBalanceSnapshot(nil, nil)
		if got, _ := out["error"].(string); got != "empty balance result" {
			t.Fatalf("error = %q, want %q", got, "empty balance result")
		}
	})

	t.Run("tool_error_message", func(t *testing.T) {
		out := compactBalanceSnapshot(&loop.ToolResult{Success: false, Error: "bad balance"}, nil)
		if got, _ := out["error"].(string); got != "bad balance" {
			t.Fatalf("error = %q, want %q", got, "bad balance")
		}
	})

	t.Run("tool_error_default_message", func(t *testing.T) {
		out := compactBalanceSnapshot(&loop.ToolResult{Success: false, Error: "   "}, nil)
		if got, _ := out["error"].(string); got != "balance check failed" {
			t.Fatalf("error = %q, want %q", got, "balance check failed")
		}
	})

	t.Run("unexpected_payload_type", func(t *testing.T) {
		out := compactBalanceSnapshot(&loop.ToolResult{Success: true, Content: "not a map"}, nil)
		if got, _ := out["error"].(string); got != "unexpected balance payload" {
			t.Fatalf("error = %q, want %q", got, "unexpected balance payload")
		}
	})

	t.Run("success_payload", func(t *testing.T) {
		out := compactBalanceSnapshot(&loop.ToolResult{
			Success: true,
			Content: map[string]any{
				"chain":       "ethereum",
				"address":     "0x123",
				"balance":     "4.2",
				"balance_raw": "4200000000000000000",
				"symbol":      "ETH",
				"decimals":    float64(18),
				"token_extra": "ignored",
			},
		}, nil)
		if ok, _ := out["ok"].(bool); !ok {
			t.Fatalf("expected ok=true, got %v", out)
		}
		if got, _ := out["chain"].(string); got != "ethereum" {
			t.Fatalf("chain = %q, want %q", got, "ethereum")
		}
		if _, ok := out["token_extra"]; ok {
			t.Fatalf("unexpected token_extra key in compact output: %v", out)
		}
	})
}
