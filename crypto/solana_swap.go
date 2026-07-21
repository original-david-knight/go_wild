package gowild_crypto

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/gagliardetto/solana-go"
)

// SwapToken swaps tokens using Jupiter aggregator.
// fromToken/toToken can be "SOL" or a mint address.
// Amount is in human-readable units.
func (w *solanaWallet) SwapToken(ctx context.Context, fromToken string, toToken string, amount string, slippageBps int) (*SwapResult, error) {
	client := w.getClient()

	// Normalize token addresses
	var inputMint, outputMint string
	if strings.EqualFold(fromToken, "SOL") {
		inputMint = NativeSOL.String()
	} else {
		inputMint = fromToken
	}
	if strings.EqualFold(toToken, "SOL") {
		outputMint = NativeSOL.String()
	} else {
		outputMint = toToken
	}

	// Parse amount and convert to base units
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amount)
	}

	var inputAmountLamports uint64
	if strings.EqualFold(fromToken, "SOL") {
		lamportsFloat := new(big.Float).Mul(amountFloat, big.NewFloat(1e9))
		inputAmountLamports, _ = lamportsFloat.Uint64()
	} else {
		mintPubkey, err := solana.PublicKeyFromBase58(fromToken)
		if err != nil {
			return nil, fmt.Errorf("invalid token mint: %w", err)
		}
		decimals, err := w.getTokenDecimals(ctx, client, mintPubkey)
		if err != nil {
			return nil, err
		}
		multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
		tokenAmount := new(big.Float).Mul(amountFloat, multiplier)
		inputAmountLamports, _ = tokenAmount.Uint64()
	}

	if slippageBps == 0 {
		slippageBps = 50 // Default 0.5%
	}

	// Get quote from Jupiter
	quoteURL := fmt.Sprintf(
		"https://quote-api.jup.ag/v6/quote?inputMint=%s&outputMint=%s&amount=%d&slippageBps=%d",
		inputMint, outputMint, inputAmountLamports, slippageBps,
	)

	quoteReq, err := http.NewRequestWithContext(ctx, "GET", quoteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create quote request: %w", err)
	}

	quoteResp, err := http.DefaultClient.Do(quoteReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}
	defer quoteResp.Body.Close()

	if quoteResp.StatusCode != 200 {
		return nil, fmt.Errorf("Jupiter quote error: status %d", quoteResp.StatusCode)
	}

	var quote map[string]any
	if err := json.NewDecoder(quoteResp.Body).Decode(&quote); err != nil {
		return nil, fmt.Errorf("failed to parse quote: %w", err)
	}

	// Get swap transaction from Jupiter
	swapURL := "https://quote-api.jup.ag/v6/swap"
	swapBody := map[string]any{
		"quoteResponse":             quote,
		"userPublicKey":             w.Address(),
		"wrapAndUnwrapSol":          true,
		"dynamicComputeUnitLimit":   true,
		"prioritizationFeeLamports": "auto",
	}

	swapBodyBytes, _ := json.Marshal(swapBody)
	swapReq, err := http.NewRequestWithContext(ctx, "POST", swapURL, strings.NewReader(string(swapBodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create swap request: %w", err)
	}
	swapReq.Header.Set("Content-Type", "application/json")

	swapResp, err := http.DefaultClient.Do(swapReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get swap transaction: %w", err)
	}
	defer swapResp.Body.Close()

	if swapResp.StatusCode != 200 {
		return nil, fmt.Errorf("Jupiter swap error: status %d", swapResp.StatusCode)
	}

	var swapResult struct {
		SwapTransaction string `json:"swapTransaction"`
	}
	if err := json.NewDecoder(swapResp.Body).Decode(&swapResult); err != nil {
		return nil, fmt.Errorf("failed to parse swap result: %w", err)
	}

	// Decode and sign the transaction
	txBytes, err := base64.StdEncoding.DecodeString(swapResult.SwapTransaction)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	tx, err := solana.TransactionFromBytes(txBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction: %w", err)
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

	outAmount := ""
	if outAmountVal, ok := quote["outAmount"].(string); ok {
		outAmount = outAmountVal
	}

	return &SwapResult{
		Chain:           ChainSolana,
		TransactionHash: sig.String(),
		FromToken:       fromToken,
		ToToken:         toToken,
		FromAmount:      amount,
		ToAmount:        outAmount,
		ExplorerURL:     fmt.Sprintf("https://solscan.io/tx/%s", sig.String()),
	}, nil
}
