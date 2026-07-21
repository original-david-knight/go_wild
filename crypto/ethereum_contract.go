package gowild_crypto

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ContractCall calls a smart contract method.
// method should be the function signature like "transfer(address,uint256)" or just "transfer".
// For read-only calls, no transaction is submitted.
func (w *ethereumWallet) ContractCall(ctx context.Context, contractAddress string, method string, args []any, value string, readOnly bool) (*ContractCallResult, error) {
	client, err := w.getClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	contractAddr := common.HexToAddress(contractAddress)

	// Build function selector from method signature
	var selector []byte
	if strings.Contains(method, "(") {
		// Full signature provided - compute selector
		selector = crypto.Keccak256([]byte(method))[:4]
	} else {
		return nil, fmt.Errorf("method must include full signature, e.g., 'transfer(address,uint256)'")
	}

	// Encode arguments (simplified - handles common types)
	var data []byte
	data = append(data, selector...)
	for _, arg := range args {
		encoded, err := encodeArg(arg)
		if err != nil {
			return nil, fmt.Errorf("failed to encode argument: %w", err)
		}
		data = append(data, encoded...)
	}

	if readOnly {
		// Call without sending transaction
		result, err := client.CallContract(ctx, ethereum.CallMsg{
			From: w.address,
			To:   &contractAddr,
			Data: data,
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("contract call failed: %w", err)
		}

		return &ContractCallResult{
			Chain:           ChainEthereum,
			ContractAddress: contractAddress,
			Method:          method,
			Result:          hexutil.Encode(result),
		}, nil
	}

	// Write transaction
	valueWei := big.NewInt(0)
	if value != "" {
		valueFloat, ok := new(big.Float).SetString(value)
		if !ok {
			return nil, fmt.Errorf("invalid value: %s", value)
		}
		weiFloat := new(big.Float).Mul(valueFloat, big.NewFloat(1e18))
		valueWei, _ = weiFloat.Int(nil)
	}

	nonce, err := client.PendingNonceAt(ctx, w.address)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	msg := ethereum.CallMsg{
		From:  w.address,
		To:    &contractAddr,
		Value: valueWei,
		Data:  data,
	}
	gasLimit, err := client.EstimateGas(ctx, msg)
	if err != nil {
		gasLimit = 200000 // Default gas limit
	}

	tx := types.NewTransaction(nonce, contractAddr, valueWei, gasLimit, gasPrice, data)

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

	return &ContractCallResult{
		Chain:           ChainEthereum,
		TransactionHash: signedTx.Hash().Hex(),
		ContractAddress: contractAddress,
		Method:          method,
		ExplorerURL:     evmTxExplorerURL(w.chainID, signedTx.Hash().Hex()),
	}, nil
}

// encodeArg encodes a single argument for a contract call.
// Handles addresses, uint256, and bytes32.
func encodeArg(arg any) ([]byte, error) {
	result := make([]byte, 32)

	switch v := arg.(type) {
	case string:
		if strings.HasPrefix(v, "0x") {
			// Address or bytes
			decoded, err := hexutil.Decode(v)
			if err != nil {
				return nil, err
			}
			if len(decoded) == 20 {
				// Address - right-align in 32 bytes
				copy(result[12:], decoded)
			} else if len(decoded) <= 32 {
				// bytes32 or smaller
				copy(result[32-len(decoded):], decoded)
			} else {
				return nil, fmt.Errorf("bytes too long: %d", len(decoded))
			}
		} else {
			// Try to parse as number
			n, ok := new(big.Int).SetString(v, 10)
			if !ok {
				return nil, fmt.Errorf("cannot encode string: %s", v)
			}
			n.FillBytes(result)
		}
	case int, int64:
		n := big.NewInt(v.(int64))
		n.FillBytes(result)
	case float64:
		n := big.NewInt(int64(v))
		n.FillBytes(result)
	case *big.Int:
		v.FillBytes(result)
	default:
		return nil, fmt.Errorf("unsupported argument type: %T", arg)
	}

	return result, nil
}
