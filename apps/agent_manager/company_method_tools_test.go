package main

import (
	"context"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestListCompanyMethodTools_PrefersCanonicalProviderMethodOverLegacyAlias(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	callerID := "company-method-caller-canonical"
	providerID := "company-method-provider-canonical"
	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "company-method-canonical", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, callerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(caller) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, providerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(provider) failed: %v", err)
	}

	displayMethod := "polymart_request_buy"
	legacyAlias := legacyCompanyMethodToolName(displayMethod)
	if err := db.Table(data.AgentCapability{}).Insert(ctx, &data.AgentCapability{
		ID:           "cap-company-method-legacy",
		AgentID:      providerID,
		Role:         "trader",
		Method:       legacyAlias,
		RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert legacy capability failed: %v", err)
	}
	if err := db.Table(data.AgentCapability{}).Insert(ctx, &data.AgentCapability{
		ID:           "cap-company-method-canonical",
		AgentID:      providerID,
		Role:         "trader",
		Method:       displayMethod,
		RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert canonical capability failed: %v", err)
	}

	specs, err := listCompanyMethodTools(ctx, db, callerID)
	if err != nil {
		t.Fatalf("listCompanyMethodTools failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one method tool spec, got %d", len(specs))
	}
	spec := specs[0]
	if spec.Method != displayMethod {
		t.Fatalf("expected normalized method %q, got %q", displayMethod, spec.Method)
	}
	if got := spec.targetMethodForProvider(providerID); got != displayMethod {
		t.Fatalf("expected canonical provider method %q, got %q", displayMethod, got)
	}
}

func TestListCompanyMethodTools_FallsBackToProviderMethodDefinition(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	callerID := "company-method-caller-fallback"
	providerID := "company-method-provider-fallback"
	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "company-method-fallback", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, callerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(caller) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, providerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(provider) failed: %v", err)
	}

	displayMethod := "legacy_buy_request"
	legacyAlias := legacyCompanyMethodToolName(displayMethod)
	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(
		ctx,
		legacyAlias,
		"Legacy fallback description",
		`{"type":"object","properties":{"amount":{"type":"number"}}}`,
		`{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if err := providerSvc.RegisterCapability(ctx, "trader", legacyAlias); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	specs, err := listCompanyMethodTools(ctx, db, callerID)
	if err != nil {
		t.Fatalf("listCompanyMethodTools failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one method tool spec, got %d", len(specs))
	}
	spec := specs[0]
	if spec.Method != displayMethod {
		t.Fatalf("expected normalized method %q, got %q", displayMethod, spec.Method)
	}
	if spec.Description != "Legacy fallback description" {
		t.Fatalf("expected fallback description from provider method definition, got %q", spec.Description)
	}
	if spec.InputSchema == nil || spec.OutputSchema == nil {
		t.Fatalf("expected schemas from provider method definition to be populated")
	}
	if got := spec.targetMethodForProvider(providerID); got != legacyAlias {
		t.Fatalf("expected provider target method %q, got %q", legacyAlias, got)
	}
}

func TestResolveCompanyMethodDefinition_UsesDisplayMethodFirst(t *testing.T) {
	defByName := map[string]data.A2AMethod{
		"display_method": {Method: "display_method", Description: "display"},
		"legacy_method":  {Method: "legacy_method", Description: "legacy"},
	}
	providerMethods := map[string]string{"provider-a": "legacy_method"}
	def, ok := resolveCompanyMethodDefinition("display_method", []string{"provider-a"}, providerMethods, defByName)
	if !ok {
		t.Fatalf("expected definition to resolve")
	}
	if def.Method != "display_method" {
		t.Fatalf("expected display method definition to win, got %q", def.Method)
	}
}

func TestResolveProviderTargetMethod(t *testing.T) {
	methodByName := map[string]data.A2AMethod{
		"canonical_method": {Method: "canonical_method"},
	}
	if got := resolveProviderTargetMethod("canonical_method", "legacy_alias", methodByName); got != "canonical_method" {
		t.Fatalf("expected canonical display method to be preferred, got %q", got)
	}
	if got := resolveProviderTargetMethod("missing_method", "legacy_alias", methodByName); got != "legacy_alias" {
		t.Fatalf("expected raw provider method fallback, got %q", got)
	}
	if got := resolveProviderTargetMethod("missing_method", "", methodByName); got != "missing_method" {
		t.Fatalf("expected display method fallback when provider method empty, got %q", got)
	}
}

func TestApplyCompanyMethodDefinitionPopulatesSchemas(t *testing.T) {
	spec := companyMethodToolSpec{}
	applyCompanyMethodDefinition(&spec, data.A2AMethod{
		Description:      "desc",
		InputSchemaJSON:  `{"type":"object","properties":{"id":{"type":"string"}}}`,
		OutputSchemaJSON: `{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
		UpdatedAt:        time.Now(),
	})
	if spec.Description != "desc" {
		t.Fatalf("expected description to be populated")
	}
	if spec.InputSchema == nil || spec.OutputSchema == nil {
		t.Fatalf("expected schemas to be parsed and populated")
	}
}
