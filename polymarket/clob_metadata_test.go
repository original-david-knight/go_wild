package gowild_polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// clobMetaServer wires a test client to an httptest server handling /book and
// /markets/{condition_id}, and counts hits to the clob-markets endpoint.
func clobMetaServer(t *testing.T, handler http.HandlerFunc) (*Client, *int32) {
	t.Helper()
	var clobMarketCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= len("/markets/") && r.URL.Path[:len("/markets/")] == "/markets/" {
			atomic.AddInt32(&clobMarketCalls, 1)
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	c := &Client{
		httpClient:   &http.Client{Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport}},
		publicClient: &http.Client{Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport}},
	}
	return c, &clobMarketCalls
}

func TestGetClobMarket_DecodesMetadata(t *testing.T) {
	c, _ := clobMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets/0xcond" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"condition_id":       "0xcond",
			"question":           "Will X happen?",
			"neg_risk":           true,
			"neg_risk_market_id": "0xnrm",
			"mos":                5.0,
			"minimum_tick_size":  "0.01",
			"tokens": []map[string]any{
				{"token_id": "1001", "outcome": "Yes"},
				{"token_id": "1002", "outcome": "No"},
			},
		})
	})

	m, err := c.GetClobMarket(context.Background(), "0xcond")
	if err != nil {
		t.Fatalf("GetClobMarket: %v", err)
	}
	if m.MinOrderSize() != 5.0 {
		t.Errorf("MinOrderSize() = %v, want 5.0 (from mos)", m.MinOrderSize())
	}
	if m.TickSize() != 0.01 {
		t.Errorf("TickSize() = %v, want 0.01", m.TickSize())
	}
	if !m.NegRisk || m.NegRiskMarketID != "0xnrm" {
		t.Errorf("neg-risk = %v / %q, want true / 0xnrm", m.NegRisk, m.NegRiskMarketID)
	}
	if len(m.Tokens) != 2 || m.Tokens[0].Outcome != "Yes" || m.Tokens[1].TokenID != "1002" {
		t.Errorf("tokens decoded incorrectly: %+v", m.Tokens)
	}
}

func TestGetOrderBookDetailed_DecodesMinOrderSize(t *testing.T) {
	c, _ := clobMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/book" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"asset_id":       "1002",
			"min_order_size": "15",
			"tick_size":      "0.01",
			"neg_risk":       false,
			"bids":           []map[string]string{{"price": "0.90", "size": "100"}},
			"asks":           []map[string]string{{"price": "0.92", "size": "120"}},
		})
	})

	book, err := c.GetOrderBookDetailed(context.Background(), "1002")
	if err != nil {
		t.Fatalf("GetOrderBookDetailed: %v", err)
	}
	if float64(book.MinOrderSize) != 15 {
		t.Errorf("MinOrderSize = %v, want 15", float64(book.MinOrderSize))
	}
	if len(book.Bids) != 1 || len(book.Asks) != 1 {
		t.Errorf("book bids/asks not decoded: %+v", book)
	}
}

func TestResolveMinOrderSize_PrefersBookSkipsClobMarkets(t *testing.T) {
	c, clobCalls := clobMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/book" {
			_ = json.NewEncoder(w).Encode(map[string]any{"asset_id": "1002", "min_order_size": 20.0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"condition_id": "0xcond", "mos": 5.0})
	})

	size, source, err := c.ResolveMinOrderSize(context.Background(), "1002", "0xcond")
	if err != nil {
		t.Fatalf("ResolveMinOrderSize: %v", err)
	}
	if size != 20 || source != MinOrderSizeSourceOrderBook {
		t.Errorf("got (%v, %q), want (20, order_book)", size, source)
	}
	if got := atomic.LoadInt32(clobCalls); got != 0 {
		t.Errorf("clob-markets was called %d times; must be skipped when book has min size", got)
	}
}

func TestResolveMinOrderSize_FallsBackToClobMos(t *testing.T) {
	c, clobCalls := clobMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/book" {
			// min_order_size absent / zero -> must fall back.
			_ = json.NewEncoder(w).Encode(map[string]any{"asset_id": "1002", "min_order_size": 0})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"condition_id": "0xcond", "mos": 5.0})
	})

	size, source, err := c.ResolveMinOrderSize(context.Background(), "1002", "0xcond")
	if err != nil {
		t.Fatalf("ResolveMinOrderSize: %v", err)
	}
	if size != 5 || source != MinOrderSizeSourceClobMos {
		t.Errorf("got (%v, %q), want (5, clob_markets_mos)", size, source)
	}
	if got := atomic.LoadInt32(clobCalls); got != 1 {
		t.Errorf("clob-markets call count = %d, want 1", got)
	}
}

func TestResolveMinOrderSize_UndeterminableSignal(t *testing.T) {
	c, _ := clobMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/book" {
			_ = json.NewEncoder(w).Encode(map[string]any{"asset_id": "1002"})
			return
		}
		// clob market also lacks any usable minimum.
		_ = json.NewEncoder(w).Encode(map[string]any{"condition_id": "0xcond"})
	})

	size, source, err := c.ResolveMinOrderSize(context.Background(), "1002", "0xcond")
	if err != nil {
		t.Fatalf("ResolveMinOrderSize: %v", err)
	}
	if size != 0 || source != "" {
		t.Errorf("got (%v, %q), want (0, \"\") undeterminable signal — caller must fail closed", size, source)
	}
}
