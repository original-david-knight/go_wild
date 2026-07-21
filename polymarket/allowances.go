package gowild_polymarket

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

const erc20AllowanceABI = `[
	{
		"inputs": [
			{"internalType": "address", "name": "owner", "type": "address"},
			{"internalType": "address", "name": "spender", "type": "address"}
		],
		"name": "allowance",
		"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{"internalType": "address", "name": "spender", "type": "address"},
			{"internalType": "uint256", "name": "value", "type": "uint256"}
		],
		"name": "approve",
		"outputs": [{"internalType": "bool", "name": "", "type": "bool"}],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

const erc1155OperatorApprovalABI = `[
	{
		"inputs": [
			{"internalType": "address", "name": "account", "type": "address"},
			{"internalType": "address", "name": "operator", "type": "address"}
		],
		"name": "isApprovedForAll",
		"outputs": [{"internalType": "bool", "name": "", "type": "bool"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{"internalType": "address", "name": "operator", "type": "address"},
			{"internalType": "bool", "name": "approved", "type": "bool"}
		],
		"name": "setApprovalForAll",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

var (
	autoApprovalThreshold = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	maxUint256Approval    = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

// supportsAutomaticAllowanceSetup returns true only when the order signer is
// also the funder wallet (EOA mode). In proxy/safe mode, the signer may not be
// able to submit on-chain approve transactions for the funder.
func (c *Client) supportsAutomaticAllowanceSetup() bool {
	if c == nil {
		return false
	}
	if c.privateKey == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.address), strings.TrimSpace(c.funder))
}

// ensureTradingAllowances ensures pUSD and CTF approvals exist for Polymarket's
// current exchange contracts. It is idempotent and only performs on-chain writes when
// the current allowance is below a high threshold.
func (c *Client) ensureTradingAllowances(ctx context.Context) error {
	c.approvalMu.Lock()
	defer c.approvalMu.Unlock()

	if c.approvalsEnsured {
		return nil
	}
	if c.ensureAllowancesFn != nil {
		if err := c.ensureAllowancesFn(ctx); err != nil {
			return err
		}
		c.approvalsEnsured = true
		return nil
	}
	if !c.supportsAutomaticAllowanceSetup() {
		c.approvalsEnsured = true
		return nil
	}

	if err := c.ensureCollateralAllowances(ctx); err != nil {
		return err
	}
	if err := c.ensureConditionalTokenApprovals(ctx); err != nil {
		return err
	}
	c.approvalsEnsured = true
	return nil
}

func (c *Client) ensureCollateralAllowances(ctx context.Context) error {
	rpcClient, rpcURL, err := c.connectPolygonRPC(ctx)
	if err != nil {
		return err
	}
	defer rpcClient.Close()

	chainID, err := rpcClient.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to read chain ID from RPC %s: %w", rpcURL, err)
	}
	if chainID.Int64() != polygonChainID {
		return fmt.Errorf("automatic allowance setup requires Polygon chain %d, got chain %s from %s", polygonChainID, chainID.String(), rpcURL)
	}

	tokenABI, err := abi.JSON(strings.NewReader(erc20AllowanceABI))
	if err != nil {
		return fmt.Errorf("failed to parse ERC20 ABI: %w", err)
	}

	owner := common.HexToAddress(c.address)
	tokenAddress := common.HexToAddress(PUSDAddress)
	spenders := []common.Address{
		common.HexToAddress(ctfExchangeAddress),
		common.HexToAddress(negRiskCTFExchangeAddress),
		common.HexToAddress(negRiskAdapterAddress),
	}

	nonce, err := rpcClient.PendingNonceAt(ctx, owner)
	if err != nil {
		return fmt.Errorf("failed to get nonce for %s: %w", owner.Hex(), err)
	}

	for _, spender := range spenders {
		allowance, err := erc20Allowance(ctx, rpcClient, tokenABI, tokenAddress, owner, spender)
		if err != nil {
			return fmt.Errorf("failed to read pUSD allowance for spender %s: %w", spender.Hex(), err)
		}
		if allowance.Cmp(autoApprovalThreshold) >= 0 {
			continue
		}

		gasPrice, err := rpcClient.SuggestGasPrice(ctx)
		if err != nil {
			return fmt.Errorf("failed to suggest gas price for allowance tx: %w", err)
		}

		if err := c.sendApproveTx(ctx, rpcClient, tokenABI, tokenAddress, owner, spender, nonce, gasPrice, chainID); err != nil {
			return err
		}
		nonce++
	}

	return nil
}

func erc20Allowance(ctx context.Context, rpcClient *ethclient.Client, tokenABI abi.ABI, tokenAddress, owner, spender common.Address) (*big.Int, error) {
	data, err := tokenABI.Pack("allowance", owner, spender)
	if err != nil {
		return nil, fmt.Errorf("failed to encode allowance call: %w", err)
	}

	raw, err := callContractWithRetry(ctx, rpcClient, ethereum.CallMsg{To: &tokenAddress, Data: data})
	if err != nil {
		return nil, fmt.Errorf("allowance call failed: %w", err)
	}

	values, err := tokenABI.Unpack("allowance", raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode allowance result: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected allowance return length: %d", len(values))
	}

	amount, ok := values[0].(*big.Int)
	if !ok || amount == nil {
		return nil, fmt.Errorf("unexpected allowance return type: %T", values[0])
	}
	return new(big.Int).Set(amount), nil
}

func (c *Client) sendApproveTx(
	ctx context.Context,
	rpcClient *ethclient.Client,
	tokenABI abi.ABI,
	tokenAddress common.Address,
	from common.Address,
	spender common.Address,
	nonce uint64,
	gasPrice *big.Int,
	chainID *big.Int,
) error {
	data, err := tokenABI.Pack("approve", spender, new(big.Int).Set(maxUint256Approval))
	if err != nil {
		return fmt.Errorf("failed to encode approve call for spender %s: %w", spender.Hex(), err)
	}

	msg := ethereum.CallMsg{From: from, To: &tokenAddress, Data: data}
	gasLimit, err := rpcClient.EstimateGas(ctx, msg)
	if err != nil {
		gasLimit = 100000
	}

	tx := types.NewTransaction(nonce, tokenAddress, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), c.privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign approve tx for spender %s: %w", spender.Hex(), err)
	}

	if err := rpcClient.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("failed to send approve tx for spender %s: %w", spender.Hex(), err)
	}

	receiptCtx, cancel := context.WithTimeout(ctx, redeemReceiptWaitTimeout)
	defer cancel()
	receipt, err := waitForReceipt(receiptCtx, rpcClient, signedTx.Hash())
	if err != nil {
		return fmt.Errorf("failed waiting for approve tx %s for spender %s: %w", signedTx.Hash().Hex(), spender.Hex(), err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("approve tx %s reverted for spender %s", signedTx.Hash().Hex(), spender.Hex())
	}
	return nil
}

