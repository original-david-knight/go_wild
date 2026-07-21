package cjdropshipping

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/authentication/getAccessToken" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "test-api-key") {
			t.Fatalf("expected api key in body, got: %s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"result":  true,
			"message": "success",
			"data": map[string]any{
				"openId":                 123,
				"accessToken":            "access-token",
				"accessTokenExpiryDate":  "2026-12-01T12:00:00+00:00",
				"refreshToken":           "refresh-token",
				"refreshTokenExpiryDate": "2027-01-01T12:00:00+00:00",
			},
		})
	}))
	defer server.Close()

	c := NewClient("", WithBaseURL(server.URL))
	resp, err := c.GetAccessToken(context.Background(), "test-api-key")
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	if resp.AccessToken != "access-token" {
		t.Fatalf("unexpected access token: %q", resp.AccessToken)
	}
	if resp.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected refresh token: %q", resp.RefreshToken)
	}
}

func TestListProductsV2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/product/listV2" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("CJ-Access-Token"); got != "access-token" {
			t.Fatalf("expected CJ-Access-Token header, got %q", got)
		}
		if got := r.URL.Query().Get("keyWord"); got != "hoodie" {
			t.Fatalf("expected keyWord=hoodie, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"result":  true,
			"message": "success",
			"data": map[string]any{
				"pageNumber":   1,
				"pageSize":     20,
				"totalRecords": 1,
				"totalPages":   1,
				"content": []any{
					map[string]any{
						"productList": []any{
							map[string]any{
								"id":           "123",
								"nameEn":       "Test Hoodie",
								"sellPrice":    "12.34",
								"bigImage":     "https://example.com/image.jpg",
								"supplierName": "CJ",
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	c := NewClient("access-token", WithBaseURL(server.URL))
	resp, err := c.ListProductsV2(context.Background(), ProductListV2Params{KeyWord: "hoodie", Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("ListProductsV2 failed: %v", err)
	}
	if resp.PageNumber.Int() != 1 {
		t.Fatalf("expected page 1, got %d", resp.PageNumber.Int())
	}
	if len(resp.Content) != 1 || len(resp.Content[0].ProductList) != 1 {
		t.Fatalf("unexpected content shape: %#v", resp.Content)
	}
	if got := resp.Content[0].ProductList[0].ID; got != "123" {
		t.Fatalf("unexpected product ID: %q", got)
	}
}

func TestAPIErrorFromBusinessCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    1600300,
			"result":  false,
			"message": "order not found",
			"data":    nil,
		})
	}))
	defer server.Close()

	c := NewClient("access-token", WithBaseURL(server.URL))
	_, err := c.GetOrderDetail(context.Background(), "missing-order", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != 1600300 {
		t.Fatalf("expected code 1600300, got %d", apiErr.Code)
	}
}
