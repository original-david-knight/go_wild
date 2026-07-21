package tools

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
)

// SwapTokenTool swaps one token for another using DEX aggregators.
func (w *WalletTools) SwapTokenTool(ctx context.Context, input SwapTokenInput) (*loop.ToolResult, error) {
	if input.FromToken == "" || input.ToToken == "" {
		return loop.NewErrorResult("from_token and to_token are required"), nil
	}
	if input.Amount == "" {
		return loop.NewErrorResult("amount is required"), nil
	}

	result, err := w.wallet.SwapToken(ctx, gowild_crypto.Chain(input.Chain), input.FromToken, input.ToToken, input.Amount, input.SlippageBps)
	if err != nil {
		w.logTransaction(ctx, &data.WalletTransaction{
			Chain:  input.Chain,
			Type:   "swap_token",
			Amount: input.Amount,
			Status: "failed",
			Error:  err.Error(),
			Metadata: map[string]any{
				"from_token":   input.FromToken,
				"to_token":     input.ToToken,
				"slippage_bps": input.SlippageBps,
			},
		})
		return loop.NewErrorResult(err.Error()), nil
	}

	w.logTransaction(ctx, &data.WalletTransaction{
		Chain:           input.Chain,
		Type:            "swap_token",
		Amount:          result.FromAmount,
		TransactionHash: result.TransactionHash,
		Status:          "pending",
		ExplorerURL:     result.ExplorerURL,
		Metadata: map[string]any{
			"from_token":  result.FromToken,
			"to_token":    result.ToToken,
			"from_amount": result.FromAmount,
			"to_amount":   result.ToAmount,
		},
	})

	return loop.NewSuccessResult(map[string]any{
		"chain":            string(result.Chain),
		"transaction_hash": result.TransactionHash,
		"from_token":       result.FromToken,
		"to_token":         result.ToToken,
		"from_amount":      result.FromAmount,
		"to_amount":        result.ToAmount,
		"explorer_url":     result.ExplorerURL,
	}), nil
}
