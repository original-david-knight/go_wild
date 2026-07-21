package tools

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// GetTransactionHistoryTool returns recent blockchain transactions.
func (w *WalletTools) GetTransactionHistoryTool(ctx context.Context, input GetTransactionHistoryInput) (*loop.ToolResult, error) {
	if w.agentService == nil {
		return loop.NewErrorResult("transaction logging not configured"), nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	transactions, err := w.agentService.GetWalletTransactions(ctx, limit)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	txList := make([]map[string]any, 0, len(transactions))
	for _, tx := range transactions {
		if input.Chain != "" && tx.Chain != input.Chain {
			continue
		}

		txMap := map[string]any{
			"id":           tx.ID,
			"timestamp":    tx.Timestamp.Format("2006-01-02 15:04:05"),
			"chain":        tx.Chain,
			"type":         tx.Type,
			"from_address": tx.FromAddress,
			"status":       tx.Status,
		}

		if tx.ToAddress != "" {
			txMap["to_address"] = tx.ToAddress
		}
		if tx.Amount != "" {
			txMap["amount"] = tx.Amount
		}
		if tx.TokenAddress != "" {
			txMap["token_address"] = tx.TokenAddress
		}
		if tx.TransactionHash != "" {
			txMap["transaction_hash"] = tx.TransactionHash
		}
		if tx.ExplorerURL != "" {
			txMap["explorer_url"] = tx.ExplorerURL
		}
		if tx.Error != "" {
			txMap["error"] = tx.Error
		}
		if len(tx.Metadata) > 0 {
			txMap["metadata"] = tx.Metadata
		}

		txList = append(txList, txMap)
	}

	return loop.NewSuccessResult(map[string]any{
		"transactions": txList,
		"count":        len(txList),
	}), nil
}
