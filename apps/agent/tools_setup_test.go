package main

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestToolEnabledForAgentDefaultsAndAliases(t *testing.T) {
	if !toolEnabledForAgent("skills", nil, false) {
		t.Fatalf("expected non-company_admin tool enabled by default when enabled list is nil")
	}
	if toolEnabledForAgent("company_admin", nil, false) {
		t.Fatalf("expected company_admin disabled by default for non-CEO")
	}
	if !toolEnabledForAgent("company_admin", nil, true) {
		t.Fatalf("expected company_admin enabled by default for CEO")
	}

	enabled := map[string]bool{
		"skills":           true,
		"wallet":           true,
		"company_commerce": true,
		"polymarket_trade": true,
	}
	if !toolEnabledForAgent("skills", enabled, false) {
		t.Fatalf("expected explicitly enabled skills group")
	}
	if !toolEnabledForAgent("company_finance", enabled, false) {
		t.Fatalf("expected company_finance enabled via wallet alias")
	}
	if !toolEnabledForAgent("shopify_read", enabled, false) {
		t.Fatalf("expected shopify_read enabled via company_commerce alias")
	}
	if !toolEnabledForAgent("shopify_write", enabled, false) {
		t.Fatalf("expected shopify_write enabled via company_commerce alias")
	}
	if !toolEnabledForAgent("polymarket_buy", enabled, false) {
		t.Fatalf("expected polymarket_buy enabled via polymarket_trade alias")
	}
	if !toolEnabledForAgent("polymarket_sell", enabled, false) {
		t.Fatalf("expected polymarket_sell enabled via polymarket_trade alias")
	}
	if toolEnabledForAgent("ads", enabled, false) {
		t.Fatalf("expected ads disabled when not configured")
	}
}

func TestDynamicMethodToolEnablement(t *testing.T) {
	if companyMethodToolEnabledForAgent("", map[string]bool{"x": true}) {
		t.Fatalf("expected empty company method tool name to be disabled")
	}
	if companyMethodToolEnabledForAgent("company_method_tool", nil) {
		t.Fatalf("expected dynamic company method tools disabled by default")
	}
	if !companyMethodToolEnabledForAgent("company_method_tool", map[string]bool{"company_method_tool": true}) {
		t.Fatalf("expected explicit company method tool enablement")
	}

	if deepResearchMethodToolEnabledForAgent("", map[string]bool{"deep_research": true}) {
		t.Fatalf("expected empty deep-research tool name to be disabled")
	}
	if deepResearchMethodToolEnabledForAgent("deep_research_method_tool", nil) {
		t.Fatalf("expected dynamic deep-research method tools disabled by default")
	}
	if !deepResearchMethodToolEnabledForAgent("deep_research_method_tool", map[string]bool{"deep_research": true}) {
		t.Fatalf("expected deep_research group to enable dynamic method tool")
	}
	if !deepResearchMethodToolEnabledForAgent("deep_research_method_tool", map[string]bool{"deep_research_method_tool": true}) {
		t.Fatalf("expected explicit deep-research method tool enablement")
	}
}

func TestCompanyMethodToolInputSchema(t *testing.T) {
	schema := companyMethodToolInputSchema(nil)
	if schema == nil || schema.Type != genai.TypeObject {
		t.Fatalf("expected default object schema, got %#v", schema)
	}

	schema = companyMethodToolInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_statement": map[string]any{"type": "string"},
		},
		"required": []any{"target_statement"},
	})
	if schema == nil {
		t.Fatalf("expected schema")
	}
	if schema.Type == "" {
		t.Fatalf("schema type should not be empty")
	}
	prop := schema.Properties["target_statement"]
	if prop == nil || !strings.EqualFold(string(prop.Type), "string") {
		t.Fatalf("expected target_statement string property, got %#v", prop)
	}
}

func TestMethodToolDescriptions(t *testing.T) {
	got := companyMethodToolDescription("fulfill_order", map[string]any{})
	if got != `Call company method "fulfill_order" on a teammate agent.` {
		t.Fatalf("unexpected default company method description: %q", got)
	}
	got = companyMethodToolDescription("fulfill_order", map[string]any{"description": "Handles fulfillment."})
	if got != `Call company method "fulfill_order" on a teammate agent. Handles fulfillment.` {
		t.Fatalf("unexpected custom company method description: %q", got)
	}

	got = deepResearchMethodToolDescription("truth_probability_calculator", map[string]any{})
	if !strings.HasPrefix(got, `Run deep research method "truth_probability_calculator".`) {
		t.Fatalf("unexpected default deep-research method description: %q", got)
	}
	if !strings.Contains(got, "EXPENSIVE") {
		t.Fatalf("expected cost warning in default deep-research method description, got %q", got)
	}
	got = deepResearchMethodToolDescription("truth_probability_calculator", map[string]any{"description": "Returns probability JSON."})
	if !strings.HasPrefix(got, `Run deep research method "truth_probability_calculator".`) {
		t.Fatalf("unexpected custom deep-research method prefix: %q", got)
	}
	if !strings.Contains(got, "Returns probability JSON.") {
		t.Fatalf("expected custom deep-research method description content, got %q", got)
	}
	if !strings.Contains(got, "EXPENSIVE") {
		t.Fatalf("unexpected custom deep-research method description: %q", got)
	}
}

func TestStringFromMap(t *testing.T) {
	value := stringFromMap(map[string]any{"k": "v"}, "k")
	if value != "v" {
		t.Fatalf("stringFromMap returned %q, want %q", value, "v")
	}
	if got := stringFromMap(map[string]any{"k": 1}, "k"); got != "" {
		t.Fatalf("expected empty string for non-string value, got %q", got)
	}
}
