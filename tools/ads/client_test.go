package ads

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

func TestMetaAdsClientCreateCampaignRequestShape(t *testing.T) {
	client := NewMetaAdsClient("token-123", "act_42", "pixel-1")
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			if !strings.Contains(req.URL.String(), "/v21.0/act_42/campaigns") {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if got := req.Header.Get("Authorization"); got != "Bearer token-123" {
				t.Fatalf("unexpected auth header: %q", got)
			}

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode payload: %v", err)
			}
			if got := int(payload["daily_budget"].(float64)); got != 1234 {
				t.Fatalf("expected daily budget 1234 cents, got %d", got)
			}
			if payload["name"] != "My Campaign" || payload["objective"] != "OUTCOME_SALES" {
				t.Fatalf("unexpected campaign payload: %#v", payload)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"id":"camp-1"}`)),
			}, nil
		}),
	}

	result, err := client.CreateCampaign(context.Background(), "My Campaign", "OUTCOME_SALES", "PAUSED", 12.34, []string{"CREDIT"})
	if err != nil {
		t.Fatalf("CreateCampaign failed: %v", err)
	}
	if result["id"] != "camp-1" {
		t.Fatalf("unexpected CreateCampaign response: %#v", result)
	}
}

func TestMetaAdsClientGetCampaignIncludesAccessTokenParam(t *testing.T) {
	client := NewMetaAdsClient("token-xyz", "act_42", "pixel-1")
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", req.Method)
			}
			q := req.URL.Query()
			if q.Get("access_token") != "token-xyz" {
				t.Fatalf("missing access_token query param: %s", req.URL.String())
			}
			if q.Get("fields") != "id,name" {
				t.Fatalf("unexpected fields query param: %q", q.Get("fields"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"id":"camp-1","name":"Test"}`)),
			}, nil
		}),
	}

	result, err := client.GetCampaign(context.Background(), "camp-1", []string{"id", "name"})
	if err != nil {
		t.Fatalf("GetCampaign failed: %v", err)
	}
	if result["id"] != "camp-1" {
		t.Fatalf("unexpected response: %#v", result)
	}
}

func TestMetaAdsClientErrorMessagePropagation(t *testing.T) {
	client := NewMetaAdsClient("token-xyz", "act_42", "pixel-1")
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid target"}}`)),
			}, nil
		}),
	}

	_, err := client.GetCampaign(context.Background(), "camp-1", []string{"id"})
	if err == nil {
		t.Fatalf("expected API error")
	}
	if !strings.Contains(err.Error(), "meta API error (400): invalid target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoogleAdsClientUpdateCampaignBuildsMutatePayload(t *testing.T) {
	client := NewGoogleAdsClient("dev-token", "123456", "refresh-token")
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", req.Method)
			}
			if !strings.Contains(req.URL.String(), "/v18/customers/123456/campaigns:mutate") {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("developer-token") != "dev-token" {
				t.Fatalf("unexpected developer-token header: %q", req.Header.Get("developer-token"))
			}
			if req.Header.Get("Authorization") != "Bearer refresh-token" {
				t.Fatalf("unexpected auth header: %q", req.Header.Get("Authorization"))
			}

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode payload: %v", err)
			}
			ops, ok := payload["operations"].([]any)
			if !ok || len(ops) != 1 {
				t.Fatalf("unexpected operations payload: %#v", payload["operations"])
			}
			op, ok := ops[0].(map[string]any)
			if !ok {
				t.Fatalf("unexpected operation entry: %#v", ops[0])
			}
			update, ok := op["update"].(map[string]any)
			if !ok {
				t.Fatalf("unexpected update payload: %#v", op["update"])
			}
			if update["resourceName"] != "customers/123456/campaigns/789" {
				t.Fatalf("expected resourceName in update payload, got %#v", update["resourceName"])
			}
			if op["updateMask"] != "*" {
				t.Fatalf("expected updateMask='*', got %#v", op["updateMask"])
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"results":[{"resourceName":"customers/123456/campaigns/789"}]}`)),
			}, nil
		}),
	}

	result, err := client.UpdateCampaign(context.Background(), "customers/123456/campaigns/789", map[string]any{"status": "PAUSED"})
	if err != nil {
		t.Fatalf("UpdateCampaign failed: %v", err)
	}
	if _, ok := result["results"]; !ok {
		t.Fatalf("unexpected UpdateCampaign response: %#v", result)
	}
}
