package ads

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// MetaCreativeTools provides tools for managing Meta ad creatives and performance.
type MetaCreativeTools struct {
	client *MetaAdsClient
}

// NewMetaCreativeTools creates a new MetaCreativeTools instance.
func NewMetaCreativeTools(client *MetaAdsClient) *MetaCreativeTools {
	return &MetaCreativeTools{client: client}
}

// AdsMetaCreateAdInput defines input for creating a Meta ad.
type AdsMetaCreateAdInput struct {
	AdsetID      string `json:"adset_id" description:"Parent ad set ID" required:"true"`
	Name         string `json:"name" description:"Ad name" required:"true"`
	Headline     string `json:"headline" description:"Ad headline text" required:"true"`
	Body         string `json:"body" description:"Ad body/primary text" required:"true"`
	ImageURL     string `json:"image_url" description:"URL of the ad image"`
	LinkURL      string `json:"link_url" description:"Destination URL when ad is clicked" required:"true"`
	CallToAction string `json:"call_to_action" description:"Call to action button" enum:"SHOP_NOW,LEARN_MORE,SIGN_UP,BUY_NOW,GET_OFFER,ORDER_NOW,SUBSCRIBE"`
	Status       string `json:"status" description:"Ad status" enum:"PAUSED,ACTIVE"`
}

// AdsMetaCreateAdTool creates a new Meta ad.
func (t *MetaCreativeTools) AdsMetaCreateAdTool(ctx context.Context, input AdsMetaCreateAdInput) (*loop.ToolResult, error) {
	if input.AdsetID == "" {
		return loop.NewErrorResult("adset_id is required"), nil
	}
	if input.Name == "" {
		return loop.NewErrorResult("name is required"), nil
	}
	if input.LinkURL == "" {
		return loop.NewErrorResult("link_url is required"), nil
	}

	status := input.Status
	if status == "" {
		status = "PAUSED"
	}
	callToAction := input.CallToAction
	if callToAction == "" {
		callToAction = "SHOP_NOW"
	}

	// Build creative spec
	creative := map[string]any{
		"object_story_spec": map[string]any{
			"link_data": map[string]any{
				"message":        input.Body,
				"name":           input.Headline,
				"link":           input.LinkURL,
				"call_to_action": map[string]any{"type": callToAction},
			},
		},
	}
	if input.ImageURL != "" {
		creative["object_story_spec"].(map[string]any)["link_data"].(map[string]any)["picture"] = input.ImageURL
	}

	params := map[string]any{
		"adset_id": input.AdsetID,
		"name":     input.Name,
		"status":   status,
		"creative": creative,
	}

	result, err := t.client.CreateAd(ctx, params)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to create ad: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"ad_id":    result["id"],
		"name":     input.Name,
		"adset_id": input.AdsetID,
		"status":   status,
	}), nil
}

// AdsMetaGetAdPerformanceInput defines input for getting ad performance.
type AdsMetaGetAdPerformanceInput struct {
	ObjectID   string `json:"object_id" description:"Ad ID or Campaign ID to get metrics for" required:"true"`
	DatePreset string `json:"date_preset" description:"Date range for metrics" enum:"today,yesterday,this_month,last_7d,last_14d,last_30d,last_90d"`
}

// AdsMetaGetAdPerformanceTool retrieves performance metrics for a Meta ad or campaign.
func (t *MetaCreativeTools) AdsMetaGetAdPerformanceTool(ctx context.Context, input AdsMetaGetAdPerformanceInput) (*loop.ToolResult, error) {
	if input.ObjectID == "" {
		return loop.NewErrorResult("object_id is required"), nil
	}

	datePreset := input.DatePreset
	if datePreset == "" {
		datePreset = "last_7d"
	}

	fields := []string{"impressions", "clicks", "spend", "cpc", "cpm", "ctr", "actions", "cost_per_action_type", "purchase_roas"}
	result, err := t.client.GetAdInsights(ctx, input.ObjectID, fields, datePreset)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get ad performance: %v", err)), nil
	}

	return loop.NewSuccessResult(result), nil
}
