# polymarket_find_markets — Specification

A smart market discovery engine for Polymarket prediction markets. It searches, filters, scores, and ranks markets to surface the best trading candidates, avoiding markets the caller already holds, low-quality markets, and entire categories (sports, crypto, stocks).

## Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `query` | string | no | `""` | Keyword search query. If empty, scans all markets via event listing or cache. |
| `limit` | int | no | auto | Max markets to return. Auto-computed from portfolio metrics (see below). Floor: 10, cap: 50. |
| `company_id` | string | no | from context | Override which company's wallet/notes to use. |
| `stale_only` | bool | no | `true` | Only return markets not acted on in the last 7 days (tracked via market properties). |

### Limit auto-scaling

When `limit` is not provided or <= 0:

```
limit = usdc_balance / (aum / 40)
```

Where `aum = liquid_usd_balance + position_value`. Floored to 10, capped at 50. This means agents with more capital to deploy see more candidates.

## Data Sources

The tool needs access to three Polymarket APIs and a local database:

### Polymarket APIs

1. **SearchMarkets(query, limit)** — Keyword search. Returns markets matching a query string.
2. **ListEvents(limit, offset)** — Paginated event listing. Each event contains one or more markets. Used for broad scanning.
3. **ListMarkets(limit, offset)** — Paginated market listing. Fallback when no cache and no query.
4. **GetPositions()** — Current wallet positions (to exclude held markets).
5. **GetOrders(market)** — Current open orders (to exclude markets with pending orders).

### Local database tables

1. **Market notes** — Per-company research notes attached to markets by condition_id. Used for note freshness scoring and augmenting output.
2. **Market properties** — Per-company key-value metadata per market. Used for staleness detection (keys: `last_managed_at`, `last_policy_check`, `last_researched_at`).
3. **Market cache** — Local mirror of Polymarket's event listing (see Cache section).

## Scan Modes

The tool operates in one of three modes, chosen automatically:

### 1. `cached_events` / `cached_query` (preferred)

Uses a local database cache of Polymarket events. The cache syncs every 30 minutes by paginating through the ListEvents API and storing all markets in a `polymarket_market_cache` table. If a query is provided, the cached markets are filtered by full-text match (all query terms must appear in the normalized search text). Otherwise all cached markets are returned.

Cache sync behavior:
- If cache is fresh (synced within 30 minutes) and has active rows, use it directly.
- If cache is stale and has rows, use stale data and queue a background refresh (single-flight, 2-minute timeout).
- If cache is stale and empty, perform a synchronous sync before proceeding.
- Sync is performed inside a transaction: upsert all markets from events, delete any cached markets no longer present, record sync timestamp.

### 2. `query` (keyword search)

When the cache is unavailable and a query is provided:
- Call SearchMarkets for each query string (currently just the raw query; the query splitter returns a single-element list).
- Request up to `limit * 4` results per search (minimum 30, maximum 100).
- Sort results newest-first before filtering.

### 3. `all_markets` (full scan)

When no cache and no query:
- Paginate through ListMarkets in pages of 100.
- Sort each page newest-first.
- Stop when enough candidates are found or pages are exhausted.

## Filtering Pipeline

Every candidate market passes through these filters in order. A market is rejected if any filter triggers.

### 1. Deduplication
Skip markets already seen (by condition_id).

### 2. Market state
Reject if `!active`, `closed`, or `!accepting_orders`.

### 3. Category exclusion: Sports
Reject if the market's tags or text match sports keywords. Two methods:

**Tag matching** — reject if any tag slug or label matches: `sports`

**Text matching** — reject if question, description, or slug (normalized to lowercase, alphanumeric + spaces) contains any of:
```
nba, nfl, mlb, nhl, ncaa, soccer, football, baseball, basketball,
tennis, golf, cricket, ufc, mma, formula 1, f1, nascar, olympics,
fifa, premier league, champions league, super bowl, world cup,
world series, stanley cup, march madness, serie a, la liga,
bundesliga, copa del rey, europa league, conference league,
club world cup, fa cup, epl, mls
```

