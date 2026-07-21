package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools/broker"
)

func addTools(ctx context.Context, agent *loop.AgenticLoop, runtime *agentRuntime) {
	enabled := getEnabledTools(ctx, runtime)
	companyCtx := getCompanyContext(ctx, runtime)
	isCompanyCEO := companyCtx != nil && companyCtx.IsCEO

	type toolEntry struct {
		id  string
		add func()
	}

	if runtime != nil && runtime.brokerClient != nil {
		brokerClient := runtime.brokerClient
		entries := []toolEntry{
			{"skills", func() { addBrokerSkillsTools(agent, brokerClient) }},
			{"web_search", func() {
				brokerSearch := broker.NewSearchTools(brokerClient)
				agent.AddTools(loop.WrapToolsWithDescriptions(brokerSearch)...)
				fmt.Println(color.HiBlackString("Tool: web_search (via broker)"))
			}},
			{"web_reader", func() { addWebReaderTools(agent, brokerClient) }},
			{"http", func() { addHTTPTools(agent) }},
			{"report", func() { addBrokerReportTools(agent, brokerClient) }},
			{"soul", func() { addBrokerSoulTools(agent, brokerClient) }},
			{"knowledge_graph", func() { addBrokerKGTools(agent, brokerClient) }},
			{"company_admin", func() { addBrokerCompanyAdminTools(agent, brokerClient) }},
			{"company_knowledge", func() { addBrokerCompanyKnowledgeTools(agent, brokerClient) }},
			{"company_finance", func() { addBrokerCompanyFinanceTools(agent, brokerClient, enabled, isCompanyCEO) }},
			{"wallet", func() { addBrokerWalletTools(agent, brokerClient) }},
			{"polymarket_read", func() { addBrokerPolymarketReadTools(agent, brokerClient) }},
			{"polymarket_buy", func() { addBrokerPolymarketBuyTools(agent, brokerClient) }},
			{"polymarket_sell", func() { addBrokerPolymarketSellTools(agent, brokerClient) }},
			{"claude_code", func() { addBrokerClaudeCodeTools(agent, brokerClient) }},
			{"shell", func() { addShellTools(agent) }},
			{"file", func() { addFileTools(agent) }},
			{"tasks", func() { addBrokerTaskTools(agent, brokerClient) }},
			{"telegram", func() { addBrokerTelegramTools(agent, brokerClient) }},
			{"email", func() { addBrokerEmailTools(agent, brokerClient) }},
			{"reuters", func() { addReutersTools(agent, brokerClient) }},
			{"messaging", func() { addBrokerMessagingTools(ctx, agent, brokerClient) }},
			{"mcp", func() { addMCPTools(ctx, agent, brokerClient) }},
			{"content", func() { addContentTools(agent) }},
			{"shopify_read", func() { addBrokerShopifyReadTools(agent, brokerClient) }},
			{"shopify_write", func() { addBrokerShopifyWriteTools(agent, brokerClient) }},
			{"supplier", func() { addBrokerSupplierTools(agent, brokerClient) }},
			{"ads", func() { addBrokerAdsTools(agent, brokerClient) }},
			{"ecommerce", func() { addBrokerEcommerceTools(agent, brokerClient) }},
			{"paywall", func() { addBrokerPaywallTools(agent, brokerClient) }},
			{"sites", func() { addBrokerSiteTools(agent, brokerClient) }},
		}

		for _, e := range entries {
			if !toolEnabledForAgent(e.id, enabled, isCompanyCEO) {
				fmt.Println(color.HiBlackString("Tool: %s (disabled)", e.id))
				continue
			}
			e.add()
		}

		addBrokerDeepResearchMethodTools(ctx, agent, brokerClient, enabled)
		addBrokerCompanyMethodTools(ctx, agent, brokerClient, enabled)
		return
	}

	entries := []toolEntry{
		{"skills", func() { addLocalSkillsTools(agent, runtime.service) }},
		{"web_search", func() { addLocalWebSearchTools(agent) }},
		{"web_reader", func() { addWebReaderTools(agent, nil) }},
		{"http", func() { addHTTPTools(agent) }},
		{"report", func() { addLocalReportTools(agent, runtime.service) }},
		{"soul", func() { addLocalSoulTools(agent, runtime.service) }},
		{"wallet", func() { addLocalWalletTools(ctx, agent, runtime.service) }},
		{"shell", func() { addShellTools(agent) }},
		{"file", func() { addFileTools(agent) }},
		{"tasks", func() { addLocalTaskTools(agent, runtime.service) }},
		{"telegram", func() { addLocalTelegramTools(ctx, agent, runtime.service) }},
		{"email", func() { addLocalEmailTools(ctx, agent, runtime.service) }},
		{"messaging", func() { addLocalMessagingTools(ctx, agent, runtime.service) }},
		{"content", func() { addContentTools(agent) }},
	}

	for _, e := range entries {
		if !toolEnabledForAgent(e.id, enabled, isCompanyCEO) {
			fmt.Println(color.HiBlackString("Tool: %s (disabled)", e.id))
			continue
		}
		e.add()
	}
}

