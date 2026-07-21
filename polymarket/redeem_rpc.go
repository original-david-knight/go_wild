package gowild_polymarket

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

func callBytes32Method(ctx context.Context, client *ethclient.Client, ctABI abi.ABI, ctAddress common.Address, method string, args ...any) (common.Hash, error) {
	data, err := ctABI.Pack(method, args...)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to encode %s: %w", method, err)
	}

	raw, err := callContractWithRetry(ctx, client, ethereum.CallMsg{
		To:   &ctAddress,
		Data: data,
	})
	if err != nil {
		return common.Hash{}, fmt.Errorf("call %s failed: %w", method, err)
	}

	values, err := ctABI.Unpack(method, raw)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to decode %s response: %w", method, err)
	}
	if len(values) != 1 {
		return common.Hash{}, fmt.Errorf("unexpected %s return length: %d", method, len(values))
	}

	switch value := values[0].(type) {
	case [32]byte:
		return common.BytesToHash(value[:]), nil
	case common.Hash:
		return value, nil
	default:
		return common.Hash{}, fmt.Errorf("unexpected %s return type: %T", method, values[0])
	}
}

func callUint256Method(ctx context.Context, client *ethclient.Client, ctABI abi.ABI, ctAddress common.Address, method string, args ...any) (*big.Int, error) {
	data, err := ctABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to encode %s: %w", method, err)
	}

	raw, err := callContractWithRetry(ctx, client, ethereum.CallMsg{
		To:   &ctAddress,
		Data: data,
	})
	if err != nil {
		return nil, fmt.Errorf("call %s failed: %w", method, err)
	}

	values, err := ctABI.Unpack(method, raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", method, err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected %s return length: %d", method, len(values))
	}

	value, ok := values[0].(*big.Int)
	if !ok || value == nil {
		return nil, fmt.Errorf("unexpected %s return type: %T", method, values[0])
	}
	return new(big.Int).Set(value), nil
}

func callContractWithRetry(ctx context.Context, client *ethclient.Client, msg ethereum.CallMsg) ([]byte, error) {
	var lastErr error
	backoff := redeemRPCRetryBaseDelay

	for attempt := 0; attempt <= redeemRPCRetryMaxRetries; attempt++ {
		raw, err := client.CallContract(ctx, msg, nil)
		if err == nil {
			return raw, nil
		}
		if !isRetryableRPCError(err) {
			return nil, err
		}
		lastErr = err
		if attempt == redeemRPCRetryMaxRetries {
			break
		}

		wait := retryDelayFromRPCError(err, backoff)
		if waitErr := waitForDelay(ctx, wait); waitErr != nil {
			return nil, waitErr
		}
		if backoff < redeemRPCRetryMaxDelay {
			backoff *= 2
			if backoff > redeemRPCRetryMaxDelay {
				backoff = redeemRPCRetryMaxDelay
			}
		}
	}

	return nil, lastErr
}

