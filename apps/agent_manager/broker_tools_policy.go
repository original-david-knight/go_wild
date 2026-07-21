package main

import (
	"context"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
)

var brokerInternalBypassTools = map[string]struct{}{
	// Agent startup/data-access pseudo tools.
	"get_soul_content":               {},
	"get_memory":                     {},
	"get_agent_name":                 {},
	"get_system_prompt":              {},
	"get_company_context":            {},
	"get_company_method_tools":       {},
	"get_deep_research_method_tools": {},
	"get_enabled_tools":              {},
	"get_history_snapshot":           {},
	"save_history_snapshot":          {},
	"delete_history_snapshot":        {},
	"get_capabilities":               {},
	"get_chat_history":               {},

	// Broker internals used by composed tools.
	"cache_get":        {},
	"cache_set":        {},
	"compress_content": {},

	// Runtime-owned company-method completion operation (not LLM-facing policy).
	"job_result": {},
}

func (h *BrokerToolsHandler) isToolCallAllowed(ctx context.Context, agentID string, svc *data.AgentService, toolName string) (bool, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false, nil
	}
	if isBrokerInternalBypassTool(toolName) {
		return true, nil
	}

	groupID, ok := toolGroupForToolName(toolName)
	if ok {
		disabledForMethod, err := h.executionMethodDisablesToolGroup(ctx, groupID)
		if err != nil {
			return false, err
		}
		if disabledForMethod {
			return false, nil
		}
		enabled, err := agentEnabledToolSet(ctx, svc)
		if err != nil {
			return false, err
		}
		if enabled == nil {
			return h.defaultToolGroupAllowance(ctx, agentID, groupID)
		}
		return isToolGroupEnabled(groupID, enabled), nil
	}

	if allowed, handled, err := h.dynamicToolAllowance(ctx, agentID, svc, toolName); handled || err != nil {
		return allowed, err
	}

	// Unknown tool names will fail in dispatch; don't block preemptively.
	return true, nil
}

func (h *BrokerToolsHandler) executionMethodDisablesToolGroup(ctx context.Context, groupID string) (bool, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || h == nil || h.db == nil {
		return false, nil
	}

	method := strings.TrimSpace(BrokerExecutionMethod(ctx))
	if method == "" {
		return false, nil
	}

	methodDef, err := data.NewAgentService(h.db, "system").GetA2AMethod(ctx, method)
	if err != nil {
		if strings.Contains(err.Error(), "method not found") {
			return false, nil
		}
		return false, err
	}
	if methodDef == nil {
		return false, nil
	}
	return methodDef.IsToolGroupDisabled(groupID), nil
}

func isBrokerInternalBypassTool(toolName string) bool {
	_, bypass := brokerInternalBypassTools[toolName]
	return bypass
}

func agentEnabledToolSet(ctx context.Context, svc *data.AgentService) (map[string]bool, error) {
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}
	return agent.EnabledTools(), nil
}

func (h *BrokerToolsHandler) defaultToolGroupAllowance(ctx context.Context, agentID, groupID string) (bool, error) {
	// Default policy: all groups enabled except company_admin, which is CEO-only.
	if groupID != "company_admin" {
		return true, nil
	}
	_, _, isCEO, err := h.resolveCompanyContext(ctx, agentID)
	if err != nil {
		return false, err
	}
	return isCEO, nil
}

func (h *BrokerToolsHandler) dynamicToolAllowance(ctx context.Context, agentID string, svc *data.AgentService, toolName string) (bool, bool, error) {
	deepResearchSpec, deepResearchTool, err := deepResearchToolSpecForName(ctx, h.db, toolName)
	if err != nil {
		return false, true, err
	}
	if deepResearchTool {
		enabled, err := agentEnabledToolSet(ctx, svc)
		if err != nil {
			return false, true, err
		}
		return deepResearchToolEnabled(enabled, deepResearchSpec), true, nil
	}

	companyMethodSpec, companyMethodTool, err := companyMethodToolSpecForAgent(ctx, h.db, agentID, toolName)
	if err != nil {
		return false, true, err
	}
	if companyMethodTool {
		enabled, err := agentEnabledToolSet(ctx, svc)
		if err != nil {
			return false, true, err
		}
		return companyMethodToolEnabled(enabled, toolName, companyMethodSpec), true, nil
	}
	return false, false, nil
}