func toolEnabledForAgent(toolID string, enabled map[string]bool, isCompanyCEO bool) bool {
	// nil means not configured yet — all enabled except company_admin defaults to CEO.
	if enabled == nil {
		if toolID == "company_admin" {
			return isCompanyCEO
		}
		return true
	}

	if enabled[toolID] {
		return true
	}

	switch toolID {
	case "company_finance":
		return enabled["wallet"] || enabled["polymarket_read"] || enabled["polymarket_buy"] || enabled["polymarket_sell"] || enabled["polymarket_trade"] || enabled["polymarket"]
	case "polymarket_read":
		return enabled["polymarket"]
	case "polymarket_buy", "polymarket_sell":
		return enabled["polymarket"] || enabled["polymarket_trade"]
	case "polymarket_trade":
		return enabled["polymarket_buy"] || enabled["polymarket_sell"] || enabled["polymarket"]
	case "polymarket":
		return enabled["polymarket_read"] || enabled["polymarket_buy"] || enabled["polymarket_sell"] || enabled["polymarket_trade"]
	case "shopify_read":
		return enabled["shopify"] || enabled["company_commerce"]
	case "shopify_write":
		return enabled["shopify"] || enabled["company_commerce"]
	default:
		return false
	}
}

// getEnabledTools fetches the set of enabled tool group IDs from the broker.
// Returns nil if tools have not been configured (all enabled by default).
func getEnabledTools(ctx context.Context, runtime *agentRuntime) map[string]bool {
	if runtime == nil {
		return nil
	}
	if runtime.brokerClient != nil {
		result, err := runtime.brokerClient.CallTool(ctx, "get_enabled_tools", map[string]any{})
		if err != nil {
			return nil
		}
		list, ok := result["enabled_tools"].([]any)
		if !ok {
			return nil
		}
		m := make(map[string]bool, len(list))
		for _, v := range list {
			if s, ok := v.(string); ok {
				m[s] = true
			}
		}
		return m
	}
	if runtime.service == nil {
		return nil
	}
	agent, err := runtime.service.GetAgent(ctx)
	if err != nil || agent == nil {
		return nil
	}
	return agent.EnabledTools()
}

type companyContext struct {
	CompanyID string
	IsCEO     bool
}

func getCompanyContext(ctx context.Context, runtime *agentRuntime) *companyContext {
	if runtime == nil {
		return nil
	}
	if runtime.brokerClient != nil {
		result, err := runtime.brokerClient.CallTool(ctx, "get_company_context", map[string]any{})
		if err != nil {
			return nil
		}
		companyID, _ := result["company_id"].(string)
		companyID = strings.TrimSpace(companyID)
		if companyID == "" {
			return nil
		}
		isCEO, _ := result["is_ceo"].(bool)
		return &companyContext{
			CompanyID: companyID,
			IsCEO:     isCEO,
		}
	}
	if runtime.db == nil || runtime.agentID == "" {
		return nil
	}
	member, err := data.GetCompanyMemberForAgent(ctx, runtime.db, runtime.agentID)
	if err != nil || member == nil || strings.TrimSpace(member.CompanyID) == "" {
		return nil
	}
	company, err := data.GetCompany(ctx, runtime.db, member.CompanyID)
	if err != nil || company == nil {
		return nil
	}
	return &companyContext{
		CompanyID: member.CompanyID,
		IsCEO:     strings.TrimSpace(company.CEOAgentID) == runtime.agentID,
	}
}
