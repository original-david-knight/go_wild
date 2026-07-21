package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// allTools returns the complete set of statically defined MCP tools.
func allTools() []mcpTool {
	tools := make([]mcpTool, 0, len(polymarketTools)+len(reutersTools)+len(webReaderTools)+len(coreTools))
	tools = append(tools, polymarketTools...)
	tools = append(tools, reutersTools...)
	tools = append(tools, webReaderTools...)
	tools = append(tools, coreTools...)
	return tools
}

// fetchDynamicTools calls the broker's mcp-tools/list endpoint and returns
// the additional tools plus any MCP routing metadata.
func fetchDynamicTools(brokerURL, token string) ([]mcpTool, map[string]mcpRoute, error) {
	raw, err := postToBroker(brokerURL+"/broker/v1/mcp-tools/list", token, "", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch dynamic tools: %w", err)
	}

	body, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch dynamic tools: marshal response: %w", err)
	}

	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"input_schema"`
			Route       string `json:"route"`
			MCPServerID string `json:"mcp_server_id"`
			MCPToolName string `json:"mcp_tool_name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("fetch dynamic tools: parse response: %w", err)
	}

	tools := make([]mcpTool, 0, len(resp.Tools))
	routes := make(map[string]mcpRoute)

	for _, t := range resp.Tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}

		tool := mcpTool{
			Name:        name,
			Description: strings.TrimSpace(t.Description),
			InputSchema: convertInputSchema(t.InputSchema),
		}
		tools = append(tools, tool)

		if strings.TrimSpace(t.Route) == "mcp" && t.MCPServerID != "" && t.MCPToolName != "" {
			routes[name] = mcpRoute{
				ServerID: t.MCPServerID,
				ToolName: t.MCPToolName,
			}
		}
	}

	if len(tools) > 0 {
		log.Printf("Discovered %d dynamic tools from broker (%d MCP-routed)", len(tools), len(routes))
	}
	return tools, routes, nil
}

// convertInputSchema converts a generic input_schema (from the broker) into
// the mcpSchema type used by the MCP protocol.
func convertInputSchema(raw any) *mcpSchema {
	if raw == nil {
		return &mcpSchema{Type: "object", Properties: map[string]*mcpSchema{}}
	}

	// Re-marshal and unmarshal into mcpSchema.
	data, err := json.Marshal(raw)
	if err != nil {
		return &mcpSchema{Type: "object", Properties: map[string]*mcpSchema{}}
	}

	var schema mcpSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return &mcpSchema{Type: "object", Properties: map[string]*mcpSchema{}}
	}

	// Ensure type is set.
	if schema.Type == "" {
		schema.Type = "object"
	}
	return &schema
}

