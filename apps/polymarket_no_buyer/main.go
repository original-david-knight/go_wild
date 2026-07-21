package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Load .env files from the app directory up to the repo root before reading
	// config. The app's own .env (wallet seed, overrides) takes precedence; the
	// repo-root .env supplies shared settings such as the Polymarket VPN proxy.
	for _, path := range loadEnvFiles() {
		fmt.Fprintf(os.Stderr, "polymarket_no_buyer: loaded env from %s\n", path)
	}
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout))
}

// realMain runs the app entry point with injectable args/env/output so it can be
// exercised headlessly. It loads config, constructs the Polymarket client and
// wallet helper, then dispatches to one-shot or scheduled mode. The integer
// return is the process exit code: 0 success, 1 runtime/credential failure,
// 2 usage/config error.
func realMain(args []string, getenv func(string) string, out io.Writer) int {
	cfg, err := LoadConfig(args, getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "polymarket_no_buyer: %v\n", err)
		return 2
	}

	clients, err := buildClients(cfg, getenv)
	if err != nil {
		NewLogger(out, newRunID()).Event("init_error", map[string]any{"error": err.Error()})
		fmt.Fprintf(os.Stderr, "polymarket_no_buyer: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := NewApp(cfg, clients)
	if err := app.Run(ctx, out); err != nil {
		fmt.Fprintf(os.Stderr, "polymarket_no_buyer: %v\n", err)
		return 1
	}
	return 0
}