func deepResearchToolEnabled(enabled map[string]bool, spec deepResearchToolSpec) bool {
	if enabled == nil {
		// Dynamic deep-research method tools are opt-in and require explicit enablement.
		return false
	}
	return enabled["deep_research"] || enabled[strings.TrimSpace(spec.ToolName)]
}

func companyMethodToolEnabled(enabled map[string]bool, requestedToolName string, spec companyMethodToolSpec) bool {
	if enabled == nil {
		// Dynamic company method tools are opt-in and require explicit enablement.
		return false
	}
	if enabled[strings.TrimSpace(requestedToolName)] {
		return true
	}
	if enabled[strings.TrimSpace(spec.ToolName)] {
		return true
	}
	legacyToolName := strings.TrimSpace(legacyCompanyMethodToolName(spec.Method))
	return legacyToolName != "" && enabled[legacyToolName]
}

func isToolGroupEnabled(groupID string, enabled map[string]bool) bool {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return true
	}
	if enabled[groupID] {
		return true
	}
	rule, ok := toolGroupEnableRules[groupID]
	if !ok {
		return false
	}
	return rule(enabled)
}

type toolGroupEnableRuleFunc func(enabled map[string]bool) bool

var toolGroupEnableRules = map[string]toolGroupEnableRuleFunc{
	"company_finance": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "wallet", "polymarket_read", "polymarket_buy", "polymarket_sell", "polymarket_trade", "polymarket")
	},
	"polymarket_read": func(enabled map[string]bool) bool {
		return enabled["polymarket"]
	},
	"polymarket_notes": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "polymarket", "polymarket_read")
	},
	"polymarket_buy": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "polymarket", "polymarket_trade")
	},
	"polymarket_sell": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "polymarket", "polymarket_trade")
	},
	"polymarket_trade": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "polymarket_buy", "polymarket_sell", "polymarket")
	},
	"polymarket": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "polymarket_read", "polymarket_buy", "polymarket_sell", "polymarket_trade")
	},
	"shopify_read": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "shopify", "company_commerce")
	},
	"shopify_write": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "shopify", "company_commerce")
	},
	"shopify_theme_read": func(enabled map[string]bool) bool {
		return enabled["shopify_theme"]
	},
	"shopify_theme_write": func(enabled map[string]bool) bool {
		return enabled["shopify_theme"]
	},
	"amazon": func(enabled map[string]bool) bool {
		return anyEnabled(enabled, "amazon", "company_commerce")
	},
}

func anyEnabled(enabled map[string]bool, keys ...string) bool {
	for _, key := range keys {
		if enabled[key] {
			return true
		}
	}
	return false
}

type toolGroupPrefixRule struct {
	prefix  string
	groupID string
}

