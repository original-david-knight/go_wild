//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/original-david-knight/go_wild/crypto"
	gowild_my "github.com/original-david-knight/go_wild/my"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func main() {
	gowild_my.LoadEnv()
	seedPhrase := os.Getenv("POLYMARKET_TEST_SEED_PHRASE")
	if seedPhrase == "" {
		seedPhrase = os.Getenv("SEED_PHRASE")
	}
	if seedPhrase == "" {
		log.Fatal("required env var not set: POLYMARKET_TEST_SEED_PHRASE or SEED_PHRASE")
	}

	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		log.Fatalf("derive keys: %v", err)
	}
	privateKey, err := crypto.HexToECDSA(derived.EthPrivateKey[2:])
	if err != nil {
		log.Fatalf("parse key: %v", err)
	}

	var opts []polymarket.Option
	if p := os.Getenv("POLYMARKET_PROXY_URL"); p != "" {
		opts = append(opts, polymarket.WithProxy(p))
	}

	client, err := polymarket.NewClient(privateKey, opts...)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}
	fmt.Printf("Address: %s\n", client.Address())

	ctx := context.Background()

	fmt.Println("\n=== Testing GetOrders (full pagination) ===")
	orders, err := client.GetOrders(ctx, "")
	if err != nil {
		log.Fatalf("GetOrders FAILED: %v", err)
	}
	fmt.Printf("GetOrders SUCCESS: %d orders returned\n", len(orders))
	for i, o := range orders {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(orders)-5)
			break
		}
		fmt.Printf("  [%d] id=%s status=%s side=%s\n", i, o.ID, o.Status, o.Side)
	}
}
