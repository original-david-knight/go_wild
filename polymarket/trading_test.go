package gowild_polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type rewriteHostTransport struct {
	base *url.URL
	rt   http.RoundTripper
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return t.rt.RoundTrip(req)
}

func TestClient_CancelOrder_SendsOrderIDAndValidatesCanceled(t *testing.T) {
	wantOrderID := "order-123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("expected method DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/order" {
			t.Fatalf("expected path /order, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v; body=%s", err, string(body))
		}
		if payload["orderID"] != wantOrderID {
			t.Fatalf("expected payload.orderID=%q, got %q", wantOrderID, payload["orderID"])
		}
		if _, ok := payload["id"]; ok {
			t.Fatalf("did not expect legacy payload key \"id\"; body=%s", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"canceled":     []string{wantOrderID},
			"not_canceled": map[string]string{},
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	c := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
		address: "0x0000000000000000000000000000000000000000",
		creds: &apiCredentials{
			APIKey:     "test",
			Secret:     "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxvbmchISE=",
			Passphrase: "test",
		},
	}

	if err := c.CancelOrder(context.Background(), wantOrderID); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
}

func TestClient_CancelOrder_ReturnsReasonWhenNotCanceled(t *testing.T) {
	wantOrderID := "order-456"
	wantReason := "order not found"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"canceled": []string{},
			"not_canceled": map[string]string{
				wantOrderID: wantReason,
			},
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	c := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
		address: "0x0000000000000000000000000000000000000000",
		creds: &apiCredentials{
			APIKey:     "test",
			Secret:     "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxvbmchISE=",
			Passphrase: "test",
		},
	}

	err = c.CancelOrder(context.Background(), wantOrderID)
	if err == nil {
		t.Fatal("expected CancelOrder to return error")
	}
	if !strings.Contains(err.Error(), wantReason) {
		t.Fatalf("expected error to contain %q, got %q", wantReason, err.Error())
	}
}

func TestClient_GetOrders_FollowsPaginatedWrappers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/orders" {
			t.Fatalf("expected path /data/orders, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("next_cursor") {
		case "MA==": // initial cursor (base64 "0"), matching py-clob-client
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "order-1", "market": "cond-1", "asset_id": "asset-1"},
				},
				"next_cursor": "cursor-2",
			})
		case "cursor-2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "order-2", "market": "cond-2", "asset_id": "asset-2"},
				},
				"next_cursor": "LTE=", // end cursor (base64 "-1")
			})
		default:
			t.Fatalf("unexpected next_cursor %q", r.URL.Query().Get("next_cursor"))
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	c := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
		address: "0x0000000000000000000000000000000000000000",
		creds: &apiCredentials{
			APIKey:     "test",
			Secret:     "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxvbmchISE=",
			Passphrase: "test",
		},
	}

	orders, err := c.GetOrders(context.Background(), "")
	if err != nil {
		t.Fatalf("GetOrders returned error: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if orders[0].ID != "order-1" || orders[1].ID != "order-2" {
		t.Fatalf("unexpected order IDs: %+v", orders)
	}
}

func TestClient_GetOrders_ThreePages_WithMarketFilter(t *testing.T) {
	var requestLog []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/orders" {
			t.Fatalf("expected path /data/orders, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		cursor := q.Get("next_cursor")
		market := q.Get("market")
		requestLog = append(requestLog, "cursor="+cursor+",market="+market)

		if market != "cond-abc" {
			t.Fatalf("expected market=cond-abc, got %q", market)
		}

		w.Header().Set("Content-Type", "application/json")
		switch cursor {
		case "MA==": // page 1
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "o1", "market": "cond-abc", "asset_id": "a1"},
					{"id": "o2", "market": "cond-abc", "asset_id": "a2"},
				},
				"next_cursor": "MjA=", // base64("20")
			})
		case "MjA=": // page 2
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "o3", "market": "cond-abc", "asset_id": "a3"},
				},
				"next_cursor": "NDA=", // base64("40")
			})
		case "NDA=": // page 3 (last)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "o4", "market": "cond-abc", "asset_id": "a4"},
				},
				"next_cursor": "LTE=", // end cursor
			})
		default:
			t.Fatalf("unexpected next_cursor %q", cursor)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	c := &Client{
		httpClient: &http.Client{
			Transport: &rewriteHostTransport{base: u, rt: http.DefaultTransport},
		},
		address: "0x0000000000000000000000000000000000000000",
		creds: &apiCredentials{
			APIKey:     "test",
			Secret:     "dGVzdC1zZWNyZXQta2V5LTMyLWJ5dGVzLWxvbmchISE=",
			Passphrase: "test",
		},
	}

	orders, err := c.GetOrders(context.Background(), "cond-abc")
	if err != nil {
		t.Fatalf("GetOrders returned error: %v", err)
	}
	if len(orders) != 4 {
		t.Fatalf("expected 4 orders across 3 pages, got %d", len(orders))
	}
	wantIDs := []string{"o1", "o2", "o3", "o4"}
	for i, want := range wantIDs {
		if orders[i].ID != want {
			t.Fatalf("order[%d].ID = %q, want %q", i, orders[i].ID, want)
		}
	}
	if len(requestLog) != 3 {
		t.Fatalf("expected 3 API requests, got %d: %v", len(requestLog), requestLog)
	}
}

