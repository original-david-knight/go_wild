package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools/ads"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// AdsTools proxies ad platform operations through the broker API.
type AdsTools struct {
	client *Client
}

// NewAdsTools creates a new AdsTools instance.
func NewAdsTools(client *Client) *AdsTools {
	return &AdsTools{client: client}
}

// --- Meta Campaign Tools ---

func (s *AdsTools) AdsMetaCreateCampaignTool(ctx context.Context, input ads.AdsMetaCreateCampaignInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_create_campaign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsMetaUpdateCampaignTool(ctx context.Context, input ads.AdsMetaUpdateCampaignInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_update_campaign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsMetaGetCampaignTool(ctx context.Context, input ads.AdsMetaGetCampaignInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_get_campaign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsMetaPauseCampaignTool(ctx context.Context, input ads.AdsMetaPauseCampaignInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_pause_campaign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Meta Ad Set Tools ---

func (s *AdsTools) AdsMetaCreateAdsetTool(ctx context.Context, input ads.AdsMetaCreateAdsetInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_create_adset", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsMetaUpdateAdsetTool(ctx context.Context, input ads.AdsMetaUpdateAdsetInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_update_adset", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Meta Creative Tools ---

func (s *AdsTools) AdsMetaCreateAdTool(ctx context.Context, input ads.AdsMetaCreateAdInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_create_ad", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsMetaGetAdPerformanceTool(ctx context.Context, input ads.AdsMetaGetAdPerformanceInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_meta_get_ad_performance", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Google Campaign Tools ---

func (s *AdsTools) AdsGoogleCreateCampaignTool(ctx context.Context, input ads.AdsGoogleCreateCampaignInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_google_create_campaign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsGoogleUpdateCampaignTool(ctx context.Context, input ads.AdsGoogleUpdateCampaignInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_google_update_campaign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// --- Budget Tools ---

func (s *AdsTools) AdsGetDailySpendTool(ctx context.Context, input ads.AdsGetDailySpendInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_get_daily_spend", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (s *AdsTools) AdsGetCampaignRoasTool(ctx context.Context, input ads.AdsGetCampaignRoasInput) (*loop.ToolResult, error) {
	result, err := s.client.CallTool(ctx, "ads_get_campaign_roas", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool returns the description for a tool by name.
// Delegates to the underlying tool packages for descriptions.
func (s *AdsTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"ads_meta_create_campaign":   "Create a new Meta (Facebook/Instagram) ad campaign. Requires a name, objective, and daily budget. The campaign is created in PAUSED status by default for review before activation.",
		"ads_meta_update_campaign":   "Update an existing Meta ad campaign. Can change name, status, daily budget, or other settings.",
		"ads_meta_get_campaign":      "Get details and current status of a Meta ad campaign by ID.",
		"ads_meta_pause_campaign":    "Pause a running Meta ad campaign immediately. Use this to stop spend on underperforming campaigns.",
		"ads_meta_create_adset":      "Create a new Meta ad set within a campaign. Defines the target audience, budget, and schedule for a group of ads.",
		"ads_meta_update_adset":      "Update an existing Meta ad set. Can change targeting, budget, status, or schedule.",
		"ads_meta_create_ad":         "Create a new Meta ad with creative content (headline, body, image, link). The ad is placed within an ad set.",
		"ads_meta_get_ad_performance": "Get performance metrics (impressions, clicks, spend, conversions, ROAS) for a specific Meta ad or campaign.",
		"ads_google_create_campaign": "Create a new Google Ads campaign. Supports Search, Shopping, and Performance Max campaign types.",
		"ads_google_update_campaign": "Update an existing Google Ads campaign. Can change name, status, budget, or bidding strategy.",
		"ads_get_daily_spend":        "Get today's total ad spend across Meta and Google Ads platforms. Returns spend broken down by platform.",
		"ads_get_campaign_roas":      "Get return on ad spend (ROAS) for a specific Meta campaign over a given period.",
	}
	return descriptions[name]
}
