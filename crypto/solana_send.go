package gowild_crypto

import (
	"context"
	"fmt"
	"math/big"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
)

// SendToken sends SOL or SPL tokens to a destination address.
// For native SOL, leave tokenMint empty.
// Amount is in human-readable units (e.g., "1.5" for 1.5 SOL).
// Optionally attach a memo to the transaction.
func (w *solanaWallet) SendToken(ctx context.Context, to string, amount string, tokenMint string, memo string) (*TransactionResult, error) {
	client := w.getClient()

	toPubkey, err := solana.PublicKeyFromBase58(to)
	if err != nil {
		return nil, fmt.Errorf("invalid destination address: %w", err)
	}

	// Parse amount
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amount)
	}

	// Get recent blockhash
	recentBlockhash, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get blockhash: %w", err)
	}

	var instructions []solana.Instruction

	if tokenMint == "" {
		// Native SOL transfer
		// Convert to lamports (9 decimals)
		lamportsFloat := new(big.Float).Mul(amountFloat, big.NewFloat(1e9))
		lamports, _ := lamportsFloat.Uint64()

		instruction := system.NewTransferInstruction(
			lamports,
			w.keypair.PublicKey(),
			toPubkey,
		).Build()

		instructions = append(instructions, instruction)
	} else {
		// SPL token transfer
		mintPubkey, err := solana.PublicKeyFromBase58(tokenMint)
		if err != nil {
			return nil, fmt.Errorf("invalid token mint: %w", err)
		}

		// Get token decimals
		decimals, err := w.getTokenDecimals(ctx, client, mintPubkey)
		if err != nil {
			return nil, fmt.Errorf("failed to get token decimals: %w", err)
		}

		// Convert amount to token units
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
		tokenAmount := new(big.Float).Mul(amountFloat, multiplier)
		tokenAmountInt, _ := tokenAmount.Uint64()

		// Get or create associated token accounts
		fromATA, _, err := solana.FindAssociatedTokenAddress(w.keypair.PublicKey(), mintPubkey)
		if err != nil {
			return nil, fmt.Errorf("failed to find source token account: %w", err)
		}

		toATA, _, err := solana.FindAssociatedTokenAddress(toPubkey, mintPubkey)
		if err != nil {
			return nil, fmt.Errorf("failed to find destination token account: %w", err)
		}

		// Create transfer instruction
		instruction := token.NewTransferInstruction(
			tokenAmountInt,
			fromATA,
			toATA,
			w.keypair.PublicKey(),
			[]solana.PublicKey{},
		).Build()

		instructions = append(instructions, instruction)
	}

	// Add memo instruction if provided
	if memo != "" {
		memoInstruction := &solana.GenericInstruction{
			ProgID: MemoProgram,
			AccountValues: solana.AccountMetaSlice{
				solana.NewAccountMeta(w.keypair.PublicKey(), true, false),
			},
			DataBytes: []byte(memo),
		}
		instructions = append(instructions, memoInstruction)
	}

	tx, err := solana.NewTransaction(
		instructions,
		recentBlockhash.Value.Blockhash,
		solana.TransactionPayer(w.keypair.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// Sign transaction
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(w.keypair.PublicKey()) {
			return &w.keypair
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send transaction
	sig, err := client.SendTransaction(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return &TransactionResult{
		Chain:           ChainSolana,
		TransactionHash: sig.String(),
		FromAddress:     w.Address(),
		ToAddress:       to,
		Amount:          amount,
		TokenAddress:    tokenMint,
		Status:          "pending",
		ExplorerURL:     fmt.Sprintf("https://solscan.io/tx/%s", sig.String()),
	}, nil
}

// getTokenDecimals fetches decimals for an SPL token.
func (w *solanaWallet) getTokenDecimals(ctx context.Context, client *rpc.Client, mint solana.PublicKey) (uint8, error) {
	info, err := client.GetAccountInfo(ctx, mint)
	if err != nil {
		return 9, nil // Default to 9 (SOL decimals)
	}

	if info.Value == nil {
		return 9, nil
	}

	// Parse mint data - decimals is at offset 44 in the mint account data
	data := info.Value.Data.GetBinary()
	if len(data) >= 45 {
		return data[44], nil
	}

	return 9, nil
}