func (c *Client) ensureConditionalTokenApprovals(ctx context.Context) error {
	rpcClient, rpcURL, err := c.connectPolygonRPC(ctx)
	if err != nil {
		return err
	}
	defer rpcClient.Close()

	chainID, err := rpcClient.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("failed to read chain ID from RPC %s: %w", rpcURL, err)
	}
	if chainID.Int64() != polygonChainID {
		return fmt.Errorf("automatic conditional-token approval setup requires Polygon chain %d, got chain %s from %s", polygonChainID, chainID.String(), rpcURL)
	}

	tokenABI, err := abi.JSON(strings.NewReader(erc1155OperatorApprovalABI))
	if err != nil {
		return fmt.Errorf("failed to parse ERC1155 approval ABI: %w", err)
	}

	owner := common.HexToAddress(c.address)
	tokenAddress := common.HexToAddress(conditionalTokensAddress)
	operators := []common.Address{
		common.HexToAddress(ctfExchangeAddress),
		common.HexToAddress(negRiskCTFExchangeAddress),
		common.HexToAddress(negRiskAdapterAddress),
	}

	nonce, err := rpcClient.PendingNonceAt(ctx, owner)
	if err != nil {
		return fmt.Errorf("failed to get nonce for %s: %w", owner.Hex(), err)
	}

	gasPrice, err := rpcClient.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("failed to suggest gas price for conditional-token approval tx: %w", err)
	}

	for _, operator := range operators {
		approved, err := erc1155IsApprovedForAll(ctx, rpcClient, tokenABI, tokenAddress, owner, operator)
		if err != nil {
			return fmt.Errorf("failed to read conditional-token operator approval for %s: %w", operator.Hex(), err)
		}
		if approved {
			continue
		}

		if err := c.sendSetApprovalForAllTx(ctx, rpcClient, tokenABI, tokenAddress, owner, operator, nonce, gasPrice, chainID); err != nil {
			return err
		}
		nonce++
	}

	return nil
}