func TestPlaceOrder_AutoApproveRetryOnAllowanceError(t *testing.T) {
	key := testKey(t)
	callCount := 0
	allowanceCalls := 0
	refreshCalls := 0

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        privateKeyToAddress(key),
		signatureType: SigTypeEOA,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0.01, nil
		},
		submitOrderFn: func(context.Context, placeOrderRequest) (*PlaceOrderResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("place order failed: API error 400: not enough balance / allowance")
			}
			return &PlaceOrderResponse{Success: true, OrderID: "ord-1"}, nil
		},
		ensureAllowancesFn: func(context.Context) error {
			allowanceCalls++
			return nil
		},
		updateBalanceAllowanceFn: func(context.Context, string, string) error {
			refreshCalls++
			return nil
		},
	}

	resp, err := c.PlaceOrder(context.Background(), "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.51, 10, Buy, GTC, false)
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}
	if resp == nil || !resp.Success || resp.OrderID != "ord-1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 submit attempts, got %d", callCount)
	}
	if allowanceCalls != 1 {
		t.Fatalf("expected 1 allowance setup call, got %d", allowanceCalls)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected 1 balance refresh call, got %d", refreshCalls)
	}
}

func TestPlaceOrder_AutoApproveRetryOnSuccessFalseAllowanceMessage(t *testing.T) {
	key := testKey(t)
	callCount := 0
	allowanceCalls := 0
	refreshCalls := 0

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        privateKeyToAddress(key),
		signatureType: SigTypeEOA,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0.01, nil
		},
		submitOrderFn: func(context.Context, placeOrderRequest) (*PlaceOrderResponse, error) {
			callCount++
			if callCount == 1 {
				return &PlaceOrderResponse{Success: false, ErrorMsg: "not enough balance / allowance"}, nil
			}
			return &PlaceOrderResponse{Success: true, OrderID: "ord-2"}, nil
		},
		ensureAllowancesFn: func(context.Context) error {
			allowanceCalls++
			return nil
		},
		updateBalanceAllowanceFn: func(context.Context, string, string) error {
			refreshCalls++
			return nil
		},
	}

	resp, err := c.PlaceOrder(context.Background(), "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.51, 10, Buy, GTC, false)
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}
	if resp == nil || !resp.Success || resp.OrderID != "ord-2" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 submit attempts, got %d", callCount)
	}
	if allowanceCalls != 1 {
		t.Fatalf("expected 1 allowance setup call, got %d", allowanceCalls)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected 1 balance refresh call, got %d", refreshCalls)
	}
}

func TestPlaceOrder_DoesNotAutoApproveWhenUnsupportedSignerFunderMode(t *testing.T) {
	key := testKey(t)
	callCount := 0
	allowanceCalls := 0

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        "0x1111111111111111111111111111111111111111",
		signatureType: SigTypePolyProxy,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0.01, nil
		},
		submitOrderFn: func(context.Context, placeOrderRequest) (*PlaceOrderResponse, error) {
			callCount++
			return nil, errors.New("place order failed: API error 400: not enough balance / allowance")
		},
		ensureAllowancesFn: func(context.Context) error {
			allowanceCalls++
			return nil
		},
	}

	_, err := c.PlaceOrder(context.Background(), "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.51, 10, Buy, GTC, false)
	if err == nil {
		t.Fatal("expected PlaceOrder error")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 submit attempt, got %d", callCount)
	}
	if allowanceCalls != 0 {
		t.Fatalf("expected 0 allowance setup calls, got %d", allowanceCalls)
	}
}

