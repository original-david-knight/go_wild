package gowild_polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClient_GetPrice_UsesShortTTLCache(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/price" {
			t.Fatalf("expected path /price, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"price": "0.42"})
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: base, rt: http.DefaultTransport},
		},
		publicClient: &http.Client{
			Transport: &rewriteHostTransport{base: base, rt: http.DefaultTransport},
		},
	}

	price1, err := client.GetPrice(context.Background(), "token-1", "buy")
	if err != nil {
		t.Fatalf("first GetPrice returned error: %v", err)
	}
	price2, err := client.GetPrice(context.Background(), "token-1", "buy")
	if err != nil {
		t.Fatalf("second GetPrice returned error: %v", err)
	}
	if price1 != "0.42" || price2 != "0.42" {
		t.Fatalf("unexpected prices %q %q", price1, price2)
	}
	if requests != 1 {
		t.Fatalf("expected one network request, got %d", requests)
	}
}

func TestClient_GetTickSize_ParsesNumericMinimumTickSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tick-size" {
			t.Fatalf("expected path /tick-size, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("token_id"); got != "token-1" {
			t.Fatalf("expected token_id=token-1, got %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"minimum_tick_size": 0.001,
		})
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: base, rt: http.DefaultTransport},
		},
	}

	tickSize, err := client.getTickSize(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("getTickSize returned error: %v", err)
	}
	if tickSize != 0.001 {
		t.Fatalf("expected tick size 0.001, got %g", tickSize)
	}
}

func TestClient_GetTickSize_ReturnsErrorOnBadResponses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantText string
	}{
		{
			name:     "server error",
			status:   http.StatusInternalServerError,
			body:     `{"error":"temporary failure"}`,
			wantText: "get tick size failed",
		},
		{
			name:     "malformed payload",
			status:   http.StatusOK,
			body:     `{"minimum_tick_size":"nope"}`,
			wantText: "failed to decode tick size",
		},
		{
			name:     "missing tick size",
			status:   http.StatusOK,
			body:     `{}`,
			wantText: "invalid minimum_tick_size",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			base, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("parse server URL: %v", err)
			}

			client := &Client{
				httpClient: &http.Client{
					Transport: &rewriteHostTransport{base: base, rt: http.DefaultTransport},
				},
			}

			_, err = client.getTickSize(context.Background(), "token-1")
			if err == nil {
				t.Fatal("expected getTickSize to fail")
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("expected error containing %q, got %q", tc.wantText, err.Error())
			}
		})
	}
}

func TestClient_GetTickSizeAndNegRisk_UseTokenMetadataCache(t *testing.T) {
	tickRequests := 0
	negRiskRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tick-size":
			tickRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"minimum_tick_size": 0.01})
		case "/neg-risk":
			negRiskRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"neg_risk": true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: base, rt: http.DefaultTransport},
		},
		publicClient: &http.Client{
			Transport: &rewriteHostTransport{base: base, rt: http.DefaultTransport},
		},
	}

	tick1, err := client.getTickSize(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("first getTickSize returned error: %v", err)
	}
	tick2, err := client.getTickSize(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("second getTickSize returned error: %v", err)
	}
	if tick1 != 0.01 || tick2 != 0.01 {
		t.Fatalf("unexpected tick sizes %g %g", tick1, tick2)
	}

	neg1, err := client.getNegRisk(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("first getNegRisk returned error: %v", err)
	}
	neg2, err := client.getNegRisk(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("second getNegRisk returned error: %v", err)
	}
	if !neg1 || !neg2 {
		t.Fatalf("expected cached negRisk true, got %v %v", neg1, neg2)
	}
	if tickRequests != 1 {
		t.Fatalf("expected one tick-size request, got %d", tickRequests)
	}
	if negRiskRequests != 1 {
		t.Fatalf("expected one neg-risk request, got %d", negRiskRequests)
	}
}
