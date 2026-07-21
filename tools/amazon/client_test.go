package amazon

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

func TestResolveMarketplace(t *testing.T) {
	cases := []struct {
		marketplace string
		wantHost    string
		wantRegion  string
	}{
		{marketplace: "US", wantHost: "webservices.amazon.com", wantRegion: "us-east-1"},
		{marketplace: "uk", wantHost: "webservices.amazon.co.uk", wantRegion: "eu-west-1"},
		{marketplace: "DE", wantHost: "webservices.amazon.de", wantRegion: "eu-west-1"},
		{marketplace: "jp", wantHost: "webservices.amazon.co.jp", wantRegion: "us-west-2"},
		{marketplace: "unknown", wantHost: "webservices.amazon.com", wantRegion: "us-east-1"},
	}

	for _, tc := range cases {
		t.Run(tc.marketplace, func(t *testing.T) {
			host, region := resolveMarketplace(tc.marketplace)
			if host != tc.wantHost || region != tc.wantRegion {
				t.Fatalf("resolveMarketplace(%q) = (%q, %q), want (%q, %q)", tc.marketplace, host, region, tc.wantHost, tc.wantRegion)
			}
		})
	}
}

func TestSearchItemsShapesRequestAndMapsResponse(t *testing.T) {
	client := NewPAAClient("access", "secret", "tag-20", "US")
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost {
				t.Fatalf("expected POST request, got %s", req.Method)
			}
			if req.URL.Path != "/paapi5/searchitems" {
				t.Fatalf("expected search path, got %s", req.URL.Path)
			}
			if got := req.Header.Get("x-amz-target"); got != "com.amazon.paapi5.v1.ProductAdvertisingAPIv1.SearchItems" {
				t.Fatalf("unexpected x-amz-target: %q", got)
			}

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode request payload: %v", err)
			}
			if payload["PartnerTag"] != "tag-20" {
				t.Fatalf("unexpected PartnerTag: %#v", payload["PartnerTag"])
			}
			if payload["SearchIndex"] != "All" {
				t.Fatalf("expected default SearchIndex All, got %#v", payload["SearchIndex"])
			}
			if got := int(payload["ItemCount"].(float64)); got != 10 {
				t.Fatalf("expected ItemCount to be clamped to 10, got %d", got)
			}

			body := `{
				"SearchResult": {
					"TotalResultCount": 1,
					"Items": [
						{
							"ASIN": "B001",
							"DetailPageURL": "https://amazon.com/dp/B001",
							"ItemInfo": {
								"Title": {"DisplayValue": "Filter Pack"},
								"ByLineInfo": {
									"Brand": {"DisplayValue": ""},
									"Manufacturer": {"DisplayValue": "AquaCorp"}
								},
								"Features": {"DisplayValues": ["Fits 3.2L fountain"]}
							},
							"Offers": {
								"Listings": [
									{
										"Price": {"Amount": 12.34, "Currency": "USD", "DisplayAmount": "$12.34"},
										"DeliveryInfo": {"IsPrimeEligible": true},
										"Availability": {"Type": "Now"},
										"MerchantInfo": {"Name": "AquaStore"}
									}
								]
							},
							"Images": {"Primary": {"Large": {"URL": "https://img"}}},
							"BrowseNodeInfo": {"BrowseNodes": [{"SalesRank": 42}]}
						}
					]
				}
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	result, err := client.SearchItems(context.Background(), SearchInput{
		Keywords: "cat fountain filter",
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("SearchItems failed: %v", err)
	}
	if result.TotalResults != 1 || len(result.Products) != 1 {
		t.Fatalf("unexpected result counts: %#v", result)
	}
	product := result.Products[0]
	if product.ASIN != "B001" || product.Title != "Filter Pack" {
		t.Fatalf("unexpected product identity: %#v", product)
	}
	if product.Brand != "AquaCorp" {
		t.Fatalf("expected manufacturer fallback brand, got %q", product.Brand)
	}
	if !product.IsPrime || !product.InStock {
		t.Fatalf("expected prime+in stock product, got %#v", product)
	}
	if product.PriceAmount != 12.34 || product.Currency != "USD" || product.Seller != "AquaStore" {
		t.Fatalf("unexpected commerce mapping: %#v", product)
	}
	if product.SalesRank != 42 {
		t.Fatalf("unexpected sales rank: %#v", product)
	}
}

func TestGetItemsRequiresAtLeastOneASIN(t *testing.T) {
	client := NewPAAClient("access", "secret", "tag-20", "US")
	_, err := client.GetItems(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected validation error for empty ASIN list")
	}
	if !strings.Contains(err.Error(), "at least one ASIN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetItemsTruncatesASINsToTen(t *testing.T) {
	client := NewPAAClient("access", "secret", "tag-20", "US")
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/paapi5/getitems" {
				t.Fatalf("expected getitems path, got %s", req.URL.Path)
			}
			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode payload: %v", err)
			}
			itemIDs, ok := payload["ItemIds"].([]any)
			if !ok {
				t.Fatalf("expected ItemIds array, got %#v", payload["ItemIds"])
			}
			if len(itemIDs) != 10 {
				t.Fatalf("expected ItemIds length 10, got %d", len(itemIDs))
			}
			body := `{"ItemsResult":{"Items":[]}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	var asins []string
	for i := 0; i < 12; i++ {
		asins = append(asins, "B00TESTASIN")
	}

	result, err := client.GetItems(context.Background(), asins)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if result.TotalResults != 0 || len(result.Products) != 0 {
		t.Fatalf("unexpected GetItems result: %#v", result)
	}
}
