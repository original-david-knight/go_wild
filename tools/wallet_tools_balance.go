package tools

import (
	"context"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
)

// GetBalanceTool returns the wallet balance for native tokens or ERC20/SPL tokens.
func (w *WalletTools) GetBalanceTool(ctx context.Context, input GetBalanceInput) (*loop.ToolResult, error) {
	var result *gowild_crypto.BalanceResult
	var err error
	chain := strings.ToLower(strings.TrimSpace(input.Chain))
	tokenAddress := strings.TrimSpace(input.TokenAddress)
	if chain == "polygon" {
		chain = "ethereum"
	}

	if tokenAddress == "" {
		// Native token balance
		result, err = w.wallet.GetBalance(ctx, gowild_crypto.Chain(chain))
	} else {
		// Token balance
		result, err = w.wallet.GetTokenBalance(ctx, gowild_crypto.Chain(chain), tokenAddress)
	}

	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	response := map[string]any{
		"chain":       string(result.Chain),
		"address":     result.Address,
		"balance":     result.Balance,
		"balance_raw": result.BalanceRaw,
		"symbol":      result.Symbol,
		"decimals":    result.Decimals,
	}
	if strings.EqualFold(strings.TrimSpace(input.Chain), "polygon") {
		response["chain"] = "polygon"
	}
	if tokenAddress == "" {
		response["balance_type"] = "native"
		if chain == "ethereum" {
			response["note"] = "Native EVM balance only (ETH/POL). ERC20 balances such as USDC require token_address."
		}
	} else {
		response["balance_type"] = "token"
	}
	if result.TokenAddress != "" {
		response["token_address"] = result.TokenAddress
	}

	return loop.NewSuccessResult(response), nil
}
