# Deep Research Engine — Technical Specification for .NET Reimplementation

## Overview

The deep research engine is a **schema-guided, iterative web research system**. Given a user query and a target JSON schema, it autonomously plans search queries, executes web searches, fetches full page content, evaluates completeness, and synthesizes a structured JSON output that conforms to the schema. It uses LLM calls (currently Gemini) for planning, completeness checking, and synthesis.

## Architecture

The engine follows a **pipeline of pluggable interfaces** coordinated by a central orchestrator loop:

```
                    ┌─────────┐
         ┌─────────│ Engine   │──────────┐
         │         └────┬─────┘          │
         │              │                │
    ┌────▼────┐   ┌─────▼─────┐   ┌─────▼──────┐
    │ Planner │   │ Searcher  │   │  Fetcher   │
    │  (LLM)  │   │(web search│   │(HTTP GET + │
    └─────────┘   │  API)     │   │ HTML→MD)   │
                  └───────────┘   └────────────┘
         │              │
    ┌────▼─────────┐    │
    │Completeness  │    │
    │  Checker     │    │
    │   (LLM)      │    │
    └──────────────┘    │
         │              │
    ┌────▼─────────┐    │
    │ Synthesizer  │    │
    │   (LLM)      │◄───┘
    └──────────────┘
```

All intermediate evidence is accumulated in a **Scratchpad** (thread-safe, keyed by objective + URL).

## Interfaces (5 total)

### 1. Searcher
```
Search(context, SearchRequest) → List<SearchHit>
```
Input: query string, objective key, depth, limit, guidance, excluded domains.
Output: list of `{URL, Title, Snippet, PublishedAt}`.

Two implementations exist:
- **Gemini Grounded Search**: Calls the Gemini API with `GoogleSearch` tool enabled. The LLM does the search and returns structured results. Also extracts URLs from `GroundingMetadata.GroundingChunks` in the response. Deduplicates by URL, merging title/snippet/date from both the JSON payload and grounding metadata.
- **Google Custom Search API**: Direct REST call to `https://www.googleapis.com/customsearch/v1` with `key`, `cx`, `q`, `num` params. Parses published dates from metatags (`article:published_time`, `og:updated_time`, `date`, `publishdate`, `pubdate`) in multiple formats (RFC3339, `yyyy-MM-dd`, RFC1123).
- **Fallback Searcher**: Tries primary (Gemini) first; if it errors or returns 0 results, falls through to the fallback (Google Custom Search).

### 2. Fetcher
```
Fetch(context, url) → FetchedDocument {URL, Title, Content}
```
HTTP GET with a Chrome-like User-Agent. Accepts HTML, markdown, and plain text.
- HTML is converted to markdown using an `html-to-markdown` library.
- Title is extracted via regex from `<title>` tag.
- Response body is limited to **8 MB**; final content capped at **2 MB**.
- Markdown cleanup: normalize line endings, collapse runs of 3+ blank lines to 2.

### 3. Planner (LLM)
```
Plan(context, PlanningRequest) → PlanningResult {Queries[], Reasoning}
```
An LLM call that receives the current query, schema, objectives, existing findings (compact: top 5 per objective with URL/title/score/excerpt/snippet), existing sources (top 8), round number, and excluded domains. Returns 3-6 diverse search queries, each tagged with an `objective_key`. Temperature: **0.2**. Max tokens: **2048**. Response is **structured JSON** (schema-constrained output).

The prompt instructs the LLM to:
- Think holistically — schema fields are interrelated, a single source may cover multiple fields
- Vary query angles (phrasings, sub-topics, source types)
- Avoid repeating queries that led to already-found sources
- Prefer recent/primary sources
- Target full-page content, not snippet aggregators

Only queries for objectives in the `MissingObjectives` list are accepted; others are discarded.

### 4. CompletenessChecker (LLM)
```
Check(context, CompletenessRequest) → CompletenessResult {Complete, MissingObjectives[], Reasoning}
```
An LLM call that evaluates whether collected evidence is sufficient to fill ALL fields of the target schema. Temperature: **0.0** (deterministic). Max tokens: **1024**. Response is **structured JSON**.

The prompt instructs the LLM to:
- Evaluate holistically, not field-by-field
- Not treat search snippets alone as sufficient evidence (require fetched page excerpts)
- Never rely on excluded domains
- If `complete=false`, return at least one `missing_objectives` entry describing remaining gaps

