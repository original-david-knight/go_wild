# Polymarket NO Buyer Milestone Tracker

Companion checklist for [`polymarket_no_buyer_prd.md`](polymarket_no_buyer_prd.md).
Each rung layers one feature and is accepted only when its
**Self-check** and its **folded-in Gates** pass — plus a **Visual proof** for
rungs whose output a human must see, hear, or interact with.

Every checkbox is tagged either:

- `[AFK]` — AI coding agents implement, run, and verify. No human needed.
- `[Human Verification]` — a human must run the demo and approve.

Work the ladder top to bottom. A rung is not "done" until every box under
it is checked and the standing gates still pass.

## Status legend

- `[ ]` not started
- `[~]` in progress
- `[x]` done / passing
- `[!]` blocked or regressed

Per-milestone status: ☐ Not started · ◐ In progress · ☑ Accepted

## At-a-glance

| Rung | Feature layer | AFK boxes | Human boxes | Status |
|------|---------------|:---------:|:-----------:|:------:|
| M0 | Rung 0 — App skeleton, config, CLI, structured logging, and run init | 18 | 0 | ☑ |
| M1 | Rung 1 — Run modes: one-shot, scheduled, interval, and dry-run | 19 | 0 | ☑ |
| M2 | Rung 2 — polymarket/ client extensions: custom GTD + CLOB metadata | 14 | 0 | ☑ |
| M3 | Rung 3 — Redeem closed/resolved positions at run start | 16 | 0 | ☑ |
| M4 | Rung 4 — Deterministic token/outcome identification and live NO midpoint | 15 | 0 | ☑ |
| M5 | Rung 5 — Venue minimum order size from CLOB metadata | 17 | 0 | ☑ |
| M6 | Rung 6 — Market eligibility filter and discovery | 15 | 0 | ☑ |
| M7 | Rung 7 — Cancel clearly stale open orders with precision-normalized comparison | 23 | 0 | ☑ |
| M8 | Rung 8 — Account value snapshot: wallet USDC + owned-share valuation | 14 | 0 | ☑ |
| M9 | Rung 9 — Per-market target sizing, committed exposure, and top-up | 15 | 0 | ☑ |
| M10 | Rung 10 — Wallet-budget tracking and minimum-order/partial-fill cash handling | 15 | 0 | ☑ |
| M11 | Rung 11 — Reconciliation pass: place/maintain NO buy limit orders at midpoint with GTD expiry | 16 | 0 | ☑ |
| M12 | Rung 12 — Resilience, idempotent convergence, and gofmt hardening | 13 | 0 | ☑ |

Totals: **210 AFK, 0 Human Verification** across the 13 rungs, plus 9 standing-gate boxes that re-verify on every rung and 1 final acceptance box.

## Standing gates (re-verify on every rung)

- [x] [AFK] Workspace builds clean: `./scripts/workspace_go.sh build ./...` (or `make build`) succeeds with no errors.
- [x] [AFK] Full test suite passes: `./scripts/workspace_go.sh test ./...` (or `make test`) is green.
- [x] [AFK] The polymarket/ library still builds and its existing tests pass after any client extensions (downstream-consumer no-regression).
- [x] [AFK] No AI/LLM/agent-loop/research component is invoked anywhere in the app or its dependencies (no LLM client imports).
- [x] [AFK] Dry-run invariant holds: dry-run mode mutates no account state — no redemption, cancellation, or order submission is sent.
- [x] [AFK] Fail-closed posture preserved: missing config, unreachable dependency, undeterminable min order size, or missing account value/wallet USDC aborts or skips rather than guessing — no hardcoded fallback/default config values.
- [x] [AFK] Per-market failure isolation holds: a redemption, cancellation, or buy failure for one market never stops processing of remaining markets.
- [x] [AFK] Determinism/idempotency holds: repeated runs converge toward the same intended orders and do not compound exposure.
- [x] [AFK] All changed Go files are gofmt-clean.

---

## M0 — Rung 0 — App skeleton, config, CLI, structured logging, and run init

**Builds on:** nothing (foundation) · **Exercises:** §App Location, §Run Workflow (1. Initialize), §Configuration, §CLI Behavior, §Logging

Status: ☑ Accepted

### Build

- [x] [AFK] Create the standalone Go app at `apps/polymarket_no_buyer/` (module + `main` package) wired into `go.work`, building a single CLI binary with no trading logic yet (no redeem, cancel, sizing, or order placement).
- [x] [AFK] Construct the existing repo `polymarket/` client and shared wallet/config helpers during init; load Polymarket credentials, wallet, proxy, and chain settings through the existing repo configuration paths/env vars (no new client library, no hardcoded fallbacks — fail loudly if a required credential/config is missing).
- [x] [AFK] Load app config from the documented keys with their PRD defaults: `POLYMARKET_NO_BUYER_MODE` (`once`), `POLYMARKET_NO_BUYER_INTERVAL` (`6h`), `POLYMARKET_NO_BUYER_DRY_RUN` (`false`), `POLYMARKET_NO_BUYER_MIN_NO_MIDPOINT` (`0.89`), `POLYMARKET_NO_BUYER_MAX_NO_MIDPOINT` (`0.99`), `POLYMARKET_NO_BUYER_TARGET_EXPOSURE_PCT` (`0.02`), `POLYMARKET_NO_BUYER_MIN_LIQUIDITY_USD` (`5000`), `POLYMARKET_NO_BUYER_MIN_HOURS_TO_CLOSE` (`48h`), `POLYMARKET_NO_BUYER_MAX_HOURS_TO_CLOSE` (`336h`), `POLYMARKET_NO_BUYER_ORDER_EXPIRY_BEFORE_CLOSE` (`24h`), `POLYMARKET_NO_BUYER_USDC_TOKEN_ADDRESS` (existing repo Polymarket USDC address), and optional `POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK` (unset).
- [x] [AFK] Parse CLI flags `--once`, `--schedule`, `--interval`, `--dry-run`, and `--min-liquidity`; flags override the corresponding config/env values, and the parsed effective config is exposed as a single struct for downstream rungs (flag wiring to run modes is M1 — this rung only parses and records them).
- [x] [AFK] Assign a unique run ID at the start of each run (deterministic, collision-resistant identifier) and thread it through a structured logger.
- [x] [AFK] Emit structured console logging per run: a run-init log line carrying run ID, mode, dry-run status, and the resolved effective config values, with one logical line/object per event so downstream rungs can attach their own events to the same run ID.

### Self-check (headless)

