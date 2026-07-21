package main

import (
	"context"
	"strings"
	"testing"
)

func TestCallAdsToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))
	handled, result, err := h.callAdsTools(context.Background(), "not_a_real_ads_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestCallAdsToolsMetaToolRequiresConfig(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))
	t.Setenv("META_ADS_ACCESS_TOKEN", "")
	t.Setenv("META_ADS_ACCOUNT_ID", "")

	handled, result, err := h.callAdsTools(context.Background(), "ads_meta_get_campaign", []byte(`{"campaign_id":"c1"}`))
	if !handled {
		t.Fatalf("expected recognized ads_meta tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result for missing config, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected missing Meta config error")
	}
	if !strings.Contains(err.Error(), "Meta Ads not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallAdsToolsGoogleToolRequiresConfig(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))
	t.Setenv("GOOGLE_ADS_DEVELOPER_TOKEN", "")
	t.Setenv("GOOGLE_ADS_CUSTOMER_ID", "")

	handled, result, err := h.callAdsTools(context.Background(), "ads_google_update_campaign", []byte(`{"resource_name":"x","status":"PAUSED"}`))
	if !handled {
		t.Fatalf("expected recognized ads_google tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result for missing config, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected missing Google config error")
	}
	if !strings.Contains(err.Error(), "Google Ads not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallAdsToolsDailySpendHandlesMissingConfigsGracefully(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))
	t.Setenv("META_ADS_ACCESS_TOKEN", "")
	t.Setenv("META_ADS_ACCOUNT_ID", "")
	t.Setenv("GOOGLE_ADS_DEVELOPER_TOKEN", "")
	t.Setenv("GOOGLE_ADS_CUSTOMER_ID", "")

	handled, result, err := h.callAdsTools(context.Background(), "ads_get_daily_spend", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected ads_get_daily_spend to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if resultMap["meta"] != "not configured" {
		t.Fatalf("expected meta=not configured, got %#v", resultMap["meta"])
	}
	if resultMap["google"] != "not configured" {
		t.Fatalf("expected google=not configured, got %#v", resultMap["google"])
	}
}

func TestIsAdsToolRecognition(t *testing.T) {
	if !isAdsTool("ads_meta_create_campaign") {
		t.Fatalf("expected known ads tool to be recognized")
	}
	if isAdsTool("ads_not_real") {
		t.Fatalf("expected unknown ads tool to be rejected")
	}
}