Fallback: if the LLM returns `complete=false` but empty `missing_objectives`, the engine populates it from any objective whose status is not "satisfied".

### 5. Synthesizer (LLM)
```
Synthesize(context, SynthesisRequest) → SynthesisResult {Output, Summary}
```
An LLM call that produces the final structured output matching the target JSON schema. Temperature: **0.1**. Max tokens: **4096**. The response schema is **dynamically generated** from the user's target JSON schema (see "Schema Conversion" below).

The prompt instructs the LLM to:
- Use ONLY provided evidence — no hallucination
- Set fields to null when evidence is insufficient
- Treat snippets as discovery hints only; rely on fetched excerpts for facts
- Never use excluded domains as evidence

Returns `{output: <schema-conforming object>, summary: <string>}`. Falls back to unconstrained JSON if the model rejects the structured schema.

## Core Data Types

### Objective
```
Key: string           // dot-separated path, e.g. "company.revenue"
Description: string   // human-readable description
Required: bool        // whether this is mandatory
```

### Finding (one piece of evidence)
```
ObjectiveKey: string
Query: string         // the search query that found this
URL: string
Domain: string        // extracted hostname
Title: string
Snippet: string       // search result snippet (cleared if page was fetched)
Excerpt: string       // fetched page content, truncated to MaxExcerptChars (default 1200)
Score: float64        // 0.0–1.0, rounded to 3 decimal places
Depth: int            // which round found this (0-indexed)
Rank: int             // position in search results (0-indexed)
RetrievedAt: DateTime
PublishedAt: DateTime
```

### Source (deduplicated from findings)
```
URL, Domain, Title, BestScore, PublishedAt
```

### ObjectiveResult
```
Objective: Objective
Status: "missing" | "partial" | "satisfied"
EvidenceCount: int
BestFinding: Finding?   // highest-scored finding
```
Status logic:
- `satisfied`: evidence count >= MinEvidencePerObjective (default 5)
- `partial`: evidence count > 0 but < threshold
- `missing`: evidence count = 0

## Scratchpad (Evidence Accumulator)

Thread-safe storage keyed as `map[objectiveKey][URL] → Finding`.

**Deduplication rule**: When inserting a finding for an objective+URL pair that already exists, the new one replaces the old only if:
1. Its score is strictly higher, OR
2. Its score is equal AND its excerpt is longer

**Snapshot** produces:
- `findings`: per-objective lists sorted by score descending, then by retrieval time descending, then URL ascending
- `sources`: deduplicated across all objectives by URL, keeping the highest score. Sorted by score descending, then URL ascending.

## Scoring Algorithm

Each search hit is scored with a weighted formula:

```
score = (0.45 × rankScore) + (0.35 × relevance) + (0.15 × trust) + (0.05 × recency) - (0.08 × depth)
```

Clamped to [0, 1], rounded to 3 decimal places.

If the page was successfully **fetched** (has an excerpt), the score gets a **+0.08 bonus** (capped at 1.0), and the snippet field is cleared.

### Sub-scores:

**Rank Score**: `1.0 / (rank + 1)` where rank is 0-indexed position in search results.

**Relevance (Token Overlap / Jaccard)**: Tokenize both the reference text (`baseQuery + objectiveKey + objectiveDescription`) and the hit text (`title + snippet`). Tokenization: lowercase, split on non-alphanumeric chars, discard tokens < 3 chars, remove stop words (`a, an, and, are, as, at, be, by, for, from, in, into, is, it, of, on, or, that, the, their, this, to, was, with`), deduplicate. Score = `|intersection| / |union|` (Jaccard similarity).

**Domain Trust Score**:
| Domain pattern | Score |
|---|---|
| `.gov`, `.edu` | 1.0 |
| Specific high-trust domains (reuters.com, apnews.com, bloomberg.com, wsj.com, ft.com, sec.gov, who.int, imf.org, worldbank.org) | 0.95 |
| `.org` | 0.8 |
| All other domains | 0.6 |
| Empty/unparseable URL | 0.4 |

**Recency Score**:
| Age | Score |
|---|---|
| ≤ 7 days | 1.0 |
| ≤ 30 days | 0.85 |
| ≤ 90 days | 0.70 |
| ≤ 365 days | 0.55 |
| > 365 days | 0.35 |
| Unknown date | 0.5 |

