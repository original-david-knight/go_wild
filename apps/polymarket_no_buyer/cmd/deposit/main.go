// Command deposit converts USDC.e into pUSD (Polymarket CLOB V2 collateral) for the
// no_buyer trading wallet, by calling Polymarket's public CollateralOnramp.
//
// The on-ramp (0x93070a847efef7f70739046a929d47a521f5b8ee) exposes a public,
// fee-free, 1:1 wrap(asset, to, amount): it pulls `amount` USDC.e from the caller
// (requires an ERC-20 approval) and mints the same `amount` of pUSD to `to`.
//
// Verified on-chain: the CLOB V2 exchange settles in pUSD
// (0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB), NOT USDC.e — so the trading wallet
// must hold pUSD. See apps/polymarket_no_buyer/client.go and polymarket/config.go.
//
// Usage (dry run is the default — nothing is broadcast without -execute):
//
//	go run ./cmd/deposit -amount 1.00              # show the plan only
//	go run ./cmd/deposit -amount 1.00 -execute     # actually approve + wrap
package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// collateralOnrampAddress is Polymarket's public USDC->pUSD on-ramp on Polygon.
// Its wrap(address,address,uint256) is gated only by onlyUnpaused (no role), so any
// caller can deposit. Confirmed from its Sourcify-verified CollateralOnramp source.
const collateralOnrampAddress = "0x93070a847efef7f70739046a929d47a521f5b8ee"

const seedEnvVar = "NO_BUYER_WALLET_SEED_PHRASE"

const erc20ABI = `[
 {"name":"approve","type":"function","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"type":"bool"}]},
 {"name":"allowance","type":"function","stateMutability":"view","inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"outputs":[{"type":"uint256"}]},
 {"name":"balanceOf","type":"function","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"type":"uint256"}]}
]`

const onrampABI = `[
 {"name":"wrap","type":"function","stateMutability":"nonpayable","inputs":[{"name":"_asset","type":"address"},{"name":"_to","type":"address"},{"name":"_amount","type":"uint256"}],"outputs":[]}
]`

