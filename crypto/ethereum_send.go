package gowild_crypto

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// SendToken sends ETH or ERC20 tokens to a destination address.
// For native ETH, leave tokenAddress empty.
// Amount is in human-readable units (e.g., "1.5" for 1.5 ETH).
func (w *ethereumWallet) SendToken(ctx context.Context, to string, amount string, tokenAddress string) (*TransactionResult, error) {
	client, err := w.getClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	toAddr := common.HexToAddress(to)

	// Parse amount
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amount)
	}

	var tx *types.Transaction

	if tokenAddress == "" {
		// Native ETH transfer
		// Convert to wei (18 decimals)
		weiFloat := new(big.Float).Mul(amountFloat, big.NewFloat(1e18))
		weiInt, _ := weiFloat.Int(nil)

		nonce, err := client.PendingNonceAt(ctx, w.address)
		if err != nil {
			return nil, fmt.Errorf("failed to get nonce: %w", err)
		}

		gasPrice, err := client.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get gas price: %w", err)
		}

		gasLimit := uint64(21000) // Standard ETH transfer

		tx = types.NewTransaction(nonce, toAddr, weiInt, gasLimit, gasPrice, nil)
	} else {
		// ERC20 token transfer
		tokenAddr := common.HexToAddress(tokenAddress)

		// Get token decimals
		decimals, err := w.getTokenDecimals(ctx, client, tokenAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to get token decimals: %w", err)
		}

		// Convert amount to token units
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
		tokenAmount := new(big.Float).Mul(amountFloat, multiplier)
		tokenAmountInt, _ := tokenAmount.Int(nil)

		// Build transfer call data
		parsedABI, _ := abi.JSON(strings.NewReader(erc20ABI))
		data, err := parsedABI.Pack("transfer", toAddr, tokenAmountInt)
		if err != nil {
			return nil, fmt.Errorf("failed to pack transfer: %w", err)
		}

		nonce, err := client.PendingNonceAt(ctx, w.address)
		if err != nil {
			return nil, fmt.Errorf("failed to get nonce: %w", err)
		}

		gasPrice, err := client.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get gas price: %w", err)
		}

		// Estimate gas
		msg := ethereum.CallMsg{
			From: w.address,
			To:   &tokenAddr,
			Data: data,
		}
		gasLimit, err := client.EstimateGas(ctx, msg)
		if err != nil {
			gasLimit = 100000 // Default for ERC20 transfers
		}

		tx = types.NewTransaction(nonce, tokenAddr, big.NewInt(0), gasLimit, gasPrice, data)
	}

	// Sign and send transaction
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(w.chainID), w.privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to send transaction from %s on %s via %s: %w",
			w.Address(),
			evmChainDisplayName(w.chainID),
			w.rpcURL,
			err,
		)
	}

	return &TransactionResult{
		Chain:           ChainEthereum,
		TransactionHash: signedTx.Hash().Hex(),
		FromAddress:     w.Address(),
		ToAddress:       to,
		Amount:          amount,
		TokenAddress:    tokenAddress,
		Status:          "pending",
		ExplorerURL:     evmTxExplorerURL(w.chainID, signedTx.Hash().Hex()),
	}, nil
}

// getTokenDecimals fetches the decimals for an ERC20 token.
func (w *ethereumWallet) getTokenDecimals(ctx context.Context, client *ethclient.Client, tokenAddr common.Address) (uint8, error) {
	parsedABI, _ := abi.JSON(strings.NewReader(erc20ABI))
	data, _ := parsedABI.Pack("decimals")

	result, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &tokenAddr,
		Data: data,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to read token decimals: %w", err)
	}

	var decimals uint8
	if err := parsedABI.UnpackIntoInterface(&decimals, "decimals", result); err != nil {
		return 0, fmt.Errorf("failed to decode token decimals: %w", err)
	}
	return decimals, nil
}
