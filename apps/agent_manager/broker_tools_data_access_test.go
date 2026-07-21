package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestBrokerHistorySnapshotTools(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	agentID := "agent-1"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(context.Background()); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	ctx := context.Background()

	saveInput, _ := json.Marshal(map[string]any{"payload": "[1,2,3]"})
	if _, err := h.callTool(ctx, agentID, svc, "save_history_snapshot", saveInput); err != nil {
		t.Fatalf("save_history_snapshot failed: %v", err)
	}

	result, err := h.callTool(ctx, agentID, svc, "get_history_snapshot", nil)
	if err != nil {
		t.Fatalf("get_history_snapshot failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if resMap["payload"] != "[1,2,3]" {
		t.Fatalf("unexpected payload: %v", resMap["payload"])
	}

	if _, err := h.callTool(ctx, agentID, svc, "delete_history_snapshot", nil); err != nil {
		t.Fatalf("delete_history_snapshot failed: %v", err)
	}

	result, err = h.callTool(ctx, agentID, svc, "get_history_snapshot", nil)
	if err != nil {
		t.Fatalf("get_history_snapshot after delete failed: %v", err)
	}
	resMap, ok = result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type after delete: %T", result)
	}
	if resMap["payload"] != "" {
		t.Fatalf("expected empty payload after delete, got: %v", resMap["payload"])
	}
}

func TestBrokerHistorySummaryTools(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	agentID := "agent-summary-1"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(context.Background()); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	ctx := context.Background()

	// Save a summary
	saveInput, _ := json.Marshal(map[string]any{"content": "This is a conversation summary."})
	if _, err := h.callTool(ctx, agentID, svc, "save_history_summary", saveInput); err != nil {
		t.Fatalf("save_history_summary failed: %v", err)
	}

	// Get the summary
	result, err := h.callTool(ctx, agentID, svc, "get_history_summary", nil)
	if err != nil {
		t.Fatalf("get_history_summary failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if resMap["content"] != "This is a conversation summary." {
		t.Fatalf("unexpected content: %v", resMap["content"])
	}

	// Update the summary (upsert)
	updateInput, _ := json.Marshal(map[string]any{"content": "Updated summary."})
	if _, err := h.callTool(ctx, agentID, svc, "save_history_summary", updateInput); err != nil {
		t.Fatalf("save_history_summary (update) failed: %v", err)
	}

	result, err = h.callTool(ctx, agentID, svc, "get_history_summary", nil)
	if err != nil {
		t.Fatalf("get_history_summary after update failed: %v", err)
	}
	resMap, ok = result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type after update: %T", result)
	}
	if resMap["content"] != "Updated summary." {
		t.Fatalf("unexpected content after update: %v", resMap["content"])
	}

	// Delete the summary
	if _, err := h.callTool(ctx, agentID, svc, "delete_history_summary", nil); err != nil {
		t.Fatalf("delete_history_summary failed: %v", err)
	}

	result, err = h.callTool(ctx, agentID, svc, "get_history_summary", nil)
	if err != nil {
		t.Fatalf("get_history_summary after delete failed: %v", err)
	}
	resMap, ok = result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type after delete: %T", result)
	}
	if resMap["content"] != "" {
		t.Fatalf("expected empty content after delete, got: %v", resMap["content"])
	}
}