var toolGroupExactMappings = map[string]string{
	"company_finance_polymarket_place_buy_order":  "polymarket_buy",
	"company_finance_polymarket_place_sell_order": "polymarket_sell",
	"company_finance_polymarket_cancel_order":     "polymarket_sell",
	"company_finance_polymarket_place_order":      "polymarket_trade",
	"shopify_update_asset":                        "shopify_theme_write",
	"shopify_delete_asset":                        "shopify_theme_write",
	"shopify_create_page":                         "shopify_theme_write",
	"shopify_update_page":                         "shopify_theme_write",
	"shopify_delete_page":                         "shopify_theme_write",
	"shopify_list_themes":                         "shopify_theme_read",
	"shopify_get_theme":                           "shopify_theme_read",
	"shopify_list_assets":                         "shopify_theme_read",
	"shopify_get_asset":                           "shopify_theme_read",
	"shopify_list_pages":                          "shopify_theme_read",
	"shopify_get_page":                            "shopify_theme_read",
	"shopify_create_product":                      "shopify_write",
	"shopify_update_product":                      "shopify_write",
	"shopify_delete_product":                      "shopify_write",
	"shopify_update_variant":                      "shopify_write",
	"shopify_update_order":                        "shopify_write",
	"shopify_create_fulfillment":                  "shopify_write",
	"shopify_set_inventory_level":                 "shopify_write",
	"shopify_upload_image":                        "shopify_write",
	"shopify_sync_inventory":                      "shopify_write",
	"shopify_create_listing":                      "shopify_write",
	"shopify_delete_listing":                      "shopify_write",
	"claude_code":                                 "claude_code",
	"polymarket_add_market_note":                  "polymarket_notes",
	"polymarket_list_market_notes":                "polymarket_notes",
	"polymarket_place_buy_order":                  "polymarket_buy",
	"polymarket_place_sell_order":                 "polymarket_sell",
	"polymarket_cancel_order":                     "polymarket_sell",
	"polymarket_place_order":                      "polymarket_trade",
	"reuters_news":                                "reuters",
	"search_reuters_news":                         "reuters",
	"read_reuters_article":                        "reuters",
	"send_company_heartbeat":                      "company_admin",
	"save_skill":                                  "skills",
	"list_skills":                                 "skills",
	"get_skill":                                   "skills",
	"delete_skill":                                "skills",
	"read_webpage":                                "web_reader",
	"read_soul":                                   "soul",
	"update_soul":                                 "soul",
	"set_report_html":                             "report",
	"get_report_html":                             "report",
	"get_wallet_addresses":                        "wallet",
	"get_wallet_address":                          "wallet",
	"get_balances":                                "wallet",
	"sign_message":                                "wallet",
	"send_token":                                  "wallet",
	"swap_token":                                  "wallet",
	"contract_call":                               "wallet",
	"get_transaction_history":                     "wallet",
	"encrypt_message":                             "wallet",
	"decrypt_message":                             "wallet",
	"get_ed25519_public_key":                      "wallet",
	"add_task":                                    "tasks",
	"mark_task_done":                              "tasks",
	"mark_task_deprecated":                        "tasks",
	"list_tasks":                                  "tasks",
	"move_task":                                   "tasks",
	"block_task":                                  "tasks",
	"unblock_task":                                "tasks",
	"sleep_task":                                  "tasks",
	"plan_task":                                   "tasks",
	"evaluate_task":                               "tasks",
	"get_pending_tasks":                           "tasks",
	"get_workable_tasks":                          "tasks",
	"get_finished_tasks":                          "tasks",
	"get_task_context":                            "tasks",
	"check_recurring_tasks":                       "tasks",
	"get_recurring_tasks":                         "tasks",
	"add_recurring_task":                          "tasks",
	"delete_recurring_task":                       "tasks",
	"list_peers":                                  "messaging",
	"send_agent_message":                          "messaging",
	"read_agent_messages":                         "messaging",
	"mark_agent_messages_read":                    "messaging",
}

var toolGroupPrefixMappings = []toolGroupPrefixRule{
	{prefix: "company_admin_", groupID: "company_admin"},
	{prefix: "company_knowledge_", groupID: "company_knowledge"},
	{prefix: "company_finance_", groupID: "company_finance"},
	{prefix: "kg_", groupID: "knowledge_graph"},
	{prefix: "shopify_", groupID: "shopify_read"},
	{prefix: "amazon_", groupID: "amazon"},
	{prefix: "supplier_", groupID: "supplier"},
	{prefix: "ads_", groupID: "ads"},
	{prefix: "ecommerce_", groupID: "ecommerce"},
	{prefix: "mcp_", groupID: "mcp"},
	{prefix: "polymarket_", groupID: "polymarket_read"},
	{prefix: "wallet_", groupID: "wallet"},
}

func toolGroupForToolName(toolName string) (string, bool) {
	if strings.HasPrefix(toolName, "company_commerce_shopify_") {
		toolName = strings.TrimPrefix(toolName, "company_commerce_")
	}

	if groupID, ok := toolGroupExactMappings[toolName]; ok {
		return groupID, true
	}

	for _, rule := range toolGroupPrefixMappings {
		if strings.HasPrefix(toolName, rule.prefix) {
			return rule.groupID, true
		}
	}

	return "", false
}
