package main

import (
	"context"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools/supplier/providers"
)

func TestSupplierClientForAgentUsesCompanyTopDawgConnection(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "supplier-member-with-conn"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "supplier-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if err := data.UpsertCompanyTopDawgConnection(ctx, db, &data.CompanyTopDawgConnection{
		CompanyID:  company.ID,
		APIKeyEnc:  "td-key",
		SupplierID: "td-supplier",
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertCompanyTopDawgConnection failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	client, companyID, err := h.supplierClientForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("supplierClientForAgent failed: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil supplier client")
	}
	if companyID != company.ID {
		t.Fatalf("expected companyID %q, got %q", company.ID, companyID)
	}
}

func TestSupplierClientForAgentRejectsDisabledCompanyTopDawgConnection(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "supplier-member-disabled-conn"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "supplier-co-disabled", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if err := data.UpsertCompanyTopDawgConnection(ctx, db, &data.CompanyTopDawgConnection{
		CompanyID:  company.ID,
		APIKeyEnc:  "td-key",
		SupplierID: "td-supplier",
		Enabled:    false,
	}); err != nil {
		t.Fatalf("UpsertCompanyTopDawgConnection failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	if _, _, err := h.supplierClientForAgent(ctx, agentID); err == nil {
		t.Fatalf("expected disabled connection error")
	} else if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSupplierClientForAgentUsesCompanyCJDropshippingConnection(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "supplier-member-with-cj"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "supplier-cj-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if err := data.UpsertCompanyCJDropshippingConnection(ctx, db, &data.CompanyCJDropshippingConnection{
		CompanyID:      company.ID,
		APIKeyEnc:      "cj-key",
		AccessTokenEnc: "cj-access-token",
		Enabled:        true,
	}); err != nil {
		t.Fatalf("UpsertCompanyCJDropshippingConnection failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	client, companyID, err := h.supplierClientForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("supplierClientForAgent failed: %v", err)
	}
	if companyID != company.ID {
		t.Fatalf("expected companyID %q, got %q", company.ID, companyID)
	}
	if _, ok := client.(*providers.CJDropshipping); !ok {
		t.Fatalf("expected cjdropshipping provider, got %T", client)
	}
}

func TestSupplierClientForAgentFallsBackToTopDawgWhenCJDisabled(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	agentID := "supplier-member-cj-disabled"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "supplier-cj-disabled-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if err := data.UpsertCompanyCJDropshippingConnection(ctx, db, &data.CompanyCJDropshippingConnection{
		CompanyID:      company.ID,
		APIKeyEnc:      "cj-key",
		AccessTokenEnc: "cj-access-token",
		Enabled:        false,
	}); err != nil {
		t.Fatalf("UpsertCompanyCJDropshippingConnection failed: %v", err)
	}
	if err := data.UpsertCompanyTopDawgConnection(ctx, db, &data.CompanyTopDawgConnection{
		CompanyID:  company.ID,
		APIKeyEnc:  "td-key",
		SupplierID: "td-supplier",
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertCompanyTopDawgConnection failed: %v", err)
	}

	h := NewBrokerToolsHandler(db)
	client, companyID, err := h.supplierClientForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("supplierClientForAgent failed: %v", err)
	}
	if companyID != company.ID {
		t.Fatalf("expected companyID %q, got %q", company.ID, companyID)
	}
	if _, ok := client.(*providers.TopDawg); !ok {
		t.Fatalf("expected topdawg provider, got %T", client)
	}
}

func TestCallSupplierToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	handled, result, err := h.callSupplierTools(ctx, "supplier-agent-unknown", "not_a_supplier_tool", nil)
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

func TestCallSupplierToolsRecognizedToolWithoutConfigReturnsHandledError(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "supplier-agent-missing-config"

	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	t.Setenv("SUPPLIER_DEFAULT_PROVIDER", "topdawg")
	t.Setenv("TOPDAWG_API_KEY", "")
	t.Setenv("TOPDAWG_SUPPLIER_ID", "")

	handled, result, err := h.callSupplierTools(ctx, agentID, "supplier_get_product", []byte(`{"product_id":"x"}`))
	if !handled {
		t.Fatalf("expected recognized supplier tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result when client resolution fails, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected missing configuration error")
	}
	if !strings.Contains(err.Error(), "TOPDAWG_API_KEY not set") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsSupplierToolRecognition(t *testing.T) {
	if !isSupplierTool("supplier_search_products") {
		t.Fatalf("expected supplier_search_products to be recognized")
	}
	if isSupplierTool("supplier_not_real") {
		t.Fatalf("unexpectedly recognized unknown supplier tool")
	}
}

func TestGetSupplierClientFromEnvDefaultsToTopDawg(t *testing.T) {
	t.Setenv("SUPPLIER_DEFAULT_PROVIDER", "")
	t.Setenv("TOPDAWG_API_KEY", "td-key")
	t.Setenv("TOPDAWG_SUPPLIER_ID", "supplier-1")

	client, err := getSupplierClientFromEnv()
	if err != nil {
		t.Fatalf("getSupplierClientFromEnv failed: %v", err)
	}
	if _, ok := client.(*providers.TopDawg); !ok {
		t.Fatalf("expected TopDawg client, got %T", client)
	}
}

func TestGetSupplierClientFromEnvCJRequiresAccessToken(t *testing.T) {
	t.Setenv("SUPPLIER_DEFAULT_PROVIDER", "cjdropshipping")
	t.Setenv("CJDROPSHIPPING_ACCESS_TOKEN", "")
	t.Setenv("CJDROPSHIPPING_PLATFORM_TOKEN", "")

	_, err := getSupplierClientFromEnv()
	if err == nil {
		t.Fatalf("expected missing CJ token error")
	}
	if !strings.Contains(err.Error(), "CJDROPSHIPPING_ACCESS_TOKEN not set") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetSupplierClientFromEnvUnknownProvider(t *testing.T) {
	t.Setenv("SUPPLIER_DEFAULT_PROVIDER", "not-real")

	_, err := getSupplierClientFromEnv()
	if err == nil {
		t.Fatalf("expected unknown provider error")
	}
	if !strings.Contains(err.Error(), "unknown supplier provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
