# Polymarket NO-Signal / YES Buyer App PRD

## Summary

Create a deterministic Polymarket trading app under `apps/` that redeems resolved positions, uses the original high-NO-price eligibility signal, and places midpoint limit buy orders for the corresponding YES shares. The directory and configuration prefix retain the legacy `polymarket_no_buyer` name. The app must use the repo's existing `polymarket/` client and shared wallet/config helpers. It must not use AI, LLMs, research agents, or subjective scoring.

The app supports two run modes:

- One-shot mode for testing and manual execution.
- Scheduled mode for production, running every 6 hours by default.

## Goals

- Redeem any closed or resolved Polymarket positions at the start of each run.
- Find active binary markets where the NO midpoint price is greater than `0.89` and less than or equal to `0.99`.
- Only buy markets whose close time is more than 48 hours away and less than 14 days away.
- Enforce a configurable minimum liquidity threshold, defaulting to `$5,000`.
- Maintain a per-market committed YES exposure target equal to `2%` of total account value.
- Place YES buy limit orders at the current YES midpoint price.
- Expire new orders 24 hours before the market close time.
- Cancel stale open orders that no longer match the strategy.
- Provide a dry-run mode that logs intended actions without redeeming, canceling, or placing orders.

## Non-Goals

- No AI or LLM involvement.
- No market research, thesis generation, or category-specific filtering.
- No portfolio-wide exposure cap beyond wallet USDC and the per-market target.
- No database-backed audit log in the first version.
- No new third-party Polymarket trading library unless the existing repo client cannot support a required operation.

## App Location

The app should live under `apps/`, with a suggested path:

```text
apps/polymarket_no_buyer/
```

The binary should be a standalone Go app that can run from the command line and can later be supervised by a scheduler, container, or service manager.

## Strategy Rules

### Market Eligibility

A market is eligible only if all of the following are true:

- It is active, accepting orders, and unresolved.
- It is a binary YES/NO market with exactly identifiable YES and NO token IDs.
- Its close time is in the future.
- Its close time is strictly more than 48 hours from the run time.
- Its close time is strictly less than 14 days from the run time.
- Its reported USD liquidity is at least the configured minimum liquidity, default `$5,000`.
- Its NO midpoint price is `> 0.89` and `<= 0.99`.
- The account does not own any NO shares in the market.

The NO midpoint price is calculated from the live order book:

```text
no_midpoint = (best_no_bid + best_no_ask) / 2
```

If a market does not have a usable two-sided NO signal book or a usable two-sided YES execution book, skip it.

Token identification must be deterministic:

- Decode the market outcomes and CLOB token IDs as ordered arrays.
- Require exactly two outcomes and exactly two CLOB token IDs.
- Map outcomes to token IDs by array index using case-insensitive `YES` and `NO` outcome labels.
- Skip markets where outcomes or token IDs are missing, malformed, duplicated, or not exactly binary YES/NO.

The app should fetch the market's venue minimum order size from Polymarket CLOB metadata before sizing. Prefer `min_order_size` from the YES execution book response when present, or the `mos` field from `GET /clob-markets/{condition_id}`. If the minimum order size cannot be determined, skip live ordering for that market and log the reason. A test-only fallback may be configurable, but production should fail closed rather than guessing.

### Account Value

Total account value is:

```text
wallet_usdc + current_value_of_all_owned_shares
```

`wallet_usdc` is determined only from the configured Polygon USDC wallet balance through existing wallet/config helpers. It must not use a Polymarket, CLOB, or exchange-reported available-balance field. The default token should be the existing Polymarket collateral USDC token used by the repo, unless config overrides it.

Owned share value should use midpoint price when available. If midpoint price is unavailable, fall back to best bid. If neither is available, value that position at zero for sizing purposes and log the fallback.

The app should compute account value after the redemption pass and after clearly stale order cancellations. Order cancellations do not change `wallet_usdc`; they are ordered first so the later reconciliation pass sees the current open-order set.

### Per-Market Target

The target exposure per market is:

```text
target_notional = total_account_value * 0.02
target_yes_shares = target_notional / latest_yes_midpoint
```

This `2%` limit is per market, not per run.

Committed YES exposure for a market is:

```text
held_yes_shares + remaining_open_yes_buy_shares
```

`remaining_open_yes_buy_shares` is `original_size - size_matched` for live/open YES buy orders in that market. Partially filled orders count only by their remaining size.

If the account already owns YES shares in the market:

- Skip the market if committed YES exposure is greater than or equal to `target_yes_shares`.
- Otherwise, calculate the top-up needed to bring committed YES exposure to `target_yes_shares`.
- Skip the top-up if the required order would be below the market's Polymarket minimum order size.