// polymarketTools defines the Polymarket tool schemas.
var polymarketTools = []mcpTool{
	{
		Name:        "polymarket_search_markets",
		Description: "Search Polymarket prediction markets by keyword",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"query": {Type: "string", Description: "Search query for prediction markets (e.g., 'election', 'bitcoin', 'fed rate')"},
				"limit": {Type: "integer", Description: "Maximum number of results to return (default 10, max 100)"},
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "polymarket_get_market",
		Description: "Get details for a specific Polymarket prediction market by its condition_id (hex string starting with 0x, from search results). Do NOT pass a token_id here.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"condition_id": {Type: "string", Description: "The condition ID (hex string starting with 0x) that identifies a market"},
			},
			Required: []string{"condition_id"},
		},
	},
	{
		Name:        "polymarket_get_prices",
		Description: "Get the current price for a Polymarket outcome token",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id": {Type: "string", Description: "The CLOB token ID to get prices for"},
				"side":     {Type: "string", Description: "Order side to get the best price for", Enum: []string{"buy", "sell"}},
			},
			Required: []string{"token_id", "side"},
		},
	},
	{
		Name:        "polymarket_get_price_history",
		Description: "Get historical prices for a Polymarket outcome token",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id": {Type: "string", Description: "The CLOB token ID to get historical prices for"},
				"interval": {Type: "string", Description: "Relative window ending now (default: 1d)", Enum: []string{"1m", "1h", "6h", "1d", "1w", "max"}},
				"start_ts": {Type: "integer", Description: "Unix start timestamp in seconds (UTC)"},
				"end_ts":   {Type: "integer", Description: "Unix end timestamp in seconds (UTC)"},
				"fidelity": {Type: "integer", Description: "Resolution in minutes between points"},
			},
			Required: []string{"token_id"},
		},
	},
	{
		Name:        "polymarket_get_candles",
		Description: "Get OHLC candles aggregated from Polymarket price history",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id":       {Type: "string", Description: "The CLOB token ID to build candles for"},
				"candle_minutes": {Type: "integer", Description: "Candle size in minutes (default 60)"},
				"interval":       {Type: "string", Description: "Relative window ending now (default: 1d)", Enum: []string{"1m", "1h", "6h", "1d", "1w", "max"}},
				"start_ts":       {Type: "integer", Description: "Unix start timestamp in seconds (UTC)"},
				"end_ts":         {Type: "integer", Description: "Unix end timestamp in seconds (UTC)"},
				"fidelity":       {Type: "integer", Description: "Resolution in minutes between samples"},
			},
			Required: []string{"token_id"},
		},
	},
	{
		Name:        "polymarket_get_orderbook",
		Description: "Get the order book for a Polymarket outcome token",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id": {Type: "string", Description: "The CLOB token ID to get the order book for"},
			},
			Required: []string{"token_id"},
		},
	},
	{
		Name:        "polymarket_order_book_depth",
		Description: "Get level-2 order book depth for a Polymarket outcome token (bids/asks with cumulative liquidity)",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id": {Type: "string", Description: "The CLOB token ID to get order book depth for"},
				"levels":   {Type: "integer", Description: "Number of price levels per side to return (default 10, max 100)"},
			},
			Required: []string{"token_id"},
		},
	},
	{
		Name:        "polymarket_get_positions",
		Description: "Get your current Polymarket positions",
		InputSchema: &mcpSchema{
			Type:       "object",
			Properties: map[string]*mcpSchema{},
		},
	},
	{
		Name:        "polymarket_place_order",
		Description: "Place a buy or sell order on Polymarket",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id":   {Type: "string", Description: "The CLOB token ID to trade"},
				"price":      {Type: "number", Description: "Price per share between 0 and 1 (e.g., 0.55 for 55 cents)"},
				"size":       {Type: "number", Description: "Number of shares to buy or sell"},
				"side":       {Type: "string", Description: "Order side", Enum: []string{"BUY", "SELL"}},
				"order_type": {Type: "string", Description: "Order time-in-force type (default GTC)", Enum: []string{"GTC", "FOK", "GTD"}},
				"neg_risk":   {Type: "boolean", Description: "Whether this market uses the NegRisk exchange"},
			},
			Required: []string{"token_id", "price", "size", "side"},
		},
	},
	{
		Name:        "polymarket_place_buy_order",
		Description: "Place a buy order on Polymarket",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id":   {Type: "string", Description: "The CLOB token ID to buy"},
				"price":      {Type: "number", Description: "Price per share between 0 and 1 (e.g., 0.55 for 55 cents)"},
				"size":       {Type: "number", Description: "Number of shares to buy"},
				"order_type": {Type: "string", Description: "Order time-in-force type (default GTC)", Enum: []string{"GTC", "FOK", "GTD"}},
				"neg_risk":   {Type: "boolean", Description: "Whether this market uses the NegRisk exchange"},
			},
			Required: []string{"token_id", "price", "size"},
		},
	},
	{
		Name:        "polymarket_place_sell_order",
		Description: "Place a sell order on Polymarket",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"token_id":   {Type: "string", Description: "The CLOB token ID to sell"},
				"price":      {Type: "number", Description: "Price per share between 0 and 1 (e.g., 0.55 for 55 cents)"},
				"size":       {Type: "number", Description: "Number of shares to sell"},
				"order_type": {Type: "string", Description: "Order time-in-force type (default GTC)", Enum: []string{"GTC", "FOK", "GTD"}},
				"neg_risk":   {Type: "boolean", Description: "Whether this market uses the NegRisk exchange"},
			},
			Required: []string{"token_id", "price", "size"},
		},
	},
	{
		Name:        "polymarket_cancel_order",
		Description: "Cancel an existing Polymarket order",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"order_id": {Type: "string", Description: "The ID of the order to cancel"},
			},
			Required: []string{"order_id"},
		},
	},
	{
		Name:        "polymarket_get_orders",
		Description: "Get your open Polymarket orders",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"market": {Type: "string", Description: "Filter orders by market condition ID. Leave empty for all orders."},
			},
		},
	},
	{
		Name:        "polymarket_get_trades",
		Description: "Get your Polymarket trade history",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"limit": {Type: "integer", Description: "Maximum number of trades to return (default 20)"},
			},
		},
	},
	{
		Name:        "polymarket_redeem_winnings",
		Description: "Redeem all currently redeemable winning balances. No input required: pass {}.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"include_losing": {Type: "boolean", Description: "If true, also redeem losing positions to clean up the portfolio"},
			},
		},
	},
	{
		Name:        "polymarket_add_market_note",
		Description: "Add a note to a Polymarket market. Notes are shared across all agents in the company.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"condition_id": {Type: "string", Description: "The condition ID of the market to add a note to"},
				"content":      {Type: "string", Description: "The note content (max 2000 characters)"},
			},
			Required: []string{"condition_id", "content"},
		},
	},
	{
		Name:        "polymarket_list_market_notes",
		Description: "List all notes for a Polymarket market. Notes are shared across all agents in the company.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"condition_id": {Type: "string", Description: "The condition ID of the market to list notes for"},
				"limit":        {Type: "integer", Description: "Maximum number of notes to return (default 50)"},
			},
			Required: []string{"condition_id"},
		},
	},
}

