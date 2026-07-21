package tools

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
)

// logTransaction logs a transaction using agentService if available.
func (w *WalletTools) logTransaction(ctx context.Context, tx *data.WalletTransaction) {
	if w.agentService != nil {
		_ = w.agentService.LogWalletTransaction(ctx, tx)
	}
}