- [x] [AFK] `cd apps/polymarket_no_buyer && go build .` produces the binary; `go vet ./...` and `gofmt -l .` report clean on all changed files.
- [x] [AFK] Running the binary with valid config/credentials in env emits a single run-init structured log line; parse it (JSON or key=value) and assert it contains a non-empty `run_id`, `mode`, `dry_run`, and the resolved config keys.
- [x] [AFK] Two consecutive invocations emit two distinct `run_id` values (assert inequality), proving per-run ID assignment.
- [x] [AFK] Flag override precedence: invoking with `--dry-run --min-liquidity 7500 --interval 12h` produces a run-init log whose `dry_run=true`, `min_liquidity_usd=7500`, and `interval=12h` override the env/default values; with no flags the logged values equal the documented PRD defaults.
- [x] [AFK] Env-var binding: setting `POLYMARKET_NO_BUYER_MIN_NO_MIDPOINT=0.90` (and other documented keys) and re-running shows the overridden value in the run-init log when no conflicting flag is passed.
- [x] [AFK] Missing-config failure: with a required Polymarket credential / wallet config unset, the binary exits non-zero and logs the failure reason (assert exit code != 0 and an error log line) rather than silently substituting a default.
- [x] [AFK] No-trading-logic guard: `grep -rIE 'RedeemPositions|PlaceOrder|CancelOrder' apps/polymarket_no_buyer/` (or equivalent client call names) returns no matches at this rung, confirming init-only scope.
- [x] [AFK] No-AI guard: `grep -rIE 'agentic_loop|gemini|llm|GEMINI_API_KEY' apps/polymarket_no_buyer/` returns no matches.

### Gates

- [x] [AFK] `go build ./...` and `(cd apps/polymarket_no_buyer && go build .)` succeed across the workspace.
- [x] [AFK] `go test ./...` (workspace) and `(cd apps/polymarket_no_buyer && go test ./...)` pass, including the config/CLI/run-init unit tests added in this rung.
- [x] [AFK] `go vet ./...` clean and `gofmt -l apps/polymarket_no_buyer` prints nothing (no-regression on existing modules).
- [x] [AFK] `go test -run TestPolymarketNoBuyer_RunInit ./...` (config-precedence + run-ID + log-shape tests) passes.

---

## M1 — Rung 1 — Run modes: one-shot, scheduled, interval, and dry-run

**Builds on:** M0 — Rung 0 — App skeleton, config, CLI, structured logging, and run init · **Exercises:** §Summary (run modes), §Run Workflow step 1 (Initialize / read mode flags), §Configuration (`POLYMARKET_NO_BUYER_MODE`, `_INTERVAL`, `_DRY_RUN`), §CLI Behavior, §Failure Handling (dry-run must not mutate)

Status: ☑ Accepted

### Build

- [x] [AFK] Add `--once` flag (and `POLYMARKET_NO_BUYER_MODE=once`) as the safe default mode: a single run executes the per-run pipeline exactly once and the process exits 0, per §CLI Behavior ("one-shot mode should be the safest default").
- [x] [AFK] Add explicit `--schedule` flag (and `POLYMARKET_NO_BUYER_MODE=schedule`) that loops, executing one run per tick; scheduled mode must be explicit and never the implicit default, per §CLI Behavior ("Scheduled mode should be explicit until deployment is ready").
- [x] [AFK] Add configurable `--interval` flag (and `POLYMARKET_NO_BUYER_INTERVAL`) parsed as a Go duration, defaulting to `6h`, controlling the gap between scheduled runs; `--interval` is honored only in schedule mode.
- [x] [AFK] Add `--dry-run` flag (and `POLYMARKET_NO_BUYER_DRY_RUN=true`, default `false`) that threads a non-mutating mode flag into the run context so downstream passes log intended actions without redeeming, canceling, or placing orders, per §Failure Handling ("Dry-run mode must not mutate account state").
- [x] [AFK] Resolve precedence deterministically: explicit CLI flags override env-var config, which overrides documented defaults (`mode=once`, `interval=6h`, `dry_run=false`); reject contradictory invocation (both `--once` and `--schedule`) with a non-zero exit and a clear error per the "fail loudly on bad config" repo rule.
- [x] [AFK] Emit the resolved mode, interval, and dry-run status into the per-run structured log line established in M0, per §Logging ("Run ID, mode, dry-run status, and config values").
- [x] [AFK] Make scheduled mode shut down cleanly on SIGINT/SIGTERM between or during runs (no compounding goroutines, deterministic exit code), so the loop can be supervised by a scheduler/container/service manager per §App Location.

### Self-check (headless)

- [x] [AFK] `go run . --once --dry-run` exits 0, runs the pipeline exactly once, and the structured log shows `mode=once dry_run=true`; assert via grep on captured stdout that exactly one run-start line is emitted.
- [x] [AFK] `go run . --once` (no `--dry-run`) logs `mode=once dry_run=false` and exits 0; assert dry-run defaults to false.
- [x] [AFK] Schedule mode fires repeatedly on its interval: run `go run . --schedule --interval 1s` for a bounded window (e.g. send SIGTERM after ~3.5s) and assert stdout contains at least 3 distinct run-start lines with monotonically increasing run IDs — proving the loop ticks at the configured interval, not a hardcoded one.
- [x] [AFK] Default interval is 6h: with `--schedule` and no `--interval`, assert the resolved-config log line prints `interval=6h`.
- [x] [AFK] Env-var configuration works without flags: `POLYMARKET_NO_BUYER_MODE=schedule POLYMARKET_NO_BUYER_INTERVAL=2s POLYMARKET_NO_BUYER_DRY_RUN=true go run .` logs `mode=schedule interval=2s dry_run=true`; assert flags-over-env precedence by additionally passing `--interval 5s` and asserting `interval=5s` wins.
- [x] [AFK] Contradictory flags fail loudly: `go run . --once --schedule` exits non-zero and prints an error naming the conflict; assert on exit code and stderr substring.
- [x] [AFK] Schedule mode terminates cleanly: assert the SIGTERM-driven run from above exits 0 (or the documented signal exit code) and prints no panic/goroutine-leak trace on stderr.

### Gates

- [x] [AFK] `./scripts/workspace_go.sh build ./...`
- [x] [AFK] `(cd apps/polymarket_no_buyer && go test ./... -run 'TestRunMode|TestSchedule|TestDryRun|TestFlagPrecedence' -v)`
- [x] [AFK] `go vet ./apps/polymarket_no_buyer/...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer` prints nothing (no unformatted files), per §Implementation Notes
- [x] [AFK] No-regression: M0 gates (`go build ./...`, M0 init/config tests) still pass

---

## M2 — Rung 2 — polymarket/ client extensions: custom GTD + CLOB metadata