// reutersTools defines the Reuters news tool schemas.
var reutersTools = []mcpTool{
	{
		Name:        "reuters_news",
		Description: "Get the latest Reuters headlines with abbreviated previews. Returns article URLs, titles, and truncated content. Use read_reuters_article to get the full text of any article.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"max_articles": {Type: "integer", Description: "Maximum number of articles to fetch from the frontpage (default: 10, max: 30)"},
			},
		},
	},
	{
		Name:        "search_reuters_news",
		Description: "Search Reuters for articles on a specific topic. Returns matching article URLs, titles, and truncated content. Use read_reuters_article to get the full text of any article.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"query":        {Type: "string", Description: "The search query to find Reuters articles about a specific topic"},
				"max_articles": {Type: "integer", Description: "Maximum number of articles to return (default: 10, max: 50)"},
			},
			Required: []string{"query"},
		},
	},
	{
		Name:        "read_reuters_article",
		Description: "Read the full untruncated text of a Reuters article by URL. Use this after reuters_news or search_reuters_news to get the complete article content.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"url": {Type: "string", Description: "The full Reuters article URL to read"},
			},
			Required: []string{"url"},
		},
	},
}

// webReaderTools defines webpage reading schemas.
var webReaderTools = []mcpTool{
	{
		Name:        "read_webpage",
		Description: "Fetch and read a webpage. Returns clean markdown by default, or raw HTML when requested.",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"url":      {Type: "string", Description: "The full HTTP or HTTPS URL to read"},
				"raw_html": {Type: "boolean", Description: "If true, return raw HTML instead of markdown"},
			},
			Required: []string{"url"},
		},
	},
}

// coreTools defines the core agent tool schemas.
var coreTools = []mcpTool{
	{
		Name:        "read_soul",
		Description: "Read the agent's identity/soul document",
		InputSchema: &mcpSchema{
			Type:       "object",
			Properties: map[string]*mcpSchema{},
		},
	},
	{
		Name:        "get_memory",
		Description: "Read the agent's short-term memory",
		InputSchema: &mcpSchema{
			Type:       "object",
			Properties: map[string]*mcpSchema{},
		},
	},
	{
		Name:        "list_tasks",
		Description: "List the agent's active tasks",
		InputSchema: &mcpSchema{
			Type:       "object",
			Properties: map[string]*mcpSchema{},
		},
	},
	{
		Name:        "add_task",
		Description: "Create a new task for the agent",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"title":       {Type: "string", Description: "Task title"},
				"description": {Type: "string", Description: "Task description"},
				"priority":    {Type: "string", Description: "Task priority", Enum: []string{"low", "medium", "high"}},
			},
			Required: []string{"title"},
		},
	},
	{
		Name:        "mark_task_done",
		Description: "Mark a task as completed",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"task_id": {Type: "string", Description: "The ID of the task to mark done"},
			},
			Required: []string{"task_id"},
		},
	},
	{
		Name:        "get_chat_history",
		Description: "Get recent conversation history",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"limit": {Type: "integer", Description: "Maximum number of messages to return (default 50)"},
			},
		},
	},
	{
		Name:        "get_recurring_tasks",
		Description: "Get the agent's recurring task definitions",
		InputSchema: &mcpSchema{
			Type:       "object",
			Properties: map[string]*mcpSchema{},
		},
	},
	{
		Name:        "update_soul",
		Description: "Update the agent's identity/soul document",
		InputSchema: &mcpSchema{
			Type: "object",
			Properties: map[string]*mcpSchema{
				"content": {Type: "string", Description: "The new soul content"},
			},
			Required: []string{"content"},
		},
	},
}
