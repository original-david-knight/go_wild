package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// emptyEnv is a getenv that reports every variable as unset.
func emptyEnv(string) string { return "" }

// mapEnv returns a getenv backed by the given map.
func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestPolymarketNoBuyer_RunInit(t *testing.T) {
	t.Run("defaults match PRD", func(t *testing.T) {
		cfg, err := LoadConfig(nil, emptyEnv)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Mode != ModeOnce {
			t.Errorf("mode = %q, want once", cfg.Mode)
		}
		if cfg.Interval != 6*time.Hour {
			t.Errorf("interval = %s, want 6h", cfg.Interval)
		}
		if cfg.DryRun {
			t.Errorf("dry_run = true, want false")
		}
		if cfg.MinNoMidpoint != 0.89 || cfg.MaxNoMidpoint != 0.99 {
			t.Errorf("midpoint bounds = (%v,%v), want (0.89,0.99)", cfg.MinNoMidpoint, cfg.MaxNoMidpoint)
		}
		if cfg.TargetExposurePct != 0.01 {
			t.Errorf("target_exposure_pct = %v, want 0.01", cfg.TargetExposurePct)
		}
		if cfg.MinLiquidityUSD != 5000 {
			t.Errorf("min_liquidity_usd = %v, want 5000", cfg.MinLiquidityUSD)
		}
		if cfg.MinHoursToClose != 48*time.Hour || cfg.MaxHoursToClose != 336*time.Hour {
			t.Errorf("close window = (%s,%s), want (48h,336h)", cfg.MinHoursToClose, cfg.MaxHoursToClose)
		}
		if cfg.OrderExpiryBeforeClose != 24*time.Hour {
			t.Errorf("order_expiry_before_close = %s, want 24h", cfg.OrderExpiryBeforeClose)
		}
		// The spendable-budget balance must be read against the CLOB V2 collateral
		// (pUSD), the token orders actually settle in — not USDC.e.
		if cfg.USDCTokenAddress != polymarket.PUSDAddress {
			t.Errorf("usdc_token_address = %q, want %q", cfg.USDCTokenAddress, polymarket.PUSDAddress)
		}
		if cfg.MinOrderSizeFallbackSet {
			t.Errorf("min_order_size_fallback should be unset by default")
		}
	})

	t.Run("env overrides defaults", func(t *testing.T) {
		env := mapEnv(map[string]string{
			envMinNoMidpoint:     "0.90",
			envMaxNoMidpoint:     "0.98",
			envTargetExposurePct: "0.05",
			envMinLiquidityUSD:   "1234",
			envInterval:          "2h",
			envMode:              "schedule",
			envDryRun:            "true",
			envMinHoursToClose:   "50h",
			envMaxHoursToClose:   "300h",
			envOrderExpiryBefore: "12h",
			envUSDCTokenAddress:  "0xABC",
		})
		cfg, err := LoadConfig(nil, env)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.MinNoMidpoint != 0.90 {
			t.Errorf("min_no_midpoint = %v, want 0.90", cfg.MinNoMidpoint)
		}
		if cfg.Mode != ModeSchedule {
			t.Errorf("mode = %q, want schedule", cfg.Mode)
		}
		if !cfg.DryRun {
			t.Errorf("dry_run = false, want true")
		}
		if cfg.Interval != 2*time.Hour {
			t.Errorf("interval = %s, want 2h", cfg.Interval)
		}
		if cfg.MinLiquidityUSD != 1234 {
			t.Errorf("min_liquidity_usd = %v, want 1234", cfg.MinLiquidityUSD)
		}
		if cfg.USDCTokenAddress != "0xABC" {
			t.Errorf("usdc_token_address = %q, want 0xABC", cfg.USDCTokenAddress)
		}
	})

	t.Run("flags override env", func(t *testing.T) {
		env := mapEnv(map[string]string{
			envDryRun:          "false",
			envMinLiquidityUSD: "5000",
			envInterval:        "6h",
		})
		cfg, err := LoadConfig([]string{"--dry-run", "--min-liquidity", "7500", "--interval", "12h"}, env)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if !cfg.DryRun {
			t.Errorf("dry_run = false, want true (flag override)")
		}
		if cfg.MinLiquidityUSD != 7500 {
			t.Errorf("min_liquidity_usd = %v, want 7500 (flag override)", cfg.MinLiquidityUSD)
		}
		if humanDuration(cfg.Interval) != "12h" {
			t.Errorf("interval = %s, want 12h (flag override)", humanDuration(cfg.Interval))
		}
	})

	t.Run("contradictory once+schedule fails loudly", func(t *testing.T) {
		_, err := LoadConfig([]string{"--once", "--schedule"}, emptyEnv)
		if err == nil {
			t.Fatalf("expected error for --once --schedule, got nil")
		}
	})

	t.Run("distinct run ids", func(t *testing.T) {
		a, b := newRunID(), newRunID()
		if a == "" || b == "" {
			t.Fatalf("empty run id: %q %q", a, b)
		}
		if a == b {
			t.Fatalf("expected distinct run ids, got %q twice", a)
		}
	})

	t.Run("run-init log shape", func(t *testing.T) {
		cfg, err := LoadConfig([]string{"--dry-run", "--min-liquidity", "7500", "--interval", "12h"}, emptyEnv)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		var buf bytes.Buffer
		logger := NewLogger(&buf, "run_test123")
		logger.Event("run_init", runInitFields(cfg))

		var obj map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &obj); err != nil {
			t.Fatalf("run-init line is not valid JSON: %v\n%s", err, buf.String())
		}
		if obj["run_id"] != "run_test123" {
			t.Errorf("run_id = %v, want run_test123", obj["run_id"])
		}
		if obj["event"] != "run_init" {
			t.Errorf("event = %v, want run_init", obj["event"])
		}
		if obj["mode"] != "once" {
			t.Errorf("mode = %v, want once", obj["mode"])
		}
		if obj["dry_run"] != true {
			t.Errorf("dry_run = %v, want true", obj["dry_run"])
		}
		if obj["interval"] != "12h" {
			t.Errorf("interval = %v, want 12h", obj["interval"])
		}
		if obj["min_liquidity_usd"].(float64) != 7500 {
			t.Errorf("min_liquidity_usd = %v, want 7500", obj["min_liquidity_usd"])
		}
		for _, k := range []string{"min_no_midpoint", "max_no_midpoint", "target_exposure_pct", "min_hours_to_close", "max_hours_to_close", "order_expiry_before_close", "usdc_token_address"} {
			if _, ok := obj[k]; !ok {
				t.Errorf("run-init log missing key %q", k)
			}
		}
	})

	t.Run("missing seed phrase fails loudly", func(t *testing.T) {
		cfg, err := LoadConfig(nil, emptyEnv)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if _, err := buildClients(cfg, emptyEnv); err == nil {
			t.Fatalf("expected error when NO_BUYER_WALLET_SEED_PHRASE unset, got nil")
		}
	})
}