**Builds on:** M0 — App skeleton, config, CLI, structured logging, and run init; M1 — Run modes: one-shot, scheduled, interval, and dry-run · **Exercises:** §Implementation Notes (custom GTD expiration, expose CLOB/order-book metadata), §Token identification, §Venue minimum order size

Status: ☑ Accepted

These are the two riskiest unknowns in the build, so they are proven up front before any trading logic depends on them. Both extensions live in the repo `polymarket/` client (not in the app) per the PRD directive to extend the client rather than work around it.

### Build

- [x] [AFK] Extend the `polymarket/` client with custom GTD expiration support: a build/place path that accepts an arbitrary absolute expiration Unix timestamp, sets `order_type` to `GTD` (constant `polymarket.GTD`), serializes `Expiration` as the given timestamp, and re-signs the order so the signature covers the custom expiration — not a fixed TTL relative to "now" (PRD: "Add custom GTD expiration support so the caller can place an order expiring exactly at `market_close_time - 24h`" / "the existing fixed GTD TTL is not sufficient").
- [x] [AFK] Reject non-future / zero / negative expiration timestamps at the client boundary with a clear typed error so callers can skip rather than place an already-expired order (PRD: "Skip if the computed expiration is not in the future").
- [x] [AFK] Expose order-book / CLOB market metadata fields on the client so callers can read, per market: `min_order_size` (from the order-book response when present) and `mos` from `GET /clob-markets/{condition_id}`, `tick size`, `neg-risk` (`NegRisk`/`NegRiskMarketID`), and the ordered outcomes ↔ CLOB token-ID arrays for YES/NO token mapping (PRD: "Expose order book or CLOB market metadata fields needed for `min_order_size`, tick size, neg-risk, and token/outcome mapping").
- [x] [AFK] Surface the metadata via typed Go struct fields/accessors (no caller-side JSON re-parsing of raw responses), reusing the existing `OrderBook`, market, and neg-risk types where they already carry the field.

### Self-check (headless)

- [x] [AFK] Go unit test: build a GTD order with an arbitrary future timestamp `T` (e.g. `time.Now().Add(72h).Unix()`) and assert the resulting order has `order_type == "GTD"` and `Expiration == strconv.FormatInt(T, 10)` exactly — not `"0"` and not a now-relative TTL.
- [x] [AFK] Go unit test: build the same order twice with two different expiration timestamps and assert the EIP-712 signatures differ, proving the custom expiration is inside the signed payload (a fixed-TTL implementation would produce identical or now-dependent signatures).
- [x] [AFK] Go unit test: passing an expiration `<= now` (and `0`, negative) returns the typed non-future error and produces no signed order.
- [x] [AFK] Go table-test against a recorded/stubbed `GET /clob-markets/{condition_id}` payload and order-book payload: assert the accessors return `min_order_size` from the book when present and fall back to `mos` from the clob-markets response when the book omits it, and that `tick size`, `NegRisk`, and the ordered outcomes/token-ID arrays are decoded into typed fields.
- [x] [AFK] Go test: a clob-markets payload missing `mos` and an order book missing `min_order_size` together yield an "undeterminable minimum order size" signal (not a guessed default), matching the PRD fail-closed contract that later rungs rely on.

### Gates

- [x] [AFK] `./scripts/workspace_go.sh build ./...` — whole workspace compiles with the extended client.
- [x] [AFK] `(cd polymarket && go test ./...)` — client extension unit tests pass.
- [x] [AFK] `go test ./... -run 'GTD|Expiration|Metadata|MinOrderSize|Clob' ./polymarket/...` — targeted client-extension suite green.
- [x] [AFK] `go vet ./polymarket/...` clean and `gofmt -l polymarket/` prints nothing (no unformatted changed files).
- [x] [AFK] No-regression: the pre-existing `polymarket/` order and auth tests still pass unchanged.

---

## M3 — Rung 3 — Redeem closed/resolved positions at run start

**Builds on:** M2 — Rung 2 — polymarket/ client extensions: custom GTD + CLOB metadata (and M0/M1 run init + modes) · **Exercises:** §"Redeem any closed or resolved Polymarket positions at the start of each run" (Goals), §1 Run Init, §2 Redeem Closed Markets

Status: ☑ Accepted

### Build

- [x] [AFK] Add a redemption pass that runs as the first trading step of every run (before any cancel/snapshot/buy logic), gated behind run init from M0/M1.
- [x] [AFK] Fetch current positions for the configured wallet via the repo `polymarket/` client.
- [x] [AFK] Identify positions in closed or resolved markets that are redeemable (non-zero redeemable balance), filtering out open/unresolved markets.
- [x] [AFK] Attempt redemption for each redeemable position via the `polymarket/` client; emit a structured per-position log line (run ID, condition/market id, outcome, attempted/succeeded/failed, reason).
- [x] [AFK] On a per-market redemption failure, log the failure and skip that market (ignored until the next run) without aborting; continue iterating remaining redeemable positions and the rest of the run.
- [x] [AFK] In `--dry-run` mode, log the redemption that would be submitted for each redeemable position and submit nothing (no mutating client calls).

### Self-check (headless)

- [x] [AFK] Unit test: given a fixture position set mixing open, resolved-redeemable, and resolved-zero-balance markets, the redeem selector returns exactly the resolved-redeemable subset (assert on returned condition ids).
- [x] [AFK] Unit test with an injected fake `polymarket/` client: redemption pass calls redeem once per redeemable position; a forced failure on one market is logged and does not stop the remaining redemptions or return an error from the pass (assert remaining redeem calls still occur and the pass returns nil).
- [x] [AFK] Ordering test: the redeem pass executes before any buy/cancel hook (assert via a recorded call-order on the fake client — redeem invocations precede any other trading call).
- [x] [AFK] Dry-run test: with `--dry-run`, no redeem (mutating) call is made on the fake client and the structured log shows a "would redeem" entry per redeemable position (assert call count == 0 and parse the emitted JSON log lines).
- [x] [AFK] Log-shape test: parse emitted structured log lines and assert each redemption attempt carries run ID, market/condition id, and an outcome status field; failure lines carry a non-empty reason.

### Gates

- [x] [AFK] `go build ./...`
- [x] [AFK] `go vet ./...`
- [x] [AFK] `go test ./apps/polymarket_no_buyer/...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer` reports no files
- [x] [AFK] No-regression: prior rung gates (M0–M2 run init, modes, and client-extension tests) still pass via `go test ./...`

---

## M4 — Rung 4 — Deterministic token/outcome identification and live NO midpoint