func TestBrokerGetWalletAddressesUsesCompanyScope(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	agentID := "agent-wallet-addresses"
	svc := data.NewAgentService(db, agentID)
	ctx := context.Background()

	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	if _, err := h.callTool(ctx, agentID, svc, "get_wallet_addresses", nil); err == nil {
		t.Fatalf("expected company membership error")
	} else if !strings.Contains(err.Error(), "company membership") {
		t.Fatalf("unexpected error: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "wallet-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	result, err := h.callTool(ctx, agentID, svc, "get_wallet_addresses", nil)
	if err != nil {
		t.Fatalf("get_wallet_addresses failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if got, _ := resMap["identity_scope"].(string); got != "company" {
		t.Fatalf("expected identity_scope company, got %q", got)
	}
	if got, _ := resMap["company_id"].(string); got != company.ID {
		t.Fatalf("expected company_id %q, got %q", company.ID, got)
	}
	if eth, _ := resMap["eth_address"].(string); strings.TrimSpace(eth) == "" {
		t.Fatalf("expected non-empty eth_address")
	}
	if sol, _ := resMap["sol_address"].(string); strings.TrimSpace(sol) == "" {
		t.Fatalf("expected non-empty sol_address")
	}

	companyResult, err := h.callTool(ctx, agentID, svc, "company_finance_get_wallet_addresses", nil)
	if err != nil {
		t.Fatalf("company_finance_get_wallet_addresses failed: %v", err)
	}
	companyMap, ok := companyResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected company_finance result type: %T", companyResult)
	}
	if got, _ := companyMap["identity_scope"].(string); got != "company" {
		t.Fatalf("expected identity_scope company for company_finance_get_wallet_addresses, got %q", got)
	}
	if got, _ := companyMap["company_id"].(string); got != company.ID {
		t.Fatalf("expected company_id %q for company_finance_get_wallet_addresses, got %q", company.ID, got)
	}
}

func TestBrokerGetCompanyContext(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	agentID := "agent-company-context"
	svc := data.NewAgentService(db, agentID)
	ctx := context.Background()

	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	result, err := h.callTool(ctx, agentID, svc, "get_company_context", nil)
	if err != nil {
		t.Fatalf("get_company_context failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if has, _ := resMap["has_company"].(bool); has {
		t.Fatalf("expected has_company=false for non-member")
	}

	company, err := data.CreateCompany(ctx, db, "ctx-co", "", agentID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	result, err = h.callTool(ctx, agentID, svc, "get_company_context", nil)
	if err != nil {
		t.Fatalf("get_company_context after membership failed: %v", err)
	}
	resMap, ok = result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type after membership: %T", result)
	}
	if has, _ := resMap["has_company"].(bool); !has {
		t.Fatalf("expected has_company=true")
	}
	if got, _ := resMap["company_id"].(string); got != company.ID {
		t.Fatalf("expected company_id %q, got %q", company.ID, got)
	}
	if got, _ := resMap["is_ceo"].(bool); !got {
		t.Fatalf("expected is_ceo=true")
	}
}

func TestBrokerGetCompanyMethodTools(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	callerID := "agent-company-methods-caller"
	providerID := "agent-company-methods-provider"
	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "company-methods-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, callerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(caller) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, providerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(provider) failed: %v", err)
	}

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(
		ctx,
		"fulfill_order",
		"Fulfill order",
		`{"type":"object","properties":{"order_id":{"type":"string"}}}`,
		`{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if err := providerSvc.RegisterCapability(ctx, "fulfillment", "fulfill_order"); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	result, err := h.callTool(ctx, callerID, callerSvc, "get_company_method_tools", nil)
	if err != nil {
		t.Fatalf("get_company_method_tools failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	rawTools, ok := resMap["tools"].([]map[string]any)
	if !ok {
		// callTool returns native Go values; tolerate generic []any if representation changes.
		fallback, okFallback := resMap["tools"].([]any)
		if !okFallback {
			t.Fatalf("expected tools array, got %T", resMap["tools"])
		}
		rawTools = make([]map[string]any, 0, len(fallback))
		for _, item := range fallback {
			row, okRow := item.(map[string]any)
			if !okRow {
				t.Fatalf("unexpected tool row type: %T", item)
			}
			rawTools = append(rawTools, row)
		}
	}
	if len(rawTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(rawTools))
	}
	toolRow := rawTools[0]
	if got, _ := toolRow["method"].(string); got != "fulfill_order" {
		t.Fatalf("expected method fulfill_order, got %q", got)
	}
	if got, _ := toolRow["tool_name"].(string); got != companyMethodToolName("fulfill_order") {
		t.Fatalf("unexpected tool_name %q", got)
	}
}

func TestBrokerGetCompanyMethodTools_NormalizesLegacyNames(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	callerID := "agent-company-methods-caller-legacy"
	providerID := "agent-company-methods-provider-legacy"
	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "company-methods-legacy", "", "")
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
	legacyMethod := legacyCompanyMethodToolName(displayMethod)
	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(
		ctx,
		legacyMethod,
		"Legacy method description",
		`{"type":"object","properties":{"amount":{"type":"number"}}}`,
		`{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod(legacy) failed: %v", err)
	}
	if err := providerSvc.RegisterCapability(ctx, "trader", legacyMethod); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	result, err := h.callTool(ctx, callerID, callerSvc, "get_company_method_tools", nil)
	if err != nil {
		t.Fatalf("get_company_method_tools failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	rawToolsAny, okAny := resMap["tools"].([]any)
	rawToolsMap, okMap := resMap["tools"].([]map[string]any)
	var toolRow map[string]any
	switch {
	case okAny && len(rawToolsAny) == 1:
		row, ok := rawToolsAny[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected tool row type: %T", rawToolsAny[0])
		}
		toolRow = row
	case okMap && len(rawToolsMap) == 1:
		toolRow = rawToolsMap[0]
	default:
		t.Fatalf("expected one tool row, got %#v", resMap["tools"])
	}
	if got, _ := toolRow["method"].(string); got != displayMethod {
		t.Fatalf("expected normalized method %q, got %q", displayMethod, got)
	}
	if got, _ := toolRow["tool_name"].(string); got != displayMethod {
		t.Fatalf("expected normalized tool_name %q, got %q", displayMethod, got)
	}
	switch aliases := toolRow["legacy_tool_names"].(type) {
	case []any:
		if len(aliases) != 1 {
			t.Fatalf("expected one legacy tool alias, got %#v", aliases)
		}
		if got, _ := aliases[0].(string); got != legacyMethod {
			t.Fatalf("expected legacy alias %q, got %q", legacyMethod, got)
		}
	case []string:
		if len(aliases) != 1 {
			t.Fatalf("expected one legacy tool alias, got %#v", aliases)
		}
		if aliases[0] != legacyMethod {
			t.Fatalf("expected legacy alias %q, got %q", legacyMethod, aliases[0])
		}
	default:
		t.Fatalf("expected one legacy tool alias, got %#v", toolRow["legacy_tool_names"])
	}
}

func TestBrokerGetDeepResearchMethodTools_ListsEnabledOnly(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	agentID := "agent-deep-research-tools"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_market_landscape",
		"Research market landscape",
		"",
		"Analyze {{topic}} market trends",
		`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":2,"search_results_per_query":3}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod(enabled) failed: %v", err)
	}
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_disabled",
		"Disabled method",
		"",
		"Analyze {{topic}}",
		`{"type":"object","properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":1}`,
		false,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod(disabled) failed: %v", err)
	}

	result, err := h.callTool(ctx, agentID, svc, "get_deep_research_method_tools", nil)
	if err != nil {
		t.Fatalf("get_deep_research_method_tools failed: %v", err)
	}
	resMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}

	rawToolsAny, okAny := resMap["tools"].([]any)
	rawToolsMap, okMap := resMap["tools"].([]map[string]any)
	var tools []map[string]any
	switch {
	case okAny:
		tools = make([]map[string]any, 0, len(rawToolsAny))
		for _, raw := range rawToolsAny {
			row, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("unexpected tool row type: %T", raw)
			}
			tools = append(tools, row)
		}
	case okMap:
		tools = append(tools, rawToolsMap...)
	default:
		t.Fatalf("expected tools array, got %T", resMap["tools"])
	}

	if len(tools) != 1 {
		t.Fatalf("expected 1 enabled deep-research tool, got %d", len(tools))
	}
	tool := tools[0]
	if got, _ := tool["tool_name"].(string); got != "deep_research_market_landscape" {
		t.Fatalf("expected tool_name deep_research_market_landscape, got %q", got)
	}
	if got, _ := tool["method"].(string); got != "deep_research_market_landscape" {
		t.Fatalf("expected method deep_research_market_landscape, got %q", got)
	}
	if provider, _ := tool["provider"].(string); provider != "deep_research" {
		t.Fatalf("expected provider deep_research, got %q", provider)
	}
	if _, ok := tool["input_schema"]; !ok {
		t.Fatalf("expected input_schema in deep-research tool payload")
	}
}

func TestBrokerGetCapabilitiesIncludesMethodInstructions(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	agentID := "agent-capability-hints"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethodWithInstructions(
		ctx,
		"reconcile_orders",
		"Reconcile orders",
		"Always verify order totals before marking reconciled.",
		`{"type":"object","properties":{"order_id":{"type":"string"}}}`,
		`{"type":"object","properties":{"status":{"type":"string"}}}`,
	); err != nil {
		t.Fatalf("CreateA2AMethodWithInstructions failed: %v", err)
	}
	if err := svc.RegisterCapability(ctx, "ops", "reconcile_orders"); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	result, err := h.callTool(ctx, agentID, svc, "get_capabilities", nil)
	if err != nil {
		t.Fatalf("get_capabilities failed: %v", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}

	rawCapsAny, okAny := resultMap["capabilities"].([]any)
	caps := make([]map[string]any, 0, 1)
	if okAny {
		for _, raw := range rawCapsAny {
			row, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("unexpected capability row type: %T", raw)
			}
			caps = append(caps, row)
		}
	} else if rawCapsMap, okMap := resultMap["capabilities"].([]map[string]any); okMap {
		caps = append(caps, rawCapsMap...)
	}
	if len(caps) != 1 {
		t.Fatalf("expected exactly one capability, got %#v", resultMap["capabilities"])
	}
	capRow := caps[0]
	if got, _ := capRow["instructions"].(string); !strings.Contains(got, "verify order totals") {
		t.Fatalf("expected instructions in capability payload, got %q", got)
	}
}
