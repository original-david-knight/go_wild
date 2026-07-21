package main

import (
	"context"
	"fmt"
	"os"

	"github.com/original-david-knight/go_wild/tools/ads"
)

type adsToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error)

var adsToolHandlers = map[string]adsToolHandlerFunc{
	// Meta campaigns
	"ads_meta_create_campaign": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaCampaignTools(client)
		return callWithInput[ads.AdsMetaCreateCampaignInput](inputJSON, func(input ads.AdsMetaCreateCampaignInput) (any, error) {
			r, err := tools.AdsMetaCreateCampaignTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_meta_update_campaign": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaCampaignTools(client)
		return callWithInput[ads.AdsMetaUpdateCampaignInput](inputJSON, func(input ads.AdsMetaUpdateCampaignInput) (any, error) {
			r, err := tools.AdsMetaUpdateCampaignTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_meta_get_campaign": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaCampaignTools(client)
		return callWithInput[ads.AdsMetaGetCampaignInput](inputJSON, func(input ads.AdsMetaGetCampaignInput) (any, error) {
			r, err := tools.AdsMetaGetCampaignTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_meta_pause_campaign": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaCampaignTools(client)
		return callWithInput[ads.AdsMetaPauseCampaignInput](inputJSON, func(input ads.AdsMetaPauseCampaignInput) (any, error) {
			r, err := tools.AdsMetaPauseCampaignTool(ctx, input)
			return toolResultContent(r, err)
		})
	},

	// Meta ad sets
	"ads_meta_create_adset": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaAdsetTools(client)
		return callWithInput[ads.AdsMetaCreateAdsetInput](inputJSON, func(input ads.AdsMetaCreateAdsetInput) (any, error) {
			r, err := tools.AdsMetaCreateAdsetTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_meta_update_adset": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaAdsetTools(client)
		return callWithInput[ads.AdsMetaUpdateAdsetInput](inputJSON, func(input ads.AdsMetaUpdateAdsetInput) (any, error) {
			r, err := tools.AdsMetaUpdateAdsetTool(ctx, input)
			return toolResultContent(r, err)
		})
	},

	// Meta creatives
	"ads_meta_create_ad": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaCreativeTools(client)
		return callWithInput[ads.AdsMetaCreateAdInput](inputJSON, func(input ads.AdsMetaCreateAdInput) (any, error) {
			r, err := tools.AdsMetaCreateAdTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_meta_get_ad_performance": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredMetaAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewMetaCreativeTools(client)
		return callWithInput[ads.AdsMetaGetAdPerformanceInput](inputJSON, func(input ads.AdsMetaGetAdPerformanceInput) (any, error) {
			r, err := tools.AdsMetaGetAdPerformanceTool(ctx, input)
			return toolResultContent(r, err)
		})
	},

	// Google campaigns
	"ads_google_create_campaign": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredGoogleAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewGoogleCampaignTools(client)
		return callWithInput[ads.AdsGoogleCreateCampaignInput](inputJSON, func(input ads.AdsGoogleCreateCampaignInput) (any, error) {
			r, err := tools.AdsGoogleCreateCampaignTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_google_update_campaign": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		client, err := h.requiredGoogleAdsClient()
		if err != nil {
			return nil, err
		}
		tools := ads.NewGoogleCampaignTools(client)
		return callWithInput[ads.AdsGoogleUpdateCampaignInput](inputJSON, func(input ads.AdsGoogleUpdateCampaignInput) (any, error) {
			r, err := tools.AdsGoogleUpdateCampaignTool(ctx, input)
			return toolResultContent(r, err)
		})
	},

	// Budget/spend tools
	"ads_get_daily_spend": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		tools := ads.NewBudgetTools(h.metaAdsClient(), h.googleAdsClient())
		return callWithInput[ads.AdsGetDailySpendInput](inputJSON, func(input ads.AdsGetDailySpendInput) (any, error) {
			r, err := tools.AdsGetDailySpendTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"ads_get_campaign_roas": func(h *BrokerToolsHandler, ctx context.Context, inputJSON []byte) (any, error) {
		tools := ads.NewBudgetTools(h.metaAdsClient(), h.googleAdsClient())
		return callWithInput[ads.AdsGetCampaignRoasInput](inputJSON, func(input ads.AdsGetCampaignRoasInput) (any, error) {
			r, err := tools.AdsGetCampaignRoasTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func (h *BrokerToolsHandler) callAdsTools(ctx context.Context, toolName string, inputJSON []byte) (bool, any, error) {
	if !isAdsTool(toolName) {
		return false, nil, nil
	}

	handler, ok := adsToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, inputJSON)
	return true, result, err
}

func isAdsTool(toolName string) bool {
	_, ok := adsToolHandlers[toolName]
	return ok
}

var (
	errMetaAdsNotConfigured   = fmt.Errorf("Meta Ads not configured: set META_ADS_ACCESS_TOKEN and META_ADS_ACCOUNT_ID environment variables")
	errGoogleAdsNotConfigured = fmt.Errorf("Google Ads not configured: set GOOGLE_ADS_DEVELOPER_TOKEN and GOOGLE_ADS_CUSTOMER_ID environment variables")
)

func (h *BrokerToolsHandler) requiredMetaAdsClient() (*ads.MetaAdsClient, error) {
	client := h.metaAdsClient()
	if client == nil {
		return nil, errMetaAdsNotConfigured
	}
	return client, nil
}

func (h *BrokerToolsHandler) requiredGoogleAdsClient() (*ads.GoogleAdsClient, error) {
	client := h.googleAdsClient()
	if client == nil {
		return nil, errGoogleAdsNotConfigured
	}
	return client, nil
}

// metaAdsClient creates a Meta Ads client from environment variables.
// Returns nil if not configured.
func (h *BrokerToolsHandler) metaAdsClient() *ads.MetaAdsClient {
	token := os.Getenv("META_ADS_ACCESS_TOKEN")
	accountID := os.Getenv("META_ADS_ACCOUNT_ID")
	if token == "" || accountID == "" {
		return nil
	}
	pixelID := os.Getenv("META_ADS_PIXEL_ID")
	return ads.NewMetaAdsClient(token, accountID, pixelID)
}

// googleAdsClient creates a Google Ads client from environment variables.
// Returns nil if not configured.
func (h *BrokerToolsHandler) googleAdsClient() *ads.GoogleAdsClient {
	devToken := os.Getenv("GOOGLE_ADS_DEVELOPER_TOKEN")
	customerID := os.Getenv("GOOGLE_ADS_CUSTOMER_ID")
	if devToken == "" || customerID == "" {
		return nil
	}
	refreshToken := os.Getenv("GOOGLE_ADS_REFRESH_TOKEN")
	return ads.NewGoogleAdsClient(devToken, customerID, refreshToken)
}
