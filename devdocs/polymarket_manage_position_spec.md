# polymarket_manage_position — Built-in Method Logic

This document describes the complete decision logic of the `polymarket_manage_position` built-in pipeline method, implemented in `gowild_agent_manager/pipelines_builtin_methods_manage_position.go`.

---

## Purpose

`polymarket_manage_position` is an autonomous position management method for a single Polymarket binary (Yes/No) market. Given a research thesis (estimated probability, confidence, reasoning) and capital context (AUM, max allowed position), it:

1. Determines which side (YES or NO) to trade
2. Computes a target position size
3. Sells opposite-side or excess same-side inventory
4. Places or maintains BUY orders to reach the target
5. Manages stale, conflicting, or oversized open orders
6. Records a structured market note for audit trail

The method is **idempotent**: repeated invocations converge toward the same target state rather than compounding. It computes the gap between the current held position and the desired target, and only acts on the delta.

---

## Entry Point and Dispatch

```
pipelineBuiltinPolymarketManagePosition(ctx, pe, run, step, params)
```

### Payload Detection

The method first tries to extract a structured `builtinPolymarketPayload` from `params`. It looks for:

1. A `"payload"` key containing the full payload object (or JSON string)
2. Any of the known payload keys directly in `params`: `estimated_probability`, `confidence`, `reasoning`, `remaining_capacity`, `resolution_date`, `tokens`, `question`, `aum`, `max_allowed`, `current_position`

If neither form is found, it falls back to two legacy code paths:

- **Legacy trade** — if `params` contains `action`, `price`, `size`, `side`, or `cancel_order_id`, it dispatches to `pipelineBuiltinPolymarketLegacyTrade` (direct place/cancel order)
- **Legacy state view** — otherwise, dispatches to `pipelineBuiltinPolymarketLegacyStateView` (read-only position/order listing)

### Payload Structure

```go
type builtinPolymarketPayload struct {
    ConditionID          string                      // Market condition ID (required)
    EstimatedProbability float64                     // Research estimate of YES probability [0,1]
    Confidence           float64                     // Confidence in the estimate [0,1]
    Question             string                      // Market question text
    Reasoning            string                      // Research reasoning summary
    RemainingCapacity    float64                     // Remaining position capacity in shares
    ResolutionDate       string                      // Market resolution date (YYYY-MM-DD or RFC3339)
    AUM                  float64                     // Total assets under management in USD
    MaxAllowed           float64                     // Maximum position size in shares
    CurrentPosition      float64                     // Current position size in shares
    Tokens               []builtinPolymarketTokenRef // YES/NO token ID mappings
}
```

---

## Initialization Phase

### 1. Company ID Resolution

Resolves which Polymarket company (trading account) to use, either from the `company_id` param or from the pipeline run context.

### 2. Client Creation

Obtains a `builtinPolymarketClient` for the resolved company ID — an interface wrapping the Polymarket CLOB API for positions, orders, prices, orderbook, and order placement.

### 3. Market Refresh

Calls `GetMarket(conditionID)` to refresh the payload with live market data:
- Updates `Question` from the live market
- Updates `ResolutionDate` from the market's `EndDate`
- Updates `Tokens` with live YES/NO token IDs from the market response

### 4. Sizing Context Hydration

When the payload is missing AUM or max_allowed (common when an upstream research pipeline only emits thesis fields), the method recovers capital context:

1. **Payload** — if AUM or max_allowed are already set, use them as-is
2. **Live cache** — check a 15-second in-memory cache of the most recent portfolio snapshot for this company
3. **Live snapshot** — fetch USDC balance + all positions, compute AUM from `position_value + liquid_usd_balance`, derive `max_allowed = AUM / 20`
4. **Current position fallback** — if balance lookup fails, cap is set to the current held position (no new exposure)

### 5. Thesis Input Resolution

Research inputs (estimated_probability, confidence, reasoning) can come from three sources, checked in order:

