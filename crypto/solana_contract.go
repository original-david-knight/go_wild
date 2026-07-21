package gowild_crypto

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// ContractCall interacts with a Solana program.
// For Solana, this is simplified - it sends a basic instruction to a program.
// For complex interactions, use the program's specific SDK.
func (w *solanaWallet) ContractCall(ctx context.Context, programAddress string, method string, args []any, value string, readOnly bool) (*ContractCallResult, error) {
	if readOnly {
		// For read-only calls, we'd need to use simulateTransaction
		// This is a simplified implementation
		return &ContractCallResult{
			Chain:           ChainSolana,
			ContractAddress: programAddress,
			Method:          method,
			Result:          "read-only calls not fully implemented for Solana - use RPC directly",
		}, nil
	}

	client := w.getClient()

	programPubkey, err := solana.PublicKeyFromBase58(programAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid program address: %w", err)
	}

	// Get recent blockhash
	recentBlockhash, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return nil, fmt.Errorf("failed to get blockhash: %w", err)
	}

	// Build instruction data from args
	var data []byte
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			data = append(data, []byte(v)...)
		case []byte:
			data = append(data, v...)
		case float64:
			// Assume it's a small integer
			data = append(data, byte(int(v)))
		}
	}

	// Create a generic instruction
	instruction := &solana.GenericInstruction{
		ProgID: programPubkey,
		AccountValues: solana.AccountMetaSlice{
			solana.NewAccountMeta(w.keypair.PublicKey(), true, true),
		},
		DataBytes: data,
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
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

	return &ContractCallResult{
		Chain:           ChainSolana,
		TransactionHash: sig.String(),
		ContractAddress: programAddress,
		Method:          method,
		ExplorerURL:     fmt.Sprintf("https://solscan.io/tx/%s", sig.String()),
	}, nil
}
