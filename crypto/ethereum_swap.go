package gowild_crypto

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// SwapToken swaps tokens using 0x API.
// fromToken/toToken can be "ETH" or a contract address.
// Amount is in human-readable units.
func (w *ethereumWallet) SwapToken(ctx context.Context, fromToken string, toToken string, amount string, slippageBps int) (*SwapResult, error) {
	client, err := w.getClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// Normalize token addresses
	sellToken := fromToken
	buyToken := toToken
	if strings.EqualFold(fromToken, "ETH") {
		sellToken = "ETH"
	}
	if strings.EqualFold(toToken, "ETH") {
		buyToken = "ETH"
	}

	// Parse amount and convert to base units
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amount)
	}

	var sellAmountWei *big.Int
	if sellToken == "ETH" {
		weiFloat := new(big.Float).Mul(amountFloat, big.NewFloat(1e18))
		sellAmountWei, _ = weiFloat.Int(nil)
	} else {
		tokenAddr := common.HexToAddress(sellToken)
		decimals, err := w.getTokenDecimals(ctx, client, tokenAddr)
		if err != nil {
			return nil, err
		}
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
		tokenAmount := new(big.Float).Mul(amountFloat, multiplier)
		sellAmountWei, _ = tokenAmount.Int(nil)
	}

	// Get quote from 0x API
	slippage := float64(slippageBps) / 10000.0
	if slippage == 0 {
		slippage = 0.005 // Default 0.5%
	}

	quoteURL := fmt.Sprintf(
		"https://api.0x.org/swap/v1/quote?sellToken=%s&buyToken=%s&sellAmount=%s&slippagePercentage=%f&takerAddress=%s",
		sellToken, buyToken, sellAmountWei.String(), slippage, w.Address(),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", quoteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("0x-api-key", "") // Add API key if available

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("0x API error: status %d", resp.StatusCode)
	}

	var quote struct {
		To        string `json:"to"`
		Data      string `json:"data"`
		Value     string `json:"value"`
		Gas       string `json:"gas"`
		GasPrice  string `json:"gasPrice"`
		BuyAmount string `json:"buyAmount"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&quote); err != nil {
		return nil, fmt.Errorf("failed to parse quote: %w", err)
	}

	// Build transaction from quote
	toAddr := common.HexToAddress(quote.To)
	value, _ := new(big.Int).SetString(quote.Value, 10)
	gasLimit, _ := new(big.Int).SetString(quote.Gas, 10)
	gasPrice, _ := new(big.Int).SetString(quote.GasPrice, 10)
	data, _ := hexutil.Decode(quote.Data)

	nonce, err := client.PendingNonceAt(ctx, w.address)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}

	tx := types.NewTransaction(nonce, toAddr, value, gasLimit.Uint64(), gasPrice, data)

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

	return &SwapResult{
		Chain:           ChainEthereum,
		TransactionHash: signedTx.Hash().Hex(),
		FromToken:       fromToken,
		ToToken:         toToken,
		FromAmount:      amount,
		ToAmount:        quote.BuyAmount,
		ExplorerURL:     evmTxExplorerURL(w.chainID, signedTx.Hash().Hex()),
	}, nil
}
