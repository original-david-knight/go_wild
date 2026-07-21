package gowild_polymarket

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// sweepDustRaw is the minimum raw (6-decimal) token balance worth wrapping into
// pUSD: $0.01. Anything smaller is left in place — a wrap transaction for
// sub-cent dust can never grow the spendable budget by a placeable amount.
var sweepDustRaw = big.NewInt(10_000)

// sweepableCollateralAssets are the wallet tokens SweepCollateralToPUSD converts,
// in sweep order: USDC.e (what ConditionalTokens redemptions pay out) and native
// USDC (what fresh deposits typically arrive as). Both are valid wrap assets of
// the pUSD CollateralToken; pUSD itself is the only collateral the CLOB V2
// exchanges settle in.
var sweepableCollateralAssets = []SweepAsset{
	{TokenAddress: USDCAddress, Symbol: "USDC.e"},
	{TokenAddress: NativeUSDCAddress, Symbol: "USDC"},
}

// SweepAsset names one wallet token the collateral sweep considers.
type SweepAsset struct {
	TokenAddress string
	Symbol       string
}

// SweptCollateral records one completed asset wrap: the source token, the amount
// converted (1:1 into pUSD, in human 6-decimal units), and the wrap transaction.
type SweptCollateral struct {
	TokenAddress string
	Symbol       string
	Amount       float64
	TxHash       string
}

// CollateralSweepResult is the outcome of one SweepCollateralToPUSD call.
type CollateralSweepResult struct {
	Swept      []SweptCollateral
	TotalSwept float64 // sum of Swept amounts; equals the pUSD minted
}

