package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// CompanyFinanceTools exposes company-scoped finance operations.
type CompanyFinanceTools struct {
	client *Client
}

func NewCompanyFinanceTools(client *Client) *CompanyFinanceTools {
	return &CompanyFinanceTools{client: client}
}

func (t *CompanyFinanceTools) CompanyFinanceGetWalletAddressesTool(ctx context.Context, input tools.CompanyFinanceGetWalletAddressesInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_finance_get_wallet_addresses", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinanceGetBalancesTool(ctx context.Context, input tools.GetBalancesInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/wallet/balances", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinanceGetTransactionHistoryTool(ctx context.Context, input tools.GetTransactionHistoryInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/wallet/history", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinancePolymarketSearchMarketsTool(ctx context.Context, input tools.PolymarketSearchMarketsInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/search", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinancePolymarketGetMarketTool(ctx context.Context, input tools.PolymarketGetMarketInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/market", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinancePolymarketGetPricesTool(ctx context.Context, input tools.PolymarketGetPricesInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/prices", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinancePolymarketGetPositionsTool(ctx context.Context, input tools.PolymarketGetPositionsInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/positions", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) CompanyFinancePolymarketPlaceBuyOrderTool(ctx context.Context, input tools.PolymarketPlaceBuyOrderInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/order", tools.PolymarketPlaceOrderInput{
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

func (t *CompanyFinanceTools) CompanyFinancePolymarketPlaceSellOrderTool(ctx context.Context, input tools.PolymarketPlaceSellOrderInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/order", tools.PolymarketPlaceOrderInput{
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

func (t *CompanyFinanceTools) CompanyFinancePolymarketCancelOrderTool(ctx context.Context, input tools.PolymarketCancelOrderInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/polymarket/cancel", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyFinanceTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"company_finance_get_wallet_addresses":        "Get company-scoped ETH and SOL wallet addresses.",
		"company_finance_get_balances":                "Get a fixed company balance snapshot: solana, eth, polygon, polygon_usdte, and polygon_usdce.",
		"company_finance_get_transaction_history":     "Get recent company wallet transaction history.",
		"company_finance_polymarket_search_markets":   "Search Polymarket markets using company identity.",
		"company_finance_polymarket_get_market":       "Get one Polymarket market using company identity.",
		"company_finance_polymarket_get_prices":       "Get Polymarket outcome prices using company identity.",
		"company_finance_polymarket_get_positions":    "Get company Polymarket positions.",
		"company_finance_polymarket_place_buy_order":  "Place a company-scoped Polymarket buy order.",
		"company_finance_polymarket_place_sell_order": "Place a company-scoped Polymarket sell order.",
		"company_finance_polymarket_cancel_order":     "Cancel a company-scoped Polymarket order.",
	}
	return descriptions[name]
}
