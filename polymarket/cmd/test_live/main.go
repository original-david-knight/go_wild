package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/original-david-knight/go_wild/crypto"
	gowild_my "github.com/original-david-knight/go_wild/my"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// requireEnv returns the value of the first non-empty env var from names.
// Panics if none are set.
func requireEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	panic(fmt.Sprintf("required env var not set: %v", names))
}

func main() {
	gowild_my.LoadEnv()
	seedPhrase := requireEnv("POLYMARKET_TEST_SEED_PHRASE", "SEED_PHRASE")

	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		log.Fatalf("Failed to derive keys: %v", err)
	}

	privateKey, err := crypto.HexToECDSA(derived.EthPrivateKey[2:])
	if err != nil {
		log.Fatalf("Failed to parse ETH private key: %v", err)
	}

	eoaAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	fmt.Printf("EOA Address: %s\n", eoaAddr.Hex())

	ctx := context.Background()

	// Connect to Polygon RPC
	ethClient, err := ethclient.Dial(polygonRPC)
	if err != nil {
		log.Fatalf("Failed to connect to Polygon: %v", err)
	}
	defer ethClient.Close()

	// Check native USDC balance
	nativeUSDCBalance, err := getERC20Balance(ctx, ethClient, common.HexToAddress(nativeUSDC), eoaAddr)
	if err != nil {
		log.Fatalf("Check native USDC balance: %v", err)
	}
	fmt.Printf("Native USDC balance: %s (%s USDC)\n", nativeUSDCBalance, formatUSDC(nativeUSDCBalance))

	// Check bridged USDC.e balance
	bridgedUSDCBalance, err := getERC20Balance(ctx, ethClient, common.HexToAddress(bridgedUSDC), eoaAddr)
	if err != nil {
		log.Fatalf("Check bridged USDC.e balance: %v", err)
	}
	fmt.Printf("Bridged USDC.e balance: %s (%s USDC)\n", bridgedUSDCBalance, formatUSDC(bridgedUSDCBalance))

	// If we have native USDC but not enough USDC.e, do the swap
	_ = nativeUSDCBalance
	swapAmount := big.NewInt(4_000_000) // 4 USDC — leave 1 USDC as buffer
	if false && bridgedUSDCBalance.Cmp(big.NewInt(100_000)) < 0 && nativeUSDCBalance.Cmp(swapAmount) >= 0 {
		fmt.Println("\n=== Swapping native USDC → USDC.e ===")

		// Step 1: Approve Uniswap router to spend native USDC
		fmt.Printf("Approving Uniswap router to spend %s native USDC...\n", formatUSDC(swapAmount))
		err = approveERC20(ctx, ethClient, privateKey, common.HexToAddress(nativeUSDC), common.HexToAddress(uniswapRouter), swapAmount)
		if err != nil {
			log.Fatalf("Approve failed: %v", err)
		}

		// Step 2: Swap native USDC → USDC.e via Uniswap V3
		// Try 0.01% fee tier first, then 0.05% if it fails
		fmt.Printf("Swapping %s native USDC → USDC.e...\n", formatUSDC(swapAmount))
		err = swapUSDCtoUSDCeWithFee(ctx, ethClient, privateKey, swapAmount, 100)
		if err != nil {
			fmt.Printf("  0.01%% pool failed: %v\n  Trying 0.05%% pool...\n", err)
			err = swapUSDCtoUSDCeWithFee(ctx, ethClient, privateKey, swapAmount, 500)
			if err != nil {
				fmt.Printf("  0.05%% pool failed: %v\n  Trying 0.3%% pool...\n", err)
				err = swapUSDCtoUSDCeWithFee(ctx, ethClient, privateKey, swapAmount, 3000)
				if err != nil {
					log.Fatalf("All swap attempts failed: %v", err)
				}
			}
		}

		// Recheck bridged USDC.e balance
		bridgedUSDCBalance, _ = getERC20Balance(ctx, ethClient, common.HexToAddress(bridgedUSDC), eoaAddr)
		fmt.Printf("New bridged USDC.e balance: %s (%s USDC)\n", bridgedUSDCBalance, formatUSDC(bridgedUSDCBalance))
	}

	// Step 3: Approve all contracts needed for Polymarket trading (skip if already done)
	if false && bridgedUSDCBalance.Cmp(big.NewInt(0)) > 0 {
		maxApproval := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

		fmt.Println("\n=== Setting up Polymarket approvals (missing only) ===")

		// USDC.e → ConditionalTokens (for splitting)
		fmt.Println("1. USDC.e → ConditionalTokens...")
		err = approveERC20(ctx, ethClient, privateKey, common.HexToAddress(bridgedUSDC), common.HexToAddress(conditionalTokens), maxApproval)
		if err != nil {
			log.Fatalf("Approve failed: %v", err)
		}

		// USDC.e → NegRisk Adapter
		fmt.Println("2. USDC.e → NegRisk Adapter...")
		err = approveERC20(ctx, ethClient, privateKey, common.HexToAddress(bridgedUSDC), common.HexToAddress(negRiskAdapter), maxApproval)
		if err != nil {
			log.Fatalf("Approve failed: %v", err)
		}

		// ConditionalTokens setApprovalForAll → CTF Exchange
		fmt.Println("3. CT → CTF Exchange (setApprovalForAll)...")
		err = setApprovalForAll(ctx, ethClient, privateKey, common.HexToAddress(conditionalTokens), common.HexToAddress(ctfExchange))
		if err != nil {
			log.Fatalf("setApprovalForAll failed: %v", err)
		}

		// ConditionalTokens setApprovalForAll → NegRisk Exchange
		fmt.Println("4. CT → NegRisk Exchange (setApprovalForAll)...")
		err = setApprovalForAll(ctx, ethClient, privateKey, common.HexToAddress(conditionalTokens), common.HexToAddress(negRiskExchange))
		if err != nil {
			log.Fatalf("setApprovalForAll failed: %v", err)
		}

		// ConditionalTokens setApprovalForAll → NegRisk Adapter
		fmt.Println("5. CT → NegRisk Adapter (setApprovalForAll)...")
		err = setApprovalForAll(ctx, ethClient, privateKey, common.HexToAddress(conditionalTokens), common.HexToAddress(negRiskAdapter))
		if err != nil {
			log.Fatalf("setApprovalForAll failed: %v", err)
		}
	}

	// Step 4: Create Polymarket client and place order
	var opts []polymarket.Option
	proxyURL := os.Getenv("POLYMARKET_PROXY_URL")
	if proxyURL != "" {
		fmt.Printf("\nUsing proxy: %s\n", proxyURL)
		opts = append(opts, polymarket.WithProxy(proxyURL))
	}

	fmt.Println("\n=== Creating Polymarket client ===")
	client, err := polymarket.NewClient(privateKey, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	fmt.Printf("Client address: %s\n", client.Address())

	// === Test SearchMarkets (events API) ===
	fmt.Println("\n=== SearchMarkets ===")
	markets, err := client.SearchMarkets(ctx, "world cup", 5)
	if err != nil {
		log.Fatalf("SearchMarkets: %v", err)
	}
	fmt.Printf("Found %d markets for 'world cup':\n", len(markets))
	for i, m := range markets {
		fmt.Printf("  %d. %s (conditionID=%s, negRisk=%v, bestAsk=%.3f, vol24h=%.0f)\n",
			i+1, m.Question, m.ConditionID[:12]+"...", m.NegRisk, m.BestAsk, m.Volume24hr)
	}

	// === Test GetPositions ===
	fmt.Println("\n=== GetPositions ===")
	positions, err := client.GetPositions(ctx)
	if err != nil {
		fmt.Printf("GetPositions error: %v\n", err)
	} else {
		fmt.Printf("Found %d positions:\n", len(positions))
		for i, p := range positions {
			fmt.Printf("  %d. %s [%s] size=%.2f avgPrice=%.4f curPrice=%.4f pnl=%.4f\n",
				i+1, p.Title, p.Outcome, p.Size, p.AvgPrice, p.CurPrice, p.CashPnl)
		}
	}

	// === Test GetTrades ===
	fmt.Println("\n=== GetTrades ===")
	trades, err := client.GetTrades(ctx, 10)
	if err != nil {
		fmt.Printf("GetTrades error: %v\n", err)
	} else {
		fmt.Printf("Found %d trades:\n", len(trades))
		for i, t := range trades {
			fmt.Printf("  %d. %s [%s %s] size=%.2f price=%.4f tx=%s\n",
				i+1, t.Title, t.Side, t.Outcome, t.Size, t.Price, t.TransactionHash[:12]+"...")
		}
	}

	// === Test GetOrderBook + order placement ===
	// "Will Italy win the 2026 FIFA World Cup?" — Yes side
	tokenID := "71902280236980528007966111072910269163651886024599423678358797794246690742124"
	fmt.Println("\n=== GetOrderBook ===")
	fmt.Println("Target: Will Italy win the 2026 FIFA World Cup? (Yes)")

	book, err := client.GetOrderBook(ctx, tokenID)
	if err != nil {
		log.Fatalf("GetOrderBook: %v", err)
	}
	if len(book.Asks) == 0 {
		log.Fatal("No asks in order book")
	}

	// CLOB API returns asks in descending order — best ask is the last element
	bestAskEntry := book.Asks[len(book.Asks)-1]
	var askPrice float64
	fmt.Sscanf(bestAskEntry.Price, "%f", &askPrice)
	var askSize float64
	fmt.Sscanf(bestAskEntry.Size, "%f", &askSize)
	fmt.Printf("Best ask: $%.3f (size=%.0f)\n", askPrice, askSize)

	fmt.Println("\nDone!")
}

func getERC20Balance(ctx context.Context, client *ethclient.Client, token, owner common.Address) (*big.Int, error) {
	selector := crypto.Keccak256([]byte("balanceOf(address)"))[:4]
	data := append(selector, common.LeftPadBytes(owner.Bytes(), 32)...)

	to := token
	result, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &to,
		Data: data,
	}, nil)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(result), nil
}

func formatUSDC(amount *big.Int) string {
	f := new(big.Float).SetInt(amount)
	f.Quo(f, new(big.Float).SetInt64(1_000_000))
	return f.Text('f', 2)
}
