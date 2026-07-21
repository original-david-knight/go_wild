package tools

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
)

// ContractCallTool calls a smart contract method.
func (w *WalletTools) ContractCallTool(ctx context.Context, input ContractCallInput) (*loop.ToolResult, error) {
	if input.ContractAddress == "" {
		return loop.NewErrorResult("contract_address is required"), nil
	}
	if input.Method == "" {
		return loop.NewErrorResult("method is required"), nil
	}

	result, err := w.wallet.ContractCall(ctx, gowild_crypto.Chain(input.Chain), input.ContractAddress, input.Method, input.Args, input.Value, input.ReadOnly)
	if err != nil {
		w.logTransaction(ctx, &data.WalletTransaction{
			Chain:        input.Chain,
			Type:         "contract_call",
			ToAddress:    input.ContractAddress,
			Amount:       input.Value,
			TokenAddress: input.ContractAddress,
			Status:       "failed",
			Error:        err.Error(),
			Metadata: map[string]any{
				"method":    input.Method,
				"args":      input.Args,
				"read_only": input.ReadOnly,
			},
		})
		return loop.NewErrorResult(err.Error()), nil
	}

	// Only log write transactions, not read-only calls
	if !input.ReadOnly && result.TransactionHash != "" {
		w.logTransaction(ctx, &data.WalletTransaction{
			Chain:           input.Chain,
			Type:            "contract_call",
			ToAddress:       input.ContractAddress,
			Amount:          input.Value,
			TokenAddress:    input.ContractAddress,
			TransactionHash: result.TransactionHash,
			Status:          "pending",
			ExplorerURL:     result.ExplorerURL,
			Metadata: map[string]any{
				"method": input.Method,
				"args":   input.Args,
			},
		})
	}

	response := map[string]any{
		"chain":            string(result.Chain),
		"contract_address": result.ContractAddress,
		"method":           result.Method,
	}

	if result.TransactionHash != "" {
		response["transaction_hash"] = result.TransactionHash
		response["explorer_url"] = result.ExplorerURL
	}
	if result.Result != nil {
		response["result"] = result.Result
	}

	return loop.NewSuccessResult(response), nil
}
