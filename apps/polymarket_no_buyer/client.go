package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Environment variables for wallet/credentials/proxy/chain. These reuse the
// existing repo configuration paths rather than introducing new ones.
const (
	// envWalletSeedPhrase is intentionally a dedicated, app-specific name (NOT the
	// shared WALLET_SEED_PHRASE) so this app never silently trades with the repo's
	// shared wallet. There is no fallback: it must be set explicitly.
	envWalletSeedPhrase = "NO_BUYER_WALLET_SEED_PHRASE"
	envProxyURL         = "POLYMARKET_PROXY_URL"
	envOnchainRPCURL    = "POLYMARKET_RPC_URL"
	envWalletETHRPCURL  = "WALLET_ETH_RPC_URL"
	envFunderAddress    = "POLYMARKET_FUNDER_ADDRESS"
	envSignatureType    = "POLYMARKET_SIGNATURE_TYPE"
)

// runtimeClients bundles the constructed Polymarket client and the wallet helper
// used for the Polygon USDC cash balance. Both are built from existing repo
// helpers; no new client library is introduced.
type runtimeClients struct {
	polymarket *polymarket.Client
	wallet     *gowild_crypto.Wallet
	address    string
}

// buildClients constructs the Polymarket CLOB client and the wallet helper from
// the repo's existing configuration env vars. It fails loudly if a required
// credential is missing rather than substituting a default.
func buildClients(cfg *Config, getenv func(string) string) (*runtimeClients, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	seedPhrase := strings.TrimSpace(getenv(envWalletSeedPhrase))
	if seedPhrase == "" {
		return nil, fmt.Errorf("%s is required (no hardcoded fallback)", envWalletSeedPhrase)
	}

	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive wallet keys from %s: %w", envWalletSeedPhrase, err)
	}

	ethHex := strings.TrimPrefix(derived.EthPrivateKey, "0x")
	privateKey, err := ethcrypto.HexToECDSA(ethHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse derived ETH private key: %w", err)
	}

	rpcURL := strings.TrimSpace(getenv(envOnchainRPCURL))
	if rpcURL == "" {
		rpcURL = strings.TrimSpace(getenv(envWalletETHRPCURL))
	}
	// Polymarket lives entirely on Polygon. The wallet helper otherwise defaults to
	// an Ethereum mainnet RPC, which would read the USDC balance on the wrong chain
	// and report $0. Default the RPC to Polygon so both the on-chain client and the
	// wallet balance read hit Polygon.
	if rpcURL == "" {
		rpcURL = polymarket.PolygonRPCURL
	}

	// All Polymarket access (CLOB, Gamma market data, and the Data API positions)
	// must egress through the VPN/SOCKS proxy — Polymarket is geo-restricted. The
	// proxy is required: rather than silently hit Polymarket directly, fail loudly
	// if it is not configured.
	proxyURL := strings.TrimSpace(getenv(envProxyURL))
	if proxyURL == "" {
		return nil, fmt.Errorf("%s is required: all Polymarket access must route through the VPN/SOCKS proxy", envProxyURL)
	}

	var opts []polymarket.Option
	opts = append(opts, polymarket.WithFullProxy(proxyURL))
	opts = append(opts, polymarket.WithOnchainRPC(rpcURL))

	var client *polymarket.Client
	if funder := strings.TrimSpace(getenv(envFunderAddress)); funder != "" {
		sigType, err := parseSignatureType(getenv(envSignatureType))
		if err != nil {
			return nil, err
		}
		client, err = polymarket.NewClientWithFunder(privateKey, funder, sigType, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create polymarket client: %w", err)
		}
	} else {
		client, err = polymarket.NewClient(privateKey, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create polymarket client: %w", err)
		}
	}

	wallet, err := gowild_crypto.NewWallet(gowild_crypto.WalletConfig{
		EthPrivateKey: derived.EthPrivateKey,
		EthRPCURL:     rpcURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet helper: %w", err)
	}

	return &runtimeClients{
		polymarket: client,
		wallet:     wallet,
		address:    client.Address(),
	}, nil
}

func parseSignatureType(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return polymarket.SigTypeEOA, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", envSignatureType, err)
	}
	if v < polymarket.SigTypeEOA || v > polymarket.SigTypePolyGnosisSafe {
		return 0, fmt.Errorf("%s out of range: %d", envSignatureType, v)
	}
	return v, nil
}
