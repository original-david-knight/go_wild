package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// PolymarketReadTools exposes read-only Polymarket operations.
type PolymarketReadTools struct {
	client *Client
}

// PolymarketBuyTools exposes buy-side order operations.
type PolymarketBuyTools struct {
	client *Client
}

// PolymarketSellTools exposes sell-side order operations.
type PolymarketSellTools struct {
	client *Client
}

var polymarketToolDescriptions = map[string]string{
	"polymarket_search_markets":    "Search Polymarket prediction markets by keyword",
	"polymarket_get_market":        "Get details for a specific Polymarket prediction market by its condition_id (hex string starting with 0x, from search results). Do NOT pass a token_id here — token_id identifies an outcome token, condition_id identifies the market.",
	"polymarket_get_prices":        "Get the current price for a Polymarket outcome token",
	"polymarket_get_price_history": "Get historical prices for a Polymarket outcome token",
	"polymarket_get_candles":       "Get OHLC candles aggregated from Polymarket price history",
	"polymarket_get_orderbook":     "Get the order book for a Polymarket outcome token",
	"polymarket_order_book_depth":  "Get level-2 order book depth for a Polymarket outcome token (bids/asks with cumulative liquidity)",
	"polymarket_get_positions":     "Get your current Polymarket positions",
	"polymarket_place_order":       "Place a buy or sell order on Polymarket",
	"polymarket_place_buy_order":   "Place a buy order on Polymarket",
	"polymarket_place_sell_order":  "Place a sell order on Polymarket",
	"polymarket_cancel_order":      "Cancel an existing Polymarket order",
	"polymarket_get_orders":        "Get your open Polymarket orders",
	"polymarket_get_trades":        "Get your Polymarket trade history",
	"polymarket_redeem_winnings":   "Redeem all currently redeemable winning balances for this wallet on Polygon. No input required: pass {}.",
	"polymarket_add_market_note":   "Add a note to a Polymarket market. Notes are shared across all agents in the company and are automatically included when viewing market details.",
	"polymarket_list_market_notes": "List all notes for a Polymarket market. Notes are shared across all agents in the company.",
}

// NewPolymarketReadTools creates broker-backed read-only Polymarket tools.
func NewPolymarketReadTools(client *Client) *PolymarketReadTools {
	return &PolymarketReadTools{client: client}
}

// NewPolymarketBuyTools creates broker-backed Polymarket buy tools.
func NewPolymarketBuyTools(client *Client) *PolymarketBuyTools {
	return &PolymarketBuyTools{client: client}
}

// NewPolymarketSellTools creates broker-backed Polymarket sell tools.
func NewPolymarketSellTools(client *Client) *PolymarketSellTools {
	return &PolymarketSellTools{client: client}
}

func (p *PolymarketReadTools) PolymarketSearchMarketsTool(ctx context.Context, input tools.PolymarketSearchMarketsInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/search", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetMarketTool(ctx context.Context, input tools.PolymarketGetMarketInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/market", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetPricesTool(ctx context.Context, input tools.PolymarketGetPricesInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/prices", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetPriceHistoryTool(ctx context.Context, input tools.PolymarketGetPriceHistoryInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/price-history", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetCandlesTool(ctx context.Context, input tools.PolymarketGetCandlesInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/candles", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetOrderbookTool(ctx context.Context, input tools.PolymarketGetOrderbookInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/orderbook", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketOrderBookDepthTool(ctx context.Context, input tools.PolymarketOrderBookDepthInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/orderbook-depth", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetPositionsTool(ctx context.Context, input tools.PolymarketGetPositionsInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/positions", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetOrdersTool(ctx context.Context, input tools.PolymarketGetOrdersInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/orders", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketGetTradesTool(ctx context.Context, input tools.PolymarketGetTradesInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/trades", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketRedeemWinningsTool(ctx context.Context, input tools.PolymarketRedeemWinningsInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/redeem", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketAddMarketNoteTool(ctx context.Context, input tools.PolymarketAddMarketNoteInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/market-notes/add", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketReadTools) PolymarketListMarketNotesTool(ctx context.Context, input tools.PolymarketListMarketNotesInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/market-notes/list", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (p *PolymarketReadTools) DescribeTool(name string) string {
	return polymarketToolDescriptions[name]
}

func (p *PolymarketBuyTools) PolymarketPlaceBuyOrderTool(ctx context.Context, input tools.PolymarketPlaceBuyOrderInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/order", tools.PolymarketPlaceOrderInput{
		TokenID:   input.TokenID,
		Price:     input.Price,
		Size:      input.Size,
		Side:      "BUY",
		OrderType: input.OrderType,
		NegRisk:   input.NegRisk,
	})
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (p *PolymarketBuyTools) DescribeTool(name string) string {
	return polymarketToolDescriptions[name]
}

func (p *PolymarketSellTools) PolymarketPlaceSellOrderTool(ctx context.Context, input tools.PolymarketPlaceSellOrderInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/order", tools.PolymarketPlaceOrderInput{
		TokenID:   input.TokenID,
		Price:     input.Price,
		Size:      input.Size,
		Side:      "SELL",
		OrderType: input.OrderType,
		NegRisk:   input.NegRisk,
	})
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (p *PolymarketSellTools) PolymarketCancelOrderTool(ctx context.Context, input tools.PolymarketCancelOrderInput) (*loop.ToolResult, error) {
	result, err := p.client.Post(ctx, "/broker/v1/polymarket/cancel", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (p *PolymarketSellTools) DescribeTool(name string) string {
	return polymarketToolDescriptions[name]
}
