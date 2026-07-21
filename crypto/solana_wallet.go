package gowild_crypto

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// solanaWallet manages a Solana wallet.
// The private key is kept internal and never exposed.
type solanaWallet struct {
	keypair solana.PrivateKey
	rpcURL  string
}

// newSolanaWallet creates a new Solana wallet from a base58-encoded private key.
// Solana private keys are typically 64 bytes (32-byte seed + 32-byte public key)
// encoded in base58.
func newSolanaWallet(privateKeyBase58 string, rpcURL string) (*solanaWallet, error) {
	keypair, err := solana.PrivateKeyFromBase58(privateKeyBase58)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	return &solanaWallet{
		keypair: keypair,
		rpcURL:  rpcURL,
	}, nil
}

// Address returns the public Solana address (base58 encoded).
func (w *solanaWallet) Address() string {
	return w.keypair.PublicKey().String()
}

// getClient creates an RPC client.
func (w *solanaWallet) getClient() *rpc.Client {
	return rpc.New(w.rpcURL)
}
