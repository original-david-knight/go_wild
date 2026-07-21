package gowild_crypto

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"strings"
)

// Default RPC endpoints (public, rate-limited - users should provide their own)
const (
	defaultEthereumRPC = "https://eth.drpc.org"
	defaultSolanaRPC   = "https://api.mainnet-beta.solana.com"
)

// Wallet manages blockchain wallets with private keys loaded from environment.
// Private keys are never exposed through the API - only public addresses and
// cryptographic operations are available.
type Wallet struct {
	ethWallet *ethereumWallet
	solWallet *solanaWallet
}

// WalletConfig holds the configuration for creating a wallet.
type WalletConfig struct {
	EthPrivateKey string // Ethereum private key (hex, with or without 0x prefix)
	SolPrivateKey string // Solana private key (base58 encoded)
	EthRPCURL     string // Ethereum RPC endpoint (optional, defaults to public endpoint)
	SolRPCURL     string // Solana RPC endpoint (optional, defaults to public endpoint)
}

// NewWallet creates a Wallet from the given configuration.
// At least one private key (ETH or SOL) must be provided.
func NewWallet(config WalletConfig) (*Wallet, error) {
	w := &Wallet{}

	// Load Ethereum wallet
	if config.EthPrivateKey != "" {
		ethRPC := config.EthRPCURL
		if ethRPC == "" {
			ethRPC = os.Getenv("WALLET_ETH_RPC_URL")
		}
		if ethRPC == "" {
			ethRPC = defaultEthereumRPC
		}
		ethWallet, err := newEthereumWallet(config.EthPrivateKey, ethRPC)
		if err != nil {
			return nil, fmt.Errorf("failed to load ethereum wallet: %w", err)
		}
		w.ethWallet = ethWallet
	}

	// Load Solana wallet
	if config.SolPrivateKey != "" {
		solRPC := config.SolRPCURL
		if solRPC == "" {
			solRPC = os.Getenv("WALLET_SOL_RPC_URL")
		}
		if solRPC == "" {
			solRPC = defaultSolanaRPC
		}
		solWallet, err := newSolanaWallet(config.SolPrivateKey, solRPC)
		if err != nil {
			return nil, fmt.Errorf("failed to load solana wallet: %w", err)
		}
		w.solWallet = solWallet
	}

	if w.ethWallet == nil && w.solWallet == nil {
		return nil, fmt.Errorf("no wallet keys provided (need EthPrivateKey or SolPrivateKey)")
	}

	return w, nil
}

// GetAddress returns the public address for the specified chain.
// Returns error if the chain is not configured.
func (w *Wallet) GetAddress(chain Chain) (*WalletInfo, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		return &WalletInfo{
			Chain:   ChainEthereum,
			Address: w.ethWallet.Address(),
		}, nil

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return &WalletInfo{
			Chain:   ChainSolana,
			Address: w.solWallet.Address(),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported chain: %s (supported: ethereum, solana)", chain)
	}
}

// SignMessage cryptographically signs a message with the wallet's private key.
// The signature can be verified by anyone with the public address.
func (w *Wallet) SignMessage(chain Chain, message string) (*SignedMessage, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		return w.ethWallet.SignMessage(message)

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return w.solWallet.SignMessage(message)

	default:
		return nil, fmt.Errorf("unsupported chain: %s (supported: ethereum, solana)", chain)
	}
}

// availableChains returns which chains have wallets configured.
func (w *Wallet) availableChains() []Chain {
	var chains []Chain
	if w.ethWallet != nil {
		chains = append(chains, ChainEthereum)
	}
	if w.solWallet != nil {
		chains = append(chains, ChainSolana)
	}
	return chains
}

// hasChain returns true if the specified chain is configured.
func (w *Wallet) hasChain(chain Chain) bool {
	chain = Chain(strings.ToLower(string(chain)))
	switch chain {
	case ChainEthereum:
		return w.ethWallet != nil
	case ChainSolana:
		return w.solWallet != nil
	default:
		return false
	}
}

// SendToken sends native tokens or ERC20/SPL tokens to a destination address.
// For native tokens (ETH/SOL), leave tokenAddress empty.
// Amount is in human-readable units (e.g., "1.5" for 1.5 ETH).
// Optionally attach a memo (Solana only).
func (w *Wallet) SendToken(ctx context.Context, chain Chain, to string, amount string, tokenAddress string, memo string) (*TransactionResult, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		if memo != "" {
			return nil, fmt.Errorf("memo not supported on Ethereum transfers")
		}
		return w.ethWallet.SendToken(ctx, to, amount, tokenAddress)

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return w.solWallet.SendToken(ctx, to, amount, tokenAddress, memo)

	default:
		return nil, fmt.Errorf("unsupported chain: %s", chain)
	}
}

