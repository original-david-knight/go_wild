package gowild_crypto

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ethereumWallet manages an Ethereum wallet.
// The private key is kept internal and never exposed.
type ethereumWallet struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
	rpcURL     string
	chainID    *big.Int
}

// newEthereumWallet creates a new Ethereum wallet from a hex-encoded private key.
// The key can be with or without the 0x prefix.
func newEthereumWallet(privateKeyHex string, rpcURL string) (*ethereumWallet, error) {
	// Remove 0x prefix if present
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	return &ethereumWallet{
		privateKey: privateKey,
		address:    address,
		rpcURL:     rpcURL,
	}, nil
}

// Address returns the public Ethereum address (checksummed).
func (w *ethereumWallet) Address() string {
	return w.address.Hex()
}

// getClient creates an ethclient connected to the RPC endpoint.
func (w *ethereumWallet) getClient(ctx context.Context) (*ethclient.Client, error) {
	client, err := ethclient.DialContext(ctx, w.rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	// Cache chain ID on first connection
	if w.chainID == nil {
		chainID, err := client.ChainID(ctx)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to get chain ID: %w", err)
		}
		w.chainID = chainID
	}

	return client, nil
}
