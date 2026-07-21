package amazon

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PAAClient provides access to the Amazon Product Advertising API v5.
type PAAClient struct {
	accessKey  string
	secretKey  string
	partnerTag string
	host       string // e.g. "webservices.amazon.com"
	region     string // e.g. "us-east-1"
	http       *http.Client
}

// NewPAAClient creates a new Amazon PAAPI v5 client.
func NewPAAClient(accessKey, secretKey, partnerTag, marketplace string) *PAAClient {
	host, region := resolveMarketplace(marketplace)
	return &PAAClient{
		accessKey:  accessKey,
		secretKey:  secretKey,
		partnerTag: partnerTag,
		host:       host,
		region:     region,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// resolveMarketplace returns the PAAPI host and region for a marketplace code.
func resolveMarketplace(marketplace string) (string, string) {
	switch strings.ToUpper(strings.TrimSpace(marketplace)) {
	case "UK", "GB":
		return "webservices.amazon.co.uk", "eu-west-1"
	case "DE":
		return "webservices.amazon.de", "eu-west-1"
	case "FR":
		return "webservices.amazon.fr", "eu-west-1"
	case "JP":
		return "webservices.amazon.co.jp", "us-west-2"
	case "CA":
		return "webservices.amazon.ca", "us-east-1"
	case "AU":
		return "webservices.amazon.com.au", "us-west-2"
	default: // US
		return "webservices.amazon.com", "us-east-1"
	}
}

// SearchItems searches the Amazon catalog.
func (c *PAAClient) SearchItems(ctx context.Context, input SearchInput) (*SearchResult, error) {
	payload := searchRequest{
		PartnerTag:  c.partnerTag,
		PartnerType: "Associates",
		Keywords:    input.Keywords,
		SearchIndex: input.Category,
		ItemCount:   input.Limit,
		Resources: []string{
			"ItemInfo.Title",
			"ItemInfo.ByLineInfo",
			"ItemInfo.Features",
			"Offers.Listings.Price",
			"Offers.Listings.DeliveryInfo.IsPrimeEligible",
			"Offers.Listings.Availability.Type",
			"Offers.Listings.MerchantInfo",
			"Images.Primary.Large",
			"BrowseNodeInfo.BrowseNodes.SalesRank",
		},
	}
	if payload.SearchIndex == "" {
		payload.SearchIndex = "All"
	}
	if payload.ItemCount == 0 {
		payload.ItemCount = 5
	}
	if payload.ItemCount > 10 {
		payload.ItemCount = 10
	}
	if input.MinPrice > 0 {
		payload.MinPrice = input.MinPrice
	}
	if input.MaxPrice > 0 {
		payload.MaxPrice = input.MaxPrice
	}
	if input.SortBy != "" {
		payload.SortBy = input.SortBy
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.signedRequest(ctx, "/paapi5/searchitems", "SearchItems", body)
	if err != nil {
		return nil, err
	}

	var resp searchResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("amazon api error: %s - %s", resp.Errors[0].Code, resp.Errors[0].Message)
	}

	result := &SearchResult{
		TotalResults: resp.SearchResult.TotalResultCount,
	}
	for _, item := range resp.SearchResult.Items {
		product := Product{
			ASIN:  item.ASIN,
			Title: item.ItemInfo.Title.DisplayValue,
			URL:   item.DetailPageURL,
		}
		if item.ItemInfo.ByLineInfo.Brand.DisplayValue != "" {
			product.Brand = item.ItemInfo.ByLineInfo.Brand.DisplayValue
		} else if item.ItemInfo.ByLineInfo.Manufacturer.DisplayValue != "" {
			product.Brand = item.ItemInfo.ByLineInfo.Manufacturer.DisplayValue
		}
		if len(item.ItemInfo.Features.DisplayValues) > 0 {
			product.Features = item.ItemInfo.Features.DisplayValues
		}
		if item.Images.Primary.Large.URL != "" {
			product.ImageURL = item.Images.Primary.Large.URL
		}
		if len(item.Offers.Listings) > 0 {
			listing := item.Offers.Listings[0]
			product.Price = listing.Price.DisplayAmount
			product.PriceAmount = listing.Price.Amount
			product.Currency = listing.Price.Currency
			product.IsPrime = listing.DeliveryInfo.IsPrimeEligible
			product.InStock = listing.Availability.Type == "Now"
			if listing.MerchantInfo.Name != "" {
				product.Seller = listing.MerchantInfo.Name
			}
		}
		if len(item.BrowseNodeInfo.BrowseNodes) > 0 {
			product.SalesRank = item.BrowseNodeInfo.BrowseNodes[0].SalesRank
		}
		result.Products = append(result.Products, product)
	}

	return result, nil
}

// GetItems looks up specific products by ASIN.
func (c *PAAClient) GetItems(ctx context.Context, asins []string) (*SearchResult, error) {
	if len(asins) == 0 {
		return nil, fmt.Errorf("at least one ASIN is required")
	}
	if len(asins) > 10 {
		asins = asins[:10]
	}

	payload := getItemsRequest{
		PartnerTag:  c.partnerTag,
		PartnerType: "Associates",
		ItemIds:     asins,
		Resources: []string{
			"ItemInfo.Title",
			"ItemInfo.ByLineInfo",
			"ItemInfo.Features",
			"Offers.Listings.Price",
			"Offers.Listings.DeliveryInfo.IsPrimeEligible",
			"Offers.Listings.Availability.Type",
			"Offers.Listings.MerchantInfo",
			"Images.Primary.Large",
			"BrowseNodeInfo.BrowseNodes.SalesRank",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.signedRequest(ctx, "/paapi5/getitems", "GetItems", body)
	if err != nil {
		return nil, err
	}

	var resp getItemsResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("amazon api error: %s - %s", resp.Errors[0].Code, resp.Errors[0].Message)
	}

	result := &SearchResult{
		TotalResults: len(resp.ItemsResult.Items),
	}
	for _, item := range resp.ItemsResult.Items {
		product := Product{
			ASIN:  item.ASIN,
			Title: item.ItemInfo.Title.DisplayValue,
			URL:   item.DetailPageURL,
		}
		if item.ItemInfo.ByLineInfo.Brand.DisplayValue != "" {
			product.Brand = item.ItemInfo.ByLineInfo.Brand.DisplayValue
		} else if item.ItemInfo.ByLineInfo.Manufacturer.DisplayValue != "" {
			product.Brand = item.ItemInfo.ByLineInfo.Manufacturer.DisplayValue
		}
		if len(item.ItemInfo.Features.DisplayValues) > 0 {
			product.Features = item.ItemInfo.Features.DisplayValues
		}
		if item.Images.Primary.Large.URL != "" {
			product.ImageURL = item.Images.Primary.Large.URL
		}
		if len(item.Offers.Listings) > 0 {
			listing := item.Offers.Listings[0]
			product.Price = listing.Price.DisplayAmount
			product.PriceAmount = listing.Price.Amount
			product.Currency = listing.Price.Currency
			product.IsPrime = listing.DeliveryInfo.IsPrimeEligible
			product.InStock = listing.Availability.Type == "Now"
			if listing.MerchantInfo.Name != "" {
				product.Seller = listing.MerchantInfo.Name
			}
		}
		if len(item.BrowseNodeInfo.BrowseNodes) > 0 {
			product.SalesRank = item.BrowseNodeInfo.BrowseNodes[0].SalesRank
		}
		result.Products = append(result.Products, product)
	}

	return result, nil
}

// signedRequest performs an AWS Signature V4 signed POST request to PAAPI.
func (c *PAAClient) signedRequest(ctx context.Context, path, operation string, body []byte) ([]byte, error) {
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	service := "ProductAdvertisingAPI"

	// 1. Create canonical request
	payloadHash := sha256Hex(body)
	headers := map[string]string{
		"content-encoding": "amz-1.0",
		"content-type":     "application/json; charset=utf-8",
		"host":             c.host,
		"x-amz-date":      amzDate,
		"x-amz-target":    "com.amazon.paapi5.v1.ProductAdvertisingAPIv1." + operation,
	}
	signedHeaders := "content-encoding;content-type;host;x-amz-date;x-amz-target"
	canonicalHeaders := ""
	for _, key := range strings.Split(signedHeaders, ";") {
		canonicalHeaders += key + ":" + headers[key] + "\n"
	}
	canonicalRequest := strings.Join([]string{
		"POST",
		path,
		"", // query string (empty)
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// 2. Create string to sign
	credentialScope := dateStamp + "/" + c.region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// 3. Calculate signing key
	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+c.secretKey), []byte(dateStamp)),
				[]byte(c.region),
			),
			[]byte(service),
		),
		[]byte("aws4_request"),
	)

	// 4. Create signature
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// 5. Build Authorization header
	authorization := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey, credentialScope, signedHeaders, signature)

	// 6. Make request
	url := "https://" + c.host + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	for key, val := range headers {
		req.Header.Set(key, val)
	}
	req.Header.Set("Authorization", authorization)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("amazon api request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("amazon api error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// --- Request/Response types ---

type searchRequest struct {
	PartnerTag  string   `json:"PartnerTag"`
	PartnerType string   `json:"PartnerType"`
	Keywords    string   `json:"Keywords"`
	SearchIndex string   `json:"SearchIndex"`
	ItemCount   int      `json:"ItemCount"`
	MinPrice    int      `json:"MinPrice,omitempty"`
	MaxPrice    int      `json:"MaxPrice,omitempty"`
	SortBy      string   `json:"SortBy,omitempty"`
	Resources   []string `json:"Resources"`
}

type getItemsRequest struct {
	PartnerTag  string   `json:"PartnerTag"`
	PartnerType string   `json:"PartnerType"`
	ItemIds     []string `json:"ItemIds"`
	Resources   []string `json:"Resources"`
}

type apiError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type searchResponse struct {
	SearchResult struct {
		TotalResultCount int        `json:"TotalResultCount"`
		Items            []apiItem  `json:"Items"`
	} `json:"SearchResult"`
	Errors []apiError `json:"Errors"`
}

type getItemsResponse struct {
	ItemsResult struct {
		Items []apiItem `json:"Items"`
	} `json:"ItemsResult"`
	Errors []apiError `json:"Errors"`
}

type apiItem struct {
	ASIN          string `json:"ASIN"`
	DetailPageURL string `json:"DetailPageURL"`
	ItemInfo      struct {
		Title struct {
			DisplayValue string `json:"DisplayValue"`
		} `json:"Title"`
		ByLineInfo struct {
			Brand struct {
				DisplayValue string `json:"DisplayValue"`
			} `json:"Brand"`
			Manufacturer struct {
				DisplayValue string `json:"DisplayValue"`
			} `json:"Manufacturer"`
		} `json:"ByLineInfo"`
		Features struct {
			DisplayValues []string `json:"DisplayValues"`
		} `json:"Features"`
	} `json:"ItemInfo"`
	Offers struct {
		Listings []struct {
			Price struct {
				Amount        float64 `json:"Amount"`
				Currency      string  `json:"Currency"`
				DisplayAmount string  `json:"DisplayAmount"`
			} `json:"Price"`
			DeliveryInfo struct {
				IsPrimeEligible bool `json:"IsPrimeEligible"`
			} `json:"DeliveryInfo"`
			Availability struct {
				Type string `json:"Type"`
			} `json:"Availability"`
			MerchantInfo struct {
				Name string `json:"Name"`
			} `json:"MerchantInfo"`
		} `json:"Listings"`
	} `json:"Offers"`
	Images struct {
		Primary struct {
			Large struct {
				URL string `json:"URL"`
			} `json:"Large"`
		} `json:"Primary"`
	} `json:"Images"`
	BrowseNodeInfo struct {
		BrowseNodes []struct {
			SalesRank int `json:"SalesRank"`
		} `json:"BrowseNodes"`
	} `json:"BrowseNodeInfo"`
}

// --- Public result types ---

// Product represents a single Amazon product from search results.
type Product struct {
	ASIN        string   `json:"asin"`
	Title       string   `json:"title"`
	Brand       string   `json:"brand,omitempty"`
	Price       string   `json:"price,omitempty"`
	PriceAmount float64  `json:"price_amount,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	IsPrime     bool     `json:"is_prime"`
	InStock     bool     `json:"in_stock"`
	Seller      string   `json:"seller,omitempty"`
	URL         string   `json:"url,omitempty"`
	ImageURL    string   `json:"image_url,omitempty"`
	Features    []string `json:"features,omitempty"`
	SalesRank   int      `json:"sales_rank,omitempty"`
}

// SearchResult holds the results of a product search.
type SearchResult struct {
	TotalResults int       `json:"total_results"`
	Products     []Product `json:"products"`
}
