package data

// ToolGroup describes a set of tools that can be enabled/disabled per agent.
type ToolGroup struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
}

// AllToolGroups is the canonical list of tool groups.
// New groups added here are enabled by default (we store disabled, not enabled).
var AllToolGroups = []ToolGroup{
	{ID: "skills", DisplayName: "Skills", Description: "Python sandbox and saved skills", Tools: []string{
		"run_python", "save_skill", "execute_skill", "test_skill", "list_skills", "get_skill", "delete_skill",
	}},
	{ID: "web_search", DisplayName: "Web Search", Description: "Web search via Gemini Grounding with Google Search", Tools: []string{
		"web_search",
	}},
	{ID: "web_reader", DisplayName: "Web Reader", Description: "Read webpages and fetch images", Tools: []string{
		"read_webpage",
	}},
	{ID: "http", DisplayName: "HTTP", Description: "Raw HTTP requests", Tools: []string{
		"http_request",
	}},
	{ID: "report", DisplayName: "Report", Description: "HTML report content for manager UI", Tools: []string{
		"set_report_html", "get_report_html",
	}},
	{ID: "soul", DisplayName: "Soul", Description: "Agent identity and values", Tools: []string{
		"read_soul", "update_soul",
	}},
	{ID: "knowledge_graph", DisplayName: "Knowledge Graph", Description: "Graph database with entities and relations", Tools: []string{
		"kg_search", "kg_add", "kg_get", "kg_update", "kg_delete", "kg_explore",
	}},
	{ID: "deep_research", DisplayName: "Deep Research", Description: "Deep research engine methods", Tools: []string{
		"dynamic: methods from Pipelines > Deep Research",
	}},
	{ID: "company_admin", DisplayName: "Company Admin", Description: "Company metadata, membership, CEO, and governance operations", Tools: []string{
		"company_admin_get_context", "company_admin_update_company", "company_admin_list_members",
		"company_admin_add_member", "company_admin_remove_member", "company_admin_set_ceo",
		"company_admin_send_heartbeat", "send_company_heartbeat",
	}},
	{ID: "company_knowledge", DisplayName: "Company Knowledge", Description: "Shared company knowledge base", Tools: []string{
		"company_knowledge_search", "company_knowledge_add", "company_knowledge_get",
		"company_knowledge_update", "company_knowledge_delete",
	}},
	{ID: "company_finance", DisplayName: "Company Finance", Description: "Company-scoped finance helpers and execution identity tools", Tools: []string{
		"company_finance_get_wallet_addresses", "company_finance_get_balances", "company_finance_get_transaction_history",
	}},
	{ID: "wallet", DisplayName: "Wallet", Description: "Crypto wallet (ETH/SOL)", Tools: []string{
		"get_wallet_address", "get_wallet_addresses", "get_balances", "sign_message",
		"send_token", "swap_token", "contract_call", "get_transaction_history",
		"encrypt_message", "decrypt_message", "get_ed25519_public_key",
	}},
	{ID: "polymarket_read", DisplayName: "Polymarket Read", Description: "Polymarket market data, positions, orders, and redeem", Tools: []string{
		"polymarket_search_markets", "polymarket_get_market", "polymarket_get_prices",
		"polymarket_get_price_history", "polymarket_get_candles", "polymarket_get_orderbook",
		"polymarket_order_book_depth", "polymarket_get_positions", "polymarket_get_orders",
		"polymarket_get_trades", "polymarket_redeem_winnings",
	}},
	{ID: "polymarket_notes", DisplayName: "Polymarket Notes", Description: "Polymarket market notes (add and list)", Tools: []string{
		"polymarket_add_market_note", "polymarket_list_market_notes",
	}},
	{ID: "polymarket_buy", DisplayName: "Polymarket Buy", Description: "Polymarket buy order placement", Tools: []string{
		"polymarket_place_buy_order",
	}},
	{ID: "polymarket_sell", DisplayName: "Polymarket Sell", Description: "Polymarket sell order placement and cancellation", Tools: []string{
		"polymarket_place_sell_order", "polymarket_cancel_order",
	}},
	{ID: "shell", DisplayName: "Shell", Description: "Shell command execution", Tools: []string{
		"run_shell",
	}},
	{ID: "claude_code", DisplayName: "Claude Code", Description: "Claude Code one-shot coding tool", Tools: []string{
		"claude_code",
	}},
	{ID: "file", DisplayName: "File", Description: "File read/write/edit operations", Tools: []string{
		"read_file", "write_file", "edit_file", "list_files",
	}},
	{ID: "tasks", DisplayName: "Tasks", Description: "Task management and planning", Tools: []string{
		"add_task", "mark_task_done", "mark_task_deprecated", "list_tasks",
		"move_task", "block_task", "unblock_task", "sleep_task", "plan_task", "evaluate_task",
	}},
	{ID: "telegram", DisplayName: "Telegram", Description: "Telegram bot messaging", Tools: []string{
		"telegram_send", "telegram_get_updates", "telegram_get_chats", "telegram_get_bot_info",
	}},
	{ID: "email", DisplayName: "Email", Description: "Email send/receive via AgentMail", Tools: []string{
		"list_emails", "read_email", "send_email",
	}},
	{ID: "reuters", DisplayName: "Reuters", Description: "Reuters news fetching and search", Tools: []string{
		"reuters_news", "search_reuters_news", "read_reuters_article",
	}},
	{ID: "messaging", DisplayName: "Messaging", Description: "Send and receive messages with peer agents", Tools: []string{
		"list_peers", "send_message", "read_messages", "mark_messages_read",
	}},
	{ID: "mcp", DisplayName: "MCP", Description: "External MCP tools (local and host-side)", Tools: []string{
		"mcp_list_servers", "mcp_set_local_server", "mcp_remove_local_server",
		"mcp_set_server_enabled", "mcp_local_<server_id>__<tool_name>", "mcp_host_<server_id>__<tool_name>",
	}},
	{ID: "shopify_read", DisplayName: "Shopify Read", Description: "Read-only Shopify data (products, orders, customers, inventory, analytics, listings)", Tools: []string{
		"shopify_get_product", "shopify_list_products", "shopify_list_variants",
		"shopify_list_orders", "shopify_get_order", "shopify_get_customer",
		"shopify_list_customers", "shopify_search_customers", "shopify_get_inventory_level",
		"shopify_get_reports", "shopify_get_orders_summary", "shopify_list_images",
		"shopify_list_listings",
	}},
	{ID: "shopify_write", DisplayName: "Shopify Write", Description: "Shopify mutations (create/update/delete products, fulfill orders, set inventory, supplier sync)", Tools: []string{
		"shopify_create_product", "shopify_update_product", "shopify_delete_product",
		"shopify_update_variant", "shopify_update_order", "shopify_create_fulfillment",
		"shopify_set_inventory_level", "shopify_upload_image",
		"shopify_sync_inventory", "shopify_create_listing", "shopify_delete_listing",
	}},
	{ID: "shopify_theme_read", DisplayName: "Shopify Theme Read", Description: "Read-only Shopify theme data (themes, assets, pages)", Tools: []string{
		"shopify_list_themes", "shopify_get_theme", "shopify_list_assets", "shopify_get_asset",
		"shopify_list_pages", "shopify_get_page",
	}},
	{ID: "shopify_theme_write", DisplayName: "Shopify Theme Write", Description: "Shopify theme mutations (create/update/delete assets and pages)", Tools: []string{
		"shopify_update_asset", "shopify_delete_asset",
		"shopify_create_page", "shopify_update_page", "shopify_delete_page",
	}},
	{ID: "amazon", DisplayName: "Amazon", Description: "Amazon Product Advertising API (search catalog, lookup ASINs, affiliate links)", Tools: []string{
		"amazon_search", "amazon_get_product",
	}},
	{ID: "supplier", DisplayName: "Supplier", Description: "Drop-shipping supplier integration (search, order, tracking)", Tools: []string{
		"supplier_search_products", "supplier_get_product", "supplier_get_shipping",
		"supplier_place_order", "supplier_get_order", "supplier_cancel_order", "supplier_get_tracking",
	}},
	{ID: "ads", DisplayName: "Ads", Description: "Meta and Google advertising (campaigns, adsets, creatives)", Tools: []string{
		"ads_meta_create_campaign", "ads_meta_update_campaign", "ads_meta_get_campaign",
		"ads_meta_pause_campaign", "ads_meta_create_adset", "ads_meta_update_adset",
		"ads_meta_create_ad", "ads_meta_get_ad_performance", "ads_google_create_campaign",
		"ads_google_update_campaign", "ads_get_daily_spend", "ads_get_campaign_roas",
	}},
	{ID: "ecommerce", DisplayName: "E-commerce", Description: "Cross-platform analytics (P&L, pricing, margins)", Tools: []string{
		"ecommerce_product_pnl", "ecommerce_daily_pnl", "ecommerce_calculate_margin", "ecommerce_suggest_price",
	}},
	{ID: "content", DisplayName: "Content", Description: "Content display tools (images, SVG, audio)", Tools: []string{
		"show_image", "show_svg", "show_audio",
	}},
	{ID: "paywall", DisplayName: "Paywall", Description: "Crypto paywall for selling digital assets (USDC on Polygon/Solana)", Tools: []string{
		"create_crypto_paywall",
	}},
	{ID: "sites", DisplayName: "Sites", Description: "Publish static HTML websites on agent402.net", Tools: []string{
		"publish_site", "list_sites",
	}},
}