const erc20BalanceOfABI = `[
	{
		"inputs": [{"internalType": "address", "name": "account", "type": "address"}],
		"name": "balanceOf",
		"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

const collateralOnrampABI = `[
	{
		"inputs": [
			{"internalType": "address", "name": "_asset", "type": "address"},
			{"internalType": "address", "name": "_to", "type": "address"},
			{"internalType": "uint256", "name": "_amount", "type": "uint256"}
		],
		"name": "wrap",
		"outputs": [],
		"stateMutability": "nonpayable",
		"type": "function"
	}
]`

// SweepCollateralToPUSD wraps the signer wallet's entire USDC.e and native USDC
// balances into pUSD via Polymarket's public CollateralOnramp (1:1, fee-free),
// minting the pUSD to the funder (the order maker, so the CLOB's balance check
// sees it). Per asset it approves the onramp when the current allowance is
// insufficient, submits wrap, and waits for the receipt. Balances below one cent
// are skipped. After a non-empty sweep it polls the funder's pUSD balance until
// the minted amount is visible (or a short timeout), so an immediately following
// balance read on a load-balanced RPC does not see a stale pre-sweep value.
//
// It requires the signing key (EOA); the first asset that fails aborts the sweep
// with the partial result of the assets already wrapped.
func (c *Client) SweepCollateralToPUSD(ctx context.Context) (*CollateralSweepResult, error) {
	if c.privateKey == nil {
		return nil, fmt.Errorf("collateral sweep requires the signing private key")
	}

	rpcClient, rpcURL, err := c.connectPolygonRPC(ctx)
	if err != nil {
		return nil, err
	}
	defer rpcClient.Close()

	balanceABI, err := abi.JSON(strings.NewReader(erc20BalanceOfABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC20 balance ABI: %w", err)
	}
	allowanceABI, err := abi.JSON(strings.NewReader(erc20AllowanceABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC20 allowance ABI: %w", err)
	}
	onrampABI, err := abi.JSON(strings.NewReader(collateralOnrampABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse onramp ABI: %w", err)
	}

	owner := common.HexToAddress(c.address)
	recipient := common.HexToAddress(c.funder) // the order maker whose pUSD the CLOB checks
	onramp := common.HexToAddress(CollateralOnrampAddress)
	pusd := common.HexToAddress(PUSDAddress)
	chainID := big.NewInt(polygonChainID)

	pusdBefore, err := erc20BalanceOf(ctx, rpcClient, balanceABI, pusd, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to read pUSD balance from %s: %w", rpcURL, err)
	}

	nonce, err := rpcClient.PendingNonceAt(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce for %s: %w", owner.Hex(), err)
	}

	res := &CollateralSweepResult{}
	totalRaw := new(big.Int)
	for _, asset := range sweepableCollateralAssets {
		token := common.HexToAddress(asset.TokenAddress)

		balance, err := erc20BalanceOf(ctx, rpcClient, balanceABI, token, owner)
		if err != nil {
			return res, fmt.Errorf("failed to read %s balance: %w", asset.Symbol, err)
		}
		if balance.Cmp(sweepDustRaw) < 0 {
			continue
		}

		gasPrice, err := rpcClient.SuggestGasPrice(ctx)
		if err != nil {
			return res, fmt.Errorf("failed to suggest gas price for %s sweep: %w", asset.Symbol, err)
		}

		allowance, err := erc20Allowance(ctx, rpcClient, allowanceABI, token, owner, onramp)
		if err != nil {
			return res, fmt.Errorf("failed to read %s allowance for onramp: %w", asset.Symbol, err)
		}
		if allowance.Cmp(balance) < 0 {
			if err := c.sendApproveTx(ctx, rpcClient, allowanceABI, token, owner, onramp, nonce, gasPrice, chainID); err != nil {
				return res, fmt.Errorf("%s onramp approval failed: %w", asset.Symbol, err)
			}
			nonce++
		}

		txHash, err := c.sendOnrampWrapTx(ctx, rpcClient, onrampABI, onramp, owner, token, recipient, balance, nonce, gasPrice, chainID)
		if err != nil {
			return res, fmt.Errorf("%s wrap failed: %w", asset.Symbol, err)
		}
		nonce++

		res.Swept = append(res.Swept, SweptCollateral{
			TokenAddress: asset.TokenAddress,
			Symbol:       asset.Symbol,
			Amount:       rawToUSDC(balance),
			TxHash:       txHash,
		})
		totalRaw.Add(totalRaw, balance)
	}
	res.TotalSwept = rawToUSDC(totalRaw)

	// The wrap receipts confirmed, so the pUSD is minted; a load-balanced RPC node
	// can still briefly serve a pre-sweep balance. Wait until the minted amount is
	// visible so the caller's next balance read reflects the sweep.
	if len(res.Swept) > 0 {
		want := new(big.Int).Add(pusdBefore, totalRaw)
		waitForBalanceAtLeast(ctx, rpcClient, balanceABI, pusd, recipient, want, 20*time.Second)
	}
	return res, nil
}

// sendOnrampWrapTx submits onramp.wrap(asset, to, amount) and waits for a
// successful receipt, returning the transaction hash.
func (c *Client) sendOnrampWrapTx(
	ctx context.Context,
	rpcClient *ethclient.Client,
	onrampABI abi.ABI,
	onramp common.Address,
	from common.Address,
	asset common.Address,
	to common.Address,
	amount *big.Int,
	nonce uint64,
	gasPrice *big.Int,
	chainID *big.Int,
) (string, error) {
	data, err := onrampABI.Pack("wrap", asset, to, amount)
	if err != nil {
		return "", fmt.Errorf("failed to encode wrap call: %w", err)
	}

	msg := ethereum.CallMsg{From: from, To: &onramp, Data: data}
	gasLimit, err := rpcClient.EstimateGas(ctx, msg)
	if err != nil {
		gasLimit = 200000
	}

	tx := types.NewTransaction(nonce, onramp, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), c.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign wrap tx: %w", err)
	}

	if err := rpcClient.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("failed to send wrap tx: %w", err)
	}

	receiptCtx, cancel := context.WithTimeout(ctx, redeemReceiptWaitTimeout)
	defer cancel()
	receipt, err := waitForReceipt(receiptCtx, rpcClient, signedTx.Hash())
	if err != nil {
		return "", fmt.Errorf("failed waiting for wrap tx %s: %w", signedTx.Hash().Hex(), err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return "", fmt.Errorf("wrap tx %s reverted", signedTx.Hash().Hex())
	}
	return signedTx.Hash().Hex(), nil
}

// erc20BalanceOf reads an ERC-20 balance.
func erc20BalanceOf(ctx context.Context, rpcClient *ethclient.Client, balanceABI abi.ABI, token, owner common.Address) (*big.Int, error) {
	data, err := balanceABI.Pack("balanceOf", owner)
	if err != nil {
		return nil, fmt.Errorf("failed to encode balanceOf call: %w", err)
	}

	raw, err := callContractWithRetry(ctx, rpcClient, ethereum.CallMsg{To: &token, Data: data})
	if err != nil {
		return nil, fmt.Errorf("balanceOf call failed: %w", err)
	}

	values, err := balanceABI.Unpack("balanceOf", raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode balanceOf result: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected balanceOf return length: %d", len(values))
	}

	amount, ok := values[0].(*big.Int)
	if !ok || amount == nil {
		return nil, fmt.Errorf("unexpected balanceOf return type: %T", values[0])
	}
	return new(big.Int).Set(amount), nil
}

// waitForBalanceAtLeast polls the token balance until it reaches `want` or the
// timeout elapses. It is a read-only convergence wait — failures and timeouts are
// ignored, since the state change itself is already receipt-confirmed.
func waitForBalanceAtLeast(ctx context.Context, rpcClient *ethclient.Client, balanceABI abi.ABI, token, owner common.Address, want *big.Int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		balance, err := erc20BalanceOf(ctx, rpcClient, balanceABI, token, owner)
		if err == nil && balance.Cmp(want) >= 0 {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// rawToUSDC converts a raw 6-decimal token amount to human units.
func rawToUSDC(raw *big.Int) float64 {
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(raw), big.NewFloat(1e6)).Float64()
	return f
}
