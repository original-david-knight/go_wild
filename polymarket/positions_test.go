package gowild_polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestClient_GetPositions_UsesAccountAddressAndPaginates(t *testing.T) {
	const (
		signer = "0x2222222222222222222222222222222222222222"
		funder = "0x1111111111111111111111111111111111111111"
	)

	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/positions" {
			t.Fatalf("expected path /positions, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("user"); got != funder {
			t.Fatalf("user query = %q, want funder %q", got, funder)
		}
		if got := r.URL.Query().Get("limit"); got != "500" {
			t.Fatalf("limit query = %q, want 500", got)
		}

		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		var positions []Position
		switch offset {
		case "0":
			positions = make([]Position, 500)
			for i := range positions {
				positions[i] = Position{Asset: fmt.Sprintf("asset-%03d", i), Size: 1}
			}
		case "500":
			positions = []Position{{Asset: "asset-500", Size: 1}}
		default:
			t.Fatalf("unexpected offset %q", offset)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(positions)
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	c := &Client{
		publicClient: &http.Client{Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport}},
		address:      signer,
		funder:       funder,
	}

	positions, err := c.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 501 {
		t.Fatalf("positions = %d, want 501", len(positions))
	}
	if positions[500].Asset != "asset-500" {
		t.Fatalf("last asset = %q, want asset-500", positions[500].Asset)
	}
	if fmt.Sprint(offsets) != "[0 500]" {
		t.Fatalf("offsets = %v, want [0 500]", offsets)
	}
}

func TestClient_GetPositionsValue_UsesAccountAddress(t *testing.T) {
	const (
		signer = "0x2222222222222222222222222222222222222222"
		funder = "0x1111111111111111111111111111111111111111"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/value" {
			t.Fatalf("expected path /value, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("user"); got != funder {
			t.Fatalf("user query = %q, want funder %q", got, funder)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"user": funder, "value": strconv.FormatFloat(123.45, 'f', 2, 64)},
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	c := &Client{
		publicClient: &http.Client{Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport}},
		address:      signer,
		funder:       funder,
	}

	value, err := c.GetPositionsValue(context.Background())
	if err != nil {
		t.Fatalf("GetPositionsValue: %v", err)
	}
	if value != 123.45 {
		t.Fatalf("value = %v, want 123.45", value)
	}
}