func main() {
	amountStr := flag.String("amount", "", "USDC.e amount to convert to pUSD: a decimal (e.g. 1.00) or \"all\" for the full balance — required")
	rpcURL := flag.String("rpc", polymarket.PolygonRPCURL, "Polygon RPC endpoint")
	toStr := flag.String("to", "", "recipient of the minted pUSD (default: the trading EOA, which is the order maker)")
	execute := flag.Bool("execute", false, "broadcast the approve+wrap transactions (default: dry run only)")
	flag.Parse()

	amountInput := strings.TrimSpace(*amountStr)
	if amountInput == "" {
		log.Fatal(`-amount is required (e.g. -amount 1.00, or -amount all)`)
	}
	amountAll := strings.EqualFold(amountInput, "all") || strings.EqualFold(amountInput, "max")
	var amountRaw *big.Int
	if !amountAll {
		var err error
		amountRaw, err = parseUSDC(amountInput)
		if err != nil {
			log.Fatalf("invalid -amount: %v", err)
		}
		if amountRaw.Sign() <= 0 {
			log.Fatal("amount must be positive")
		}
	}

	privateKey, eoa := loadKey()
	to := eoa
	if strings.TrimSpace(*toStr) != "" {
		to = common.HexToAddress(*toStr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := ethclient.DialContext(ctx, *rpcURL)
	if err != nil {
		log.Fatalf("dial %s: %v", *rpcURL, err)
	}
	defer client.Close()

	usdce := common.HexToAddress(polymarket.USDCAddress)
	pusd := common.HexToAddress(polymarket.PUSDAddress)
	onramp := common.HexToAddress(collateralOnrampAddress)
	ercABI := mustABI(erc20ABI)
	wrapABI := mustABI(onrampABI)

	usdceBal := readBalance(ctx, client, ercABI, usdce, eoa)
	pusdBefore := readBalance(ctx, client, ercABI, pusd, to)
	gas, err := client.BalanceAt(ctx, eoa, nil)
	if err != nil {
		log.Fatalf("read gas balance: %v", err)
	}

	// "all"/"max" deposits the full on-chain balance using its exact raw value
	// (no float rounding), leaving no USDC.e dust behind.
	if amountAll {
		amountRaw = new(big.Int).Set(usdceBal)
		if amountRaw.Sign() <= 0 {
			log.Fatal("no USDC.e balance to deposit")
		}
	}

	fmt.Println("=== USDC.e -> pUSD deposit ===")
	fmt.Printf("  trading EOA (maker): %s\n", eoa.Hex())
	fmt.Printf("  recipient (to):      %s\n", to.Hex())
	fmt.Printf("  amount:              %s USDC.e -> %s pUSD (raw %s)\n", fmtUSDC(amountRaw), fmtUSDC(amountRaw), amountRaw)
	fmt.Printf("  USDC.e balance:      %s\n", fmtUSDC(usdceBal))
	fmt.Printf("  pUSD balance (to):   %s\n", fmtUSDC(pusdBefore))
	fmt.Printf("  gas (POL):           %s\n", weiToEth(gas))

	if usdceBal.Cmp(amountRaw) < 0 {
		log.Fatalf("insufficient USDC.e: have %s, need %s", fmtUSDC(usdceBal), fmtUSDC(amountRaw))
	}
	if gas.Sign() == 0 {
		log.Fatal("wallet has 0 POL for gas")
	}

	if !*execute {
		fmt.Println("\nDRY RUN — no transactions sent. Re-run with -execute to broadcast:")
		fmt.Printf("  1. approve %s USDC.e to on-ramp %s\n", fmtUSDC(amountRaw), onramp.Hex())
		fmt.Printf("  2. onramp.wrap(USDC.e, %s, %s)\n", to.Hex(), amountRaw)
		return
	}

	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatalf("chain id: %v", err)
	}
	signer := types.NewEIP155Signer(chainID)
	nonce, err := client.PendingNonceAt(ctx, eoa)
	if err != nil {
		log.Fatalf("nonce: %v", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		log.Fatalf("gas price: %v", err)
	}

	// 1. Approve the exact deposit amount (minimal allowance) if needed.
	if allowance := readAllowance(ctx, client, ercABI, usdce, eoa, onramp); allowance.Cmp(amountRaw) < 0 {
		approveData, _ := ercABI.Pack("approve", onramp, amountRaw)
		fmt.Printf("\n[1/2] approving %s USDC.e to on-ramp...\n", fmtUSDC(amountRaw))
		sendAndWait(ctx, client, signer, privateKey, eoa, usdce, approveData, nonce, gasPrice)
		nonce++
	} else {
		fmt.Printf("\n[1/2] allowance already sufficient (%s USDC.e), skipping approve\n", fmtUSDC(allowance))
	}

	// 2. Wrap: pulls USDC.e, mints pUSD 1:1 to `to`.
	wrapData, _ := wrapABI.Pack("wrap", usdce, to, amountRaw)
	fmt.Printf("[2/2] wrapping USDC.e -> pUSD to %s...\n", to.Hex())
	sendAndWait(ctx, client, signer, privateKey, eoa, onramp, wrapData, nonce, gasPrice)

	// Confirm the minted pUSD landed. The wrap mints exactly amountRaw, so poll until
	// the balance reflects it — a single read can hit a load-balanced RPC node that is
	// a block behind and misleadingly report no change on a successful deposit.
	want := new(big.Int).Add(pusdBefore, amountRaw)
	pusdAfter := pollBalanceAtLeast(ctx, client, ercABI, pusd, to, want, 20*time.Second)
	fmt.Printf("\nDone. pUSD balance: %s -> %s (+%s)\n",
		fmtUSDC(pusdBefore), fmtUSDC(pusdAfter), fmtUSDC(new(big.Int).Sub(pusdAfter, pusdBefore)))
	if pusdAfter.Cmp(want) < 0 {
		fmt.Printf("note: RPC still reports a lagging balance; the wrap tx confirmed, so the pUSD is credited. Re-check with cmd/walletcheck if unsure.\n")
	}
}

func sendAndWait(ctx context.Context, client *ethclient.Client, signer types.Signer, key *ecdsa.PrivateKey, from, to common.Address, data []byte, nonce uint64, gasPrice *big.Int) {
	gasLimit, err := client.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, Data: data})
	if err != nil {
		gasLimit = 150000
	}
	signed, err := types.SignTx(types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data), signer, key)
	if err != nil {
		log.Fatalf("sign tx: %v", err)
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		log.Fatalf("send tx: %v", err)
	}
	fmt.Printf("      tx %s — waiting for receipt...\n", signed.Hash().Hex())
	rcpt, err := waitReceipt(ctx, client, signed.Hash())
	if err != nil {
		log.Fatalf("receipt: %v", err)
	}
	if rcpt.Status != types.ReceiptStatusSuccessful {
		log.Fatalf("tx %s reverted", signed.Hash().Hex())
	}
	fmt.Printf("      confirmed in block %d\n", rcpt.BlockNumber.Uint64())
}