**Phrase matching** — also reject if text contains: "listed club", "officially crowned the winner", "league winner", or the pattern "will ... win the ... (league|cup|finals|title)".

### 4. Category exclusion: Crypto
Reject if tags match: `crypto`, `crypto-prices`, `bitcoin`, `ethereum`, `solana`

Or text contains:
```
crypto, cryptocurrency, bitcoin, btc, ethereum, eth, solana, xrp,
dogecoin, doge, cardano, ada, memecoin, token price, coin price
```

### 5. Category exclusion: Stocks
Reject if tags match: `stock-prices`, `stocks`, `equities`, `stock-market`

Or text contains:
```
stock price, stock prices, share price, share prices, equities,
equity, stock market, close at
```

### 6. Already held
Reject if the caller has a position with `size > 0` in this market.

### 7. Open orders
Reject if the caller has any open orders on this market.

### 8. Low volume
Reject if total volume < $50,000.

### 9. Missing end date
Reject if the market's end date cannot be parsed.

### 10. Already expired
Reject if end date is in the past.

### 11. Resolution too far out
Reject if end date is more than 6 months from now.

### 12. Recently touched (stale_only mode)
When `stale_only=true` (default), reject if any of these market property keys have a datetime value within the last 7 days:
- `last_managed_at`
- `last_policy_check`
- `last_researched_at`

## Scoring Algorithm

Surviving candidates receive a composite score (0.0 to 1.0) from seven weighted components:

### Component weights

| Component | Weight | Target/Scale |
|-----------|--------|--------------|
| Volume | 0.24 | $250,000 |
| Liquidity | 0.24 | $50,000 |
| 24h Activity | 0.15 | $50,000 |
| Spread | 0.14 | tiered |
| Time to resolution | 0.13 | tiered |
| Note freshness | 0.06 | 21-day scale |
| Listing recency | 0.04 | tiered |

### Volume, Liquidity, 24h Activity (log-normalized)

```
score = log(1 + value) / log(1 + target)
```

Clamped to [0, 1]. A market at the target value scores ~1.0. Markets above the target can exceed 1.0 but are clamped.

### Spread score (tiered)

| Spread | Score |
|--------|-------|
| 0 (no data) | 0.6 |
| <= 1 cent | 1.0 |
| <= 2 cents | 0.9 |
| <= 4 cents | 0.7 |
| <= 6 cents | 0.45 |
| > 6 cents | 0.15 |

Spread is computed as `best_ask - best_bid` when both are available. Fallback: derive from outcome prices as `abs((yes_price + no_price) - 1)`.

### Time to resolution score (tiered)

| Days to end | Score |
|-------------|-------|
| <= 0 | 0.0 |
| <= 2 | 0.35 |
| 3 - 14 | 1.0 (sweet spot) |
| 15 - 45 | 0.85 |
| 46 - 90 | 0.6 |
| > 90 | 0.25 |

### Note freshness score

Encourages markets the team hasn't researched recently:

| Condition | Score |
|-----------|-------|
| No notes exist | 1.0 |
| Last note >= 21 days ago | 1.0 |
| Last note < 21 days ago | `days_since_last_note / 21` (min 0.05) |
| Unparseable note date | 0.5 |

### Listing recency score (tiered)

| Days since listing | Score |
|--------------------|-------|
| <= 1 day | 1.0 |
| <= 7 days | 0.85 |
| <= 30 days | 0.6 |
| > 30 days | 0.3 |

Listing date is derived from the first parseable value among: `created_at`, `creation_date`, `start_date_iso`, `start_date`.

### Final formula

```
score = (volume * 0.24) + (liquidity * 0.24) + (activity * 0.15)
      + (spread * 0.14) + (time * 0.13) + (note_freshness * 0.06)
      + (recency * 0.04)
```

## Sorting

