package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"
	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools"
	"github.com/original-david-knight/go_wild/tools/broker"
)

// addWebReaderTools adds web reading tools to the agent.
func addWebReaderTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	var compress tools.ContentCompressor
	if brokerClient != nil {
		compress = func(ctx context.Context, markdown string) (string, error) {
			result, err := brokerClient.CallTool(ctx, "compress_content", map[string]any{
				"content": markdown,
			})
			if err != nil {
				return "", err
			}
			if s, ok := result["content"].(string); ok {
				return s, nil
			}
			return "", fmt.Errorf("unexpected compress_content response")
		}
	}
	webReaderTools := tools.NewWebReaderTools(compress)
	agent.AddTools(loop.WrapToolsWithDescriptions(webReaderTools)...)
	fmt.Println(color.HiBlackString("Tool: read_webpage"))
}

// addHTTPTools adds HTTP request tools to the agent.
func addHTTPTools(agent *loop.AgenticLoop) {
	httpTools := tools.NewHTTPTools()
	agent.AddTools(loop.WrapToolsWithDescriptions(httpTools)...)
	fmt.Println(color.HiBlackString("Tool: http_request"))
}

// addShellTools adds shell command execution tools to the agent.
// Only enabled when running inside a Docker container (sandboxed environment).
func addShellTools(agent *loop.AgenticLoop) {
	shellTools := tools.NewShellTools()
	if shellTools == nil {
		// Not running in a container - shell tools disabled for security
		return
	}

	agent.AddTools(loop.WrapToolsWithDescriptions(shellTools)...)
	fmt.Println(color.HiBlackString("Tool: run_shell (container mode)"))
}

// addBrokerClaudeCodeTools adds Claude Code execution tools proxied via broker.
func addBrokerClaudeCodeTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	claudeTools := broker.NewClaudeCodeTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(claudeTools)...)
	fmt.Println(color.HiBlackString("Tool: claude_code (via broker)"))
}

// addFileTools adds file operation tools to the agent.
// Only enabled when running inside a Docker container (sandboxed environment).
func addFileTools(agent *loop.AgenticLoop) {
	fileTools := tools.NewFileTools()
	if fileTools == nil {
		// Not running in a container - file tools disabled for security
		return
	}

	agent.AddTools(loop.WrapToolsWithDescriptions(fileTools)...)
	fmt.Println(color.HiBlackString("Tools: read_file, write_file, edit_file, list_files (container mode)"))
}

// addReutersTools adds Reuters news tools proxied through the broker.
func addReutersTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	reutersTools := broker.NewReutersTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(reutersTools)...)
	fmt.Println(color.HiBlackString("Tools: reuters_news, search_reuters_news, read_reuters_article (via broker)"))
}

func addContentTools(agent *loop.AgenticLoop) {
	contentTools := tools.NewContentTools(output)
	agent.AddTools(loop.WrapToolsWithDescriptions(contentTools)...)
	fmt.Println(color.HiBlackString("Tools: show_image, show_svg, show_audio"))
}

// Broker tool wrappers

func addBrokerPolymarketReadTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	polymarketTools := broker.NewPolymarketReadTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(polymarketTools)...)
	fmt.Println(color.HiBlackString("Tools: polymarket read (via broker)"))
}

func addBrokerPolymarketBuyTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	polymarketTools := broker.NewPolymarketBuyTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(polymarketTools)...)
	fmt.Println(color.HiBlackString("Tools: polymarket buy (via broker)"))
}

func addBrokerPolymarketSellTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	polymarketTools := broker.NewPolymarketSellTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(polymarketTools)...)
	fmt.Println(color.HiBlackString("Tools: polymarket sell (via broker)"))
}

func addBrokerPaywallTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	paywallTools := broker.NewPaywallTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(paywallTools)...)
	fmt.Println(color.HiBlackString("Tools: paywall (via broker)"))
}

func addBrokerSiteTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	siteTools := broker.NewSiteTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(siteTools)...)
	fmt.Println(color.HiBlackString("Tools: publish_site, list_sites (via broker)"))
}

func addBrokerWalletTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	walletTools := broker.NewWalletTools(brokerClient)
	defs := []loop.ToolDefinition{
		{
			Name:        "get_wallet_address",
			Description: walletTools.DescribeTool("get_wallet_address"),
			Method:      "GetWalletAddressTool",
		},
		{
			Name:        "get_balances",
			Description: walletTools.DescribeTool("get_balances"),
			Method:      "GetBalancesTool",
		},
		{
			Name:        "sign_message",
			Description: walletTools.DescribeTool("sign_message"),
			Method:      "SignMessageTool",
		},
		{
			Name:        "send_token",
			Description: walletTools.DescribeTool("send_token"),
			Method:      "SendTokenTool",
		},
		{
			Name:        "swap_token",
			Description: walletTools.DescribeTool("swap_token"),
			Method:      "SwapTokenTool",
		},
		{
			Name:        "contract_call",
			Description: walletTools.DescribeTool("contract_call"),
			Method:      "ContractCallTool",
		},
		{
			Name:        "get_transaction_history",
			Description: walletTools.DescribeTool("get_transaction_history"),
			Method:      "GetTransactionHistoryTool",
		},
		{
			Name:        "encrypt_message",
			Description: walletTools.DescribeTool("encrypt_message"),
			Method:      "EncryptMessageTool",
		},
		{
			Name:        "decrypt_message",
			Description: walletTools.DescribeTool("decrypt_message"),
			Method:      "DecryptMessageTool",
		},
		{
			Name:        "get_ed25519_public_key",
			Description: walletTools.DescribeTool("get_ed25519_public_key"),
			Method:      "GetEd25519PublicKeyTool",
		},
	}
	agent.AddTools(loop.WrapToolsWithDefinitions(walletTools, defs)...)
	fmt.Println(color.HiBlackString("Tools: wallet (via broker)"))
}

func addBrokerTelegramTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	telegramTools := broker.NewTelegramTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(telegramTools)...)
	fmt.Println(color.HiBlackString("Tools: telegram (via broker)"))
}

func addBrokerEmailTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	emailTools := broker.NewEmailTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(emailTools)...)
	fmt.Println(color.HiBlackString("Tools: list_emails, read_email, send_email (via broker)"))
}

func addBrokerReportTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	reportTools := broker.NewReportTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(reportTools)...)
	fmt.Println(color.HiBlackString("Tools: set_report_html, get_report_html (via broker)"))
}

func addBrokerSoulTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	soulTools := broker.NewSoulTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(soulTools)...)
	fmt.Println(color.HiBlackString("Tools: read_soul, update_soul (via broker)"))
}

func addBrokerKGTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	kgTools := broker.NewKGTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(kgTools)...)
	fmt.Println(color.HiBlackString("Tools: knowledge graph (via broker)"))
}

func addBrokerCompanyAdminTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	companyAdminTools := broker.NewCompanyAdminTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(companyAdminTools)...)
	fmt.Println(color.HiBlackString("Tools: company admin (via broker)"))
}

func addBrokerCompanyKnowledgeTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	companyKnowledgeTools := broker.NewCompanyKnowledgeTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(companyKnowledgeTools)...)
	fmt.Println(color.HiBlackString("Tools: company knowledge (via broker)"))
}

func addBrokerCompanyFinanceTools(agent *loop.AgenticLoop, brokerClient *broker.Client, enabled map[string]bool, isCompanyCEO bool) {
	_ = enabled
	_ = isCompanyCEO
	companyFinanceTools := broker.NewCompanyFinanceTools(brokerClient)
	defs := []loop.ToolDefinition{
		{
			Name:        "company_finance_get_wallet_addresses",
			Description: companyFinanceTools.DescribeTool("company_finance_get_wallet_addresses"),
			Method:      "CompanyFinanceGetWalletAddressesTool",
		},
		{
			Name:        "company_finance_get_balances",
			Description: companyFinanceTools.DescribeTool("company_finance_get_balances"),
			Method:      "CompanyFinanceGetBalancesTool",
		},
		{
			Name:        "company_finance_get_transaction_history",
			Description: companyFinanceTools.DescribeTool("company_finance_get_transaction_history"),
			Method:      "CompanyFinanceGetTransactionHistoryTool",
		},
	}

	agent.AddTools(loop.WrapToolsWithDefinitions(companyFinanceTools, defs)...)
	fmt.Println(color.HiBlackString("Tools: company finance (via broker)"))
}

func addBrokerTaskTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	taskTools := broker.NewTaskTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(taskTools)...)
	fmt.Println(color.HiBlackString("Tools: add_task, mark_task_done, mark_task_deprecated, list_tasks, move_task, block_task, unblock_task, plan_task, evaluate_task (via broker)"))
}

func addBrokerSkillsTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	pythonTools, err := tools.NewPythonTools()
	if err != nil {
		fmt.Println(color.HiBlackString("Tools: skills (disabled - no Python available)"))
		return
	}
	skillsTools := broker.NewSkillsTools(brokerClient, pythonTools)
	agent.AddTools(loop.WrapToolsWithDescriptions(pythonTools)...)
	agent.AddTools(loop.WrapToolsWithDescriptions(skillsTools)...)
	fmt.Println(color.HiBlackString("Tool: run_python"))
	fmt.Println(color.HiBlackString("Tools: save_skill, execute_skill, test_skill, list_skills, get_skill, delete_skill (via broker)"))
}

func addBrokerMessagingTools(ctx context.Context, agent *loop.AgenticLoop, brokerClient *broker.Client) {
	// Check if the agent has any peers before registering tools
	result, err := brokerClient.CallTool(ctx, "list_peers", map[string]any{})
	if err != nil {
		fmt.Println(color.HiBlackString("Tools: messaging (disabled - no peers)"))
		return
	}
	peers, ok := result["peers"].([]any)
	if !ok || len(peers) == 0 {
		fmt.Println(color.HiBlackString("Tools: messaging (disabled - no peers)"))
		return
	}

	messagingTools := broker.NewMessagingTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(messagingTools)...)
	fmt.Println(color.HiBlackString("Tools: list_peers, send_message, read_messages, mark_messages_read (via broker)"))
}

func addBrokerShopifyReadTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	t := broker.NewShopifyTools(brokerClient)
	defs := []loop.ToolDefinition{
		{Name: "shopify_get_product", Description: t.DescribeTool("shopify_get_product"), Method: "ShopifyGetProductTool"},
		{Name: "shopify_list_products", Description: t.DescribeTool("shopify_list_products"), Method: "ShopifyListProductsTool"},
		{Name: "shopify_list_variants", Description: t.DescribeTool("shopify_list_variants"), Method: "ShopifyListVariantsTool"},
		{Name: "shopify_list_orders", Description: t.DescribeTool("shopify_list_orders"), Method: "ShopifyListOrdersTool"},
		{Name: "shopify_get_order", Description: t.DescribeTool("shopify_get_order"), Method: "ShopifyGetOrderTool"},
		{Name: "shopify_get_customer", Description: t.DescribeTool("shopify_get_customer"), Method: "ShopifyGetCustomerTool"},
		{Name: "shopify_list_customers", Description: t.DescribeTool("shopify_list_customers"), Method: "ShopifyListCustomersTool"},
		{Name: "shopify_search_customers", Description: t.DescribeTool("shopify_search_customers"), Method: "ShopifySearchCustomersTool"},
		{Name: "shopify_get_inventory_level", Description: t.DescribeTool("shopify_get_inventory_level"), Method: "ShopifyGetInventoryLevelTool"},
		{Name: "shopify_get_reports", Description: t.DescribeTool("shopify_get_reports"), Method: "ShopifyGetReportsTool"},
		{Name: "shopify_get_orders_summary", Description: t.DescribeTool("shopify_get_orders_summary"), Method: "ShopifyGetOrdersSummaryTool"},
		{Name: "shopify_list_images", Description: t.DescribeTool("shopify_list_images"), Method: "ShopifyListImagesTool"},
	}
	agent.AddTools(loop.WrapToolsWithDefinitions(t, defs)...)
	fmt.Println(color.HiBlackString("Tools: shopify read (via broker)"))
}

func addBrokerShopifyWriteTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	t := broker.NewShopifyTools(brokerClient)
	defs := []loop.ToolDefinition{
		{Name: "shopify_create_product", Description: t.DescribeTool("shopify_create_product"), Method: "ShopifyCreateProductTool"},
		{Name: "shopify_update_product", Description: t.DescribeTool("shopify_update_product"), Method: "ShopifyUpdateProductTool"},
		{Name: "shopify_delete_product", Description: t.DescribeTool("shopify_delete_product"), Method: "ShopifyDeleteProductTool"},
		{Name: "shopify_update_variant", Description: t.DescribeTool("shopify_update_variant"), Method: "ShopifyUpdateVariantTool"},
		{Name: "shopify_update_order", Description: t.DescribeTool("shopify_update_order"), Method: "ShopifyUpdateOrderTool"},
		{Name: "shopify_create_fulfillment", Description: t.DescribeTool("shopify_create_fulfillment"), Method: "ShopifyCreateFulfillmentTool"},
		{Name: "shopify_set_inventory_level", Description: t.DescribeTool("shopify_set_inventory_level"), Method: "ShopifySetInventoryLevelTool"},
		{Name: "shopify_upload_image", Description: t.DescribeTool("shopify_upload_image"), Method: "ShopifyUploadImageTool"},
		{Name: "shopify_sync_inventory", Description: t.DescribeTool("shopify_sync_inventory"), Method: "ShopifySyncInventoryTool"},
		{Name: "shopify_create_listing", Description: t.DescribeTool("shopify_create_listing"), Method: "ShopifyCreateListingTool"},
		{Name: "shopify_list_listings", Description: t.DescribeTool("shopify_list_listings"), Method: "ShopifyListListingsTool"},
		{Name: "shopify_delete_listing", Description: t.DescribeTool("shopify_delete_listing"), Method: "ShopifyDeleteListingTool"},
	}
	agent.AddTools(loop.WrapToolsWithDefinitions(t, defs)...)
	fmt.Println(color.HiBlackString("Tools: shopify write (via broker)"))
}

func addBrokerSupplierTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	supplierTools := broker.NewSupplierTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(supplierTools)...)
	fmt.Println(color.HiBlackString("Tools: supplier (via broker)"))
}

func addBrokerAdsTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	adsTools := broker.NewAdsTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(adsTools)...)
	fmt.Println(color.HiBlackString("Tools: ads (via broker)"))
}

func addBrokerEcommerceTools(agent *loop.AgenticLoop, brokerClient *broker.Client) {
	ecommerceTools := broker.NewEcommerceTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(ecommerceTools)...)
	fmt.Println(color.HiBlackString("Tools: ecommerce (via broker)"))
}

func addBrokerCompanyMethodTools(ctx context.Context, agent *loop.AgenticLoop, brokerClient *broker.Client, enabled map[string]bool) {
	result, err := brokerClient.CallTool(ctx, "get_company_method_tools", map[string]any{})
	if err != nil {
		return
	}
	rawTools, ok := result["tools"].([]any)
	if !ok || len(rawTools) == 0 {
		return
	}

	added := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		spec, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		toolName, _ := spec["tool_name"].(string)
		toolName = strings.TrimSpace(toolName)
		method, _ := spec["method"].(string)
		method = strings.TrimSpace(method)
		if toolName == "" || method == "" {
			continue
		}
		if !companyMethodToolEnabledForAgent(toolName, enabled) {
			continue
		}

		schema := companyMethodToolInputSchema(spec["input_schema"])
		description := companyMethodToolDescription(method, spec)
		toolNameCopy := toolName
		agent.AddTools(loop.NewFuncTool(
			toolNameCopy,
			description,
			schema,
			func(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
				if input == nil {
					input = map[string]any{}
				}
				callResult, err := brokerClient.CallTool(ctx, toolNameCopy, input)
				if err != nil {
					return loop.NewErrorResult(err.Error()), nil
				}
				return loop.NewSuccessResult(callResult), nil
			},
		))
		added = append(added, method)
	}
	if len(added) == 0 {
		return
	}
	sort.Strings(added)
	fmt.Println(color.HiBlackString("Tools: company methods (via broker): %s", strings.Join(added, ", ")))
}