## Schema → Objectives Extraction

The engine walks the user-provided JSON schema tree recursively, extracting **leaf nodes** as objectives:

1. **Object nodes** (type="object" or has `properties`): recurse into each property. Child required-ness = parent required AND child is in the `required` array.
2. **Array nodes** (type="array" or has `items`): recurse into `items`, appending `[]` to the path.
3. **Leaf nodes** (string, number, boolean, or object/array with no children): emit as an Objective with the dot-separated path as key.

Descriptions are inherited from ancestors if the leaf has none. Objectives are deduplicated by key (required wins, first non-empty description wins), then sorted: required first, then alphabetically.

## Engine Main Loop

```
function Run(request):
    opts = request.Options with defaults
    objectives = extract from request.Objectives (or schema, or default to single "research" objective)
    scratchpad = new Scratchpad(objectives)
    unsatisfied = objectives  // all start as unsatisfied

    for depth = 0; depth <= MaxDepth AND unsatisfied is not empty; depth++:
        round = depth + 1

        // PHASE 1: PLANNING (if planner available)
        planningResult = planner.Plan(query, schema, objectives, unsatisfied,
                                       scratchpad.snapshot(), round, excludedDomains)
        plannedQueries = group planningResult.Queries by objective_key

        // PHASE 2: SEARCH + FETCH (parallel)
        findings = executeRound(unsatisfied, plannedQueries, opts)
        scratchpad.addMany(findings)

        // Recalculate unsatisfied based on evidence count
        unsatisfied = objectives where scratchpad.count(key) < MinEvidencePerObjective

        // PHASE 3: COMPLETENESS CHECK (if checker available)
        checkResult = checker.Check(query, schema, objectives, objectiveResults,
                                     scratchpad.snapshot(), round, excludedDomains)
        if checkResult.Complete:
            unsatisfied = empty → exit loop
        else:
            // Merge any NEW objectives the checker identified
            unsatisfied = merge(unsatisfied, checkResult.MissingObjectives)

    // PHASE 4: SYNTHESIS (if synthesizer available AND schema provided)
    synthesisResult = synthesizer.Synthesize(query, schema, objectives, findings, sources)
    result.Output = synthesisResult.Output
    return result
```

### executeRound (parallel search + fetch)

For each unsatisfied objective:
1. If the planner provided queries for it, use those. Otherwise, build a fallback query: `"{baseQuery} {objectiveKey} {objectiveDescription}"` (at depth > 0, also append `"detailed evidence source"`).
2. Execute searches **in parallel** with:
   - A **semaphore** limiting concurrency to `MaxWorkers` (default 6, max 32)
   - A **rate limiter** at `SearchesPerSecond` (default 3.0 QPS)
