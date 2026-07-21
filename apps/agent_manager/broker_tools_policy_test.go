package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestBrokerToolPolicy_DeniesDisabledGroup(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-agent-disabled-group"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	allowed, err := h.isToolCallAllowed(ctx, agentID, svc, "company_knowledge_search")
	if err != nil {
		t.Fatalf("isToolCallAllowed returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected company_knowledge_search to be denied when group is disabled")
	}
}

func TestBrokerToolPolicy_ExecutionMethodDisabledGroupsOverrideAgentEnablement(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-execution-method-disabled"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"wallet", "polymarket_buy"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	if _, err := data.NewAgentService(db, "system").CreateA2AMethodWithConfig(
		ctx,
		"market_review",
		"Review market",
		"",
		`{"type":"object"}`,
		`{"type":"object"}`,
		false,
		false,
		false,
		false,
		false,
		data.WithDisabledToolGroups([]string{"wallet", "polymarket_buy"}),
	); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	methodCtx := context.WithValue(ctx, brokerExecutionMethodKey, "market_review")

	allowed, err := h.isToolCallAllowed(methodCtx, agentID, svc, "get_wallet_addresses")
	if err != nil {
		t.Fatalf("isToolCallAllowed(get_wallet_addresses) returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected execution method to disable wallet tools even when the agent enables wallet")
	}

	allowed, err = h.isToolCallAllowed(methodCtx, agentID, svc, "company_finance_polymarket_place_buy_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(company_finance_polymarket_place_buy_order) returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected execution method to disable polymarket_buy tools even when the agent enables polymarket_buy")
	}

	allowed, err = h.isToolCallAllowed(ctx, agentID, svc, "get_wallet_addresses")
	if err != nil {
		t.Fatalf("isToolCallAllowed(get_wallet_addresses) without execution method returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected wallet tool to remain allowed outside the method-scoped execution context")
	}
}

func TestBrokerToolPolicy_CompanyMethodToolsRequireExplicitEnable(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	callerID := "policy-company-method-caller"
	providerID := "policy-company-method-provider"
	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "policy-company-method", "", "")
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

	toolName := companyMethodToolName("fulfill_order")
	allowed, err := h.isToolCallAllowed(ctx, callerID, callerSvc, toolName)
	if err != nil {
		t.Fatalf("isToolCallAllowed returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected company method tool to be denied when enabled_tools is nil")
	}

	agent, err := callerSvc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{toolName})
	if err := callerSvc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	allowed, err = h.isToolCallAllowed(ctx, callerID, callerSvc, toolName)
	if err != nil {
		t.Fatalf("isToolCallAllowed returned error after enable: %v", err)
	}
	if !allowed {
		t.Fatalf("expected explicitly enabled company method tool to be allowed")
	}
}