Results are sorted by:
1. Score descending (highest first)
2. Spread ascending (tighter is better, tiebreaker)
3. Newest first (by listing date, final tiebreaker)

## Output

### Per-market fields

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | Always `"polymarket"` |
| `id` | string | Polymarket market ID |
| `condition_id` | string | Market condition ID (primary identifier) |
| `question` | string | The market question |
| `description` | string | Market description text |
| `slug` | string | URL slug |
| `probability` | float | Yes-outcome price (0-1) |
| `tokens` | array | Per-outcome objects: `{outcome, token_id, price}` |
| `volume` | float | Total volume in USD |
| `volume_24hr` | float | Last 24h volume in USD |
| `liquidity` | float | Current liquidity in USD |
| `best_bid` | float | Best bid price |
| `best_ask` | float | Best ask price |
| `spread` | float | Ask - bid |
| `days_to_end` | float | Days until resolution |
| `end_date` | string | Resolution date (YYYY-MM-DD) |
| `resolution_date` | string | Same as end_date |
| `selection_score` | float | Composite score (0-1) |
| `neg_risk` | bool | Whether market uses NegRisk exchange |
| `current_position` | float | Always 0 (held markets are excluded) |
| `note_count` | int | Number of company notes on this market |
| `last_note_at` | string | ISO timestamp of most recent note (if any) |
| `max_allowed` | float | Position size cap: `aum / 20` (if aum > 0) |
| `remaining_capacity` | float | Same as max_allowed (no existing position) |
| `aum` | float | Total assets under management (if > 0) |

### Envelope fields

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | `"polymarket"` |
| `company_id` | string | Resolved company ID |
| `query` | string | Original query |
| `queries_used` | []string | Actual search queries executed |
| `scan_mode` | string | `"cached_events"`, `"cached_query"`, `"query"`, or `"all_markets"` |
| `limit` | int | Effective limit used |
| `markets` | array | The scored/filtered market list |
| `items` | array | Same as markets (alias) |
| `markets_found` | int | Length of markets array |
| `candidates_examined` | int | Total markets evaluated before filtering |
| `pages_scanned` | int | Pages fetched in all_markets mode |
| `cache_used` | bool | Whether the local cache was used |
| `cache_sync_performed` | bool | Whether a sync happened during this call |
| `cache_sync_queued` | bool | Whether a background refresh was queued |
| `cache_synced_at` | string | Timestamp of last cache sync |
| `cache_sync_error` | string | Error from last sync attempt (if any) |
| `cache_markets_loaded` | int | Markets loaded from cache |
| `stale_only` | bool | Whether staleness filtering was active |
| `min_volume` | float | Volume threshold used ($50,000) |
| `recent_note_days` | int | Note recency window (7 days) |
| `max_resolution_date` | string | Cutoff date (YYYY-MM-DD, 6 months out) |
| `skipped_sports` | int | Markets rejected as sports |
| `skipped_crypto` | int | Markets rejected as crypto |
| `skipped_stocks` | int | Markets rejected as stocks |
| `skipped_existing_orders` | int | Markets with open orders |
| `skipped_existing_shares` | int | Markets already held |
| `skipped_low_volume` | int | Markets below volume threshold |
| `skipped_recent_notes` | int | Markets with recent notes |
| `skipped_far_resolution` | int | Markets resolving too far out |
| `skipped_expired` | int | Markets already expired |
| `skipped_missing_end` | int | Markets with no parseable end date |
| `skipped_stale` | int | Markets recently touched (stale_only mode) |

## Market Cache

### Schema: `polymarket_market_cache`