3. For each search hit:
   - Skip if URL is empty or domain is excluded
   - Compute score
   - If a Fetcher is available, fetch the full page. If the fetch succeeds and has content, replace the snippet with a truncated excerpt (MaxExcerptChars, default 1200 chars), add +0.08 to score. If fetch fails or returns empty content, **skip this hit entirely** (it's not added to findings).
   - Emit progress events

### Domain exclusion check
Normalizes both the hit domain and excluded domains by lowercasing and stripping `www.` prefix. Matches exact or suffix (e.g., excluding `example.com` also excludes `sub.example.com`).

### mergeMissingObjectives
When the completeness checker returns missing objectives, they may include:
- Objectives already known → added back to the unsatisfied list
- **Brand new objectives** not in the original list → created as new required objectives and added to both the master list and unsatisfied list. This allows the checker to dynamically expand the research scope.

## Configuration / Options (with defaults)

| Parameter | Default | Description |
|---|---|---|
| MaxDepth | 2 | Max number of additional rounds (loop runs depth 0..MaxDepth, so up to 3 rounds) |
| MaxWorkers | 6 | Parallel search concurrency (capped at 32) |
| SearchResultsPerQuery | 10 | Max hits per search query |
| MinEvidencePerObjective | 5 | Findings needed to mark an objective "satisfied" |
| MaxExcerptChars | 1200 | Truncation limit for fetched page excerpts |
| SearchesPerSecond | 3.0 | Rate limit for search + fetch calls |
| TimeoutSeconds | 300 | Overall timeout (not enforced in engine — caller should set context deadline) |
| ExcludedDomains | [] | Domains to block (normalized: lowercased, www-stripped, deduped) |

## LLM Model Configuration

Each LLM phase reads its model from an environment variable with a fallback chain:
1. Phase-specific env var: `DEEP_RESEARCH_PLANNER_MODEL`, `DEEP_RESEARCH_CHECKER_MODEL`, `DEEP_RESEARCH_SEARCH_MODEL`, `DEEP_RESEARCH_SYNTHESIZER_MODEL`
2. Global env var: `DEEP_RESEARCH_MODEL`
3. Hardcoded default: `gemini-3-flash-preview`

All LLM calls use Gemini's structured output mode (`ResponseMIMEType: "application/json"` + `ResponseSchema`). The synthesizer falls back to unconstrained JSON if structured output fails.

## Schema Conversion (JSON Schema → LLM Response Schema)

The synthesizer dynamically converts the user's JSON schema into the LLM's native schema format. Recursion depth is capped at 12. Key mappings:
- `type: "object"` → recurse into `properties`, carry `required` array
- `type: "array"` → recurse into `items`, carry `minItems`/`maxItems`
- `type: "string"` → carry `minLength`/`maxLength`, `pattern`, `format`
- `type: "number"/"integer"` → carry `minimum`/`maximum`
- `type: "boolean"` → direct map
- `anyOf` with `type: "null"` → marks the field as nullable
- `type: ["string", "null"]` (array of types) → picks the non-null type + marks nullable
- `enum` values → carried through as string array
- Property ordering is alphabetical

## Progress Events

The engine emits events throughout execution via an optional callback. Event stages:
`run_start`, `round_start`, `planned_query`, `search_start`, `source`, `warning`, `round_complete`, `run_complete`

Each event carries: stage, round (1-indexed), depth (0-indexed), objective key, query, URL, title, rank, warning message as applicable.

## LLM Prompt Templates

### Planner Prompt
The planner receives a prompt containing:
- Problem context (guidance or query)
- Current date/time (UTC + Eastern)
- The base user query
- Current round number
- Top 8 existing sources (title + URL)
- Current findings as compact JSON (top 5 per objective: URL, title, score, excerpt, snippet)
- Target JSON schema
- Instructions to return 3-6 diverse queries, all using the same objective_key, varying angles, avoiding already-found sources

### Completeness Checker Prompt
The checker receives:
- Problem context
- Current date/time
- Strict JSON contract to follow
- Base query
- Round number and total evidence count
- Current findings as compact JSON (top 5 per objective: URL, title, snippet, score)
- Target schema
- Instructions: evaluate holistically, require full-page excerpts (not just snippets), never rely on excluded domains

### Synthesizer Prompt
The synthesizer receives:
- Problem context
- Current date/time
- Excluded domains list
- Base query
- Number of rounds executed
- Objective status summary (key, status, evidence count, best source URL)
- Top 10 sources (title + URL)
- Current findings as compact JSON (top 3 per objective: URL, title, snippet, excerpt, score)
- Warnings
- Target schema JSON
- Instructions: use only provided evidence, set null for insufficient fields, rely on excerpts not snippets

### Gemini Search Prompt
The search prompt contains:
- Problem context
- Current date/time
- Objective key and depth
- The search query
- Excluded domains
- Requested result count
- Instructions: prefer URLs with substantial full-page content, avoid thin pages and snippet aggregators, prioritize official docs and primary reporting

## .NET Implementation Recommendations

1. **Define the 5 interfaces** (Searcher, Fetcher, Planner, CompletenessChecker, Synthesizer) as C# interfaces. They're the extension points.
2. **Scratchpad** can be a `ConcurrentDictionary<string, ConcurrentDictionary<string, Finding>>`.
3. **Parallel execution** maps well to `Task.WhenAll` + `SemaphoreSlim` + a custom token-bucket rate limiter.
4. **Scoring** is pure math — port the formula and lookup tables directly.
5. **Schema extraction** is a recursive tree walk over `JObject`/`JsonElement`.
6. **LLM calls** — replace Gemini with whatever LLM your .NET project uses. The key requirement is structured JSON output. If your LLM doesn't support schema-constrained output natively, parse the JSON response and validate it yourself.
7. **HTML-to-markdown** — use a library like `ReverseMarkdown` for the fetcher.
8. **Cancellation** — the Go code uses `context.Context`; map to `CancellationToken` throughout.
