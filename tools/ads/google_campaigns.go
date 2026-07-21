package ads

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// GoogleCampaignTools provides tools for managing Google Ads campaigns.
type GoogleCampaignTools struct {
	client *GoogleAdsClient
}

// NewGoogleCampaignTools creates a new GoogleCampaignTools instance.
func NewGoogleCampaignTools(client *GoogleAdsClient) *GoogleCampaignTools {
	return &GoogleCampaignTools{client: client}
}

// AdsGoogleCreateCampaignInput defines input for creating a Google Ads campaign.
type AdsGoogleCreateCampaignInput struct {
	Name            string  `json:"name" description:"Campaign name" required:"true"`
	CampaignType    string  `json:"campaign_type" description:"Campaign type" enum:"SEARCH,SHOPPING,PERFORMANCE_MAX" required:"true"`
	DailyBudget     float64 `json:"daily_budget" description:"Daily budget in USD" required:"true"`
	BiddingStrategy string  `json:"bidding_strategy" description:"Bidding strategy" enum:"MAXIMIZE_CONVERSIONS,MAXIMIZE_CONVERSION_VALUE,TARGET_CPA,TARGET_ROAS,MANUAL_CPC"`
	TargetCpa       float64 `json:"target_cpa" description:"Target CPA in USD (for TARGET_CPA strategy)"`
	TargetRoas      float64 `json:"target_roas" description:"Target ROAS multiplier (for TARGET_ROAS strategy, e.g. 2.0 = 200%)"`
	Status          string  `json:"status" description:"Initial campaign status" enum:"PAUSED,ENABLED"`
}

// AdsGoogleCreateCampaignTool creates a new Google Ads campaign.
func (t *GoogleCampaignTools) AdsGoogleCreateCampaignTool(ctx context.Context, input AdsGoogleCreateCampaignInput) (*loop.ToolResult, error) {
	if input.Name == "" {
		return loop.NewErrorResult("campaign name is required"), nil
	}
	if input.DailyBudget <= 0 {
		return loop.NewErrorResult("daily_budget must be positive"), nil
	}

	status := input.Status
	if status == "" {
		status = "PAUSED"
	}
	biddingStrategy := input.BiddingStrategy
	if biddingStrategy == "" {
		biddingStrategy = "MAXIMIZE_CONVERSIONS"
	}

	params := map[string]any{
		"name":                     input.Name,
		"advertisingChannelType":   input.CampaignType,
		"status":                   status,
		"campaignBudget":           map[string]any{"amountMicros": int64(input.DailyBudget * 1_000_000)},
	}

	// Set bidding strategy
	switch biddingStrategy {
	case "MAXIMIZE_CONVERSIONS":
		params["maximizeConversions"] = map[string]any{}
	case "MAXIMIZE_CONVERSION_VALUE":
		params["maximizeConversionValue"] = map[string]any{}
	case "TARGET_CPA":
		params["targetCpa"] = map[string]any{"targetCpaMicros": int64(input.TargetCpa * 1_000_000)}
	case "TARGET_ROAS":
		params["targetRoas"] = map[string]any{"targetRoas": input.TargetRoas}
	case "MANUAL_CPC":
		params["manualCpc"] = map[string]any{}
	}

	result, err := t.client.CreateCampaign(ctx, params)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to create Google campaign: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"campaign":     result,
		"name":         input.Name,
		"status":       status,
		"daily_budget": input.DailyBudget,
	}), nil
}

// AdsGoogleUpdateCampaignInput defines input for updating a Google Ads campaign.
type AdsGoogleUpdateCampaignInput struct {
	ResourceName    string  `json:"resource_name" description:"Google Ads campaign resource name (e.g. customers/123/campaigns/456)" required:"true"`
	Name            string  `json:"name" description:"New campaign name"`
	Status          string  `json:"status" description:"New campaign status" enum:"PAUSED,ENABLED,REMOVED"`
	DailyBudget     float64 `json:"daily_budget" description:"New daily budget in USD"`
	BiddingStrategy string  `json:"bidding_strategy" description:"New bidding strategy" enum:"MAXIMIZE_CONVERSIONS,MAXIMIZE_CONVERSION_VALUE,TARGET_CPA,TARGET_ROAS,MANUAL_CPC"`
	TargetCpa       float64 `json:"target_cpa" description:"New target CPA in USD"`
	TargetRoas      float64 `json:"target_roas" description:"New target ROAS multiplier"`
}

// AdsGoogleUpdateCampaignTool updates an existing Google Ads campaign.
func (t *GoogleCampaignTools) AdsGoogleUpdateCampaignTool(ctx context.Context, input AdsGoogleUpdateCampaignInput) (*loop.ToolResult, error) {
	if input.ResourceName == "" {
		return loop.NewErrorResult("resource_name is required"), nil
	}

	fields := map[string]any{}
	if input.Name != "" {
		fields["name"] = input.Name
	}
	if input.Status != "" {
		fields["status"] = input.Status
	}
	if input.DailyBudget > 0 {
		fields["campaignBudget"] = map[string]any{"amountMicros": int64(input.DailyBudget * 1_000_000)}
	}

	// Update bidding strategy if specified
	if input.BiddingStrategy != "" {
		switch input.BiddingStrategy {
		case "MAXIMIZE_CONVERSIONS":
			fields["maximizeConversions"] = map[string]any{}
		case "MAXIMIZE_CONVERSION_VALUE":
			fields["maximizeConversionValue"] = map[string]any{}
		case "TARGET_CPA":
			fields["targetCpa"] = map[string]any{"targetCpaMicros": int64(input.TargetCpa * 1_000_000)}
		case "TARGET_ROAS":
			fields["targetRoas"] = map[string]any{"targetRoas": input.TargetRoas}
		case "MANUAL_CPC":
			fields["manualCpc"] = map[string]any{}
		}
	}

	if len(fields) == 0 {
		return loop.NewErrorResult("at least one field to update is required"), nil
	}

	result, err := t.client.UpdateCampaign(ctx, input.ResourceName, fields)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to update Google campaign: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"campaign":      result,
		"resource_name": input.ResourceName,
		"updated":       fields,
	}), nil
}
