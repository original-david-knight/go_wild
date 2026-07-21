package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 600 * time.Second}

// polymarketRoutes maps tool names to broker Polymarket API paths.
var polymarketRoutes = map[string]string{
	"polymarket_search_markets":    "/broker/v1/polymarket/search",
	"polymarket_get_market":        "/broker/v1/polymarket/market",
	"polymarket_get_prices":        "/broker/v1/polymarket/prices",
	"polymarket_get_price_history": "/broker/v1/polymarket/price-history",
	"polymarket_get_candles":       "/broker/v1/polymarket/candles",
	"polymarket_get_orderbook":     "/broker/v1/polymarket/orderbook",
	"polymarket_order_book_depth":  "/broker/v1/polymarket/orderbook-depth",
	"polymarket_get_positions":     "/broker/v1/polymarket/positions",
	"polymarket_place_order":       "/broker/v1/polymarket/order",
	"polymarket_place_buy_order":   "/broker/v1/polymarket/order",
	"polymarket_place_sell_order":  "/broker/v1/polymarket/order",
	"polymarket_cancel_order":      "/broker/v1/polymarket/cancel",
	"polymarket_get_orders":        "/broker/v1/polymarket/orders",
	"polymarket_get_trades":        "/broker/v1/polymarket/trades",
	"polymarket_redeem_winnings":   "/broker/v1/polymarket/redeem",
	"polymarket_add_market_note":   "/broker/v1/polymarket/market-notes/add",
	"polymarket_list_market_notes": "/broker/v1/polymarket/market-notes/list",
}

// callBrokerTool routes a tool call to the appropriate broker endpoint.
func callBrokerTool(brokerURL, token, executionMethod, toolName string, args map[string]any) (any, error) {
	// Handle buy/sell order side injection
	if toolName == "polymarket_place_buy_order" {
		if args == nil {
			args = map[string]any{}
		}
		args["side"] = "BUY"
	} else if toolName == "polymarket_place_sell_order" {
		if args == nil {
			args = map[string]any{}
		}
		args["side"] = "SELL"
	}

	// Check polymarket routes first
	if route, ok := polymarketRoutes[toolName]; ok {
		return postToBroker(brokerURL+route, token, executionMethod, args)
	}

	// Core tools go to /broker/v1/tools/{toolName}
	return postToBroker(brokerURL+"/broker/v1/tools/"+toolName, token, executionMethod, args)
}

// callMCPTool calls a host-side MCP server tool via the broker's mcp_call_tool handler.
func callMCPTool(brokerURL, token, executionMethod, serverID, toolName string, args map[string]any) (any, error) {
	body := map[string]any{
		"server_id": serverID,
		"tool_name": toolName,
	}
	if args != nil {
		body["arguments"] = args
	}
	return postToBroker(brokerURL+"/broker/v1/tools/mcp_call_tool", token, executionMethod, body)
}

func postToBroker(url, token, executionMethod string, body any) (any, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	} else {
		bodyReader = strings.NewReader("{}")
	}

	req, err := http.NewRequest("POST", url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if executionMethod != "" {
		req.Header.Set("X-Gowild-Execution-Method", executionMethod)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("broker returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Return raw text if not valid JSON
		return string(respBody), nil
	}
	return result, nil
}