1. **Payload** — explicitly provided in the request params
2. **Latest note** — if no research fields are in the payload, the method loads the most recent structured market note (from `data.ListMarketNotes`) for this condition_id that has reusable thesis data (valid probability + confidence)
3. **Missing** — if neither source provides inputs, the method returns a `FAILED` status with an error explaining the requirement

---

## Position Sizing

### Capacity Derivation

```go
func deriveBuiltinPolymarketCapacity(payload, currentPosition) (maxAllowed, remainingCapacity)
```

Priority order for determining `maxAllowed`:
1. If `AUM > 0`: `maxAllowed = AUM / 20` (5% of AUM)
2. If `MaxAllowed > 0`: use directly
3. If `RemainingCapacity > 0`: `maxAllowed = currentPosition + remainingCapacity`
4. Otherwise: `maxAllowed = currentPosition` (hold only, no new exposure)

`remainingCapacity = max(maxAllowed - currentPosition, 0)` — always recomputed from live state.

### Confidence Scale

Maps confidence to a 0–1 scale that determines what fraction of `maxAllowed` to target:

| Confidence Range | Scale Range | Behavior |
|---|---|---|
| ≤ 0.45 | 0.0 | No trading — below minimum threshold |
| 0.45 – 0.50 | 0.0 → 0.25 | Linear ramp-in |
| 0.50 – 0.60 | 0.25 → 0.50 | Linear ramp |
| 0.60 – 0.80 | 0.50 → 1.0 | Linear ramp |
| > 0.80 | 1.0 | Full allocation |

### Thesis Drift

Compares the current research inputs against the most recent stored thesis note (within the last 14 days) to detect conviction weakening:

**Inputs:**
- Prior side, probability, confidence from the stored note
- Current side, probability, confidence from the payload
- Whether the thesis hash changed (combining condition_id, question, reasoning, probability, confidence, side, resolution_date)

**Severity calculation:**
```
negativeProbabilityDrop = max(priorSideProbability - currentSideProbability, 0)
negativeConfidenceDrop = max(-confidenceDelta, 0)
severity = (negativeProbabilityDrop / 0.18) * 0.65 + (negativeConfidenceDrop / 0.35) * 0.35
```
Clamped to [0, 1].

**Effects:**
- `RetentionScale = max(0.35, 1 - severity * 0.6)` — reduces target position when thesis weakens
- `BlockNewExposure = true` when severity ≥ 0.6 AND probability drop ≥ 0.05 — prevents adding new shares
- Side flip detected → noted but doesn't directly block (the new side's logic handles it)

**Final position scale:**
```
positionScale = baseConfidenceScale × thesisDrift.RetentionScale
```

### Target Position

```
targetPosition = max(maxAllowed × positionScale, 0)
```
Capped at `maxAllowed`. If no AUM/capacity signals exist at all, target equals the current held shares (hold-only mode).

---

## Trade Side Selection

### Trade Candidate Selection

Compares the research probability against live bid/ask quotes for both YES and NO tokens:

```
yesEdge = estimatedProbability - yesAskPrice
noEdge = (1 - estimatedProbability) - noAskPrice
```

Selects the side with the **higher absolute edge**. Also computes relative edge: `edge / askPrice`.

If quotes fail to load from the price API, falls back to market metadata (best bid/ask, outcome prices, or market probability).

### Fallback Side

If no quotes are available at all, falls back purely on the estimated probability:
- `estimatedProbability ≥ 0.5` → YES
- `estimatedProbability < 0.5` → NO

---

## Execution Signal (Spread and Slippage Adjustment)

Before deciding to trade, the method evaluates whether the theoretical edge survives real market microstructure costs.

### Orderbook Metrics

Fetches the full orderbook for the target-side token:
- Best bid/ask prices and sizes
- Spread = bestAsk - bestBid
- Midpoint = (bestAsk + bestBid) / 2

### Cost Components

**Spread cost:** `spread / 2` (half-spread, representing crossing cost)

