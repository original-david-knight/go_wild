package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

func TestPolymarketTools_GetPriceHistory(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"history": []any{
				map[string]any{"t": float64(1735689600), "p": 0.52},
			},
		})
	}))

	pt := NewPolymarketReadTools(c)
	result, err := pt.PolymarketGetPriceHistoryTool(context.Background(), tools.PolymarketGetPriceHistoryInput{
		TokenID:  "token-1",
		Interval: "1d",
		Fidelity: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if gotPath != "/broker/v1/polymarket/price-history" {
		t.Fatalf("expected /broker/v1/polymarket/price-history, got %s", gotPath)
	}
	if gotBody["token_id"] != "token-1" {
		t.Fatalf("expected token_id token-1, got %v", gotBody["token_id"])
	}
	if gotBody["interval"] != "1d" {
		t.Fatalf("expected interval 1d, got %v", gotBody["interval"])
	}
	if gotBody["fidelity"] != float64(5) {
		t.Fatalf("expected fidelity 5, got %v", gotBody["fidelity"])
	}
}

func TestPolymarketTools_GetPriceHistory_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "history unavailable"})
	}))

	pt := NewPolymarketReadTools(c)
	result, err := pt.PolymarketGetPriceHistoryTool(context.Background(), tools.PolymarketGetPriceHistoryInput{
		TokenID: "token-1",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Fatal("expected tool failure")
	}
}

func TestPolymarketTools_GetCandles(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candles": []any{
				map[string]any{
					"start_ts":     float64(1735689600),
					"end_ts":       float64(1735693200),
					"open":         0.5,
					"high":         0.55,
					"low":          0.49,
					"close":        0.54,
					"sample_count": float64(12),
				},
			},
			"candle_minutes":   float64(60),
			"volume_available": false,
		})
	}))

	pt := NewPolymarketReadTools(c)
	result, err := pt.PolymarketGetCandlesTool(context.Background(), tools.PolymarketGetCandlesInput{
		TokenID:       "token-1",
		CandleMinutes: 60,
		Interval:      "1d",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if gotPath != "/broker/v1/polymarket/candles" {
		t.Fatalf("expected /broker/v1/polymarket/candles, got %s", gotPath)
	}
	if gotBody["token_id"] != "token-1" {
		t.Fatalf("expected token_id token-1, got %v", gotBody["token_id"])
	}
	if gotBody["candle_minutes"] != float64(60) {
		t.Fatalf("expected candle_minutes 60, got %v", gotBody["candle_minutes"])
	}
}

func TestPolymarketTools_OrderBookDepth(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"depth": map[string]any{
				"token_id": "token-1",
				"bids": []any{
					map[string]any{"level": float64(1), "price": 0.72, "size": float64(1000)},
				},
				"asks": []any{
					map[string]any{"level": float64(1), "price": 0.73, "size": float64(1200)},
				},
			},
		})
	}))

	pt := NewPolymarketReadTools(c)
	result, err := pt.PolymarketOrderBookDepthTool(context.Background(), tools.PolymarketOrderBookDepthInput{
		TokenID: "token-1",
		Levels:  2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if gotPath != "/broker/v1/polymarket/orderbook-depth" {
		t.Fatalf("expected /broker/v1/polymarket/orderbook-depth, got %s", gotPath)
	}
	if gotBody["token_id"] != "token-1" {
		t.Fatalf("expected token_id token-1, got %v", gotBody["token_id"])
	}
	if gotBody["levels"] != float64(2) {
		t.Fatalf("expected levels 2, got %v", gotBody["levels"])
	}
}

func TestPolymarketBuyTools_PlaceBuyOrder_UsesFixedBuySide(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))

	pt := NewPolymarketBuyTools(c)
	result, err := pt.PolymarketPlaceBuyOrderTool(context.Background(), tools.PolymarketPlaceBuyOrderInput{
		TokenID:   "token-1",
		Price:     0.42,
		Size:      25,
		OrderType: "GTC",
		NegRisk:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if gotPath != "/broker/v1/polymarket/order" {
		t.Fatalf("expected /broker/v1/polymarket/order, got %s", gotPath)
	}
	if gotBody["side"] != "BUY" {
		t.Fatalf("expected side BUY, got %#v", gotBody["side"])
	}
}

func TestPolymarketSellTools_PlaceSellOrder_UsesFixedSellSide(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))

	pt := NewPolymarketSellTools(c)
	result, err := pt.PolymarketPlaceSellOrderTool(context.Background(), tools.PolymarketPlaceSellOrderInput{
		TokenID:   "token-1",
		Price:     0.58,
		Size:      10,
		OrderType: "GTC",
		NegRisk:   false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if gotPath != "/broker/v1/polymarket/order" {
		t.Fatalf("expected /broker/v1/polymarket/order, got %s", gotPath)
	}
	if gotBody["side"] != "SELL" {
		t.Fatalf("expected side SELL, got %#v", gotBody["side"])
	}
}

func TestPolymarketTools_DescribeTool_IncludesPriceHistory(t *testing.T) {
	pt := NewPolymarketReadTools(nil)
	if pt.DescribeTool("polymarket_get_price_history") == "" {
		t.Fatal("expected non-empty description for polymarket_get_price_history")
	}
	if pt.DescribeTool("polymarket_get_candles") == "" {
		t.Fatal("expected non-empty description for polymarket_get_candles")
	}
	if pt.DescribeTool("polymarket_redeem_winnings") == "" {
		t.Fatal("expected non-empty description for polymarket_redeem_winnings")
	}
	redeemDesc := pt.DescribeTool("polymarket_redeem_winnings")
	if !strings.Contains(redeemDesc, "No input required") {
		t.Fatalf("expected redeem description to mention no-input usage, got: %s", redeemDesc)
	}
	if strings.Contains(redeemDesc, "condition_id") || strings.Contains(redeemDesc, "collateral_token_address") {
		t.Fatalf("expected redeem description to hide all redeem arguments, got: %s", redeemDesc)
	}
	if pt.DescribeTool("polymarket_order_book_depth") == "" {
		t.Fatal("expected non-empty description for polymarket_order_book_depth")
	}
}

func TestPolymarketTools_RedeemWinnings(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"conditions_redeemed": float64(1),
			"transactions": []any{
				map[string]any{
					"condition_id":     "0xabc",
					"transaction_hash": "0x123",
				},
			},
		})
	}))

	pt := NewPolymarketReadTools(c)
	result, err := pt.PolymarketRedeemWinningsTool(context.Background(), tools.PolymarketRedeemWinningsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if gotPath != "/broker/v1/polymarket/redeem" {
		t.Fatalf("expected /broker/v1/polymarket/redeem, got %s", gotPath)
	}
	if len(gotBody) != 0 {
		t.Fatalf("expected empty JSON body for redeem-all tool, got %#v", gotBody)
	}
}