**Builds on:** M3 — Rung 3 — Redeem closed/resolved positions at run start (and M2 — Rung 2 — polymarket/ client extensions: custom GTD + CLOB metadata) · **Exercises:** §Eligibility (token identification, NO midpoint), §Polymarket Client Usage (token/outcome mapping, order book metadata)

Status: ☑ Accepted

### Build

- [x] [AFK] Add a deterministic token/outcome decoder that reads a market's `outcomes` and CLOB token IDs as ordered arrays, requiring exactly two outcomes and exactly two CLOB token IDs.
- [x] [AFK] Map outcomes to token IDs by array index using case-insensitive `YES` and `NO` outcome labels, producing a typed result that exposes the YES token ID and NO token ID separately.
- [x] [AFK] Reject (skip) markets where outcomes or token IDs are missing, malformed, duplicated, or not exactly a binary `YES`/`NO` pair, returning a structured non-eligible reason rather than erroring out the run.
- [x] [AFK] Compute `no_midpoint = (best_no_bid + best_no_ask) / 2` from the live order book for the resolved NO token ID, sourcing best bid/ask via the M2 order-book/CLOB metadata client extension.
- [x] [AFK] Skip markets that do not present a usable two-sided NO order book (missing best bid, missing best ask, empty/one-sided book), emitting a structured skip reason and leaving the run to continue with other markets.
- [x] [AFK] Emit a structured per-market log line capturing condition ID, decoded YES/NO token IDs, best_no_bid, best_no_ask, and computed no_midpoint (or the skip reason when non-eligible).

### Self-check (headless)

- [x] [AFK] Table-driven unit test for the decoder: a valid `["Yes","No"]` / two-token-ID market maps NO and YES to the correct index-aligned token IDs; case variants (`yes`, `NO`, `No`) all resolve identically.
- [x] [AFK] Decoder rejection tests: one-outcome, three-outcome, one-token-ID, three-token-ID, duplicated token IDs, duplicated outcome labels, and non-YES/NO labels (e.g. `["Up","Down"]`) each return the expected structured non-binary skip reason and never panic.
- [x] [AFK] Midpoint test with a fixed two-sided book (e.g. best_no_bid `0.90`, best_no_ask `0.92`) asserts `no_midpoint == 0.91` bit-for-bit via the chosen decimal/float representation.
- [x] [AFK] One-sided and empty book fixtures (bid-only, ask-only, no levels) assert the market is skipped with a "no usable two-sided NO order book" reason and no midpoint is produced.
- [x] [AFK] Assert the structured log line for an eligible fixture contains condition ID, both token IDs, best_no_bid, best_no_ask, and no_midpoint; assert a skipped fixture emits its reason field.

### Gates

- [x] [AFK] `go build ./...` (workspace builds clean via `./scripts/workspace_go.sh build ./...`)
- [x] [AFK] `go test ./...` for `apps/polymarket_no_buyer/...` and any touched `polymarket/...` packages (label: `tokens`, `midpoint`)
- [x] [AFK] `go vet ./...` and `gofmt -l` report no findings on changed files
- [x] [AFK] No regression: M0–M3 gates (skeleton, run modes, client extensions, redemption) remain green

---

## M5 — Rung 5 — Venue minimum order size from CLOB metadata

**Builds on:** M4 — Deterministic token/outcome identification and live NO midpoint (and M2 — polymarket/ client extensions: custom GTD + CLOB metadata) · **Exercises:** §Token/Outcome Identification & Venue Minimum Order Size, §Configuration (`POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK`)

Status: ☑ Accepted

### Build

- [x] [AFK] Add a `resolveMinOrderSize(market)` step that runs before any sizing logic and returns the per-market venue minimum order size plus the source it was read from.
- [x] [AFK] Prefer `min_order_size` from the live order-book response (the M4 book fetch / M2 CLOB metadata extension) when it is present and a positive number.
- [x] [AFK] Fall back to the `mos` field from `GET /clob-markets/{condition_id}` (via the M2 client extension) when the order-book `min_order_size` is absent or unusable.
- [x] [AFK] When neither source yields a usable positive minimum, treat the market as undeterminable: skip live ordering for that market and emit a structured log entry with the market/condition_id and the reason.
- [x] [AFK] Honor `POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK` only as a test-only override; in production (fallback unset) the path fails closed by skipping rather than guessing a value.
- [x] [AFK] Log the resolved minimum and its source (`order_book` | `clob_markets_mos` | `fallback`) per market in the structured per-run logs from M0.

### Self-check (headless)

- [x] [AFK] `go test ./apps/polymarket_no_buyer/...` covers: order book carries a positive `min_order_size` → resolver returns it with source `order_book` and does NOT call `/clob-markets`.
- [x] [AFK] Table test: order book `min_order_size` missing/zero/negative but `/clob-markets/{condition_id}` returns positive `mos` → resolver returns `mos` with source `clob_markets_mos`.
- [x] [AFK] Table test: both sources undeterminable and fallback unset → resolver returns a skip signal, the market is excluded from live ordering, and a log line naming the condition_id + reason is asserted on captured stdout/log output.
- [x] [AFK] Test: both sources undeterminable but `POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK` set → resolver returns the fallback with source `fallback` (test-only path); assert this path is gated and not taken when the env var is unset.
- [x] [AFK] Assert fail-closed precedence: a present positive order-book `min_order_size` is used even when a fallback env var is also set (real data wins over fallback).

### Gates

- [x] [AFK] `go build ./...`
- [x] [AFK] `go test ./...`
- [x] [AFK] `go vet ./apps/polymarket_no_buyer/... ./polymarket/...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer polymarket` returns no changed files
- [x] [AFK] `go test ./apps/polymarket_no_buyer/... -run MinOrderSize` (label: `venue-min`)
- [x] [AFK] No-regression: M0–M4 gates (`config`, `runmodes`, `clob-meta`, `redeem`, `midpoint`) remain green

---

## M6 — Rung 6 — Market eligibility filter and discovery

**Builds on:** M5 — Rung 5 — Venue minimum order size from CLOB metadata (and M4 token/outcome + NO midpoint, M3 redemption, M0–M2 skeleton/modes/client extensions) · **Exercises:** §Market Eligibility, §5. Discover Eligible Markets, §Goals

Status: ☑ Accepted

### Build

