package data

import (
	"context"
	"testing"
)

func TestCompanyWebhookConfigUpsertAndLookup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	company, err := CreateCompany(ctx, db, "Webhook Config Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	cfg := &WebhookConfig{
		CompanyID:    company.ID,
		Source:       "shopify",
		Event:        "orders/create",
		EventPath:    "orders_create",
		TargetRole:   "fulfiller",
		TargetMethod: "fulfill_order",
		HMACSecret:   "shpss",
		Enabled:      true,
	}
	if err := UpsertCompanyWebhookConfig(ctx, db, cfg); err != nil {
		t.Fatalf("UpsertCompanyWebhookConfig(insert) failed: %v", err)
	}

	got, err := GetCompanyWebhookConfigByPath(ctx, db, company.ID, "shopify", "orders_create")
	if err != nil {
		t.Fatalf("GetCompanyWebhookConfigByPath failed: %v", err)
	}
	if got == nil {
		t.Fatalf("expected webhook config")
	}
	if got.TargetMethod != "fulfill_order" {
		t.Fatalf("unexpected target method: %q", got.TargetMethod)
	}

	cfg.HMACSecret = "shpss-updated"
	cfg.Enabled = false
	if err := UpsertCompanyWebhookConfig(ctx, db, cfg); err != nil {
		t.Fatalf("UpsertCompanyWebhookConfig(update) failed: %v", err)
	}
	list, err := ListCompanyWebhookConfigs(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("ListCompanyWebhookConfigs failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one webhook config, got %d", len(list))
	}
	if list[0].HMACSecret != "shpss-updated" || list[0].Enabled != false {
		t.Fatalf("unexpected updated webhook config: %+v", list[0])
	}
}
