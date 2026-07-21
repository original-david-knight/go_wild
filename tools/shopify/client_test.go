package shopify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestToGID(t *testing.T) {
	if got := toGID("Product", "123"); got != "gid://shopify/Product/123" {
		t.Fatalf("unexpected GID for plain ID: %q", got)
	}
	if got := toGID("Product", "gid://shopify/Product/456"); got != "gid://shopify/Product/456" {
		t.Fatalf("unexpected GID for already-normalized input: %q", got)
	}
	if got := toGID("Product", "gid://shopify/Collection/999"); got != "gid://shopify/Product/999" {
		t.Fatalf("unexpected GID conversion for different resource: %q", got)
	}
}

func TestGraphqlRequestReturnsDataPayload(t *testing.T) {
	client := NewShopifyClient("demo.myshopify.com", "2025-01", "shpca_token")
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			if req.URL.Path != "/admin/api/2025-01/graphql.json" {
				t.Fatalf("unexpected graphql path: %s", req.URL.Path)
			}
			if req.Header.Get("X-Shopify-Access-Token") != "shpca_token" {
				t.Fatalf("missing access token header")
			}

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode graphql payload: %v", err)
			}
			if payload["query"] == "" {
				t.Fatalf("expected query payload")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"data":{"product":{"id":"gid://shopify/Product/1"}}}`)),
			}, nil
		}),
	}

	data, err := client.graphqlRequest(context.Background(), "query { product { id } }", nil)
	if err != nil {
		t.Fatalf("graphqlRequest failed: %v", err)
	}
	product, ok := data["product"].(map[string]any)
	if !ok || product["id"] != "gid://shopify/Product/1" {
		t.Fatalf("unexpected graphql data payload: %#v", data)
	}
}

func TestGraphqlRequestReturnsGraphQLErrors(t *testing.T) {
	client := NewShopifyClient("demo.myshopify.com", "2025-01", "shpca_token")
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"errors":[{"message":"invalid query"}]}`)),
			}, nil
		}),
	}

	_, err := client.graphqlRequest(context.Background(), "query { nope }", nil)
	if err == nil {
		t.Fatalf("expected graphql error")
	}
	if !strings.Contains(err.Error(), "shopify graphql errors") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestRequestDeleteEmptyBodyReturnsSuccess(t *testing.T) {
	client := NewShopifyClient("demo.myshopify.com", "2025-01", "shpca_token")
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodDelete {
				t.Fatalf("expected DELETE, got %s", req.Method)
			}
			if req.URL.Path != "/admin/api/2025-01/products/1.json" {
				t.Fatalf("unexpected rest path: %s", req.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}

	result, err := client.restRequest(context.Background(), http.MethodDelete, "products/1.json", nil)
	if err != nil {
		t.Fatalf("restRequest failed: %v", err)
	}
	if result["success"] != true {
		t.Fatalf("expected success=true for empty DELETE response, got %#v", result)
	}
}

func TestRestRequestReturnsStatusError(t *testing.T) {
	client := NewShopifyClient("demo.myshopify.com", "2025-01", "shpca_token")
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate_limited"}`)),
			}, nil
		}),
	}

	_, err := client.restRequest(context.Background(), http.MethodGet, "products.json", nil)
	if err == nil {
		t.Fatalf("expected rest error")
	}
	if !strings.Contains(err.Error(), "shopify error (429)") {
		t.Fatalf("unexpected error: %v", err)
	}
}
