package tools

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
)

// SendTokenTool sends native tokens (ETH/SOL) or ERC20/SPL tokens to an address.
func (w *WalletTools) SendTokenTool(ctx context.Context, input SendTokenInput) (*loop.ToolResult, error) {
	if input.To == "" {
		return loop.NewErrorResult("destination address is required"), nil
	}
	if input.Amount == "" {
		return loop.NewErrorResult("amount is required"), nil
	}

	result, err := w.wallet.SendToken(ctx, gowild_crypto.Chain(input.Chain), input.To, input.Amount, input.TokenAddress, input.Memo)
	if err != nil {
		w.logTransaction(ctx, &data.WalletTransaction{
			Chain:        input.Chain,
			Type:         "send_token",
			ToAddress:    input.To,
			Amount:       input.Amount,
			TokenAddress: input.TokenAddress,
			Status:       "failed",
			Error:        err.Error(),
		})
		return loop.NewErrorResult(err.Error()), nil
	}

	w.logTransaction(ctx, &data.WalletTransaction{
		Chain:           input.Chain,
		Type:            "send_token",
		FromAddress:     result.FromAddress,
		ToAddress:       result.ToAddress,
		Amount:          result.Amount,
		TokenAddress:    result.TokenAddress,
		TransactionHash: result.TransactionHash,
		Status:          result.Status,
		ExplorerURL:     result.ExplorerURL,
		Metadata:        map[string]any{"memo": input.Memo},
	})

	response := map[string]any{
		"chain":            string(result.Chain),
		"transaction_hash": result.TransactionHash,
		"from_address":     result.FromAddress,
		"to_address":       result.ToAddress,
		"amount":           result.Amount,
		"token_address":    result.TokenAddress,
		"status":           result.Status,
		"explorer_url":     result.ExplorerURL,
	}
	if input.Memo != "" {
		response["memo"] = input.Memo
	}

	return loop.NewSuccessResult(response), nil
}
