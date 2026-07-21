package gowild_crypto

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestKnownEVMTokenMetadata(t *testing.T) {
	tests := []struct {
		name       string
		chainID    *big.Int
		token      string
		wantOK     bool
		wantDec    uint8
		wantSymbol string
	}{
		{
			name:       "polygon_usdt",
			chainID:    big.NewInt(chainIDPolygonMainnet),
			token:      "0xC2132D05D31c914a87C6611C10748AEb04B58e8F",
			wantOK:     true,
			wantDec:    6,
			wantSymbol: "USDT",
		},
		{
			name:       "polygon_usdc_e",
			chainID:    big.NewInt(chainIDPolygonMainnet),
			token:      "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
			wantOK:     true,
			wantDec:    6,
			wantSymbol: "USDC",
		},
		{
			name:       "ethereum_usdc",
			chainID:    big.NewInt(chainIDEthereumMainnet),
			token:      "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			wantOK:     true,
			wantDec:    6,
			wantSymbol: "USDC",
		},
		{
			name:    "unknown_token",
			chainID: big.NewInt(chainIDPolygonMainnet),
			token:   "0x0000000000000000000000000000000000000001",
			wantOK:  false,
			wantDec: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := knownEVMTokenMetadata(tc.chainID, common.HexToAddress(tc.token))
			if ok != tc.wantOK {
				t.Fatalf("knownEVMTokenMetadata ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.decimals != tc.wantDec {
				t.Fatalf("knownEVMTokenMetadata decimals = %d, want %d", got.decimals, tc.wantDec)
			}
			if got.symbol != tc.wantSymbol {
				t.Fatalf("knownEVMTokenMetadata symbol = %q, want %q", got.symbol, tc.wantSymbol)
			}
		})
	}
}