- [x] [AFK] Discover candidate markets through the existing repo `polymarket/` market/event listing facilities (no new listing API); collect the full set of live markets to be filtered.
- [x] [AFK] Implement the full eligibility predicate, requiring ALL of: market is active, accepting orders, and unresolved; it is binary YES/NO with exactly identifiable YES and NO token IDs (reusing M4 deterministic decoding); close time is in the future; close time is strictly more than 48 hours from run time; close time is strictly less than 14 days from run time; reported USD liquidity is at least the configured minimum (`--min-liquidity`, default `$5,000`); NO midpoint price (reusing M4 `no_midpoint = (best_no_bid + best_no_ask)/2` from a usable two-sided book) is `> 0.89` and `<= 0.99`; and the account owns no YES shares in the market.
- [x] [AFK] Skip (never include) any market that fails any single criterion, lacks a usable two-sided NO order book, or is malformed/non-binary per M4, logging the specific rejection reason per market.
- [x] [AFK] Sort the surviving eligible markets by close time ascending (earliest close first) and expose them as the ordered eligible set consumed by later rungs.
- [x] [AFK] Emit a structured per-run summary including number of markets scanned and number eligible, plus a per-market accept/reject line carrying the deciding criterion.

### Self-check (headless)

- [x] [AFK] Add a table-driven test feeding synthetic markets that each violate exactly one criterion (inactive, not accepting orders, resolved, non-binary, duplicated/missing token IDs, close in past, close at 48h boundary, close at 14d boundary, liquidity below min, NO midpoint `== 0.89`, NO midpoint `> 0.99`, NO midpoint `<= 0.99` accepted edge, one-sided/no book, YES shares owned); assert each is rejected for the expected reason and an all-pass market is accepted.
- [x] [AFK] Assert boundary semantics exactly: close at exactly 48h is rejected (strictly greater required), close at exactly 14d is rejected (strictly less required), liquidity exactly equal to the minimum is accepted, NO midpoint of `0.99` is accepted and `0.89` is rejected.
- [x] [AFK] Assert the configurable `--min-liquidity` value is honored: lowering it admits a previously-rejected market and raising it rejects a previously-eligible one.
- [x] [AFK] Assert the returned eligible slice is sorted by close time ascending and stable, including ties, by feeding markets out of order and checking the output order.
- [x] [AFK] Run a one-shot `--dry-run` against a fixture/mock listing and assert stdout reports the scanned count, the eligible count, and one reason line per rejected market, with no order or transaction submitted.

### Gates

- [x] [AFK] `go build ./...`
- [x] [AFK] `go test ./... -run TestEligibility` (package: `apps/polymarket_no_buyer`)
- [x] [AFK] `go vet ./...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer` returns no files
- [x] [AFK] No regression: prior rung tests (M0–M5) still green via `go test ./...`

---

## M7 — Rung 7 — Cancel clearly stale open orders with precision-normalized comparison

**Builds on:** M3 — Rung 3 — Redeem closed/resolved positions at run start, M4 — Rung 4 — Deterministic token/outcome identification and live NO midpoint, M5 — Rung 5 — Venue minimum order size from CLOB metadata, and M6 — Rung 6 — Market eligibility filter and discovery · **Exercises:** §3 Cancel Clearly Stale Open Orders, §Comparison precision normalization (PRD lines 158–171), §Required features 9 and 33

Status: ☑ Accepted

### Build

