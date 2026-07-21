package tools

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/crypto"
)

// SignMessageTool cryptographically signs a message with the wallet's private key.
// The signature proves the message was created by the wallet owner.
func (w *WalletTools) SignMessageTool(ctx context.Context, input SignMessageInput) (*loop.ToolResult, error) {
	if input.Message == "" {
		return loop.NewErrorResult("message cannot be empty"), nil
	}

	signed, err := w.wallet.SignMessage(gowild_crypto.Chain(input.Chain), input.Message)
	if err != nil {
		w.logTransaction(ctx, &data.WalletTransaction{
			Chain:       input.Chain,
			Type:        "sign_message",
			FromAddress: "",
			Status:      "failed",
			Error:       err.Error(),
			Metadata:    map[string]any{"message": input.Message},
		})
		return loop.NewErrorResult(err.Error()), nil
	}

	w.logTransaction(ctx, &data.WalletTransaction{
		Chain:       input.Chain,
		Type:        "sign_message",
		FromAddress: signed.Address,
		Status:      "confirmed",
		Metadata:    map[string]any{"message": input.Message, "signature": signed.Signature},
	})

	return loop.NewSuccessResult(map[string]any{
		"chain":     string(signed.Chain),
		"address":   signed.Address,
		"message":   signed.Message,
		"signature": signed.Signature,
	}), nil
}
