package ads

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// MetaAdsetTools provides tools for managing Meta ad sets (audience targeting).
type MetaAdsetTools struct {
	client *MetaAdsClient
}

// NewMetaAdsetTools creates a new MetaAdsetTools instance.
func NewMetaAdsetTools(client *MetaAdsClient) *MetaAdsetTools {
	return &MetaAdsetTools{client: client}
}

// AdsMetaCreateAdsetInput defines input for creating a Meta ad set.
type AdsMetaCreateAdsetInput struct {
	CampaignID      string   `json:"campaign_id" description:"Parent campaign ID" required:"true"`
	Name            string   `json:"name" description:"Ad set name" required:"true"`
	DailyBudget     float64  `json:"daily_budget" description:"Daily budget in USD" required:"true"`
	BillingEvent    string   `json:"billing_event" description:"Billing event type" enum:"IMPRESSIONS,LINK_CLICKS,POST_ENGAGEMENT"`
	OptimizationGoal string  `json:"optimization_goal" description:"Optimization goal" enum:"REACH,IMPRESSIONS,LINK_CLICKS,LANDING_PAGE_VIEWS,CONVERSIONS,VALUE"`
	Status          string   `json:"status" description:"Ad set status" enum:"PAUSED,ACTIVE"`
	AgeMin          int      `json:"age_min" description:"Minimum target age (13-65)"`
	AgeMax          int      `json:"age_max" description:"Maximum target age (13-65)"`
	Genders         []int    `json:"genders" description:"Target genders (1=male, 2=female, 0=all)"`
	Interests       []string `json:"interests" description:"Interest targeting keywords"`
	Locations       []string `json:"locations" description:"Target country codes (e.g. US, GB, CA)"`
	StartTime       string   `json:"start_time" description:"Start time in ISO 8601 format"`
	EndTime         string   `json:"end_time" description:"End time in ISO 8601 format"`
}

// AdsMetaCreateAdsetTool creates a new Meta ad set.
func (t *MetaAdsetTools) AdsMetaCreateAdsetTool(ctx context.Context, input AdsMetaCreateAdsetInput) (*loop.ToolResult, error) {
	if input.CampaignID == "" {
		return loop.NewErrorResult("campaign_id is required"), nil
	}
	if input.Name == "" {
		return loop.NewErrorResult("name is required"), nil
	}
	if input.DailyBudget <= 0 {
		return loop.NewErrorResult("daily_budget must be positive"), nil
	}

	status := input.Status
	if status == "" {
		status = "PAUSED"
	}
	billingEvent := input.BillingEvent
	if billingEvent == "" {
		billingEvent = "IMPRESSIONS"
	}
	optimizationGoal := input.OptimizationGoal
	if optimizationGoal == "" {
		optimizationGoal = "LINK_CLICKS"
	}

	// Build targeting spec
	targeting := map[string]any{}
	if input.AgeMin > 0 {
		targeting["age_min"] = input.AgeMin
	}
	if input.AgeMax > 0 {
		targeting["age_max"] = input.AgeMax
	}
	if len(input.Genders) > 0 {
		targeting["genders"] = input.Genders
	}
	if len(input.Interests) > 0 {
		interests := make([]map[string]any, len(input.Interests))
		for i, interest := range input.Interests {
			interests[i] = map[string]any{"name": interest}
		}
		targeting["flexible_spec"] = []map[string]any{
			{"interests": interests},
		}
	}
	if len(input.Locations) > 0 {
		targeting["geo_locations"] = map[string]any{
			"countries": input.Locations,
		}
	}

	params := map[string]any{
		"campaign_id":       input.CampaignID,
		"name":              input.Name,
		"daily_budget":      int(input.DailyBudget * 100),
		"billing_event":     billingEvent,
		"optimization_goal": optimizationGoal,
		"status":            status,
		"targeting":         targeting,
	}
	if input.StartTime != "" {
		params["start_time"] = input.StartTime
	}
	if input.EndTime != "" {
		params["end_time"] = input.EndTime
	}

	result, err := t.client.CreateAdSet(ctx, params)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to create ad set: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"adset_id":     result["id"],
		"name":         input.Name,
		"campaign_id":  input.CampaignID,
		"daily_budget": input.DailyBudget,
		"status":       status,
	}), nil
}

// AdsMetaUpdateAdsetInput defines input for updating a Meta ad set.
type AdsMetaUpdateAdsetInput struct {
	AdsetID         string   `json:"adset_id" description:"Meta ad set ID to update" required:"true"`
	Name            string   `json:"name" description:"New ad set name"`
	DailyBudget     float64  `json:"daily_budget" description:"New daily budget in USD"`
	Status          string   `json:"status" description:"New ad set status" enum:"PAUSED,ACTIVE,ARCHIVED"`
	AgeMin          int      `json:"age_min" description:"New minimum target age"`
	AgeMax          int      `json:"age_max" description:"New maximum target age"`
	Interests       []string `json:"interests" description:"New interest targeting keywords"`
	Locations       []string `json:"locations" description:"New target country codes"`
}

// AdsMetaUpdateAdsetTool updates an existing Meta ad set.
func (t *MetaAdsetTools) AdsMetaUpdateAdsetTool(ctx context.Context, input AdsMetaUpdateAdsetInput) (*loop.ToolResult, error) {
	if input.AdsetID == "" {
		return loop.NewErrorResult("adset_id is required"), nil
	}

	fields := map[string]any{}
	if input.Name != "" {
		fields["name"] = input.Name
	}
	if input.DailyBudget > 0 {
		fields["daily_budget"] = int(input.DailyBudget * 100)
	}
	if input.Status != "" {
		fields["status"] = input.Status
	}

	// Build targeting update if any targeting fields changed
	targeting := map[string]any{}
	if input.AgeMin > 0 {
		targeting["age_min"] = input.AgeMin
	}
	if input.AgeMax > 0 {
		targeting["age_max"] = input.AgeMax
	}
	if len(input.Interests) > 0 {
		interests := make([]map[string]any, len(input.Interests))
		for i, interest := range input.Interests {
			interests[i] = map[string]any{"name": interest}
		}
		targeting["flexible_spec"] = []map[string]any{
			{"interests": interests},
		}
	}
	if len(input.Locations) > 0 {
		targeting["geo_locations"] = map[string]any{
			"countries": input.Locations,
		}
	}
	if len(targeting) > 0 {
		fields["targeting"] = targeting
	}

	if len(fields) == 0 {
		return loop.NewErrorResult("at least one field to update is required"), nil
	}

	result, err := t.client.UpdateAdSet(ctx, input.AdsetID, fields)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to update ad set: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"success":  result["success"],
		"adset_id": input.AdsetID,
		"updated":  fields,
	}), nil
}