func erc1155IsApprovedForAll(
	ctx context.Context,
	rpcClient *ethclient.Client,
	tokenABI abi.ABI,
	tokenAddress common.Address,
	owner common.Address,
	operator common.Address,
) (bool, error) {
	data, err := tokenABI.Pack("isApprovedForAll", owner, operator)
	if err != nil {
		return false, fmt.Errorf("failed to encode isApprovedForAll call: %w", err)
	}

	raw, err := callContractWithRetry(ctx, rpcClient, ethereum.CallMsg{To: &tokenAddress, Data: data})
	if err != nil {
		return false, fmt.Errorf("isApprovedForAll call failed: %w", err)
	}

	values, err := tokenABI.Unpack("isApprovedForAll", raw)
	if err != nil {
		return false, fmt.Errorf("failed to decode isApprovedForAll result: %w", err)
	}
	if len(values) != 1 {
		return false, fmt.Errorf("unexpected isApprovedForAll return length: %d", len(values))
	}

	approved, ok := values[0].(bool)
	if !ok {
		return false, fmt.Errorf("unexpected isApprovedForAll return type: %T", values[0])
	}
	return approved, nil
}

func (c *Client) sendSetApprovalForAllTx(
	ctx context.Context,
	rpcClient *ethclient.Client,
	tokenABI abi.ABI,
	tokenAddress common.Address,
	from common.Address,
	operator common.Address,
	nonce uint64,
	gasPrice *big.Int,
	chainID *big.Int,
) error {
	data, err := tokenABI.Pack("setApprovalForAll", operator, true)
	if err != nil {
		return fmt.Errorf("failed to encode setApprovalForAll call for operator %s: %w", operator.Hex(), err)
	}

	msg := ethereum.CallMsg{From: from, To: &tokenAddress, Data: data}
	gasLimit, err := rpcClient.EstimateGas(ctx, msg)
	if err != nil {
		gasLimit = 120000
	}

	tx := types.NewTransaction(nonce, tokenAddress, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), c.privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign setApprovalForAll tx for operator %s: %w", operator.Hex(), err)
	}

	if err := rpcClient.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("failed to send setApprovalForAll tx for operator %s: %w", operator.Hex(), err)
	}

	receiptCtx, cancel := context.WithTimeout(ctx, redeemReceiptWaitTimeout)
	defer cancel()
	receipt, err := waitForReceipt(receiptCtx, rpcClient, signedTx.Hash())
	if err != nil {
		return fmt.Errorf("failed waiting for setApprovalForAll tx %s for operator %s: %w", signedTx.Hash().Hex(), operator.Hex(), err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("setApprovalForAll tx %s reverted for operator %s", signedTx.Hash().Hex(), operator.Hex())
	}
	return nil
}