If the account owns NO shares in the market:

- Skip the market entirely.

For a new YES position where the calculated `2%` order is below the market's Polymarket minimum order size:

- The app may bump the initial order up to the market's Polymarket minimum order size.
- Skip the market if `run_usdc_remaining` cannot cover the minimum order notional.
- Log that the order used the minimum-order exception.

### Available Cash Handling

Eligible markets are processed earliest close first.

For each market:

- Re-check the live order book and eligibility immediately before order handling.
- Calculate the desired YES order using the current account-value snapshot and latest YES midpoint.
- Track a local `run_usdc_remaining` value initialized from `wallet_usdc` and reduced by orders the app places or intentionally leaves open during this run. This prevents one run from planning more new/maintained notional than the wallet balance, without relying on Polymarket's exchange-side available balance.
- If the full desired order cannot be funded but `run_usdc_remaining` can cover the market's minimum order notional, place the largest valid order that does not exceed the target exposure.
- If `run_usdc_remaining` is below the market's minimum order notional, skip the market.
- Continue processing remaining markets after skips or order failures.

The strategy intentionally has no portfolio-wide hard cap. It should keep attempting to deploy wallet USDC across eligible markets, constrained by per-market target exposure, market eligibility, minimum order size, and `run_usdc_remaining`.

## Run Workflow

Each run performs these steps in order.

### 1. Initialize

- Load config and Polymarket credentials through existing repo helpers.
- Create the existing Polymarket client.
- Assign a run ID for logging.
- Read mode flags such as `--dry-run`, `--once`, or scheduled mode config.

### 2. Redeem Closed Markets

- Fetch current positions.
- Identify positions in closed or resolved markets that can be redeemed.
- Attempt redemption before any buy logic.
- If redemption fails for a market, log the failure and ignore that market until the next run.
- Continue the run after redemption failures.
- In dry-run mode, log the redemption that would happen and do not submit it.

### 3. Cancel Clearly Stale Open Orders

Fetch open orders for the account and cancel orders that can be proven stale before account-value sizing:

- Cancel orders for markets that are closed, resolved, inactive, not accepting orders, outside the close-time window, below the liquidity threshold, or no longer binary YES/NO.
- Cancel orders for markets where the account owns NO shares.
- Cancel orders on the wrong side or wrong asset.
- Cancel YES buy orders whose limit price differs from the latest normalized YES midpoint price.
- Cancel YES buy orders whose expiration differs from `market_close_time - 24h`.
- Cancel duplicate or conflicting YES buy orders for the same market, keeping at most one candidate order for the later reconciliation pass.
- Cancel legacy NO orders as wrong-asset orders.

Do not cancel solely because an order amount differs from the final desired amount in this step; desired size depends on the account-value snapshot. Size reconciliation happens in step 6.

Order comparisons should normalize to Polymarket's accepted price and size precision before comparing. Small comparison tolerances may be configurable if required by the existing client.

### 4. Snapshot Account Value

- Fetch wallet USDC balance through the existing wallet/config helpers.
- Fetch owned positions.
- Value all owned shares using midpoint, best bid fallback, or zero fallback.
- Compute total account value.
- Abort the buy pass if account value or wallet USDC cannot be computed.

### 5. Discover Eligible Markets

- Use the existing Polymarket market/event listing facilities.
- Filter by active state, binary YES/NO structure, close-time window, liquidity, ownership, and NO midpoint.
- Sort surviving markets by close time ascending.

### 6. Place Or Maintain Orders

For each eligible market, earliest close first:

- Re-fetch the NO signal book and the YES execution book.
- Recompute the NO signal midpoint and YES execution midpoint.
- Re-run all eligibility checks.
- Skip if the latest midpoint is no longer `> 0.89` and `<= 0.99`.
- Fetch or verify the market minimum order size from the YES execution book/metadata.
- Calculate target YES shares and any needed top-up.
- Respect one open YES buy order that already matches the desired side, asset, normalized price, remaining amount, and expiration.
- Cancel and replace open orders when limit price, amount, side, or expiration differs.
- Place a YES buy limit order at the latest YES midpoint price.
- Set order expiration to `market_close_time - 24h`.
- Skip if the computed expiration is not in the future.
- Continue after per-market order failures.

## Configuration

Recommended config keys:

