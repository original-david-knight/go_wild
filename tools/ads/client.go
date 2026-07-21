package ads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// MetaAdsClient communicates with the Meta Marketing API.
type MetaAdsClient struct {
	accessToken string
	accountID   string // e.g. "act_123456"
	pixelID     string
	httpClient  *http.Client
}

// NewMetaAdsClient creates a new Meta Ads API client.
func NewMetaAdsClient(accessToken, accountID, pixelID string) *MetaAdsClient {
	return &MetaAdsClient{
		accessToken: accessToken,
		accountID:   accountID,
		pixelID:     pixelID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

const metaAPIVersion = "v21.0"
const metaBaseURL = "https://graph.facebook.com"

// metaURL builds a Meta API endpoint URL.
func (c *MetaAdsClient) metaURL(path string) string {
	return fmt.Sprintf("%s/%s/%s", metaBaseURL, metaAPIVersion, path)
}

// post sends a POST request to the Meta API with JSON body.
func (c *MetaAdsClient) post(ctx context.Context, path string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.metaURL(path), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	return c.doRequest(req)
}

// get sends a GET request to the Meta API.
func (c *MetaAdsClient) get(ctx context.Context, path string, params url.Values) (map[string]any, error) {
	u := c.metaURL(path)
	if params == nil {
		params = url.Values{}
	}
	params.Set("access_token", c.accessToken)
	u += "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.doRequest(req)
}

func (c *MetaAdsClient) doRequest(req *http.Request) (map[string]any, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta API request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.StatusCode >= 400 {
		if errObj, ok := result["error"].(map[string]any); ok {
			msg, _ := errObj["message"].(string)
			return nil, fmt.Errorf("meta API error (%d): %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("meta API error (%d): %s", resp.StatusCode, string(data))
	}

	return result, nil
}

// CreateCampaign creates a campaign in the Meta ad account.
func (c *MetaAdsClient) CreateCampaign(ctx context.Context, name, objective, status string, dailyBudget float64, specialAdCategories []string) (map[string]any, error) {
	body := map[string]any{
		"name":                 name,
		"objective":            objective,
		"status":               status,
		"daily_budget":         int(dailyBudget * 100), // Meta uses cents
		"special_ad_categories": specialAdCategories,
	}
	return c.post(ctx, c.accountID+"/campaigns", body)
}

// UpdateCampaign updates fields on an existing campaign.
func (c *MetaAdsClient) UpdateCampaign(ctx context.Context, campaignID string, fields map[string]any) (map[string]any, error) {
	return c.post(ctx, campaignID, fields)
}

// GetCampaign retrieves a campaign by ID.
func (c *MetaAdsClient) GetCampaign(ctx context.Context, campaignID string, fields []string) (map[string]any, error) {
	params := url.Values{}
	if len(fields) > 0 {
		fieldStr := ""
		for i, f := range fields {
			if i > 0 {
				fieldStr += ","
			}
			fieldStr += f
		}
		params.Set("fields", fieldStr)
	}
	return c.get(ctx, campaignID, params)
}

// PauseCampaign sets a campaign status to PAUSED.
func (c *MetaAdsClient) PauseCampaign(ctx context.Context, campaignID string) (map[string]any, error) {
	return c.post(ctx, campaignID, map[string]any{"status": "PAUSED"})
}

// CreateAdSet creates an ad set under a campaign.
func (c *MetaAdsClient) CreateAdSet(ctx context.Context, params map[string]any) (map[string]any, error) {
	return c.post(ctx, c.accountID+"/adsets", params)
}

// UpdateAdSet updates fields on an existing ad set.
func (c *MetaAdsClient) UpdateAdSet(ctx context.Context, adsetID string, fields map[string]any) (map[string]any, error) {
	return c.post(ctx, adsetID, fields)
}

// CreateAd creates an ad under an ad set.
func (c *MetaAdsClient) CreateAd(ctx context.Context, params map[string]any) (map[string]any, error) {
	return c.post(ctx, c.accountID+"/ads", params)
}

// GetAdInsights retrieves performance metrics for an ad or campaign.
func (c *MetaAdsClient) GetAdInsights(ctx context.Context, objectID string, fields []string, datePreset string) (map[string]any, error) {
	params := url.Values{}
	if len(fields) > 0 {
		fieldStr := ""
		for i, f := range fields {
			if i > 0 {
				fieldStr += ","
			}
			fieldStr += f
		}
		params.Set("fields", fieldStr)
	}
	if datePreset != "" {
		params.Set("date_preset", datePreset)
	}
	return c.get(ctx, objectID+"/insights", params)
}

// GetAccountInsights retrieves account-level spend insights.
func (c *MetaAdsClient) GetAccountInsights(ctx context.Context, datePreset string) (map[string]any, error) {
	params := url.Values{}
	params.Set("fields", "spend,impressions,clicks,actions")
	if datePreset != "" {
		params.Set("date_preset", datePreset)
	}
	return c.get(ctx, c.accountID+"/insights", params)
}

// GoogleAdsClient communicates with the Google Ads REST API.
type GoogleAdsClient struct {
	developerToken string
	customerID     string
	refreshToken   string
	httpClient     *http.Client
}

// NewGoogleAdsClient creates a new Google Ads API client.
func NewGoogleAdsClient(developerToken, customerID, refreshToken string) *GoogleAdsClient {
	return &GoogleAdsClient{
		developerToken: developerToken,
		customerID:     customerID,
		refreshToken:   refreshToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

const googleAdsAPIVersion = "v18"
const googleAdsBaseURL = "https://googleads.googleapis.com"

// googleAdsURL builds a Google Ads API endpoint URL.
func (c *GoogleAdsClient) googleAdsURL(path string) string {
	return fmt.Sprintf("%s/%s/customers/%s/%s", googleAdsBaseURL, googleAdsAPIVersion, c.customerID, path)
}

// post sends a POST request to the Google Ads API.
func (c *GoogleAdsClient) post(ctx context.Context, path string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.googleAdsURL(path), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("developer-token", c.developerToken)
	req.Header.Set("Authorization", "Bearer "+c.refreshToken)

	return c.doRequest(req)
}

func (c *GoogleAdsClient) doRequest(req *http.Request) (map[string]any, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google ads API request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if resp.StatusCode >= 400 {
		if errObj, ok := result["error"].(map[string]any); ok {
			msg, _ := errObj["message"].(string)
			return nil, fmt.Errorf("google ads API error (%d): %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("google ads API error (%d): %s", resp.StatusCode, string(data))
	}

	return result, nil
}

// CreateCampaign creates a Google Ads campaign.
func (c *GoogleAdsClient) CreateCampaign(ctx context.Context, params map[string]any) (map[string]any, error) {
	return c.post(ctx, "campaigns:mutate", map[string]any{
		"operations": []map[string]any{
			{"create": params},
		},
	})
}

// UpdateCampaign updates a Google Ads campaign.
func (c *GoogleAdsClient) UpdateCampaign(ctx context.Context, resourceName string, fields map[string]any) (map[string]any, error) {
	fields["resourceName"] = resourceName
	return c.post(ctx, "campaigns:mutate", map[string]any{
		"operations": []map[string]any{
			{"update": fields, "updateMask": "*"},
		},
	})
}

// Query executes a GAQL query against the Google Ads API.
func (c *GoogleAdsClient) Query(ctx context.Context, query string) (map[string]any, error) {
	return c.post(ctx, "googleAds:searchStream", map[string]any{
		"query": query,
	})
}