func addBrokerDeepResearchMethodTools(ctx context.Context, agent *loop.AgenticLoop, brokerClient *broker.Client, enabled map[string]bool) {
	result, err := brokerClient.CallTool(ctx, "get_deep_research_method_tools", map[string]any{})
	if err != nil {
		return
	}
	rawTools, ok := result["tools"].([]any)
	if !ok || len(rawTools) == 0 {
		return
	}

	added := make([]string, 0, len(rawTools))
	for _, raw := range rawTools {
		spec, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		toolName, _ := spec["tool_name"].(string)
		toolName = strings.TrimSpace(toolName)
		method, _ := spec["method"].(string)
		method = strings.TrimSpace(method)
		if toolName == "" || method == "" {
			continue
		}
		if !deepResearchMethodToolEnabledForAgent(toolName, enabled) {
			continue
		}

		schema := companyMethodToolInputSchema(spec["input_schema"])
		description := deepResearchMethodToolDescription(method, spec)
		toolNameCopy := toolName
		agent.AddTools(loop.NewFuncTool(
			toolNameCopy,
			description,
			schema,
			func(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
				if input == nil {
					input = map[string]any{}
				}
				callResult, err := brokerClient.CallTool(ctx, toolNameCopy, input)
				if err != nil {
					return loop.NewErrorResult(err.Error()), nil
				}
				return loop.NewSuccessResult(callResult), nil
			},
		))
		added = append(added, method)
	}
	if len(added) == 0 {
		return
	}
	sort.Strings(added)
	fmt.Println(color.HiBlackString("Tools: deep research methods (via broker): %s", strings.Join(added, ", ")))
}

func companyMethodToolEnabledForAgent(toolName string, enabled map[string]bool) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	// Dynamic company method tools are explicit opt-in.
	if enabled == nil {
		return false
	}
	return enabled[toolName]
}

func deepResearchMethodToolEnabledForAgent(toolName string, enabled map[string]bool) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	// Dynamic deep-research method tools are explicit opt-in.
	if enabled == nil {
		return false
	}
	return enabled["deep_research"] || enabled[toolName]
}

func companyMethodToolInputSchema(raw any) *genai.Schema {
	if raw == nil {
		return &genai.Schema{Type: genai.TypeObject}
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return &genai.Schema{Type: genai.TypeObject}
	}
	var schema genai.Schema
	if err := json.Unmarshal(blob, &schema); err != nil {
		return &genai.Schema{Type: genai.TypeObject}
	}
	if schema.Type == "" {
		schema.Type = genai.TypeObject
	}
	return &schema
}

func companyMethodToolDescription(method string, spec map[string]any) string {
	desc, _ := spec["description"].(string)
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return fmt.Sprintf("Call company method %q on a teammate agent.", method)
	}
	return fmt.Sprintf("Call company method %q on a teammate agent. %s", method, desc)
}

func deepResearchMethodToolDescription(method string, spec map[string]any) string {
	desc, _ := spec["description"].(string)
	desc = strings.TrimSpace(desc)
	costWarning := "EXPENSIVE — runs a multi-step research engine (1-3 minutes, multiple API calls). You MUST NOT call this tool more than once on the same topic. If you already called any deep research tool on this subject, use those results. Calling deep_research_answer and deep_research_report on the same topic counts as a duplicate."
	if desc == "" {
		return fmt.Sprintf("Run deep research method %q. %s", method, costWarning)
	}
	return fmt.Sprintf("Run deep research method %q. %s\n%s", method, desc, costWarning)
}
