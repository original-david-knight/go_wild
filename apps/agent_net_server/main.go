package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/agent_net/server"
	"github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/my"
)

func main() {
	// Load .env file
	gowild_my.LoadEnv()

	// Parse flags
	addr := flag.String("addr", ":8080", "Server address")
	dbURL := flag.String("db", "", "PostgreSQL connection URL (postgres://user:pass@host:port/dbname)")
	solanaRPC := flag.String("solana-rpc", "", "Solana RPC URL for premium verification")
	treasury := flag.String("treasury", "", "Treasury address for Solana payments")
	a2aCallbackKey := flag.String("a2a-callback-key", "", "Optional A2A callback signing key (base64url/base64/hex seed or private key)")
	flag.Parse()

	// Get database URL from flag or environment
	connStr := *dbURL
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" {
		log.Fatal("Database URL required: use -db flag or DATABASE_URL environment variable")
	}

	// Create PostgreSQL database connection
	db, err := gowild_data.NewPostgresDatabase(connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Register tables (auto-creates if not exist)
	if err := gowild_data.AddAllTables(db); err != nil {
		log.Fatalf("Failed to register database tables: %v", err)
	}

	// Configure server
	config := server.DefaultConfig()

	// Address: flag > PORT env (Render) > default
	if *addr != ":8080" {
		config.Address = *addr
	} else if port := os.Getenv("PORT"); port != "" {
		config.Address = ":" + port
	} else {
		config.Address = *addr
	}

	// Solana RPC URL: flag > env
	if *solanaRPC != "" {
		config.SolanaRPCURL = *solanaRPC
	} else if rpc := os.Getenv("SOLANA_RPC_URL"); rpc != "" {
		config.SolanaRPCURL = rpc
	}

	// Treasury address: flag > env
	if *treasury != "" {
		config.Treasury.Solana = *treasury
	} else if addr := os.Getenv("SOLANA_TREASURY"); addr != "" {
		config.Treasury.Solana = addr
	}

	// PoW difficulty: env (default from constant)
	if diffStr := os.Getenv("POW_DIFFICULTY"); diffStr != "" {
		if diff, err := strconv.Atoi(diffStr); err == nil && diff >= 0 && diff <= 32 {
			config.BaseDifficulty = diff
		}
	}

	// Upgrade fee: env (overrides default)
	if fee := os.Getenv("UPGRADE_FEE_SOL"); fee != "" {
		gowild_agent_net.UpgradeAmounts[gowild_agent_net.ChainSolana] = fee
	}

	// A2A callback signing key: flag > env
	if *a2aCallbackKey != "" {
		config.A2ACallbackSigningKey = *a2aCallbackKey
	} else if key := os.Getenv("A2A_CALLBACK_SIGNING_KEY"); key != "" {
		config.A2ACallbackSigningKey = key
	}

	// Create server
	srv := server.NewServer(db, config)

	// Handle shutdown gracefully
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	// Start server
	log.Printf("Starting agent network server on %s", config.Address)
	log.Printf("PoW difficulty: %d bits", config.BaseDifficulty)
	log.Printf("Database: PostgreSQL")
	if config.SolanaRPCURL != "" {
		log.Printf("Solana RPC: %s", config.SolanaRPCURL)
	}
	if config.Treasury.Solana != "" {
		log.Printf("Solana Treasury: %s", config.Treasury.Solana)
		log.Printf("Upgrade Fee: %s SOL", gowild_agent_net.UpgradeAmounts[gowild_agent_net.ChainSolana])
	} else {
		log.Printf("Warning: No Solana treasury configured - premium upgrades disabled")
	}
	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