// SwapToken swaps one token for another using DEX aggregators.
// Uses Uniswap/0x for Ethereum and Jupiter for Solana.
// Amount is in human-readable units of the source token.
// Set slippageBps to control max slippage (e.g., 50 = 0.5%).
func (w *Wallet) SwapToken(ctx context.Context, chain Chain, fromToken string, toToken string, amount string, slippageBps int) (*SwapResult, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		return w.ethWallet.SwapToken(ctx, fromToken, toToken, amount, slippageBps)

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return w.solWallet.SwapToken(ctx, fromToken, toToken, amount, slippageBps)

	default:
		return nil, fmt.Errorf("unsupported chain: %s", chain)
	}
}

// GetBalance returns the native token balance (ETH or SOL) for the specified chain.
func (w *Wallet) GetBalance(ctx context.Context, chain Chain) (*BalanceResult, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		return w.ethWallet.GetBalance(ctx)

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return w.solWallet.GetBalance(ctx)

	default:
		return nil, fmt.Errorf("unsupported chain: %s (supported: ethereum, solana)", chain)
	}
}

// Ed25519PublicKey returns the raw 32-byte Ed25519 public key from the Solana wallet.
func (w *Wallet) Ed25519PublicKey() (ed25519.PublicKey, error) {
	if w.solWallet == nil {
		return nil, fmt.Errorf("solana wallet not configured")
	}
	// solana.PrivateKey is []byte (64 bytes: seed + public key), compatible with ed25519.PrivateKey
	edPriv := ed25519.PrivateKey(w.solWallet.keypair)
	return edPriv.Public().(ed25519.PublicKey), nil
}

// EncryptMessage encrypts plaintext for a recipient using NaCl box.
// Uses the Solana wallet's Ed25519 key, converted to X25519.
func (w *Wallet) EncryptMessage(plaintext []byte, recipientEd25519PubKey []byte) (ciphertext, nonce []byte, err error) {
	if w.solWallet == nil {
		return nil, nil, fmt.Errorf("solana wallet not configured")
	}

	edPriv := ed25519.PrivateKey(w.solWallet.keypair)
	senderX25519Priv := ed25519PrivateKeyToX25519(edPriv)

	recipientX25519Pub, err := ed25519PublicKeyToX25519(ed25519.PublicKey(recipientEd25519PubKey))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid recipient public key: %w", err)
	}

	ct, n, err := naclBoxSeal(plaintext, &recipientX25519Pub, &senderX25519Priv)
	if err != nil {
		return nil, nil, err
	}
	return ct, n[:], nil
}

// DecryptMessage decrypts ciphertext from a sender using NaCl box.
// Uses the Solana wallet's Ed25519 key, converted to X25519.
func (w *Wallet) DecryptMessage(ciphertext []byte, nonce [24]byte, senderEd25519PubKey []byte) ([]byte, error) {
	if w.solWallet == nil {
		return nil, fmt.Errorf("solana wallet not configured")
	}

	edPriv := ed25519.PrivateKey(w.solWallet.keypair)
	recipientX25519Priv := ed25519PrivateKeyToX25519(edPriv)

	senderX25519Pub, err := ed25519PublicKeyToX25519(ed25519.PublicKey(senderEd25519PubKey))
	if err != nil {
		return nil, fmt.Errorf("invalid sender public key: %w", err)
	}

	return naclBoxOpen(ciphertext, &nonce, &senderX25519Pub, &recipientX25519Priv)
}

// GetTokenBalance returns the balance of a specific token (ERC20 or SPL).
func (w *Wallet) GetTokenBalance(ctx context.Context, chain Chain, tokenAddress string) (*BalanceResult, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		return w.ethWallet.GetTokenBalance(ctx, tokenAddress)

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return w.solWallet.GetTokenBalance(ctx, tokenAddress)

	default:
		return nil, fmt.Errorf("unsupported chain: %s (supported: ethereum, solana)", chain)
	}
}

// ContractCall calls a smart contract method.
// For read-only calls, no transaction is submitted.
// For write calls, a transaction is signed and broadcast.
func (w *Wallet) ContractCall(ctx context.Context, chain Chain, contractAddress string, method string, args []any, value string, readOnly bool) (*ContractCallResult, error) {
	chain = Chain(strings.ToLower(string(chain)))

	switch chain {
	case ChainEthereum:
		if w.ethWallet == nil {
			return nil, fmt.Errorf("ethereum wallet not configured")
		}
		return w.ethWallet.ContractCall(ctx, contractAddress, method, args, value, readOnly)

	case ChainSolana:
		if w.solWallet == nil {
			return nil, fmt.Errorf("solana wallet not configured")
		}
		return w.solWallet.ContractCall(ctx, contractAddress, method, args, value, readOnly)

	default:
		return nil, fmt.Errorf("unsupported chain: %s", chain)
	}
}