- [x] [AFK] Fetch the account's open orders via the existing `polymarket/` client before any account-value sizing, and run the stale-cancellation pass strictly before reconciliation so the later pass sees the cleaned open-order set.
- [x] [AFK] Implement precision normalization helpers that round/quantize an order's limit price and size to Polymarket's accepted price tick and size precision (sourced from the CLOB metadata exposed in M2/M5) before any comparison; expose optional configurable comparison tolerances if required by the client.
- [x] [AFK] Cancel orders for markets that fail eligibility: closed, resolved, inactive, not accepting orders, outside the close-time window, below the liquidity threshold, or no longer binary YES/NO (reusing the M6 eligibility filter).
- [x] [AFK] Cancel orders for any market where the account owns YES shares.
- [x] [AFK] Cancel orders on the wrong side (not a NO buy) or wrong asset (not the market's NO token).
- [x] [AFK] Cancel NO buy orders whose normalized limit price differs from the latest normalized NO midpoint price.
- [x] [AFK] Cancel NO buy orders whose expiration differs from `market_close_time - 24h`.
- [x] [AFK] Cancel duplicate or conflicting NO buy orders for the same market, deterministically keeping at most one candidate order for the later reconciliation pass.
- [x] [AFK] Never cancel an order solely because its amount differs from the final desired amount; size reconciliation is deferred to the sizing step (M9). Emit a structured per-order log line recording each cancellation decision and its reason.
- [x] [AFK] In dry-run mode, log the cancellations that would happen with their reasons and submit no cancel requests; honor per-market failure isolation so one cancel error does not stop unrelated markets.

### Self-check (headless)

- [x] [AFK] Unit test the precision normalization helper: prices/sizes at, just below, and just above a tick boundary normalize to the expected accepted values, and two raw values that quantize to the same accepted value compare equal under the configured tolerance.
- [x] [AFK] Table-driven test feeding synthetic open orders that each isolate one stale reason (ineligible market, YES owned, wrong side, wrong asset, price != normalized midpoint, expiration != close-24h) asserts each is flagged for cancellation with the correct reason string.
- [x] [AFK] Assert an order whose normalized price equals midpoint and whose expiration equals `close-24h` for an eligible NO-token market is NOT canceled (the survivor/candidate).
- [x] [AFK] Assert that an order differing ONLY in amount from the desired size (price, side, asset, expiration all matching) is NOT canceled in this pass.
- [x] [AFK] Duplicate-order test: given N matching NO buy orders for one market, exactly one is kept as the candidate and the remaining N-1 are flagged for cancellation; the keep choice is deterministic across repeated runs.
- [x] [AFK] Dry-run test: with stale orders present, stdout/log lists each intended cancellation and reason and the client's cancel method is invoked zero times (verified via a fake/mock client call counter).
- [x] [AFK] Failure-isolation test: a forced cancel error for one market does not prevent cancellations being attempted for unrelated markets (assert subsequent cancels still issued, non-zero exit not required).

### Gates

- [x] [AFK] `go build ./...`
- [x] [AFK] `go test ./apps/polymarket_no_buyer/...`
- [x] [AFK] `go test -run TestStaleCancel ./apps/polymarket_no_buyer/...`
- [x] [AFK] `go vet ./apps/polymarket_no_buyer/...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer` prints nothing (no unformatted changed files)
- [x] [AFK] No-regression: prior rung suites (M0–M6) still pass via `go test ./...`

---

## M8 — Rung 8 — Account value snapshot: wallet USDC + owned-share valuation

**Builds on:** M3 — Rung 3 — Redeem closed/resolved positions at run start, M4 — Rung 4 — Deterministic token/outcome identification and live NO midpoint, M6 — Rung 6 — Market eligibility filter and discovery, and M7 — Rung 7 — Cancel clearly stale open orders with precision-normalized comparison · **Exercises:** §Account Value, §4. Snapshot Account Value, §Configuration (`POLYMARKET_NO_BUYER_USDC_TOKEN_ADDRESS`), §Logging, §Acceptance Criteria

Status: ☑ Accepted

### Build

- [x] [AFK] Compute the account-value snapshot only after the redemption pass (M3) and after clearly stale order cancellations are ordered (M7), per §Account Value: order cancellations do not change `wallet_usdc`, so the snapshot reflects the post-redemption position set with no compounding from the same run.
- [x] [AFK] Fetch `wallet_usdc` solely from the configured Polygon USDC wallet balance through the existing repo wallet/config helpers; never read a Polymarket, CLOB, or exchange-reported available-balance field. Use the existing Polymarket collateral USDC token as the default, overridable via `POLYMARKET_NO_BUYER_USDC_TOKEN_ADDRESS`.
- [x] [AFK] Fetch owned positions and value each owned share with the strict fallback chain: live NO/YES midpoint when available → best bid when midpoint is unavailable → zero when neither is available; log every best-bid and zero fallback with the condition ID and token.
- [x] [AFK] Compute `total_account_value = wallet_usdc + current_value_of_all_owned_shares` and emit a structured account-value log line including `wallet_usdc`, aggregated position value, and the total, per §Logging.
- [x] [AFK] Abort the buy pass (return/skip sizing) if `wallet_usdc` cannot be computed OR if total account value cannot be computed, per §4 and §Acceptance Criteria; the abort must log a clear reason and must not fall back to a hardcoded or exchange-reported value.

### Self-check (headless)

- [x] [AFK] Table-driven test of the per-share valuation function: a position with a usable two-sided book uses midpoint; a position with bids only but no usable midpoint uses best bid; a position with neither falls back to zero — assert the chosen price and that the zero/best-bid cases emit the fallback log.
- [x] [AFK] Assert `total_account_value` equals `wallet_usdc` plus the sum of per-position values for a fixture of mixed positions, byte-exact on the computed total.
- [x] [AFK] Assert `wallet_usdc` is sourced through the repo wallet helper against the configured/overridden USDC token; inject a fake exchange/CLOB available-balance field and assert it is never read (value comes only from the Polygon wallet helper).
- [x] [AFK] Abort path: with the wallet helper returning an error (uncomputable `wallet_usdc`), assert the buy pass is aborted, no order sizing runs, and an abort-reason log line is emitted; separately, with positions valuation failing the total, assert the same abort.
- [x] [AFK] Ordering: assert the snapshot is computed after the redemption pass (M3) and after stale-cancel ordering (M7) by checking call sequencing on a fake client, and that cancellation does not mutate `wallet_usdc`.

### Gates

- [x] [AFK] `go build ./...` (workspace wrapper: `./scripts/workspace_go.sh build ./...`)
- [x] [AFK] `go test ./... -run TestAccountValue -v` in `apps/polymarket_no_buyer/`
- [x] [AFK] `go vet ./...` and `gofmt -l` reports no changed Go files
- [x] [AFK] No-regression: full `./scripts/workspace_go.sh test ./...` stays green

---

## M9 — Rung 9 — Per-market target sizing, committed exposure, and top-up

**Builds on:** M4 (Deterministic token/outcome identification and live NO midpoint), M5 (Venue minimum order size from CLOB metadata), M6 (Market eligibility filter and discovery), M7 (Cancel clearly stale open orders), M8 (Account value snapshot) · **Exercises:** §Per-Market Target (target_notional / target_no_shares, committed NO exposure, top-up, YES-owned skip), §Acceptance criteria (2% per-market target, committed-exposure top-up, YES-owned skip)

Status: ☑ Accepted

### Build

- [x] [AFK] Compute per-market `target_notional = total_account_value * POLYMARKET_NO_BUYER_TARGET_EXPOSURE_PCT` (default `0.02`) using the M8 account-value snapshot, then `target_no_shares = target_notional / latest_no_midpoint` from the M4 live NO midpoint; treat the 2% as a per-market (not per-run) target.
- [x] [AFK] Compute committed NO exposure per market as `held_no_shares + remaining_open_no_buy_shares`, where `remaining_open_no_buy_shares = original_size - size_matched` summed over live/open NO buy orders in that market (partially filled orders contribute only their unfilled remainder).
- [x] [AFK] Skip any market where the account owns YES shares entirely — return no sizing decision and emit a structured `skip` log with reason `yes_owned` before any committed-exposure or top-up math runs.
- [x] [AFK] When the account already holds/has committed NO exposure: skip the market if committed NO exposure `>= target_no_shares`; otherwise compute `topup_shares = target_no_shares - committed_no_shares` as the candidate top-up bringing committed exposure up to (never over) target.
- [x] [AFK] Skip the top-up (no order) when the required top-up order would be below the market's M5-resolved Polymarket minimum order size; log the skip with reason `topup_below_min`. Sizing is non-mutating here — it produces a per-market intended NO-share quantity (or an explicit skip) consumed by later rungs, and never places orders.

### Self-check (headless)

- [x] [AFK] `go test ./apps/polymarket_no_buyer/...` covers sizing arithmetic: given `total_account_value` and `no_midpoint`, assert `target_notional == value*0.02` and `target_no_shares == target_notional/no_midpoint` (bit-exact within the project's rounding helper), and that `POLYMARKET_NO_BUYER_TARGET_EXPOSURE_PCT` overrides the 0.02 default.
- [x] [AFK] Table-driven committed-exposure test: held-only, open-buy-only, and held+open-buy markets each yield `held + Σ(original_size - size_matched)`; a partially filled order (`size_matched > 0`) contributes only its remainder; a fully matched order contributes zero.
- [x] [AFK] Assert the YES-owned branch: a market with any YES shares returns a `skip` decision with reason `yes_owned` and the sizing/top-up path is never entered (no top-up shares computed).
- [x] [AFK] Assert top-up branching: committed `>= target_no_shares` returns skip (reason `at_or_over_target`); committed `< target` returns `topup_shares = target - committed`; a top-up below the M5 minimum order size returns skip (reason `topup_below_min`); assert the function never emits a quantity that would push committed exposure above `target_no_shares`.
- [x] [AFK] Assert idempotence/determinism: re-running sizing against an unchanged snapshot + book + position set yields byte-identical per-market decisions (no compounding of exposure across repeated calls).

### Gates

- [x] [AFK] `go build ./...` (workspace-clean via `./scripts/workspace_go.sh build ./...`)
- [x] [AFK] `go test ./apps/polymarket_no_buyer/...` (label: `sizing`)
- [x] [AFK] `go vet ./apps/polymarket_no_buyer/...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer` reports no files
- [x] [AFK] No-regression: M4–M8 suites (`midpoint`, `mos`, `eligibility`, `cancel`, `accountvalue`) stay green

---

## M10 — Rung 10 — Wallet-budget tracking and minimum-order/partial-fill cash handling

**Builds on:** M9 — Rung 9 — Per-market target sizing, committed exposure, and top-up; M8 — Rung 8 — Account value snapshot: wallet USDC + owned-share valuation; M5 — Rung 5 — Venue minimum order size from CLOB metadata · **Exercises:** §Sizing (minimum-order exception, top-up below-minimum skip), §Available Cash Handling (run_usdc_remaining, partial-fill, no portfolio-wide cap)

Status: ☑ Accepted

### Build

- [x] [AFK] Initialize a per-run `run_usdc_remaining` from the M8 `wallet_usdc` snapshot (Polygon collateral USDC via repo wallet/config helpers, never an exchange/CLOB-reported available balance) and decrement it by the notional of every order the run places or intentionally leaves open, so total planned new/maintained notional never exceeds the wallet balance.
- [x] [AFK] Implement the minimum-order-size exception for a new NO position: when the computed 2% `target_no_shares` order notional is below the market's resolved venue minimum order size, bump the order up to that minimum; skip the market (logging the reason) if `run_usdc_remaining` cannot cover the minimum order notional; and emit a log entry recording that the minimum-order exception was used.
- [x] [AFK] Implement partial-fill order sizing: when the full desired NO order cannot be funded from `run_usdc_remaining` but the remaining budget still covers the market's minimum order notional, place the largest valid order that does not exceed `target_no_shares` exposure and fits within `run_usdc_remaining`; if `run_usdc_remaining` is below the minimum order notional, skip the market.
- [x] [AFK] Carry the M9 below-minimum top-up skip rule into the budget loop (top-ups whose required order is below the venue minimum are skipped) and process eligible markets earliest-close-first, continuing past any per-market skip or order failure without aborting the pass.
- [x] [AFK] Enforce no portfolio-wide hard cap: the run keeps attempting to deploy wallet USDC across all eligible markets, constrained only by per-market target exposure, market eligibility, minimum order size, and `run_usdc_remaining` — there is no aggregate notional/position-count ceiling.

### Self-check (headless)

- [x] [AFK] Unit test that `run_usdc_remaining` starts equal to `wallet_usdc`, decreases by exactly each placed/maintained order's notional, and that the sum of all planned order notionals in a run is `<= wallet_usdc` even when many eligible markets would individually request funding.
- [x] [AFK] Unit test the minimum-order exception: a new NO market whose 2% order is below the venue minimum is bumped to exactly the minimum order size when `run_usdc_remaining` covers it, is skipped when it does not, and the bumped case emits the documented minimum-order-exception log line (assert on captured structured-log output).
- [x] [AFK] Unit test partial-fill sizing: with `run_usdc_remaining` set between the market minimum notional and the full desired notional, assert the placed order is the largest size that is both `<= target_no_shares` and affordable; with `run_usdc_remaining` below the market minimum notional, assert the market is skipped with the skip reason logged.
- [x] [AFK] Unit test loop ordering and resilience: eligible markets are handled earliest-close-first, and an injected skip/failure on one market does not prevent subsequent markets from being sized and funded; assert no aggregate cap is applied (a run with budget for N markets places across all N eligible markets).
- [x] [AFK] Assert `wallet_usdc` is sourced via the repo wallet/config Polygon-collateral helper and never from a Polymarket/CLOB exchange-reported balance field (e.g. fail the test if the budget path references an exchange available-balance accessor).

### Gates

- [x] [AFK] `go build ./...`
- [x] [AFK] `go test ./apps/polymarket_no_buyer/... -run 'Budget|MinOrder|PartialFill|Remaining'`
- [x] [AFK] `go vet ./apps/polymarket_no_buyer/...`
- [x] [AFK] `gofmt -l apps/polymarket_no_buyer` returns no changed files
- [x] [AFK] `go test ./...` (no regression across prior rungs)

---

## M11 — Rung 11 — Reconciliation pass: place/maintain NO buy limit orders at midpoint with GTD expiry

**Builds on:** M10 — Wallet-budget tracking and minimum-order/partial-fill cash handling (and the full chain M4–M10 for midpoint, eligibility, sizing, and stale-order cancellation) · **Exercises:** §"Place Or Maintain Orders" (step 6), §Implementation Notes (custom GTD, re-check before order), §Acceptance Criteria (matching-order maintenance, cancel+replace, midpoint price, close-24h expiry)

Status: ☑ Accepted

### Build

- [x] [AFK] Implement the reconciliation pass that iterates eligible markets earliest-close-first (the close-time-ascending ordering established in M6), processing each market independently so one failure cannot halt the rest.
- [x] [AFK] Immediately before handling each market's order, re-fetch the market/order book, recompute `no_midpoint`, and re-run the full eligibility checks; skip the market for ordering if the latest midpoint is no longer `> POLYMARKET_NO_BUYER_MIN_NO_MIDPOINT` and `<= POLYMARKET_NO_BUYER_MAX_NO_MIDPOINT`, or if any other eligibility check now fails (feature 27).
- [x] [AFK] Leave at most one existing open NO buy order unchanged when it already matches the desired side (buy), asset (NO token ID), normalized limit price (latest midpoint at accepted precision), remaining amount (per M9 target/top-up), and expiration (`market_close_time - POLYMARKET_NO_BUYER_ORDER_EXPIRY_BEFORE_CLOSE`); reserve its notional in `run_usdc_remaining` and emit the "existing matching order left unchanged" log (feature 28).
- [x] [AFK] When an open order diverges on limit price, amount, side, or expiration, cancel it and replace it with a freshly computed order rather than mutating in place (cancel+replace on divergence, feature 26).
- [x] [AFK] Place the NO buy limit order at the latest recomputed `no_midpoint` price (not a stale pre-pass value), using the M9/M10 sized amount bounded by `run_usdc_remaining` (feature 29).
- [x] [AFK] Set the order's custom GTD expiration to `market_close_time - POLYMARKET_NO_BUYER_ORDER_EXPIRY_BEFORE_CLOSE` via the M2 client extension, and skip placement entirely if that computed expiration is not strictly in the future (feature 30).
- [x] [AFK] In `--dry-run` mode, log every intended place/maintain/cancel-replace decision with condition ID, question, NO token ID, midpoint, shares, notional, close time, expiration, and `run_usdc_remaining` after reservation, without submitting any order or cancellation.

### Self-check (headless)

- [x] [AFK] Add a deterministic Go test driving the reconciliation pass against a mocked polymarket client and order book: assert markets are processed in close-time-ascending order and that each market triggers a fresh book/midpoint/eligibility re-check call before any order decision.
- [x] [AFK] Assert that a market whose re-checked midpoint falls to `<= 0.89` or `> 0.99` (or otherwise becomes ineligible) is skipped for ordering and places no new order, even though it passed the earlier discovery pass.
- [x] [AFK] Assert that an existing open order matching side+asset+normalized-price+remaining-amount+expiration is left unchanged (no cancel, no place calls issued) and that its notional is reserved against `run_usdc_remaining`.
- [x] [AFK] Assert that an existing open order diverging on price, amount, side, or expiration produces exactly one cancel followed by one place (cancel+replace), and that the replacement's limit price equals the latest normalized midpoint and expiration equals `market_close_time - 24h`.
- [x] [AFK] Assert the placed order's GTD expiration timestamp equals `market_close_time - POLYMARKET_NO_BUYER_ORDER_EXPIRY_BEFORE_CLOSE` to the second, and that a market whose computed expiration is in the past produces zero place calls.
- [x] [AFK] Run `go run . --once --dry-run` against the mocked/fixture client and grep stdout to confirm intended place/maintain/cancel-replace lines (with condition ID, midpoint, shares, notional, expiration, and post-reservation `run_usdc_remaining`) are printed while the mock records zero submitted orders or cancellations.

### Gates

- [x] [AFK] `go build ./...` (workspace) via `./scripts/workspace_go.sh build ./...`
- [x] [AFK] `go test ./...` for `apps/polymarket_no_buyer/...` and `polymarket/...` (label: `reconcile`) — includes the new reconciliation-pass tests and all prior M4–M10 gates with no regression.
- [x] [AFK] `go vet ./...` and `gofmt -l` report no issues on changed Go files.

---

## M12 — Rung 12 — Resilience, idempotent convergence, and gofmt hardening

**Builds on:** M11 — Rung 11 — Reconciliation pass: place/maintain NO buy limit orders at midpoint with GTD expiry (and the full M0–M11 stack it completes) · **Exercises:** §Failure Handling, §Acceptance Criteria, §Implementation Notes (determinism/idempotency, gofmt)

Status: ☑ Accepted

### Build

- [x] [AFK] Wrap every per-market step (redeem at run start, stale-order cancellation, target sizing, and buy/cancel-replace) in isolated error handling so a failure on one market logs the reason and is skipped, while all other markets continue to be processed — redemption failure, cancellation failure, buy-order failure, and market/orderbook fetch failure must each be non-fatal to the run (§Failure Handling, §Acceptance Criteria "Buy attempts continue after per-market failures").
- [x] [AFK] Abort the buy pass (no orders placed, no cancel-replace) when the account value snapshot or wallet USDC cannot be computed, because sizing cannot be trusted; emit a structured log explaining the abort and exit the pass cleanly without partial mutation (§Failure Handling "Missing account value or missing wallet USDC aborts the buy pass").
- [x] [AFK] Ensure deterministic, idempotent convergence: a second run against unchanged market/wallet/position state must leave existing matching orders unchanged and place no new orders, so repeated runs converge toward the same intended orders rather than compounding exposure (committed NO exposure = held shares + remaining open NO buys is honored on every run) (§Implementation Notes "deterministic and idempotent").
- [x] [AFK] Run `gofmt` on every Go file changed across the project so the entire `apps/polymarket_no_buyer/` tree and any touched `polymarket/` files are gofmt-clean (§Implementation Notes "Use gofmt on all changed Go files").

### Self-check (headless)

- [x] [AFK] Inject a fake polymarket client whose redeem, cancel, and place-order calls fail for a designated condition ID; run a one-shot pass over multiple eligible markets and assert via stdout/logs that the failing market is logged-and-skipped while every other market still produces its expected redeem/cancel/place action (run exit code 0).
- [x] [AFK] Drive the account-value snapshot helper to return an uncomputable value (and separately an uncomputable wallet USDC) and assert the buy pass aborts: no `place_order` / `cancel_order` calls are recorded on the fake client, and a structured "buy pass aborted" log line is emitted.
- [x] [AFK] Idempotency replay: run the reconciliation pass twice against a frozen fixture (same book, positions, wallet, orders). Assert the first run reaches the intended-order set and the second run records zero new placements and zero cancellations (existing matching orders left unchanged), proving convergence with no compounding exposure.
- [x] [AFK] Resilience cross-step: assert that a market/orderbook fetch error for one condition ID skips only that market and does not suppress orders for sibling markets, by diffing the per-market action log against the expected set.
- [x] [AFK] Run `gofmt -l apps/polymarket_no_buyer polymarket` (relative to repo root) and assert the command prints nothing (no unformatted files); fail the check if any path is listed.

### Gates

- [x] [AFK] `gofmt -l apps/polymarket_no_buyer polymarket` prints no files (gofmt-clean gate).
- [x] [AFK] `./scripts/workspace_go.sh vet ./...` and `go vet` in `apps/polymarket_no_buyer` pass clean.
- [x] [AFK] `./scripts/workspace_go.sh build ./...` succeeds across the workspace (no-regression build gate).
- [x] [AFK] `./scripts/workspace_go.sh test ./...` passes, including `go test ./apps/polymarket_no_buyer/...` covering the resilience, abort, and idempotency self-checks (no-regression test gate; all prior M0–M11 gates still green).

---

## Out of scope / future

- AI / LLM / agent loop / research component anywhere in the app — Explicit Non-Goal; app must be deterministic with no AI/LLM/research/subjective scoring (also enforced as acceptance criterion).
- Market research, thesis generation, or category-specific filtering — Explicit Non-Goal — the strategy is purely rule-based.
- Portfolio-wide exposure cap beyond wallet USDC and the per-market target — Explicit Non-Goal — strategy intentionally has no portfolio-wide hard cap.
- Database-backed audit log — Explicit Non-Goal in the first version; structured console logging is sufficient.
- New third-party Polymarket trading library — Non-Goal unless the existing repo client cannot support a required operation; otherwise extend the existing client.
- Test-only minimum order size fallback (POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK) — Optional and unset by default; production should fail closed rather than guess, so this is test-only.
- Configurable comparison tolerances for price/size precision — Optional — only added if required by the existing client.

---

## Acceptance

The work is accepted when:

- [x] [AFK] All required rungs accepted (self-check + gates pass on each).
- [x] [AFK] Standing gates pass on the final rung.
