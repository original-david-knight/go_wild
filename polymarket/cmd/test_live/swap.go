package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	polygonRPC = "https://polygon-bor-rpc.publicnode.com"

	nativeUSDC  = "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359"
	bridgedUSDC = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174" // USDC.e

	// Uniswap V3 SwapRouter on Polygon
	uniswapRouter = "0xE592427A0AEce92De3Edee1F18E0157C05861564"

	// Polymarket exchanges
	negRiskExchange   = "0xC5d563A36AE78145C45a50134d48A1215220f80a"
	ctfExchange       = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	conditionalTokens = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"
	negRiskAdapter    = "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296"
)

// approveERC20 sends an ERC20 approve transaction.
func approveERC20(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, tokenAddr, spenderAddr common.Address, amount *big.Int) error {
	addressType, _ := abi.NewType("address", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	args := abi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
	}
	packed, err := args.Pack(spenderAddr, amount)
	if err != nil {
		return fmt.Errorf("pack approve: %w", err)
	}

	selector := crypto.Keccak256([]byte("approve(address,uint256)"))[:4]
	data := append(selector, packed...)

	return sendTx(ctx, client, key, tokenAddr, big.NewInt(0), data)
}

// setApprovalForAll calls ERC1155.setApprovalForAll(operator, true).
func setApprovalForAll(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, tokenAddr, operatorAddr common.Address) error {
	addressType, _ := abi.NewType("address", "", nil)
	boolType, _ := abi.NewType("bool", "", nil)
	args := abi.Arguments{
		{Type: addressType},
		{Type: boolType},
	}
	packed, err := args.Pack(operatorAddr, true)
	if err != nil {
		return fmt.Errorf("pack setApprovalForAll: %w", err)
	}

	selector := crypto.Keccak256([]byte("setApprovalForAll(address,bool)"))[:4]
	data := append(selector, packed...)

	return sendTx(ctx, client, key, tokenAddr, big.NewInt(0), data)
}

// swapUSDCtoUSDCe swaps native USDC to bridged USDC.e via Uniswap V3 with 0.01% fee tier.
func swapUSDCtoUSDCe(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, amount *big.Int) error {
	return swapUSDCtoUSDCeWithFee(ctx, client, key, amount, 100)
}

// swapUSDCtoUSDCeWithFee swaps native USDC to USDC.e with a specified fee tier.
func swapUSDCtoUSDCeWithFee(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, amount *big.Int, feeTier int64) error {
	recipient := crypto.PubkeyToAddress(key.PublicKey)

	// exactInputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))
	tupleType, _ := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "tokenIn", Type: "address"},
		{Name: "tokenOut", Type: "address"},
		{Name: "fee", Type: "uint24"},
		{Name: "recipient", Type: "address"},
		{Name: "deadline", Type: "uint256"},
		{Name: "amountIn", Type: "uint256"},
		{Name: "amountOutMinimum", Type: "uint256"},
		{Name: "sqrtPriceLimitX96", Type: "uint160"},
	})

	args := abi.Arguments{{Type: tupleType}}

	deadline := big.NewInt(time.Now().Unix() + 600)
	minOut := new(big.Int).Mul(amount, big.NewInt(99))
	minOut.Div(minOut, big.NewInt(100)) // 1% slippage

	type ExactInputSingleParams struct {
		TokenIn           common.Address
		TokenOut          common.Address
		Fee               *big.Int
		Recipient         common.Address
		Deadline          *big.Int
		AmountIn          *big.Int
		AmountOutMinimum  *big.Int
		SqrtPriceLimitX96 *big.Int
	}

	params := ExactInputSingleParams{
		TokenIn:           common.HexToAddress(nativeUSDC),
		TokenOut:          common.HexToAddress(bridgedUSDC),
		Fee:               big.NewInt(feeTier),
		Recipient:         recipient,
		Deadline:          deadline,
		AmountIn:          amount,
		AmountOutMinimum:  minOut,
		SqrtPriceLimitX96: big.NewInt(0),
	}

	packed, err := args.Pack(params)
	if err != nil {
		return fmt.Errorf("pack swap params: %w", err)
	}

	selector := crypto.Keccak256([]byte("exactInputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))"))[:4]
	data := append(selector, packed...)

	return sendTx(ctx, client, key, common.HexToAddress(uniswapRouter), big.NewInt(0), data)
}

// sendTx builds, signs, and broadcasts a transaction, then waits for confirmation.
func sendTx(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, to common.Address, value *big.Int, data []byte) error {
	from := crypto.PubkeyToAddress(key.PublicKey)

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return fmt.Errorf("get nonce: %w", err)
	}

	suggestedPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("suggest gas price: %w", err)
	}
	// Add 50% buffer for faster confirmation
	gasPrice := new(big.Int).Mul(suggestedPrice, big.NewInt(150))
	gasPrice.Div(gasPrice, big.NewInt(100))

	gasLimit, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From:  from,
		To:    &to,
		Value: value,
		Data:  data,
	})
	if err != nil {
		return fmt.Errorf("estimate gas: %w", err)
	}

	tx := types.NewTransaction(nonce, to, value, gasLimit+20000, gasPrice, data)

	chainID := big.NewInt(137)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key)
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return fmt.Errorf("send tx: %w", err)
	}

	fmt.Printf("  Tx sent: %s\n", signedTx.Hash().Hex())

	// Wait for receipt with retries
	for i := 0; i < 60; i++ {
		time.Sleep(3 * time.Second)
		receipt, err := client.TransactionReceipt(ctx, signedTx.Hash())
		if err == nil {
			if receipt.Status == 1 {
				fmt.Printf("  Tx confirmed in block %d (gas used: %d)\n", receipt.BlockNumber, receipt.GasUsed)
				return nil
			}
			return fmt.Errorf("tx reverted in block %d", receipt.BlockNumber)
		}
	}
	return fmt.Errorf("tx not confirmed after 180s")
}
