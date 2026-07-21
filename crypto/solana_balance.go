package gowild_crypto

import (
	"context"
	"fmt"
	"math/big"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// GetBalance returns the native SOL balance.
func (w *solanaWallet) GetBalance(ctx context.Context) (*BalanceResult, error) {
	client := w.getClient()

	balance, err := client.GetBalance(ctx, w.keypair.PublicKey(), rpc.CommitmentConfirmed)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Convert lamports to SOL
	balanceFloat := new(big.Float).SetUint64(balance.Value)
	solBalance := new(big.Float).Quo(balanceFloat, big.NewFloat(1e9))

	return &BalanceResult{
		Chain:      ChainSolana,
		Address:    w.Address(),
		Balance:    solBalance.Text('f', 9),
		BalanceRaw: fmt.Sprintf("%d", balance.Value),
		Symbol:     "SOL",
		Decimals:   9,
	}, nil
}

// GetTokenBalance returns the balance of an SPL token.
func (w *solanaWallet) GetTokenBalance(ctx context.Context, tokenMint string) (*BalanceResult, error) {
	client := w.getClient()

	mintPubkey, err := solana.PublicKeyFromBase58(tokenMint)
	if err != nil {
		return nil, fmt.Errorf("invalid token mint: %w", err)
	}

	// Find associated token account
	ata, _, err := solana.FindAssociatedTokenAddress(w.keypair.PublicKey(), mintPubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to find token account: %w", err)
	}

	// Get token account balance
	tokenBalance, err := client.GetTokenAccountBalance(ctx, ata, rpc.CommitmentConfirmed)
	if err != nil {
		// Account might not exist - return zero balance
		return &BalanceResult{
			Chain:        ChainSolana,
			Address:      w.Address(),
			Balance:      "0",
			BalanceRaw:   "0",
			Symbol:       "TOKEN",
			Decimals:     9,
			TokenAddress: tokenMint,
		}, nil
	}

	decimals := int(tokenBalance.Value.Decimals)
	symbol := "TOKEN"

	// Try to get symbol from well-known tokens
	if tokenMint == USDC.String() {
		symbol = "USDC"
	}

	return &BalanceResult{
		Chain:        ChainSolana,
		Address:      w.Address(),
		Balance:      tokenBalance.Value.UiAmountString,
		BalanceRaw:   tokenBalance.Value.Amount,
		Symbol:       symbol,
		Decimals:     decimals,
		TokenAddress: tokenMint,
	}, nil
}