**Slippage penalty:** Based on how the desired order size compares to best ask depth:
- `desiredShares / bestAskSize ≤ 1` → no slippage
- `1 < ratio ≤ 2` → `spread × 0.25 × (ratio - 1)`
- `ratio > 2` → `spread × 0.25 + min(0.05, 0.01 × (ratio - 2))`

If no orderbook depth is available but the book exists: flat `max(spread × 0.5, 0.01)` penalty.

### Net Edge

```
netEdge = max(absoluteEdge - spreadCost - slippagePenalty, 0)
```

### Net Edge Scale

Maps net edge to a 0–1 execution scale:
- `netEdge ≤ 0.01` → scale = 0 (no trade)
- `netEdge ≥ 0.06` → scale = 1 (full size)
- Between: linear interpolation `(netEdge - 0.01) / 0.05`

The final desired buy size = `targetExposureGap × executionSignal.Scale`.

---

## Order Execution Logic

### Pre-checks

#### Resolution Date Check
If the market's resolution date has passed:
1. Cancel all open BUY orders
2. Return status "neutral" with no further action

#### Conflicting Order Cleanup
Cancel any open BUY orders on the **opposite** side (e.g., if target is YES, cancel any NO BUY orders).

### Sell Logic

Two sell scenarios are evaluated in order:

#### 1. Sell Opposite-Side Inventory
If the account holds shares on the opposite side of the target, sell them all.

#### 2. Trim Excess Target-Side Inventory
If `targetHeldShares > targetPosition + 0.01`, sell the excess: `targetHeldShares - targetPosition`.

**Sell order mechanics:**
1. Check held shares and locked shares (shares committed to existing sell orders)
2. `sellableShares = heldShares - lockedShares`
3. If the requested sell is ≥ 80% of held shares or exceeds sellable shares, cancel existing same-side SELL orders first to free up inventory, then reload state
4. Price at the current bid (from quotes)
5. Place GTC SELL order
6. If rejected with "not enough balance": reload state, cancel any remaining SELL orders, retry with the updated sellable amount
7. If size is below venue minimum (5 shares): skip with a `min_order_blocked` annotation

### Buy Logic — Edge Eligibility Tiers

Two tiers of buy eligibility, with different aggressiveness:

#### Tier 1 — Aggressive (GTC at best ask)
Requirements:
- `netEdge ≥ 0.05` AND `relativeEdge ≥ 0.15`
- Spread not "too wide": `spread ≤ max(0.04, absoluteEdge × 0.9)`
- Book not "too thin for aggressive": `targetExposureGap ≤ bestAskSize × 8`

Order: **GTC** at the best ask price from the orderbook (or quote ask price).

