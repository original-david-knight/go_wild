package ads

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// MetaCampaignTools provides tools for managing Meta (Facebook/Instagram) ad campaigns.
type MetaCampaignTools struct {
	client *MetaAdsClient
}

// NewMetaCampaignTools creates a new MetaCampaignTools instance.
func NewMetaCampaignTools(client *MetaAdsClient) *MetaCampaignTools {
	return &MetaCampaignTools{client: client}
}

// AdsMetaCreateCampaignInput defines input for creating a Meta campaign.
type AdsMetaCreateCampaignInput struct {
	Name                string   `json:"name" description:"Campaign name" required:"true"`
	Objective           string   `json:"objective" description:"Campaign objective" enum:"OUTCOME_AWARENESS,OUTCOME_ENGAGEMENT,OUTCOME_LEADS,OUTCOME_SALES,OUTCOME_TRAFFIC,OUTCOME_APP_PROMOTION" required:"true"`
	DailyBudget         float64  `json:"daily_budget" description:"Daily budget in USD" required:"true"`
	Status              string   `json:"status" description:"Initial campaign status" enum:"PAUSED,ACTIVE"`
	SpecialAdCategories []string `json:"special_ad_categories" description:"Special ad categories if applicable (e.g. HOUSING, CREDIT, EMPLOYMENT)"`
}

// AdsMetaCreateCampaignTool creates a new Meta ad campaign.
func (t *MetaCampaignTools) AdsMetaCreateCampaignTool(ctx context.Context, input AdsMetaCreateCampaignInput) (*loop.ToolResult, error) {
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
	categories := input.SpecialAdCategories
	if categories == nil {
		categories = []string{}
	}

	result, err := t.client.CreateCampaign(ctx, input.Name, input.Objective, status, input.DailyBudget, categories)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to create campaign: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"campaign_id":  result["id"],
		"name":         input.Name,
		"status":       status,
		"daily_budget": input.DailyBudget,
	}), nil
}

// AdsMetaUpdateCampaignInput defines input for updating a Meta campaign.
type AdsMetaUpdateCampaignInput struct {
	CampaignID  string  `json:"campaign_id" description:"Meta campaign ID to update" required:"true"`
	Name        string  `json:"name" description:"New campaign name"`
	Status      string  `json:"status" description:"New campaign status" enum:"PAUSED,ACTIVE,ARCHIVED"`
	DailyBudget float64 `json:"daily_budget" description:"New daily budget in USD"`
}

// AdsMetaUpdateCampaignTool updates an existing Meta campaign.
func (t *MetaCampaignTools) AdsMetaUpdateCampaignTool(ctx context.Context, input AdsMetaUpdateCampaignInput) (*loop.ToolResult, error) {
	if input.CampaignID == "" {
		return loop.NewErrorResult("campaign_id is required"), nil
	}

	fields := map[string]any{}
	if input.Name != "" {
		fields["name"] = input.Name
	}
	if input.Status != "" {
		fields["status"] = input.Status
	}
	if input.DailyBudget > 0 {
		fields["daily_budget"] = int(input.DailyBudget * 100)
	}

	if len(fields) == 0 {
		return loop.NewErrorResult("at least one field to update is required"), nil
	}

	result, err := t.client.UpdateCampaign(ctx, input.CampaignID, fields)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to update campaign: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"success":     result["success"],
		"campaign_id": input.CampaignID,
		"updated":     fields,
	}), nil
}

// AdsMetaGetCampaignInput defines input for getting a Meta campaign.
type AdsMetaGetCampaignInput struct {
	CampaignID string `json:"campaign_id" description:"Meta campaign ID to retrieve" required:"true"`
}

// AdsMetaGetCampaignTool retrieves a Meta campaign by ID.
func (t *MetaCampaignTools) AdsMetaGetCampaignTool(ctx context.Context, input AdsMetaGetCampaignInput) (*loop.ToolResult, error) {
	if input.CampaignID == "" {
		return loop.NewErrorResult("campaign_id is required"), nil
	}

	fields := []string{"id", "name", "objective", "status", "daily_budget", "lifetime_budget", "created_time", "updated_time", "start_time", "stop_time"}
	result, err := t.client.GetCampaign(ctx, input.CampaignID, fields)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get campaign: %v", err)), nil
	}

	return loop.NewSuccessResult(result), nil
}

// AdsMetaPauseCampaignInput defines input for pausing a Meta campaign.
type AdsMetaPauseCampaignInput struct {
	CampaignID string `json:"campaign_id" description:"Meta campaign ID to pause" required:"true"`
}

// AdsMetaPauseCampaignTool pauses a Meta campaign.
func (t *MetaCampaignTools) AdsMetaPauseCampaignTool(ctx context.Context, input AdsMetaPauseCampaignInput) (*loop.ToolResult, error) {
	if input.CampaignID == "" {
		return loop.NewErrorResult("campaign_id is required"), nil
	}

	_, err := t.client.PauseCampaign(ctx, input.CampaignID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to pause campaign: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"success":     true,
		"campaign_id": input.CampaignID,
		"status":      "PAUSED",
	}), nil
}