func waitReceipt(ctx context.Context, client *ethclient.Client, hash common.Hash) (*types.Receipt, error) {
	for {
		if rcpt, err := client.TransactionReceipt(ctx, hash); err == nil {
			return rcpt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func readBalance(ctx context.Context, client *ethclient.Client, ercABI abi.ABI, token, owner common.Address) *big.Int {
	data, _ := ercABI.Pack("balanceOf", owner)
	raw, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		log.Fatalf("balanceOf %s: %v", token.Hex(), err)
	}
	return new(big.Int).SetBytes(raw)
}

// pollBalanceAtLeast reads the token balance repeatedly until it reaches `want` or
// the timeout elapses, then returns the last value read. Tolerates a load-balanced
// RPC briefly serving a stale (block-behind) view right after a confirmed tx.
func pollBalanceAtLeast(ctx context.Context, client *ethclient.Client, ercABI abi.ABI, token, owner common.Address, want *big.Int, timeout time.Duration) *big.Int {
	deadline := time.Now().Add(timeout)
	for {
		bal := readBalance(ctx, client, ercABI, token, owner)
		if bal.Cmp(want) >= 0 || time.Now().After(deadline) {
			return bal
		}
		select {
		case <-ctx.Done():
			return bal
		case <-time.After(2 * time.Second):
		}
	}
}

func readAllowance(ctx context.Context, client *ethclient.Client, ercABI abi.ABI, token, owner, spender common.Address) *big.Int {
	data, _ := ercABI.Pack("allowance", owner, spender)
	raw, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, nil)
	if err != nil {
		log.Fatalf("allowance: %v", err)
	}
	return new(big.Int).SetBytes(raw)
}

// loadKey derives the trading wallet key from the seed phrase (env or .env file),
// matching apps/polymarket_no_buyer/client.go (account index 0).
func loadKey() (*ecdsa.PrivateKey, common.Address) {
	seed := strings.TrimSpace(os.Getenv(seedEnvVar))
	if seed == "" {
		seed = seedFromDotEnv(".env")
	}
	if seed == "" {
		log.Fatalf("%s not set (env or ./.env)", seedEnvVar)
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seed, 0)
	if err != nil {
		log.Fatalf("derive keys: %v", err)
	}
	key, err := ethcrypto.HexToECDSA(strings.TrimPrefix(derived.EthPrivateKey, "0x"))
	if err != nil {
		log.Fatalf("parse derived key: %v", err)
	}
	return key, ethcrypto.PubkeyToAddress(key.PublicKey)
}

func seedFromDotEnv(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, seedEnvVar+"="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func parseUSDC(s string) (*big.Int, error) {
	f, ok := new(big.Float).SetString(strings.TrimSpace(s))
	if !ok {
		return nil, fmt.Errorf("not a number: %q", s)
	}
	out, _ := new(big.Float).Mul(f, big.NewFloat(1e6)).Int(nil)
	return out, nil
}

func fmtUSDC(v *big.Int) string {
	return new(big.Float).Quo(new(big.Float).SetInt(v), big.NewFloat(1e6)).Text('f', 6)
}

func weiToEth(v *big.Int) string {
	return new(big.Float).Quo(new(big.Float).SetInt(v), big.NewFloat(1e18)).Text('f', 6)
}

func mustABI(s string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(s))
	if err != nil {
		log.Fatalf("parse ABI: %v", err)
	}
	return a
}
