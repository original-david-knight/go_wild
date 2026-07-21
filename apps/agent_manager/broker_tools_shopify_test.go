package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestCompanyShopifyClientForAgentRequiresMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "shopify-no-company"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	if _, _, err := h.companyShopifyClientForAgent(ctx, agentID); err == nil {
		t.Fatalf("expected company membership error")
	} else if !strings.Contains(err.Error(), "company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompanyShopifyClientForAgentRequiresConfiguredConnection(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "shopify-member-no-conn"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "shopify-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	if _, _, err := h.companyShopifyClientForAgent(ctx, agentID); err == nil {
		t.Fatalf("expected missing connection error")
	} else if !strings.Contains(err.Error(), "missing or disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompanyShopifyClientForAgentUsesCompanyConnection(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "shopify-member-with-conn"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "shopify-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if err := data.UpsertCompanyShopifyConnection(ctx, db, &data.CompanyShopifyConnection{
		CompanyID:        company.ID,
		ShopURL:          "demo.myshopify.com",
		APIVersion:       "2025-01",
		ClientID:         "client-abc",
		ClientSecretEnc:  "secret-abc",
		AccessTokenEnc:   "token-abc",
		AccessTokenExpAt: time.Now().Add(1 * time.Hour),
		Enabled:          true,
	}); err != nil {
		t.Fatalf("UpsertCompanyShopifyConnection failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	client, companyID, err := h.companyShopifyClientForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("companyShopifyClientForAgent failed: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil Shopify client")
	}
	if companyID != company.ID {
		t.Fatalf("expected companyID %q, got %q", company.ID, companyID)
	}
}

func TestCompanyCommerceShopifyToolNameRequiresMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "shopify-prefixed-no-company"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	_, err := h.callTool(ctx, agentID, agentSvc, "company_commerce_shopify_list_products", nil)
	if err == nil {
		t.Fatalf("expected company membership error")
	}
	if !strings.Contains(err.Error(), "company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompanyCommerceShopifyToolNameRequiresConfiguredConnection(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "shopify-prefixed-no-conn"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "shopify-prefixed-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	_, err = h.callTool(ctx, agentID, agentSvc, "company_commerce_shopify_list_products", nil)
	if err == nil {
		t.Fatalf("expected missing connection error")
	}
	if !strings.Contains(err.Error(), "missing or disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallShopifyToolsDispatchAndMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "shopify-dispatch-no-company"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	handled, result, err := h.callShopifyTools(ctx, agentID, "not_shopify_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error for unknown tool: %v", err)
	}
	if handled {
		t.Fatalf("expected unknown tool to be unhandled, got result=%#v", result)
	}

	handled, _, err = h.callShopifyTools(ctx, agentID, "shopify_list_products", nil)
	if !handled {
		t.Fatalf("expected shopify_list_products to be handled")
	}
	if err == nil {
		t.Fatalf("expected company membership error")
	}
	if !strings.Contains(err.Error(), "company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}