#### Tier 2 — Passive (GTD at midpoint)
Requirements:
- `netEdge ≥ 0.02` AND `relativeEdge ≥ 0.10` (but `relativeEdge < 0.15` — i.e., doesn't meet Tier 1)
- USDC balance > 25% of AUM
- `midpointEdge ≥ 0.015` (edge measured from the orderbook midpoint, not the ask)

Order: **GTD** (Good-Til-Date) at the orderbook midpoint.

**Tight-band Tier 2 special case:** If `absoluteEdge < 0.03` but all Tier 2 conditions are met and USDC balance is sufficient, the trade is still eligible. This allows passive limit orders when the raw edge is narrow but the balance and midpoint edge support it.

### Buy Execution Steps

When a tier is active and `wantsNewExposure` is true:

1. **Size the order:**
   - `desiredSize = targetExposureGap × executionSignal.Scale`
   - Cap by affordable size: `min(desiredSize, usdcBalance / price)`
   - Tier 1 depth cap: `min(desiredSize, max(bestAskSize × 5, bestAskSize + 1))` — avoid moving the market

2. **Manage existing aligned BUY orders:**
   - Classify existing target-side BUY orders as "stale" (wrong price/type/tier) or "aligned"
   - Cancel stale orders and reload state
   - If aligned orders are **oversized** (total remaining > desiredSize + 0.01), cancel them and reload

3. **Place or hold:**
   - If aligned orders exist and total remaining < desiredSize: place an **additional** order for the gap
   - If aligned orders exist and total remaining ≥ desiredSize: **hold** (no action, keep existing orders)
   - If no aligned orders exist: place a **fresh** order for the full desired size

4. **Venue minimum enforcement:** Any buy order < 5 shares is blocked with `min_order_blocked` annotation.

### No-Trade Conditions

When the edge or execution conditions don't support a buy:

- Cancel any existing aligned BUY orders with a descriptive reason:
  - "edge no longer supports re-entry"
  - "thesis weakened materially"
  - "net edge after spread and slippage no longer supports re-entry"
  - "confidence below minimum trading threshold"
  - "no remaining capacity"
  - "target position already filled"

Similarly, when edge IS present and target inventory is held, cancel any stale target-side SELL orders that would lock shares against the thesis direction.

---

## Status Outcomes

The `status` field in the result indicates what happened:

| Status | Meaning |
|---|---|
| `placed` | A new BUY order was placed |
| `updated` | Orders were modified (cancellations, sells, or incremental buys on top of aligned orders) |
| `held` | Position or orders retained without changes |
| `neutral` | No position, no orders placed — edge/capacity/thesis didn't justify action |
| `FAILED: <reason>` | An error occurred during execution |

---

## Post-Execution

### Market Note Annotation

After every execution (via `defer`), the method writes a structured market note to the database containing:

- **Text note:** status, action taken, side, price, amount, edge, reasoning, errors
- **Structured metadata:** kind, status, action, side, question, reasoning, estimated_probability, confidence, current_position, max_allowed, remaining_capacity, price, edge, relative_edge, spread, orderbook_depth, market probability, volume, 24hr volume, liquidity, days to end, thesis hash, market fingerprint

Notes are **skipped** for low-volume markets (< $50,000 total volume).

The thesis hash is a stable hash of `(condition_id, question, reasoning, estimated_probability, confidence, side, resolution_date)` — used to detect thesis drift on subsequent runs.

### Completion Properties

Two market properties are set in the database:
- `last_managed_at` — current UTC timestamp
- `management_result` — `"true"` or `"false"` based on whether status starts with "FAILED"

These enable downstream pipelines (e.g., `find_markets`) to prioritize markets that haven't been managed recently.

---

## Constants

| Constant | Value | Purpose |
|---|---|---|
| `builtinPolymarketMinOrderShares` | 5.0 | Polymarket venue minimum order size |
| `builtinPolymarketThesisActiveDays` | 14 | Days a stored thesis note remains relevant for drift detection |
| `builtinPolymarketFindMarketsMinVolume` | 50,000 | Minimum market volume for note annotation |
| `builtinPolymarketSizingCacheTTL` | 15s | TTL for the in-memory portfolio sizing cache |

---

## Key Design Principles

1. **Idempotency** — The method computes the gap between current state and target state. Running it twice with the same inputs and market conditions produces no additional orders.

2. **Defensive order management** — Before placing any order, existing conflicting or stale orders are cancelled and state is reloaded to prevent double-counting.

3. **Thesis continuity** — Research inputs can come from the payload or from the most recent stored note, enabling "fire and forget" scheduling where only the condition_id needs to be passed.

4. **Graduated sizing** — Position size is the product of three independent scales: confidence scale (conviction), thesis drift retention (continuity), and execution signal scale (market microstructure). Each can independently reduce or zero out the trade.

5. **Execution quality gates** — Raw edge is decomposed into spread cost + slippage penalty + net edge. Only net edge drives trade decisions, preventing orders that look profitable on paper but would lose to market impact.

6. **Two-tier execution** — Aggressive fills (GTC at ask) for high-conviction + high-edge + liquid books; passive limits (GTD at midpoint) for moderate edge + sufficient capital. This avoids paying the spread when the edge is marginal.

7. **Venue minimum handling** — Orders below 5 shares are gracefully blocked rather than rejected, with diagnostic annotations explaining the block.

8. **Audit trail** — Every execution writes a structured note with full metadata, enabling portfolio-level analytics and thesis drift detection across runs.
