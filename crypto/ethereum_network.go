package gowild_crypto

import (
	"fmt"
	"math/big"
)

const (
	chainIDEthereumMainnet int64 = 1
	chainIDPolygonMainnet  int64 = 137
	chainIDEthereumSepolia int64 = 11155111
	chainIDPolygonAmoy     int64 = 80002
)

func evmChainDisplayName(chainID *big.Int) string {
	if chainID == nil {
		return "unknown EVM chain"
	}

	switch chainID.Int64() {
	case chainIDEthereumMainnet:
		return "Ethereum Mainnet (1)"
	case chainIDPolygonMainnet:
		return "Polygon Mainnet (137)"
	case chainIDEthereumSepolia:
		return "Ethereum Sepolia (11155111)"
	case chainIDPolygonAmoy:
		return "Polygon Amoy (80002)"
	default:
		return fmt.Sprintf("EVM chain %s", chainID.String())
	}
}

func evmNativeSymbol(chainID *big.Int) string {
	if chainID == nil {
		return "ETH"
	}

	switch chainID.Int64() {
	case chainIDPolygonMainnet, chainIDPolygonAmoy:
		return "POL"
	default:
		return "ETH"
	}
}

func evmTxExplorerURL(chainID *big.Int, txHash string) string {
	if chainID == nil || txHash == "" {
		return ""
	}

	switch chainID.Int64() {
	case chainIDEthereumMainnet:
		return fmt.Sprintf("https://etherscan.io/tx/%s", txHash)
	case chainIDPolygonMainnet:
		return fmt.Sprintf("https://polygonscan.com/tx/%s", txHash)
	case chainIDEthereumSepolia:
		return fmt.Sprintf("https://sepolia.etherscan.io/tx/%s", txHash)
	case chainIDPolygonAmoy:
		return fmt.Sprintf("https://amoy.polygonscan.com/tx/%s", txHash)
	default:
		return ""
	}
}
