package main

import (
	"context"
	"io"
	"time"

	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// tradingClient is the union of Polymarket client operations the run pipeline
// uses. It is an interface so the passes can be exercised with a fake in tests;
// *polymarket.Client satisfies it. Later rungs extend this with the order-book,
// order, and placement methods they need.
type tradingClient interface {
	GetPositions(ctx context.Context) ([]polymarket.Position, error)
	RedeemWinnings(ctx context.Context, conditionID string, indexSets []int, collateralTokenAddress string, includeLosing bool) (*polymarket.RedeemWinningsResult, error)
	SweepCollateralToPUSD(ctx context.Context) (*polymarket.CollateralSweepResult, error)
	ListMarketsClosingBetween(ctx context.Context, minClose, maxClose time.Time, minLiquidity float64, limit, offset int) ([]polymarket.Market, error)
	ListMarketsClosingBetweenKeyset(ctx context.Context, minClose, maxClose time.Time, minLiquidity float64, limit int, afterCursor string) (polymarket.MarketPage, error)
	GetOrderBookDetailed(ctx context.Context, tokenID string) (*polymarket.OrderBookDetail, error)
	GetClobMarket(ctx context.Context, conditionID string) (*polymarket.ClobMarket, error)
	ListMarkets(ctx context.Context, limit, offset int) ([]polymarket.Market, error)
	GetOrders(ctx context.Context, market string) ([]polymarket.Order, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetMarket(ctx context.Context, conditionID string) (*polymarket.Market, error)
	PlaceOrderWithExpiration(ctx context.Context, tokenID string, price, size float64, side string, expirationUnix int64) (*polymarket.PlaceOrderResponse, error)
}

// walletClient is the subset of the Polygon wallet helper the account-value
// snapshot uses. It is an interface so the snapshot can be exercised with a fake
// in tests; *gowild_crypto.Wallet satisfies it. The wallet is the SOLE source of
// the spendable USDC cash balance — the snapshot never reads any
// Polymarket/CLOB exchange available-balance field.
type walletClient interface {
	GetTokenBalance(ctx context.Context, chain gowild_crypto.Chain, tokenAddress string) (*gowild_crypto.BalanceResult, error)
}

// App holds the resolved configuration and constructed clients for a process.
// A single App may execute many runs (one per scheduled tick); each run gets a
// fresh run ID and logger so its events are independently attributable.
type App struct {
	cfg     *Config
	clients *runtimeClients
	trading tradingClient
	wallet  walletClient

	// human selects readable text output (set by the binary). Tests leave it
	// false so their in-memory loggers emit parseable JSON.
	human bool

	// newRunID and now are injectable for deterministic testing.
	newRunID func() string
	now      func() time.Time
}

// NewApp constructs an App with production run-ID and clock sources. The binary
// renders human-readable output; verbosity comes from the resolved config.
func NewApp(cfg *Config, clients *runtimeClients) *App {
	return &App{
		cfg:      cfg,
		clients:  clients,
		trading:  clients.polymarket,
		wallet:   clients.wallet,
		human:    true,
		newRunID: newRunID,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// makeLogger builds the per-run logger: human-readable text for the binary,
// structured JSON otherwise (tests).
func (a *App) makeLogger(out io.Writer) *Logger {
	if a.human {
		return NewHumanLogger(out, a.newRunID(), a.cfg.Verbose)
	}
	return NewLogger(out, a.newRunID())
}

// Run dispatches to one-shot or scheduled execution based on the resolved mode.
// It returns nil on clean completion or graceful signal-driven shutdown.
func (a *App) Run(ctx context.Context, out io.Writer) error {
	if a.cfg.Mode == ModeSchedule {
		return a.runSchedule(ctx, out)
	}
	a.runTick(ctx, out)
	return nil
}

// runTick assigns a fresh run ID, builds a logger, and executes one pipeline run.
// Per-run failures are logged inside runOnce and never abort the surrounding loop.
func (a *App) runTick(ctx context.Context, out io.Writer) {
	a.runOnce(ctx, a.makeLogger(out))
}

// runOnce executes a single pipeline run. At this rung it emits the structured
// run-start and run-done events; later rungs hook the redeem, stale-cancel,
// account-value snapshot, discovery, and reconciliation passes in between.
func (a *App) runOnce(ctx context.Context, logger *Logger) {
	logger.Event("run_start", a.cfg.fields())

	// The trading pipeline only runs when a client is configured. Run-loop tests
	// exercise scheduling with no client and skip straight to run_done.
	if a.trading != nil {
		// 1. Redeem closed/resolved positions before any other trading logic.
		a.redeemPass(ctx, logger)

		// 2. Sweep stray wallet USDC.e / native USDC into pUSD (the only CLOB V2
		// collateral) so just-redeemed winnings and fresh deposits are spendable.
		// Ordered after redemption (which pays out USDC.e) and before the snapshot
		// (which reads the pUSD balance the budget is seeded from).
		a.sweepPass(ctx, logger)

		// 3. Cancel clearly stale open orders before account-value sizing, so the
		// later reconciliation pass sees a cleaned open-order set. Ordered after
		// redemption and before any snapshot/discover/reconcile pass.
		a.stalePass(ctx, logger)

		// 4. Compute the account-value snapshot after redemption and stale-cancel
		// ordering (cancellations do not change the wallet USDC cash balance). The
		// snapshot needs both the wallet helper and the trading client; an
		// uncomputable wallet balance or positions fetch aborts the run. The snapshot
		// is captured: the reconciliation pass sizes against Total and seeds its run
		// budget from WalletUSDC.
		if a.wallet != nil {
			snap, err := a.snapshotAccountValue(ctx, logger)
			if err != nil {
				logger.Event("account_value_abort", map[string]any{"error": err.Error()})
				return
			}

			// 4. Discover the markets where the unchanged NO signal is eligible for
			// the reversed YES-buying strategy. A failure to list markets or
			// fetch positions is fatal for this run's ordering — log and return without
			// placing any orders rather than reconciling against a partial market set.
			eligible, err := a.discoverEligibleMarkets(ctx, logger)
			if err != nil {
				logger.Event("discover_abort", map[string]any{"error": err.Error()})
				return
			}

			// 5. Reconcile: place/maintain one YES buy order per eligible market at
			// the fresh YES midpoint with the GTD expiry. Per-market failures are isolated.
			a.reconcilePass(ctx, logger, snap, eligible)
		}
	}

	logger.Event("run_done", map[string]any{
		"mode":    a.cfg.Mode,
		"dry_run": a.cfg.DryRun,
	})
}

// runSchedule runs one tick immediately, then one per interval, until the
// context is canceled (SIGINT/SIGTERM). It exits cleanly without leaking the
// ticker goroutine or compounding overlapping runs.
func (a *App) runSchedule(ctx context.Context, out io.Writer) error {
	a.runTick(ctx, out)

	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.runTick(ctx, out)
		}
	}
}