func TestPlaceOrder_AllowanceSetupFailureIsSurfaced(t *testing.T) {
	key := testKey(t)
	callCount := 0

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        privateKeyToAddress(key),
		signatureType: SigTypeEOA,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0.01, nil
		},
		submitOrderFn: func(context.Context, placeOrderRequest) (*PlaceOrderResponse, error) {
			callCount++
			return nil, errors.New("place order failed: API error 400: not enough balance / allowance")
		},
		ensureAllowancesFn: func(context.Context) error {
			return errors.New("rpc unavailable")
		},
	}

	_, err := c.PlaceOrder(context.Background(), "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.51, 10, Buy, GTC, false)
	if err == nil {
		t.Fatal("expected PlaceOrder error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "allowance") {
		t.Fatalf("expected allowance context in error, got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rpc unavailable") {
		t.Fatalf("expected allowance setup failure context in error, got %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 submit attempt, got %d", callCount)
	}
}

func TestBuildAndPreviewOrder_FailsWhenTickSizeLookupFails(t *testing.T) {
	key := testKey(t)

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        privateKeyToAddress(key),
		signatureType: SigTypeEOA,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0, errors.New("tick size endpoint unavailable")
		},
	}

	req, err := c.buildAndPreviewOrder(context.Background(), "token-1", 0.024, 10, Buy, GTC, false)
	if err == nil {
		t.Fatal("expected buildAndPreviewOrder error")
	}
	if req != nil {
		t.Fatalf("expected nil request, got %+v", req)
	}
	if !strings.Contains(err.Error(), "failed to get tick size") {
		t.Fatalf("expected tick-size context in error, got %v", err)
	}
}

func TestPlaceOrder_FailsWhenTickSizeLookupFails(t *testing.T) {
	key := testKey(t)
	submitCalls := 0

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        privateKeyToAddress(key),
		signatureType: SigTypeEOA,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0, errors.New("tick size endpoint unavailable")
		},
		submitOrderFn: func(context.Context, placeOrderRequest) (*PlaceOrderResponse, error) {
			submitCalls++
			return &PlaceOrderResponse{Success: true, OrderID: "unexpected"}, nil
		},
	}

	resp, err := c.PlaceOrder(context.Background(), "token-1", 0.024, 10, Buy, GTC, false)
	if err == nil {
		t.Fatal("expected PlaceOrder error")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if submitCalls != 0 {
		t.Fatalf("expected 0 submit attempts, got %d", submitCalls)
	}
	if !strings.Contains(err.Error(), "failed to get tick size") {
		t.Fatalf("expected tick-size context in error, got %v", err)
	}
}

func TestPlaceOrder_GTDUsesFutureExpiration(t *testing.T) {
	key := testKey(t)
	var captured placeOrderRequest

	c := &Client{
		privateKey:    key,
		address:       privateKeyToAddress(key),
		funder:        privateKeyToAddress(key),
		signatureType: SigTypeEOA,
		chainID:       polygonChainID,
		creds:         &apiCredentials{APIKey: "k", Secret: "s", Passphrase: "p"},
		getNegRiskFn: func(context.Context, string) (bool, error) {
			return false, nil
		},
		getTickSizeFn: func(context.Context, string) (float64, error) {
			return 0.01, nil
		},
		submitOrderFn: func(_ context.Context, req placeOrderRequest) (*PlaceOrderResponse, error) {
			captured = req
			return &PlaceOrderResponse{Success: true, OrderID: "ord-gtd"}, nil
		},
	}

	before := time.Now().UTC().Add(time.Minute).Unix()
	resp, err := c.PlaceOrder(context.Background(), "52114319501245915516055106046884209969926127482827954674443846427166160274944", 0.51, 10, Buy, GTD, false)
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if captured.OrderType != GTD {
		t.Fatalf("expected GTD order type, got %q", captured.OrderType)
	}
	if captured.Order == nil {
		t.Fatal("expected signed order in request")
	}
	expirationUnix, err := strconv.ParseInt(strings.TrimSpace(captured.Order.Expiration), 10, 64)
	if err != nil {
		t.Fatalf("parse expiration: %v", err)
	}
	if expirationUnix <= before {
		t.Fatalf("expected GTD expiration after %d, got %d", before, expirationUnix)
	}
}
