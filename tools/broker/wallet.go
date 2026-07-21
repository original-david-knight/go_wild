package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// WalletTools proxies wallet operations through the broker API.
type WalletTools struct {
	client *Client
}

// NewWalletTools creates broker-backed wallet tools.
func NewWalletTools(client *Client) *WalletTools {
	return &WalletTools{client: client}
}

func (w *WalletTools) GetWalletAddressTool(ctx context.Context, input tools.GetWalletAddressInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/address", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) GetBalancesTool(ctx context.Context, input tools.GetBalancesInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/balances", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) SignMessageTool(ctx context.Context, input tools.SignMessageInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/sign", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) SendTokenTool(ctx context.Context, input tools.SendTokenInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/send", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) SwapTokenTool(ctx context.Context, input tools.SwapTokenInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/swap", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) ContractCallTool(ctx context.Context, input tools.ContractCallInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/contract", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) GetTransactionHistoryTool(ctx context.Context, input tools.GetTransactionHistoryInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/history", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) EncryptMessageTool(ctx context.Context, input tools.EncryptMessageInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/encrypt", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) DecryptMessageTool(ctx context.Context, input tools.DecryptMessageInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/decrypt", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (w *WalletTools) GetEd25519PublicKeyTool(ctx context.Context, input tools.GetEd25519PublicKeyInput) (*loop.ToolResult, error) {
	result, err := w.client.Post(ctx, "/broker/v1/wallet/pubkey", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (w *WalletTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"get_wallet_address":      "Get your wallet address for a blockchain (Ethereum or Solana)",
		"get_balances":            "Get a fixed balance snapshot with no options: solana, eth, polygon, polygon_usdte, and polygon_usdce.",
		"sign_message":            "Cryptographically sign a message with your private key",
		"send_token":              "Send native tokens (ETH/SOL) or ERC20/SPL tokens",
		"swap_token":              "Swap tokens on a DEX",
		"contract_call":           "Call a smart contract method",
		"get_transaction_history": "View your transaction history",
		"encrypt_message":         "Encrypt a message for a recipient using NaCl box",
		"decrypt_message":         "Decrypt a message sent to you using NaCl box",
		"get_ed25519_public_key":  "Get your Ed25519 public key for receiving encrypted messages",
	}
	return descriptions[name]
}