func waitForReceipt(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}
		wait := redeemReceiptPollDelay
		if errors.Is(err, ethereum.NotFound) {
			// Transaction is not mined yet; continue polling.
		} else if isRetryableRPCError(err) {
			wait = retryDelayFromRPCError(err, redeemRPCRetryBaseDelay)
		} else {
			return nil, err
		}

		if err := waitForDelay(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func waitForDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Second
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableRPCError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"too many requests",
		"rate limit",
		"429",
		"call rate limit exhausted",
		"timeout",
		"deadline exceeded",
		"temporarily unavailable",
		"connection reset",
		"connection refused",
		"eof",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

func retryDelayFromRPCError(err error, fallback time.Duration) time.Duration {
	if fallback <= 0 {
		fallback = redeemRPCRetryBaseDelay
	}
	if err == nil {
		return fallback
	}

	match := retryAfterSecondsPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return fallback
	}

	seconds, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func parsePayoutRedemption(ctABI abi.ABI, receipt *types.Receipt, ctAddress common.Address, redeemer common.Address, collateral common.Address, parentCollectionID common.Hash, conditionHash common.Hash) (*big.Int, error) {
	event, ok := ctABI.Events["PayoutRedemption"]
	if !ok {
		return nil, fmt.Errorf("missing PayoutRedemption event in ABI")
	}

	for _, entry := range receipt.Logs {
		if entry.Address != ctAddress {
			continue
		}
		if len(entry.Topics) < 4 || entry.Topics[0] != event.ID {
			continue
		}
		if common.BytesToAddress(entry.Topics[1].Bytes()) != redeemer {
			continue
		}
		if common.BytesToAddress(entry.Topics[2].Bytes()) != collateral {
			continue
		}
		if entry.Topics[3] != parentCollectionID {
			continue
		}

		values, err := event.Inputs.NonIndexed().Unpack(entry.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode PayoutRedemption log: %w", err)
		}
		if len(values) != 3 {
			return nil, fmt.Errorf("unexpected PayoutRedemption payload size: %d", len(values))
		}

		decodedCondition, ok := values[0].([32]byte)
		if !ok {
			return nil, fmt.Errorf("unexpected condition_id payload type: %T", values[0])
		}
		if common.BytesToHash(decodedCondition[:]) != conditionHash {
			continue
		}

		payout, ok := values[2].(*big.Int)
		if !ok || payout == nil {
			return nil, fmt.Errorf("unexpected payout payload type: %T", values[2])
		}
		return new(big.Int).Set(payout), nil
	}

	return nil, fmt.Errorf("no matching PayoutRedemption event found")
}

func parseNegRiskPayoutRedemption(adapterABI abi.ABI, receipt *types.Receipt, adapterAddress common.Address, redeemer common.Address, conditionHash common.Hash) (*big.Int, error) {
	event, ok := adapterABI.Events["PayoutRedemption"]
	if !ok {
		return nil, fmt.Errorf("missing PayoutRedemption event in neg-risk adapter ABI")
	}

	for _, entry := range receipt.Logs {
		if entry.Address != adapterAddress {
			continue
		}
		if len(entry.Topics) < 3 || entry.Topics[0] != event.ID {
			continue
		}
		if common.BytesToAddress(entry.Topics[1].Bytes()) != redeemer {
			continue
		}
		if entry.Topics[2] != conditionHash {
			continue
		}

		values, err := event.Inputs.NonIndexed().Unpack(entry.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode neg-risk PayoutRedemption log: %w", err)
		}
		if len(values) != 2 {
			return nil, fmt.Errorf("unexpected neg-risk PayoutRedemption payload size: %d", len(values))
		}

		payout, ok := values[1].(*big.Int)
		if !ok || payout == nil {
			return nil, fmt.Errorf("unexpected neg-risk payout payload type: %T", values[1])
		}
		return new(big.Int).Set(payout), nil
	}

	return nil, fmt.Errorf("no matching neg-risk PayoutRedemption event found")
}

// connectPolygonRPC connects to a Polygon RPC endpoint and verifies the chain ID.
// If the RPC returns an auth error (e.g. disabled API key / tenant disabled),
// it falls back to alternative public Polygon RPCs.
func (c *Client) connectPolygonRPC(ctx context.Context) (*ethclient.Client, string, error) {
	rpcURL := c.onchainRPCURL

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		if isRPCAuthError(err) {
			return c.fallbackToPublicRPC(ctx, rpcURL, err)
		}
		return nil, rpcURL, fmt.Errorf("failed to connect to Polygon RPC %s: %w", rpcURL, err)
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		if isRPCAuthError(err) {
			return c.fallbackToPublicRPC(ctx, rpcURL, err)
		}
		return nil, rpcURL, fmt.Errorf("failed to read chain ID from RPC %s: %w", rpcURL, err)
	}

	if chainID.Int64() != polygonChainID {
		client.Close()
		return nil, rpcURL, fmt.Errorf("redeem_winnings requires Polygon chain %d, got chain %s from %s", polygonChainID, chainID.String(), rpcURL)
	}

	return client, rpcURL, nil
}

// polygonFallbackRPCs are public Polygon RPC endpoints tried when the configured
// or default RPC fails with an auth error.
var polygonFallbackRPCs = []string{
	"https://polygon-bor-rpc.publicnode.com",
	"https://polygon.drpc.org",
	"https://rpc.ankr.com/polygon",
}

// fallbackToPublicRPC connects to a public Polygon RPC after the
// configured RPC returned an auth error.
func (c *Client) fallbackToPublicRPC(ctx context.Context, failedURL string, originalErr error) (*ethclient.Client, string, error) {
	log.Printf("Polygon RPC %s returned auth error (%v), trying fallback RPCs", failedURL, originalErr)

	for _, rpcURL := range polygonFallbackRPCs {
		if rpcURL == failedURL {
			continue
		}
		client, err := ethclient.DialContext(ctx, rpcURL)
		if err != nil {
			log.Printf("Fallback RPC %s dial failed: %v", rpcURL, err)
			continue
		}
		chainID, err := client.ChainID(ctx)
		if err != nil {
			client.Close()
			log.Printf("Fallback RPC %s chain ID check failed: %v", rpcURL, err)
			continue
		}
		if chainID.Int64() != polygonChainID {
			client.Close()
			log.Printf("Fallback RPC %s returned wrong chain %s", rpcURL, chainID.String())
			continue
		}
		log.Printf("Using fallback Polygon RPC %s", rpcURL)
		return client, rpcURL, nil
	}

	return nil, "", fmt.Errorf("all Polygon RPCs failed (original: %s error: %v)", failedURL, originalErr)
}

// isRPCAuthError checks if an error indicates an authentication/authorization failure
// from an RPC provider (e.g. disabled API key, tenant disabled, 401/403 responses).
func isRPCAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tenant disabled") ||
		strings.Contains(msg, "api key disabled") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "rest code: 403") ||
		strings.Contains(msg, "rest code: 401")
}
