package ads

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// BudgetTools provides cross-platform ad spend tracking tools.
type BudgetTools struct {
	metaClient   *MetaAdsClient
	googleClient *GoogleAdsClient
}

// NewBudgetTools creates a new BudgetTools instance.
func NewBudgetTools(metaClient *MetaAdsClient, googleClient *GoogleAdsClient) *BudgetTools {
	return &BudgetTools{
		metaClient:   metaClient,
		googleClient: googleClient,
	}
}

// AdsGetDailySpendInput defines input for getting daily ad spend.
type AdsGetDailySpendInput struct {
	DatePreset string `json:"date_preset" description:"Date range for spend" enum:"today,yesterday,last_7d,last_30d"`
}

// AdsGetDailySpendTool retrieves daily ad spend across platforms.
func (t *BudgetTools) AdsGetDailySpendTool(ctx context.Context, input AdsGetDailySpendInput) (*loop.ToolResult, error) {
	datePreset := input.DatePreset
	if datePreset == "" {
		datePreset = "today"
	}

	result := map[string]any{
		"date_preset": datePreset,
	}

	// Get Meta spend
	if t.metaClient != nil {
		metaInsights, err := t.metaClient.GetAccountInsights(ctx, datePreset)
		if err != nil {
			result["meta_error"] = err.Error()
		} else {
			result["meta"] = metaInsights
		}
	} else {
		result["meta"] = "not configured"
	}

	// Get Google spend
	if t.googleClient != nil {
		query := fmt.Sprintf(`SELECT metrics.cost_micros, metrics.impressions, metrics.clicks
			FROM campaign WHERE segments.date DURING %s`, datePresetToGAQL(datePreset))
		googleInsights, err := t.googleClient.Query(ctx, query)
		if err != nil {
			result["google_error"] = err.Error()
		} else {
			result["google"] = googleInsights
		}
	} else {
		result["google"] = "not configured"
	}

	return loop.NewSuccessResult(result), nil
}

// AdsGetCampaignRoasInput defines input for getting campaign ROAS.
type AdsGetCampaignRoasInput struct {
	CampaignID string `json:"campaign_id" description:"Meta campaign ID to get ROAS for" required:"true"`
	DatePreset string `json:"date_preset" description:"Date range" enum:"last_7d,last_14d,last_30d,last_90d,this_month"`
}

// AdsGetCampaignRoasTool retrieves ROAS for a specific campaign.
func (t *BudgetTools) AdsGetCampaignRoasTool(ctx context.Context, input AdsGetCampaignRoasInput) (*loop.ToolResult, error) {
	if input.CampaignID == "" {
		return loop.NewErrorResult("campaign_id is required"), nil
	}

	datePreset := input.DatePreset
	if datePreset == "" {
		datePreset = "last_7d"
	}

	if t.metaClient == nil {
		return loop.NewErrorResult("Meta Ads client not configured"), nil
	}

	fields := []string{"spend", "purchase_roas", "actions", "action_values", "impressions", "clicks", "cpc", "ctr"}
	insights, err := t.metaClient.GetAdInsights(ctx, input.CampaignID, fields, datePreset)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get campaign ROAS: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"campaign_id": input.CampaignID,
		"date_preset": datePreset,
		"insights":    insights,
	}), nil
}

// datePresetToGAQL converts a Meta date preset to a Google Ads GAQL date range.
func datePresetToGAQL(preset string) string {
	switch preset {
	case "today":
		return "TODAY"
	case "yesterday":
		return "YESTERDAY"
	case "last_7d":
		return "LAST_7_DAYS"
	case "last_30d":
		return "LAST_30_DAYS"
	default:
		return "TODAY"
	}
}