| Column | Type | Description |
|--------|------|-------------|
| `id` | string (PK) | Condition ID |
| `market_id` | string | Polymarket market ID |
| `event_id` | string | Parent event ID |
| `event_title` | string | Parent event title |
| `event_slug` | string | Parent event slug |
| `question` | string | Market question |
| `description` | string | Market description |
| `created_at` | string | Market creation timestamp |
| `creation_date` | string | Alternate creation date |
| `start_date` | string | Start date |
| `start_date_iso` | string | ISO start date |
| `slug` | string | URL slug |
| `end_date` | string | Resolution date |
| `volume` | string | Total volume (string-encoded float) |
| `liquidity` | string | Current liquidity (string-encoded float) |
| `outcome_prices` | string | JSON-encoded price array |
| `outcomes` | string | JSON-encoded outcome labels |
| `clob_token_ids` | string | JSON-encoded token ID array |
| `active` | bool | Market is active |
| `closed` | bool | Market is closed |
| `accepting_orders` | bool | Market accepts orders |
| `neg_risk` | bool | Uses NegRisk exchange |
| `best_bid` | float | Best bid |
| `best_ask` | float | Best ask |
| `volume_24hr` | float | 24h volume |
| `tags_json` | string | JSON array of `{id, label, slug}` tags (merged from event + market) |
| `tag_slugs` | string | Space-separated sorted tag slugs (for filtering) |
| `search_text` | string | Normalized full-text: event title + slug + question + description + slug + tag labels/slugs |
| `image` | string | Market image URL |
| `icon` | string | Market icon URL |
| `is_sports` | bool | Pre-computed sports classification |
| `is_crypto` | bool | Pre-computed crypto classification |
| `is_stocks` | bool | Pre-computed stocks classification |
| `synced_at` | timestamp | When this row was last synced |

### Sync behavior

- **Interval**: every 30 minutes.
- **Freshness check**: last sync timestamp (stored in a settings table) + at least one active row.
- **Full sync**: paginate ListEvents (page size 100), upsert all markets, delete any cached rows no longer present in the API response. Runs inside a transaction.
- **Concurrency**: mutex-protected. Double-checked locking (check freshness before and after acquiring lock).
- **Background refresh**: if cache has data but is stale, a single-flight goroutine refreshes in the background (2-minute timeout). The current request uses stale data.
- **Error handling**: sync errors are recorded to a setting key. If sync fails and cache is empty, the error propagates to the caller.

### Cache query matching

When a query is provided, the normalized query is split into terms. A cached market matches only if ALL terms appear in its `search_text` field. Text normalization: lowercase, strip non-alphanumeric, collapse whitespace, pad with leading/trailing spaces.

### Cache sort order

Cached markets are sorted by:
1. Newest first (by creation date)
2. Liquidity descending
3. 24h volume descending
4. Total volume descending
5. Question alphabetically (final tiebreaker)

## Note Staleness Detection

When `stale_only=true`, the tool checks per-market properties (key-value store scoped to company + condition_id). If any of these keys have a datetime value newer than 7 days ago, the market is considered "recently touched" and skipped:

- `last_managed_at` — set when a position management pipeline acts on the market
- `last_policy_check` — set when a policy evaluation runs for the market
- `last_researched_at` — set when a research pipeline completes for the market

This prevents the tool from repeatedly surfacing markets that are already being actively managed.

## Text Normalization

Used for both category filtering and cache search matching:

1. Lowercase the input
2. Replace all non-alphanumeric characters with spaces
3. Collapse consecutive spaces
4. Pad with leading and trailing space (so keyword matching like `" nba "` works without partial word hits)

## Implementation Notes

- All floats are rounded to specified precision before output (scores to 4-6 decimals, prices to 4, dollar amounts to 2).
- Condition IDs and other string fields are always trimmed before comparison.
- The `tokens` array in each result is built by decoding the JSON-encoded `outcomes`, `clob_token_ids`, and `outcome_prices` strings from the market. The count is the max length of any of these three arrays.
- Probability is the price of the first outcome labeled "yes" (case-insensitive). Falls back to the first outcome's price if no "yes" label found.
- Spread fallback when bid/ask aren't available: `abs((yes_price + no_price) - 1)`.
- Position sizing: `max_allowed = aum / 20` — each position should be at most 5% of AUM.
- The `items` field is an alias for `markets` to support generic pipeline consumers that expect an `items` key.
