package gowild_crypto

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// GetBalance returns the native EVM balance for the connected chain.
func (w *ethereumWallet) GetBalance(ctx context.Context) (*BalanceResult, error) {
	client, err := w.getClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	balance, err := client.BalanceAt(ctx, w.address, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Convert wei to ETH
	balanceFloat := new(big.Float).SetInt(balance)
	ethBalance := new(big.Float).Quo(balanceFloat, big.NewFloat(1e18))

	return &BalanceResult{
		Chain:      ChainEthereum,
		Address:    w.Address(),
		Balance:    ethBalance.Text('f', 18),
		BalanceRaw: balance.String(),
		Symbol:     evmNativeSymbol(w.chainID),
		Decimals:   18,
	}, nil
}

// GetTokenBalance returns the balance of an ERC20 token.
func (w *ethereumWallet) GetTokenBalance(ctx context.Context, tokenAddress string) (*BalanceResult, error) {
	client, err := w.getClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	tokenAddr := common.HexToAddress(tokenAddress)
	knownMeta, hasKnownMeta := knownEVMTokenMetadata(w.chainID, tokenAddr)
	parsedABI, _ := abi.JSON(strings.NewReader(erc20ABI))

	// Get balance
	balanceData, _ := parsedABI.Pack("balanceOf", w.address)
	balanceResult, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &tokenAddr,
		Data: balanceData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get token balance: %w", err)
	}

	var balance *big.Int
	if err := parsedABI.UnpackIntoInterface(&balance, "balanceOf", balanceResult); err != nil {
		return nil, fmt.Errorf("failed to parse balance: %w", err)
	}

	decimals := uint8(18)
	symbol := "TOKEN"
	if hasKnownMeta {
		// Use canonical metadata directly to avoid extra RPC calls and provider rate-limit variance.
		decimals = knownMeta.decimals
		symbol = knownMeta.symbol
	} else {
		// Get decimals
		decimalsOut, err := w.getTokenDecimals(ctx, client, tokenAddr)
		if err == nil {
			decimals = decimalsOut
		}

		// Get symbol
		symbolData, _ := parsedABI.Pack("symbol")
		symbolResult, err := client.CallContract(ctx, ethereum.CallMsg{
			To:   &tokenAddr,
			Data: symbolData,
		}, nil)
		if err == nil {
			var sym string
			if err := parsedABI.UnpackIntoInterface(&sym, "symbol", symbolResult); err == nil {
				symbol = sym
			}
		}
	}

	// Convert to human-readable
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	balanceFloat := new(big.Float).SetInt(balance)
	humanBalance := new(big.Float).Quo(balanceFloat, divisor)

	return &BalanceResult{
		Chain:        ChainEthereum,
		Address:      w.Address(),
		Balance:      humanBalance.Text('f', int(decimals)),
		BalanceRaw:   balance.String(),
		Symbol:       symbol,
		Decimals:     int(decimals),
		TokenAddress: tokenAddress,
	}, nil
}
