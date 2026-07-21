package main

import (
	"context"
	"strconv"
	"strings"

	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// sweepDryRunAssets are the wallet tokens the dry-run sweep scan reports on:
// USDC.e (what ConditionalTokens redemptions pay out) and native USDC (what
// fresh deposits typically arrive as). The live pass delegates the equivalent
// list to the polymarket client.
var sweepDryRunAssets = []polymarket.SweepAsset{
	{TokenAddress: polymarket.USDCAddress, Symbol: "USDC.e"},
	{TokenAddress: polymarket.NativeUSDCAddress, Symbol: "USDC"},
}

// sweepPass converts stray wallet USDC.e / native USDC into pUSD via Polymarket's
// public CollateralOnramp (1:1, fee-free) so the funds become spendable CLOB V2
// collateral. The CLOB only checks the maker's pUSD balance, but redemptions of
// closed markets pay out in USDC.e (the ConditionalTokens collateral) and user
// deposits arrive as USDC — without this pass that money is invisible to the
// account snapshot and the run budget. Ordered after redeemPass (so just-redeemed
// winnings are included) and before the snapshot (so the budget sees them).
//
// In dry-run mode it reads and logs the balances it would wrap and submits
// nothing. It never returns an error: a sweep failure must not stop the rest of
// the run — the snapshot then simply sizes against the smaller, un-swept pUSD
// balance.
func (a *App) sweepPass(ctx context.Context, logger *Logger) {
	if a.cfg.DryRun {
		a.sweepDryRun(ctx, logger)
		return
	}

	res, err := a.trading.SweepCollateralToPUSD(ctx)
	if res != nil {
		for _, s := range res.Swept {
			logger.Event("sweep", map[string]any{
				"status": "wrapped",
				"token":  s.TokenAddress,
				"symbol": s.Symbol,
				"amount": s.Amount,
				"tx":     s.TxHash,
			})
		}
		logger.Event("sweep_result", map[string]any{
			"assets_swept":       len(res.Swept),
			"total_usdc_wrapped": res.TotalSwept,
		})
	}
	if err != nil {
		logger.Event("sweep_error", map[string]any{"error": err.Error()})
	}
}

// sweepDryRun reports the wallet balances the live pass would wrap, reading them
// through the wallet helper (the sweep itself needs the trading client's signing
// key, which dry-run must not exercise). Per-asset read errors are logged and
// skipped — the scan is informational only.
func (a *App) sweepDryRun(ctx context.Context, logger *Logger) {
	if a.wallet == nil {
		return
	}
	wouldWrap := 0
	for _, asset := range sweepDryRunAssets {
		bal, err := a.wallet.GetTokenBalance(ctx, gowild_crypto.ChainEthereum, asset.TokenAddress)
		if err != nil {
			logger.Event("sweep_error", map[string]any{
				"token":  asset.TokenAddress,
				"symbol": asset.Symbol,
				"error":  err.Error(),
			})
			continue
		}
		amount, err := strconv.ParseFloat(strings.TrimSpace(bal.Balance), 64)
		if err != nil {
			logger.Event("sweep_error", map[string]any{
				"token":  asset.TokenAddress,
				"symbol": asset.Symbol,
				"error":  "unparseable balance " + bal.Balance,
			})
			continue
		}
		// Mirror the live sweep's one-cent dust threshold.
		if amount < 0.01 {
			continue
		}
		wouldWrap++
		logger.Event("sweep", map[string]any{
			"status": "would_wrap",
			"token":  asset.TokenAddress,
			"symbol": asset.Symbol,
			"amount": amount,
		})
	}
	logger.Event("sweep_result", map[string]any{
		"assets_swept":       wouldWrap,
		"total_usdc_wrapped": 0.0,
		"dry_run":            true,
	})
}
