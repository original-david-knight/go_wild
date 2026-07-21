package tools

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
)

// GetWalletAddressTool returns the public address for a blockchain wallet.
// The private key is never exposed - only the public address is returned.
func (w *WalletTools) GetWalletAddressTool(ctx context.Context, input GetWalletAddressInput) (*loop.ToolResult, error) {
	info, err := w.wallet.GetAddress(gowild_crypto.Chain(input.Chain))
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"chain":   string(info.Chain),
		"address": info.Address,
	}), nil
}