| Key | Default | Description |
| --- | --- | --- |
| `POLYMARKET_NO_BUYER_MODE` | `once` | `once` or `schedule`. |
| `POLYMARKET_NO_BUYER_INTERVAL` | `6h` | Scheduled run interval. |
| `POLYMARKET_NO_BUYER_DRY_RUN` | `false` | Logs actions without submitting them. |
| `POLYMARKET_NO_BUYER_MIN_NO_MIDPOINT` | `0.89` | Exclusive lower bound for NO midpoint. |
| `POLYMARKET_NO_BUYER_MAX_NO_MIDPOINT` | `0.99` | Inclusive upper bound for NO midpoint. |
| `POLYMARKET_NO_BUYER_TARGET_EXPOSURE_PCT` | `0.02` | Per-market target exposure. |
| `POLYMARKET_NO_BUYER_MIN_LIQUIDITY_USD` | `5000` | Minimum market liquidity. |
| `POLYMARKET_NO_BUYER_MIN_HOURS_TO_CLOSE` | `48h` | Minimum time remaining before close. |
| `POLYMARKET_NO_BUYER_MAX_HOURS_TO_CLOSE` | `336h` | Maximum time remaining before close, 14 days. |
| `POLYMARKET_NO_BUYER_ORDER_EXPIRY_BEFORE_CLOSE` | `24h` | Order expiration offset from market close. |
| `POLYMARKET_NO_BUYER_USDC_TOKEN_ADDRESS` | existing repo Polymarket USDC address | Polygon USDC token used for wallet cash balance. |
| `POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK` | unset | Optional test-only minimum order size fallback if CLOB metadata is unavailable. |

Polymarket credentials, wallet settings, proxy settings, and chain settings should use the existing repo configuration paths and environment variables.

## CLI Behavior

Suggested flags:

```text
--once
--schedule
--interval 6h
--dry-run
--min-liquidity 5000
```

Expected examples:

```sh
cd apps/polymarket_no_buyer
go run . --once --dry-run
go run . --once
go run . --schedule --interval 6h
```

For the first implementation, one-shot mode should be the safest default. Scheduled mode should be explicit until deployment is ready.

## Logging

Structured console logging is sufficient for the first version.

Each run should log:

- Run ID, mode, dry-run status, and config values.
- Redemption attempts, successes, skips, and failures.
- Account value calculation, including USDC and position value.
- Number of markets scanned and number eligible.
- Skip reason for each rejected candidate when verbose logging is enabled.
- Stale order cancellations.
- Existing matching orders left unchanged.
- New order details: condition ID, question, YES token ID, NO signal midpoint, YES execution midpoint, shares, notional, close time, expiration, and `run_usdc_remaining` after reservation.
- Per-market failures without stopping the run.

## Failure Handling

- Redemption failure for one market does not stop the run.
- Order cancellation failure for one market does not stop unrelated markets.
- Buy order failure for one market does not stop remaining markets.
- Market/orderbook fetch failure skips that market.
- Missing account value or missing wallet USDC aborts the buy pass because sizing cannot be trusted.
- Dry-run mode must not mutate account state.

## Acceptance Criteria

- A one-shot dry run prints redemptions, stale-order cancellations, eligible markets, and intended orders without submitting any transaction or order.
- A one-shot live run redeems eligible closed positions before attempting buys.
- The app never places an order for a market closing in 48 hours or less.
- The app never places an order for a market closing in 14 days or more.
- The app only places YES buy limit orders where the latest NO signal midpoint is `> 0.89` and `<= 0.99`.
- The app places new YES buy orders at the latest YES midpoint price.
- New orders expire 24 hours before market close.
- The app skips markets where the account owns NO shares.
- The app treats `2%` of total account value as the per-market committed YES exposure target.
- Existing YES positions plus remaining open YES buy orders are topped up only when below the per-market target and the top-up meets Polymarket's per-market minimum order size.
- Existing matching open orders are left unchanged.
- Open orders with different price, amount, side, expiration, or no longer eligible market are canceled.
- Live orders use a custom GTD expiration timestamp equal to `market_close_time - 24h`; the existing fixed GTD TTL is not sufficient.
- Markets are skipped for live ordering if their per-market minimum order size cannot be read from CLOB metadata.
- Buy attempts continue after per-market failures.
- Scheduled mode runs every 6 hours by default.
- No AI, LLM, agent loop, or research component is invoked anywhere in the app.

## Implementation Notes

- Prefer existing repo wallet helpers for wallet USDC balance and existing `polymarket/` client capabilities for positions, markets, order books, order placement, order cancellation, and redemption.
- Extend the existing `polymarket/` client where needed instead of working around it:
  - Add custom GTD expiration support so the caller can place an order expiring exactly at `market_close_time - 24h`.
  - Expose order book or CLOB market metadata fields needed for `min_order_size`, tick size, neg-risk, and token/outcome mapping.
- Keep the app deterministic and idempotent: repeated runs should converge toward the same intended orders rather than compounding exposure.
- Re-check live orderbook state immediately before each order because midpoint eligibility can change during the run.
- Use `gofmt` on all changed Go files if implementation follows this PRD.
