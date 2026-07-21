package gowild_crypto

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type evmTokenMetadata struct {
	decimals uint8
	symbol   string
}

// knownEVMTokenMetadata returns canonical metadata for well-known stablecoins.
// This protects balance formatting when RPC providers fail/lie on token metadata calls.
func knownEVMTokenMetadata(chainID *big.Int, tokenAddr common.Address) (evmTokenMetadata, bool) {
	normalized := strings.ToLower(tokenAddr.Hex())

	switch normalized {
	case strings.ToLower("0xC2132D05D31c914a87C6611C10748AEb04B58e8F"):
		// Polygon bridged USDT (often shown as USDT.e).
		return evmTokenMetadata{decimals: 6, symbol: "USDT"}, true
	case strings.ToLower("0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"):
		// Polygon bridged USDC (USDC.e).
		return evmTokenMetadata{decimals: 6, symbol: "USDC"}, true
	case strings.ToLower("0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359"):
		// Polygon native USDC.
		return evmTokenMetadata{decimals: 6, symbol: "USDC"}, true
	case strings.ToLower("0xdAC17F958D2ee523a2206206994597C13D831ec7"):
		// Ethereum mainnet USDT.
		return evmTokenMetadata{decimals: 6, symbol: "USDT"}, true
	case strings.ToLower("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"):
		// Ethereum mainnet USDC.
		return evmTokenMetadata{decimals: 6, symbol: "USDC"}, true
	}

	// Keep this parameter for future chain-specific metadata branches.
	_ = chainID
	return evmTokenMetadata{}, false
}