func TestBrokerToolPolicy_CompanyMethodToolsLegacyEnableAlias(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	callerID := "policy-company-method-caller-legacy"
	providerID := "policy-company-method-provider-legacy"
	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "policy-company-method-legacy", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, callerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(caller) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, providerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(provider) failed: %v", err)
	}

	method := "polymart_request_buy"
	if _, err := data.NewAgentService(db, "system").CreateA2AMethod(
		ctx,
		method,
		"Buy request",
		`{"type":"object"}`,
		`{"type":"object"}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if err := providerSvc.RegisterCapability(ctx, "trader", method); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	legacyAlias := legacyCompanyMethodToolName(method)
	caller, err := callerSvc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent(caller) failed: %v", err)
	}
	caller.SetEnabledTools([]string{legacyAlias})
	if err := callerSvc.UpdateAgent(ctx, caller); err != nil {
		t.Fatalf("UpdateAgent(caller) failed: %v", err)
	}

	allowed, err := h.isToolCallAllowed(ctx, callerID, callerSvc, method)
	if err != nil {
		t.Fatalf("isToolCallAllowed returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected legacy enabled alias %q to allow tool %q", legacyAlias, method)
	}
}

func TestBrokerToolPolicy_CompanyAdminDefaultCEOOnly(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	ceoID := "policy-ceo"
	memberID := "policy-member"
	ceoSvc := data.NewAgentService(db, ceoID)
	memberSvc := data.NewAgentService(db, memberID)
	if _, err := ceoSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(ceo) failed: %v", err)
	}
	if _, err := memberSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(member) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "policy-company", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, memberID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(member) failed: %v", err)
	}

	ceoAllowed, err := h.isToolCallAllowed(ctx, ceoID, ceoSvc, "company_admin_list_members")
	if err != nil {
		t.Fatalf("isToolCallAllowed(ceo) returned error: %v", err)
	}
	if !ceoAllowed {
		t.Fatalf("expected CEO to be allowed company_admin by default")
	}

	memberAllowed, err := h.isToolCallAllowed(ctx, memberID, memberSvc, "company_admin_list_members")
	if err != nil {
		t.Fatalf("isToolCallAllowed(member) returned error: %v", err)
	}
	if memberAllowed {
		t.Fatalf("expected non-CEO member to be denied company_admin by default")
	}
}

func TestBrokerToolPolicy_ExplicitCompanyAdminEnableOverridesDefault(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	ceoID := "policy-ceo-explicit"
	memberID := "policy-member-explicit"
	ceoSvc := data.NewAgentService(db, ceoID)
	memberSvc := data.NewAgentService(db, memberID)
	if _, err := ceoSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(ceo) failed: %v", err)
	}
	memberAgent, err := memberSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent(member) failed: %v", err)
	}
	memberAgent.SetEnabledTools([]string{"company_admin"})
	if err := memberSvc.UpdateAgent(ctx, memberAgent); err != nil {
		t.Fatalf("UpdateAgent(member) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "policy-company-explicit", "", ceoID)
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, memberID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(member) failed: %v", err)
	}

	allowed, err := h.isToolCallAllowed(ctx, memberID, memberSvc, "company_admin_list_members")
	if err != nil {
		t.Fatalf("isToolCallAllowed returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected explicit company_admin enable to allow non-CEO member")
	}
}

func TestBrokerToolPolicy_AliasGroupsEnableCompanyScopedTools(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	commerceAgentID := "policy-commerce"
	commerceSvc := data.NewAgentService(db, commerceAgentID)
	commerceAgent, err := commerceSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent(commerce) failed: %v", err)
	}
	commerceAgent.SetEnabledTools([]string{"company_commerce"})
	if err := commerceSvc.UpdateAgent(ctx, commerceAgent); err != nil {
		t.Fatalf("UpdateAgent(commerce) failed: %v", err)
	}

	commerceAllowed, err := h.isToolCallAllowed(ctx, commerceAgentID, commerceSvc, "shopify_list_products")
	if err != nil {
		t.Fatalf("isToolCallAllowed(shopify) returned error: %v", err)
	}
	if !commerceAllowed {
		t.Fatalf("expected company_commerce to allow shopify tool")
	}
	commercePrefixedAllowed, err := h.isToolCallAllowed(ctx, commerceAgentID, commerceSvc, "company_commerce_shopify_list_products")
	if err != nil {
		t.Fatalf("isToolCallAllowed(company_commerce_shopify_list_products) returned error: %v", err)
	}
	if !commercePrefixedAllowed {
		t.Fatalf("expected company_commerce to allow company_commerce_shopify_list_products")
	}

	financeAgentID := "policy-finance"
	financeSvc := data.NewAgentService(db, financeAgentID)
	financeAgent, err := financeSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent(finance) failed: %v", err)
	}
	financeAgent.SetEnabledTools([]string{"company_finance"})
	if err := financeSvc.UpdateAgent(ctx, financeAgent); err != nil {
		t.Fatalf("UpdateAgent(finance) failed: %v", err)
	}

	financeAllowed, err := h.isToolCallAllowed(ctx, financeAgentID, financeSvc, "get_wallet_addresses")
	if err != nil {
		t.Fatalf("isToolCallAllowed(get_wallet_addresses) returned error: %v", err)
	}
	if financeAllowed {
		t.Fatalf("expected company_finance without wallet to deny wallet-scoped tool")
	}
	financeBalancesAllowed, err := h.isToolCallAllowed(ctx, financeAgentID, financeSvc, "get_balances")
	if err != nil {
		t.Fatalf("isToolCallAllowed(get_balances) returned error: %v", err)
	}
	if financeBalancesAllowed {
		t.Fatalf("expected company_finance without wallet to deny get_balances")
	}
	financePrefixedAllowed, err := h.isToolCallAllowed(ctx, financeAgentID, financeSvc, "company_finance_get_wallet_addresses")
	if err != nil {
		t.Fatalf("isToolCallAllowed(company_finance_get_wallet_addresses) returned error: %v", err)
	}
	if !financePrefixedAllowed {
		t.Fatalf("expected company_finance to allow company_finance_get_wallet_addresses")
	}

	financePolyBuyDenied, err := h.isToolCallAllowed(ctx, financeAgentID, financeSvc, "company_finance_polymarket_place_buy_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(company_finance_polymarket_place_buy_order) returned error: %v", err)
	}
	if financePolyBuyDenied {
		t.Fatalf("expected company_finance_polymarket_place_buy_order to require polymarket_buy")
	}
}

func TestBrokerToolPolicy_PolymarketReadBuySellSeparation(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-poly-read-buy-sell"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	agent.SetEnabledTools([]string{"polymarket_read"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(read) failed: %v", err)
	}

	readAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_redeem_winnings")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_redeem_winnings) returned error: %v", err)
	}
	if !readAllowed {
		t.Fatalf("expected polymarket_read to allow polymarket_redeem_winnings")
	}

	buyDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_buy_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_buy_order) returned error: %v", err)
	}
	if buyDenied {
		t.Fatalf("expected polymarket_place_buy_order to be denied without polymarket_buy")
	}

	sellDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_sell_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_sell_order) returned error: %v", err)
	}
	if sellDenied {
		t.Fatalf("expected polymarket_place_sell_order to be denied without polymarket_sell")
	}

	companyBuyDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "company_finance_polymarket_place_buy_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(company_finance_polymarket_place_buy_order) returned error: %v", err)
	}
	if companyBuyDenied {
		t.Fatalf("expected company_finance_polymarket_place_buy_order to be denied without polymarket_buy")
	}

	agent.SetEnabledTools([]string{"polymarket_buy"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(buy) failed: %v", err)
	}

	buyAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_buy_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_buy_order) returned error: %v", err)
	}
	if !buyAllowed {
		t.Fatalf("expected polymarket_buy to allow polymarket_place_buy_order")
	}

	sellStillDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_sell_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_sell_order) returned error: %v", err)
	}
	if sellStillDenied {
		t.Fatalf("expected polymarket_place_sell_order to be denied without polymarket_sell")
	}

	readDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_get_prices")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_get_prices) returned error: %v", err)
	}
	if readDenied {
		t.Fatalf("expected polymarket_get_prices to be denied without polymarket_read")
	}

	agent.SetEnabledTools([]string{"polymarket_sell"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(sell) failed: %v", err)
	}

	sellAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_sell_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_sell_order) returned error: %v", err)
	}
	if !sellAllowed {
		t.Fatalf("expected polymarket_sell to allow polymarket_place_sell_order")
	}

	cancelAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_cancel_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_cancel_order) returned error: %v", err)
	}
	if !cancelAllowed {
		t.Fatalf("expected polymarket_sell to allow polymarket_cancel_order")
	}

	legacyTradeReadDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_get_prices")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_get_prices) returned error for sell-only: %v", err)
	}
	if legacyTradeReadDenied {
		t.Fatalf("expected polymarket_get_prices to be denied with sell-only enablement")
	}

	agent.SetEnabledTools([]string{"polymarket_trade"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(legacy polymarket_trade) failed: %v", err)
	}

	legacyBuyAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_buy_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_buy_order) returned error for legacy polymarket_trade: %v", err)
	}
	if !legacyBuyAllowed {
		t.Fatalf("expected legacy polymarket_trade enablement to allow polymarket buy tools")
	}

	legacySellAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_sell_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_sell_order) returned error for legacy polymarket_trade: %v", err)
	}
	if !legacySellAllowed {
		t.Fatalf("expected legacy polymarket_trade enablement to allow polymarket sell tools")
	}

	legacyTradeReadOnlyDenied, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_get_prices")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_get_prices) returned error for legacy polymarket_trade: %v", err)
	}
	if legacyTradeReadOnlyDenied {
		t.Fatalf("expected legacy polymarket_trade enablement to keep polymarket read tools disabled")
	}

	agent.SetEnabledTools([]string{"polymarket"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(legacy polymarket) failed: %v", err)
	}

	legacyReadAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_get_prices")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_get_prices) returned error for legacy polymarket: %v", err)
	}
	if !legacyReadAllowed {
		t.Fatalf("expected legacy polymarket enablement to allow polymarket read tools")
	}

	legacyTradeAllowed, err := h.isToolCallAllowed(ctx, agentID, svc, "polymarket_place_sell_order")
	if err != nil {
		t.Fatalf("isToolCallAllowed(polymarket_place_sell_order) returned error for legacy polymarket: %v", err)
	}
	if !legacyTradeAllowed {
		t.Fatalf("expected legacy polymarket enablement to allow polymarket sell tools")
	}
}

func TestDeepResearchToolEnabled(t *testing.T) {
	spec := deepResearchToolSpec{ToolName: "deep_research_market_scan"}
	if deepResearchToolEnabled(nil, spec) {
		t.Fatalf("expected nil enabled set to deny deep research dynamic tool")
	}
	if !deepResearchToolEnabled(map[string]bool{"deep_research": true}, spec) {
		t.Fatalf("expected deep_research group enable to allow dynamic tool")
	}
	if !deepResearchToolEnabled(map[string]bool{"deep_research_market_scan": true}, spec) {
		t.Fatalf("expected explicit dynamic tool enable to allow dynamic tool")
	}
	if deepResearchToolEnabled(map[string]bool{"skills": true}, spec) {
		t.Fatalf("expected unrelated enable set to deny dynamic tool")
	}
}

func TestCompanyMethodToolEnabled(t *testing.T) {
	spec := companyMethodToolSpec{
		ToolName: "polymart_request_buy",
		Method:   "polymart_request_buy",
	}
	legacyAlias := legacyCompanyMethodToolName(spec.Method)

	if companyMethodToolEnabled(nil, spec.ToolName, spec) {
		t.Fatalf("expected nil enabled set to deny company method dynamic tool")
	}
	if !companyMethodToolEnabled(map[string]bool{spec.ToolName: true}, "ignored_tool_name", spec) {
		t.Fatalf("expected canonical dynamic tool enable to allow tool")
	}
	if !companyMethodToolEnabled(map[string]bool{legacyAlias: true}, "ignored_tool_name", spec) {
		t.Fatalf("expected legacy alias enable to allow tool")
	}
	if !companyMethodToolEnabled(map[string]bool{"explicit_request_name": true}, "explicit_request_name", spec) {
		t.Fatalf("expected exact requested tool-name enable to allow tool")
	}
	if companyMethodToolEnabled(map[string]bool{"skills": true}, "explicit_request_name", spec) {
		t.Fatalf("expected unrelated enable set to deny company method tool")
	}
}

func TestBrokerToolPolicy_BypassToolsAlwaysAllowed(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-bypass"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	for _, toolName := range []string{
		"get_enabled_tools",
		"get_system_prompt",
		"get_company_context",
		"get_deep_research_method_tools",
		"a2a_claim_jobs",
		"job_result",
	} {
		allowed, err := h.isToolCallAllowed(ctx, agentID, svc, toolName)
		if err != nil {
			t.Fatalf("isToolCallAllowed(%s) returned error: %v", toolName, err)
		}
		if !allowed {
			t.Fatalf("expected bypass tool %s to be allowed", toolName)
		}
	}
}

func TestBrokerToolPolicy_DeepResearchToolsOptInAndConfigurable(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-deep-research-agent"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_policy_test",
		"Policy test method",
		"",
		"Analyze {{topic}}",
		`{"type":"object","properties":{"topic":{"type":"string"}}}`,
		`{"type":"object","properties":{"summary":{"type":"string"}}}`,
		`{"max_depth":1}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod failed: %v", err)
	}

	// Deep research tools should be denied by default (opt-in).
	allowed, err := h.isToolCallAllowed(ctx, agentID, svc, "deep_research_policy_test")
	if err != nil {
		t.Fatalf("isToolCallAllowed(default) returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected deep-research tool to be denied by default (opt-in)")
	}

	agent.SetEnabledTools([]string{"skills"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(disable deep_research) failed: %v", err)
	}
	allowed, err = h.isToolCallAllowed(ctx, agentID, svc, "deep_research_policy_test")
	if err != nil {
		t.Fatalf("isToolCallAllowed(disabled) returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected deep-research tool to be denied when allowlist omits deep_research")
	}

	agent.SetEnabledTools([]string{"skills", "deep_research"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent(enable deep_research group) failed: %v", err)
	}
	allowed, err = h.isToolCallAllowed(ctx, agentID, svc, "deep_research_policy_test")
	if err != nil {
		t.Fatalf("isToolCallAllowed(enabled) returned error: %v", err)
	}
	if !allowed {
		t.Fatalf("expected deep-research tool to be allowed when deep_research group is enabled")
	}
}

func TestBrokerToolPolicy_DeniedToolFailsAtCallTool(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-calltool-deny"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "deny-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	toolInput, _ := json.Marshal(map[string]any{"query": "x"})
	_, err = h.callTool(ctx, agentID, svc, "company_knowledge_search", toolInput)
	if err == nil {
		t.Fatalf("expected disabled tool error")
	}
	if !strings.Contains(err.Error(), "is disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolGroupForToolNameMappings(t *testing.T) {
	cases := []struct {
		toolName  string
		wantGroup string
		wantOK    bool
	}{
		{toolName: "company_finance_polymarket_place_buy_order", wantGroup: "polymarket_buy", wantOK: true},
		{toolName: "company_finance_polymarket_place_sell_order", wantGroup: "polymarket_sell", wantOK: true},
		{toolName: "company_finance_polymarket_cancel_order", wantGroup: "polymarket_sell", wantOK: true},
		{toolName: "company_finance_polymarket_place_order", wantGroup: "polymarket_trade", wantOK: true},
		{toolName: "company_admin_list_members", wantGroup: "company_admin", wantOK: true},
		{toolName: "company_knowledge_search", wantGroup: "company_knowledge", wantOK: true},
		{toolName: "company_finance_get_wallet_addresses", wantGroup: "company_finance", wantOK: true},
		{toolName: "kg_query", wantGroup: "knowledge_graph", wantOK: true},
		{toolName: "shopify_list_products", wantGroup: "shopify_read", wantOK: true},
		{toolName: "shopify_create_product", wantGroup: "shopify_write", wantOK: true},
		{toolName: "shopify_list_themes", wantGroup: "shopify_theme_read", wantOK: true},
		{toolName: "shopify_get_asset", wantGroup: "shopify_theme_read", wantOK: true},
		{toolName: "shopify_update_asset", wantGroup: "shopify_theme_write", wantOK: true},
		{toolName: "shopify_create_page", wantGroup: "shopify_theme_write", wantOK: true},
		{toolName: "company_commerce_shopify_list_products", wantGroup: "shopify_read", wantOK: true},
		{toolName: "company_commerce_shopify_create_product", wantGroup: "shopify_write", wantOK: true},
		{toolName: "company_commerce_shopify_list_themes", wantGroup: "shopify_theme_read", wantOK: true},
		{toolName: "company_commerce_shopify_update_asset", wantGroup: "shopify_theme_write", wantOK: true},
		{toolName: "amazon_search_catalog", wantGroup: "amazon", wantOK: true},
		{toolName: "supplier_lookup", wantGroup: "supplier", wantOK: true},
		{toolName: "ads_list_campaigns", wantGroup: "ads", wantOK: true},
		{toolName: "ecommerce_get_catalog", wantGroup: "ecommerce", wantOK: true},
		{toolName: "mcp_call", wantGroup: "mcp", wantOK: true},
		{toolName: "claude_code", wantGroup: "claude_code", wantOK: true},
		{toolName: "polymarket_place_buy_order", wantGroup: "polymarket_buy", wantOK: true},
		{toolName: "polymarket_place_sell_order", wantGroup: "polymarket_sell", wantOK: true},
		{toolName: "polymarket_cancel_order", wantGroup: "polymarket_sell", wantOK: true},
		{toolName: "polymarket_place_order", wantGroup: "polymarket_trade", wantOK: true},
		{toolName: "polymarket_get_prices", wantGroup: "polymarket_read", wantOK: true},
		{toolName: "wallet_get_address", wantGroup: "wallet", wantOK: true},
		{toolName: "send_company_heartbeat", wantGroup: "company_admin", wantOK: true},
		{toolName: "save_skill", wantGroup: "skills", wantOK: true},
		{toolName: "read_soul", wantGroup: "soul", wantOK: true},
		{toolName: "set_report_html", wantGroup: "report", wantOK: true},
		{toolName: "get_balances", wantGroup: "wallet", wantOK: true},
		{toolName: "plan_task", wantGroup: "tasks", wantOK: true},
		{toolName: "send_agent_message", wantGroup: "messaging", wantOK: true},
		{toolName: "unknown_tool_name", wantGroup: "", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.toolName, func(t *testing.T) {
			gotGroup, gotOK := toolGroupForToolName(tc.toolName)
			if gotOK != tc.wantOK || gotGroup != tc.wantGroup {
				t.Fatalf("toolGroupForToolName(%q) = (%q,%v), want (%q,%v)", tc.toolName, gotGroup, gotOK, tc.wantGroup, tc.wantOK)
			}
		})
	}
}

func TestBrokerToolPolicy_LegacyCompanyCommerceShopifyToolsRespectGroupEnablement(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "policy-legacy-company-commerce-disabled"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	// Explicitly configure enabled tool groups without any Shopify/commerce group.
	agent.SetEnabledTools([]string{"skills"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	allowed, err := h.isToolCallAllowed(ctx, agentID, svc, "company_commerce_shopify_list_products")
	if err != nil {
		t.Fatalf("isToolCallAllowed returned error: %v", err)
	}
	if allowed {
		t.Fatalf("expected legacy company_commerce_shopify_list_products to be denied when Shopify groups are disabled")
	}
}

func TestIsToolGroupEnabledAliases(t *testing.T) {
	cases := []struct {
		name    string
		groupID string
		enabled map[string]bool
		want    bool
	}{
		{name: "empty_group_defaults_true", groupID: "", enabled: map[string]bool{}, want: true},
		{name: "direct_enable", groupID: "wallet", enabled: map[string]bool{"wallet": true}, want: true},
		{name: "company_finance_via_wallet", groupID: "company_finance", enabled: map[string]bool{"wallet": true}, want: true},
		{name: "polymarket_read_via_legacy_polymarket", groupID: "polymarket_read", enabled: map[string]bool{"polymarket": true}, want: true},
		{name: "polymarket_buy_via_legacy_trade", groupID: "polymarket_buy", enabled: map[string]bool{"polymarket_trade": true}, want: true},
		{name: "polymarket_sell_via_legacy_trade", groupID: "polymarket_sell", enabled: map[string]bool{"polymarket_trade": true}, want: true},
		{name: "polymarket_trade_via_buy", groupID: "polymarket_trade", enabled: map[string]bool{"polymarket_buy": true}, want: true},
		{name: "polymarket_via_read", groupID: "polymarket", enabled: map[string]bool{"polymarket_read": true}, want: true},
		{name: "shopify_read_via_company_commerce", groupID: "shopify_read", enabled: map[string]bool{"company_commerce": true}, want: true},
		{name: "shopify_write_via_company_commerce", groupID: "shopify_write", enabled: map[string]bool{"company_commerce": true}, want: true},
		{name: "shopify_read_via_legacy_shopify", groupID: "shopify_read", enabled: map[string]bool{"shopify": true}, want: true},
		{name: "shopify_write_via_legacy_shopify", groupID: "shopify_write", enabled: map[string]bool{"shopify": true}, want: true},
		{name: "unknown_group_disabled", groupID: "unknown", enabled: map[string]bool{}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isToolGroupEnabled(tc.groupID, tc.enabled); got != tc.want {
				t.Fatalf("isToolGroupEnabled(%q, %#v) = %v, want %v", tc.groupID, tc.enabled, got, tc.want)
			}
		})
	}
}

func TestAnyEnabled(t *testing.T) {
	enabled := map[string]bool{
		"wallet": true,
	}

	if !anyEnabled(enabled, "wallet", "polymarket") {
		t.Fatalf("expected anyEnabled to return true when at least one key is enabled")
	}
	if anyEnabled(enabled, "polymarket", "company_commerce") {
		t.Fatalf("expected anyEnabled to return false when no keys are enabled")
	}
}
