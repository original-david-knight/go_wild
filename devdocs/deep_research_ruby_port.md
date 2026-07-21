# Deep Research Ruby Port Specification

## Purpose

This document describes the current deep research system as it exists in the Go codebase on 2026-03-20. It is intended to give Ruby engineers enough detail to reproduce the existing behavior, including the parts that are deliberate, the parts that are integration-specific, and the parts that are inconsistent with older docs.

The main point: treat this document and the code as the source of truth, not `gowild_deep_research/README.md` or `gowild_deep_research/DEEP_RESEARCH_SPEC.md`. Those older docs describe a broader design than what is actually running now.

## Scope

The current system has four layers:

1. `gowild_deep_research/`
   Core engine, provider adapters, schema conversion, scoring, scratchpad, result types.
2. `gowild_agent_manager/`
   Method registry, admin/test endpoints, runtime wiring, broker tool dispatch.
3. `gowild_agent/`
   Dynamic deep research methods exposed as agent tools.
4. `gowild_agent_node/`
   A separate graph-node integration that runs deep research directly.

If the Ruby port is meant to replace the full product behavior, it needs all four.

## Canonical Source Files

Core engine:

- `gowild_deep_research/engine.go`
- `gowild_deep_research/types.go`
- `gowild_deep_research/schema.go`
- `gowild_deep_research/scratchpad.go`
- `gowild_deep_research/score.go`
- `gowild_deep_research/searcher_gemini.go`
- `gowild_deep_research/searcher_claude.go`
- `gowild_deep_research/fetcher_webpage.go`
- `gowild_deep_research/planner_gemini.go`
- `gowild_deep_research/checker_gemini.go`
- `gowild_deep_research/synthesizer_gemini.go`
- `gowild_deep_research/claude_helpers.go`
- `gowild_deep_research/model_config.go`
- `gowild_deep_research/rate_limit.go`

Manager and broker integration:

- `gowild_agent_manager/deep_research_methods_data.go`
- `gowild_agent_manager/service_deep_research_methods.go`
- `gowild_agent_manager/handlers_deep_research_methods.go`
- `gowild_agent_manager/deep_research_runtime.go`
- `gowild_agent_manager/deep_research_tools.go`
- `gowild_agent_manager/broker_tools_deep_research_methods.go`
- `gowild_agent_manager/broker_tools_policy.go`
- `gowild_agent_manager/broker_agent_tools_list.go`
- `gowild_agent_manager/capability_schemas.go`

Agent and node integration:

- `gowild_agent/tools_setup_broker.go`
- `gowild_agent/default_system_prompt.md`
- `gowild_agentic_loop/loop_run.go`
- `gowild_agentic_loop/loop_types.go`
- `gowild_agent_node/deep_research.go`
- `gowild_agent_node/types.go`
- `gowild_agent_node/events.go`

Fetcher dependency:

- `gowild_tools/web_reader.go`

Tests worth mirroring:

- `gowild_deep_research/engine_test.go`
- `gowild_deep_research/schema_test.go`
- `gowild_deep_research/searcher_gemini_test.go`
- `gowild_deep_research/searcher_claude_test.go`
- `gowild_deep_research/checker_gemini_test.go`
- `gowild_deep_research/synthesizer_gemini_test.go`
- `gowild_deep_research/fetcher_webpage_test.go`
- `gowild_deep_research/model_config_test.go`
- `gowild_deep_research/claude_helpers_test.go`
- `gowild_agent_manager/handlers_deep_research_methods_test.go`
- `gowild_agent_manager/broker_tools_deep_research_methods_test.go`

## High-Level Architecture

The product behavior is:

1. A deep research method is stored in the manager database.
2. The method defines:
   - a method/tool name
   - a human description
   - free-form instructions
   - an optional query template
   - an input JSON schema
   - a research output JSON schema
   - engine options
3. The method can be:
   - tested through manager HTTP endpoints
   - streamed through an NDJSON test endpoint
   - exposed as a dynamic tool to agents through the broker
4. At runtime, the manager builds:
   - a searcher
   - a webpage fetcher
   - optional reasoning stages: planner, completeness checker, synthesizer
5. The engine executes iterative search/fetch rounds and returns:
   - objective coverage
   - findings
   - deduplicated sources
   - summary
   - optional schema-conforming structured output
   - warnings

## Core Data Model

These are the types that matter for parity.

### Objective

- `key`: string
- `description`: string
- `required`: bool

### SearchRequest

- `query`
- `objective_key`
- `depth`
- `limit`
- `guidance`
- `excluded_domains`

### SearchHit

- `url`
- `title`
- `snippet`
- `published_at`

### FetchedDocument

- `url`
- `title`
- `content`

### Finding

- `objective_key`
- `query`
- `url`
- `domain`
- `title`
- `snippet`
- `excerpt`
- `score`
- `depth`
- `rank`
- `retrieved_at`
- `published_at`

### Source

- `url`
- `domain`
- `title`
- `best_score`
- `published_at`

### ObjectiveResult

- `objective`
- `status`: `missing`, `partial`, `satisfied`
- `evidence_count`
- `best_finding`

### Options

Fields:

- `max_depth`
- `max_workers`
- `search_results_per_query`
- `min_evidence_per_objective`
- `max_excerpt_chars`
- `excluded_domains`
- `searches_per_second`
- `timeout_seconds`
- `llm_backend`

Core defaulting in `Options.withDefaults()`:

| Field | Default | Notes |
| --- | --- | --- |
| `max_depth` | `2` | Important: the loop is inclusive, so default means 3 rounds: depths 0, 1, 2. |
| `max_workers` | `6` | Hard-capped to `32`. |
| `search_results_per_query` | `10` | |
| `min_evidence_per_objective` | `5` | Count is based on unique URLs per objective. |
| `max_excerpt_chars` | `1200` | |
| `searches_per_second` | `3.0` | Shared by searches and fetches. |
| `timeout_seconds` | `300` | Stored in options, but the engine does not enforce it directly. |
| `excluded_domains` | normalized | Lowercase, trim, strip `www.`, dedupe. |

Manager method runtime adds another layer before calling the engine:

- if `search_results_per_query == 0`, set it to `10`
- if `max_excerpt_chars == 0`, set it to `12000`

That means manager-run deep research methods default to much longer excerpts than the bare library.

### Request

- `query`
- `objectives`
- `schema`
- `options`
- `guidance`
- `progress` callback

### Result

- `query`
- `objectives`
- `findings`
- `sources`
- `summary`
- `rounds`
- `schema_satisfied`
- `missing_objective_keys`
- `output`
- `warnings`
- `started_at`
- `finished_at`

## Engine Behavior

### Entry Conditions

`Engine.Run()` requires:

- non-nil engine
- configured searcher
- non-empty `query`

If either is missing, it returns an error immediately.

### Objective Selection

This is a critical detail.

Current engine behavior is:

- it uses `Request.Objectives` if present
- it does not automatically call `ObjectivesFromSchema(Request.Schema)`
- if no objectives are supplied, it creates a single required objective:
  - `key = "research"`
  - `required = true`

So the default runtime shape is not "one objective per schema leaf". It is "single objective, schema passed to LLM phases for guidance and synthesis".

### Main Loop

The loop is:

```text
for depth := 0; depth <= max_depth && unsatisfied not empty; depth++
```

That means total rounds are `max_depth + 1`.

Per round:

1. emit `round_start`
2. optionally call planner
3. execute search and fetch work
4. add findings to scratchpad
5. recompute unsatisfied objectives from evidence counts
6. optionally call completeness checker
7. possibly merge checker-provided missing objectives
8. emit `round_complete`

At the start of each round, if `ctx.Err()` is non-nil, the engine returns:

- a partial `Result`
- the context error

If cancellation happens mid-round, partial findings from that round may still be included, because the engine finalizes from the current scratchpad state.

### Fallback Query Construction

If the planner does not provide queries for an objective, the engine generates:

```text
<base query> <objective key> <objective description>
```

For `depth > 0`, it appends:

```text
detailed evidence source
```

### Unsatisfied Objective Logic

The engine treats an objective as unresolved when:

```text
scratchpad.count(objective_key) < min_evidence_per_objective
```

This is strictly count-based.

### Checker Merge Logic

The completeness checker can return `missing_objectives`. The engine merges them as follows:

- if the key matches an existing objective, it is added back to the current unsatisfied list
- if the key is new, a new required objective is created
- if the existing objective had no description and the checker provided `question`, the description is filled in

This means the checker can dynamically expand the objective set.

### Result Finalization

At the end of the run, the engine always builds:

- `objective_results` from scratchpad counts
- `findings` snapshot
- deduplicated `sources`
- a default textual `summary`

Then:

- `missing_objective_keys` is set from the current unsatisfied list
- `schema_satisfied` is `len(missing_objective_keys) == 0`
- if synthesis succeeds, `output` is filled
- if synthesis returns a non-empty summary, it replaces the default textual summary

Important subtlety:

- `schema_satisfied` is driven by the unsatisfied list, not by objective status strings alone
- if the checker says `complete=true`, `schema_satisfied` can be `true` even when some `ObjectiveResult` entries are still `partial` by raw evidence count

## Search and Fetch Execution

### Search Job Model

`executeRound()` builds a list of search jobs:

- one job per objective per planner query
- or one fallback query per objective when there are no planned queries

### Concurrency Model

Concurrency is not per search hit. It is per search job.

Implementation details to preserve:

- one goroutine/task per search job
- a semaphore limits concurrent jobs to `max_workers`
- each job performs:
  - one search request
  - then serial fetches for each returned hit
- searches and fetches share the same rate limiter

### Rate Limiting

The engine uses one shared token bucket:

- rate = `searches_per_second`
- burst = `max(1, int(searches_per_second))`

The limiter is consumed for:

- each search call
- each fetch call

### Filtering

For each search hit:

- skip empty URL
- skip URLs whose domain matches `excluded_domains`

Excluded domain matching:

- lowercase
- strip `www.`
- exact host match or suffix match
- excluding `example.com` also excludes `sub.example.com`

### Finding Creation

The engine initially creates a finding from search metadata:

- snippet from search hit
- no excerpt yet
- score from ranking function

If fetch succeeds:

- title may be replaced with fetched title
- `excerpt = truncate(doc.content, max_excerpt_chars)`
- `snippet = ""`
- score gets a `+0.08` bonus, capped to `1.0`

If fetch fails:

- warning is recorded
- default behavior is to discard the finding

There are two exceptions:

1. If the search snippet length is at least 200 chars:
   - promote truncated snippet into `excerpt`
   - clear `snippet`
   - keep the finding
2. If the fetch error is HTTP 404 and the snippet is non-empty:
   - keep the finding with snippet-only evidence

If fetch succeeds but content is empty after trimming:

- warning is recorded
- finding is discarded

### Progress Events

Engine stages:

- `run_start`
- `round_start`
- `planned_query`
- `search_start`
- `source`
- `warning`
- `round_complete`
- `run_complete`

Each event can carry:

- `stage`
- `round`
- `depth`
- `objective_key`
- `query`
- `url`
- `title`
- `rank`
- `warning`

### Warning Behavior

Warnings are deduplicated by exact string equality.

404 fetch warnings are collapsed after each round:

- non-404 warnings stay as-is
- multiple 404 warnings become one summary line
- if 3 or fewer 404 URLs: all are listed
- if more than 3: list first 3 and append `(and N more)`

## Scratchpad Behavior

The scratchpad is a thread-safe map:

```text
objective_key -> url -> Finding
```

Deduplication rule per objective+URL:

- replace existing finding if new score is higher
- if scores are equal, replace only if new excerpt is longer
- otherwise keep existing finding

Snapshot behavior:

- findings are sorted by:
  1. score descending
  2. retrieved_at descending
  3. URL ascending
- sources are deduplicated across all objectives by URL
- source ordering:
  1. best_score descending
  2. URL ascending

Objective status calculation:

- `satisfied`: evidence count `>= min_evidence_per_objective`
- `partial`: evidence count `> 0` and below threshold
- `missing`: evidence count `== 0`

## Scoring

Use the actual code, not the older spec.

### Actual Formula

```text
score = 0.50 * rank_score
      + 0.35 * relevance
      + 0.15 * trust
      - 0.08 * depth
```

Then:

- clamp to `[0, 1]`
- round to 3 decimals
- if fetch produced content, add `0.08` and clamp again

### Rank Score

```text
1.0 / (rank + 1)
```

### Relevance

Jaccard similarity between token sets of:

- reference text:
  - `base_query + objective.key + objective.description`
- hit text:
  - `title + snippet`

Tokenization rules:

- lowercase
- split on non `[a-z0-9]`
- ignore tokens shorter than 3 chars
- remove stop words:
  - `a an and are as at be by for from in into is it of on or that the their this to was with`
- dedupe tokens

### Domain Trust

| Domain | Score |
| --- | --- |
| empty/unparseable | `0.4` |
| `.gov`, `.edu` | `1.0` |
| exact/suffix match of `reuters.com`, `apnews.com`, `bloomberg.com`, `wsj.com`, `ft.com`, `sec.gov`, `who.int`, `imf.org`, `worldbank.org` | `0.95` |
| `.org` | `0.8` |
| everything else | `0.6` |

### Notably Absent

The current score does not include recency. `published_at` is stored but not used in ranking.

## Search Providers

### Gemini Searcher

Implementation: `geminiGroundedDeepResearchSearcher`

#### Requirements

- `GEMINI_API_KEY`
- search model resolved by:
  1. `DEEP_RESEARCH_SEARCH_MODEL`
  2. `DEEP_RESEARCH_MODEL`
  3. `FAST_MODEL`

If no model env in that chain exists, the helper panics.

#### Runtime Behavior

- 120-second per-search context timeout
- if caller passes `limit <= 0`, default to `5`
- final limit clamped to `1..10`
- Gemini call uses:
  - `GoogleSearch` tool enabled
  - temperature `0.2`
  - max output tokens `4096`
- no structured response schema is actually attached, even though a helper exists for one

#### Parsing Behavior

The searcher:

1. reads candidate text
2. tries to extract JSON from that text
3. parses:
   - `results[].url`
   - `results[].title`
   - `results[].snippet`
   - `results[].published_at`
4. merges in URLs from Gemini grounding metadata

It deduplicates by exact URL string.

If the text JSON is truncated, it attempts to salvage complete objects from the partial array.

If a grounding URL is a Vertex redirect URL:

- it sends an HTTP `HEAD`
- timeout is 5 seconds
- it uses the `Location` header if the redirect resolves to a non-Vertex target

Published timestamps are parsed only if they are RFC3339.

If no hits survive parsing and grounding merge, the searcher returns an error.

#### Retry Behavior

Gemini calls are wrapped in a retry helper:

- initial call plus up to 5 retries
- exponential backoff
- base delay `3s`
- max delay `60s`
- jitter `30%`
- retries only for errors containing:
  - `429`
  - `resource_exhausted`
  - `rate limit`
  - `quota`

### Claude Searcher

Implementation: `claudeDeepResearchSearcher`

#### Requirements

- model resolved by:
  1. `DEEP_RESEARCH_SEARCH_MODEL`
  2. `CLAUDE_FAST_MODEL`

If neither exists, `NewClaudeSearcher()` panics.

#### Runtime Behavior

Claude searcher is configured to:

- use `WebSearch`
- disallow `WebFetch`
- use strict MCP config
- use the research output style
- set a 2-minute timeout on the Claude client

The prompt explicitly says:

- use Claude's built-in `WebSearch`
- do not fetch/open pages
- return JSON only

The parser:

- uses `extractJSON()` to tolerate prose or code fences
- parses `results[]`
- dedupes by exact URL
- if caller passes `limit <= 0`, default to `5`
- truncates to limit
- parses `published_at` only as RFC3339

There is no retry wrapper like Gemini has.

### Fetcher

The deep research fetcher is not a custom HTTP client. It is a wrapper around the existing `read_webpage` tool.

#### Deep Research Wrapper

`deepResearchWebpageFetcher`:

- calls `ReadWebpageTool`
- expects a successful tool result with:
  - `url`
  - `title`
  - `content`
- treats empty content as an error
- converts `"HTTP error: <code> ..."` strings into a typed `FetchHTTPError`

#### Underlying `read_webpage` Behavior

The Ruby port should either call an equivalent webpage-reader service or reproduce these semantics:

- only `http` and `https` URLs are allowed
- 30-second HTTP client timeout
- Chrome-like user-agent
- request headers:
  - `Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8`
  - `Accept-Language: en-US,en;q=0.9`
  - `Accept-Encoding: identity`
  - `Upgrade-Insecure-Requests: 1`
- response body read cap: `50 MB`
- accepted content types:
  - HTML
  - markdown
  - plain text
- HTML is converted to markdown using `html-to-markdown`
- relative markdown links/images are resolved to absolute URLs
- multiple blank lines collapse to double newlines
- title is extracted from HTML `<title>`
- markdown/plain-text output is truncated to `1 MB`
- raw HTML mode exists in `read_webpage`, but deep research does not use it

Reddit special case:

- for `reddit.com`, `www.reddit.com`, or `np.reddit.com`, it tries `old.reddit.com` first
- if old Reddit fails, it retries the original host

Compression note:

- `read_webpage` supports optional LLM compression of large pages
- deep research does not use that feature, because `NewWebpageFetcher()` passes `nil` as the compressor

## Reasoning Stages

### Planner

#### Gemini Planner

Requirements:

- `GEMINI_API_KEY`
- model resolved by:
  1. `DEEP_RESEARCH_PLANNER_MODEL`
  2. `DEEP_RESEARCH_MODEL`
  3. `SMART_MODEL`

If the model env chain is empty, helper code panics.

Runtime config:

- structured JSON output
- response MIME type `application/json`
- response schema attached
- temperature `0.2`
- max output tokens `16384`

Prompt inputs:

- guidance or query as "problem"
- current UTC/Eastern time string
- base query
- round number
- top 8 current sources
- compact findings JSON with top 5 findings per objective
- target schema JSON

Actual prompt quirk:

- it selects the first missing objective key only
- it instructs the model that every query must use that one `objective_key`

So although the planner result type supports multiple objectives, the prompt is effectively single-objective.

Planner output filtering:

- discard empty objective key or empty query
- discard objective keys not present in `MissingObjectives`
- planner reasoning is not preserved in the final result

### Completeness Checker

#### Gemini Checker

Requirements:

- `GEMINI_API_KEY`
- model resolved by:
  1. `DEEP_RESEARCH_CHECKER_MODEL`
  2. `DEEP_RESEARCH_MODEL`
  3. `FAST_MODEL`

Runtime config:

- structured JSON output
- response MIME type `application/json`
- response schema attached
- temperature `0.0`
- max output tokens `16384`

Prompt inputs:

- guidance or query
- current UTC/Eastern time string
- base query
- round number
- total evidence count
- compact findings JSON
- target schema
- excluded domains

Actual prompt quirk:

- it also chooses a single `objective_key` placeholder for the JSON contract
- it uses the last non-empty key seen in `ObjectiveResults`

Actual data quirk:

- the compact findings passed to the checker include:
  - URL
  - title
  - snippet
  - score
  - published_at
- they do not include `excerpt`

So the prompt says "require full-page excerpt evidence", but the checker is not actually given excerpts.

Fallback behavior:

- if checker returns `complete=false` and no `missing_objectives`
- the code synthesizes missing objectives from all non-satisfied objective results

Checker reasoning behavior:

- non-empty reasoning is appended to engine warnings as:
  - `checker_reasoning: <reasoning>`

### Synthesizer

#### Gemini Synthesizer

Requirements:

- `GEMINI_API_KEY`
- model resolved by:
  1. `DEEP_RESEARCH_SYNTHESIZER_MODEL`
  2. `DEEP_RESEARCH_MODEL`
  3. `SMART_MODEL`

Runtime config:

- first attempt:
  - structured JSON output
  - wrapper schema with `output` and `summary`
  - temperature `0.1`
  - max output tokens `16384`
- fallback attempt if structured output fails:
  - plain JSON only

Prompt inputs:

- guidance or query
- current UTC/Eastern time string
- excluded domains
- base query
- rounds executed
- objective status summary
- top 10 sources
- compact findings JSON with top 3 findings per objective
- warnings
- target schema JSON

Wrapper parsing behavior:

- preferred response shape:
  - `{ "output": ..., "summary": "..." }`
- if wrapper missing, parse raw JSON as the output object

If summary is present and non-empty, it replaces the engine's default summary.

## JSON Schema Handling

There are two separate schema behaviors in the system.

### 1. Method Input/Research Schemas in the Manager

Manager CRUD validates schemas at write time:

- input schema and research schema must be valid JSON
- they must decode to a JSON object
- they are compiled with `github.com/santhosh-tekuri/jsonschema/v5`
- compiled schemas are cached by exact JSON string

Input schema is used for validating method payloads.

Research schema is used later as the synthesis target.

### 2. Engine Schema Utilities

#### Objective Extraction Helper

`ObjectivesFromSchema()` exists and works like this:

- recurse through objects via `properties`
- recurse through arrays via `items`, appending `[]` to the path
- emit leaf objectives
- requiredness is inherited through the ancestor chain
- descriptions are inherited from parent nodes when the child has none
- dedupe by key
- `required=true` wins during dedupe
- first non-empty description wins during dedupe
- final ordering:
  - required objectives first
  - then alphabetical by key

Important: the main engine does not call this automatically today.

#### Synthesis Schema Conversion

The synthesizer converts JSON Schema to Gemini schema using a limited subset:

- object:
  - `properties`
  - `required`
  - `minProperties`
  - `maxProperties`
- array:
  - `items`
  - `minItems`
  - `maxItems`
- string:
  - `minLength`
  - `maxLength`
  - `pattern`
  - `format`
- number/integer:
  - `minimum`
  - `maximum`
- boolean
- null
- `enum`
- `anyOf` with `null`
- `type: ["x", "null"]`

Conversion details:

- recursion depth cap is `12`
- if depth is exceeded, return a generic object schema
- property ordering is alphabetical
- `required` is sorted alphabetically
- enum values are stringified, even if the original JSON Schema enum contained numbers or booleans
- if `type` is missing:
  - infer `object` from `properties`
  - infer `array` from `items`
  - infer `string` from `enum`
  - otherwise use `object`

## Model and Time Configuration

### Shared Current Time Formatting

Prompts use a helper that formats both:

- UTC
- America/New_York

The engine intentionally feeds current time into prompts to bias search/planning/synthesis toward recent information.

### Gemini Model Resolution

Helper resolution order:

1. phase-specific env
2. `DEEP_RESEARCH_MODEL`
3. tier env (`FAST_MODEL` or `SMART_MODEL`)

If none exist, the helper panics.

### Claude Model Resolution

The library helper used by `NewClaudeSearcher()` resolves:

1. phase-specific env
2. Claude tier env (`CLAUDE_FAST_MODEL` or `CLAUDE_SMART_MODEL`)

If none exist, it panics.

Manager runtime quirk:

- Claude planner/checker/synthesizer in `handlers_deep_research_methods.go` do not use the helper resolution chain
- they build Claude clients directly from:
  - `CLAUDE_SMART_MODEL`
  - `CLAUDE_FAST_MODEL`

So manager-side Claude reasoning phases ignore `DEEP_RESEARCH_*_MODEL`.

## Manager Method System

### Database Model

`DeepResearchMethod` stores:

- `method`
- `description`
- `instructions`
- `query_template`
- `input_schema_json`
- `research_schema_json`
- `options_json`
- `enabled`
- `last_tested_at`
- `created_at`
- `updated_at`

Primary key is `method`.

### Method CRUD API

Routes:

- `GET /api/deep-research-methods`
- `POST /api/deep-research-methods`
- `GET /api/deep-research-methods/{method}`
- `PUT /api/deep-research-methods/{method}`
- `DELETE /api/deep-research-methods/{method}`
- `POST /api/deep-research-methods/{method}/test`
- `POST /api/deep-research-methods/{method}/test-stream`

Validation rules:

- `method` required on create
- method name regex:
  - `^[a-zA-Z0-9._-]{1,128}$`
- `input_schema`, `research_schema`, `options` must be valid JSON objects if present
- `research_schema` may be omitted
- `query_template` may be omitted

Response shaping:

- CRUD responses return parsed `input_schema`, `research_schema`, and `options`, not the raw stored JSON strings
- timestamps are RFC3339 strings

### Query Resolution Rules

Method execution resolves query in this order:

1. `req.query` if non-empty
2. render `method.query_template` if present
3. `input["query"]` if non-empty
4. JSON-serialize the full input object

Template format:

- `{{field}}`
- nested lookup with dots, for example `{{company.name}}`

Rendering behavior:

- missing placeholders become empty string
- final output collapses repeated whitespace
- if template rendering becomes empty, fall back to steps 3 and 4

This means a method can be "JSON first" and use the full input payload as the search query when no template is supplied.

### Method Execution Flow

`runDeepResearchMethodTestWithProgress()` does the following:

1. Acquire a global semaphore:
   - env var: `DEEP_RESEARCH_MAX_CONCURRENT`
   - default: `1`
2. Validate input payload against method input schema, if any
3. Resolve the query
4. Parse the research schema into a Ruby hash/object map
5. Parse method options
6. Parse request override options
7. Merge options
8. Set guidance:
   - `instructions` if non-empty
   - else `description`
9. Apply manager-specific defaults:
   - `search_results_per_query = 10` if zero
   - `max_excerpt_chars = 12000` if zero
10. Build searcher, fetcher, planner, checker, synthesizer
11. Run the engine
12. Append provider warnings
13. Return full result for tests, or structured output only for broker tool calls

### Option Merge Semantics

Current override merge is partial.

Override can replace:

- `max_depth`
- `max_workers`
- `search_results_per_query`
- `min_evidence_per_objective`
- `max_excerpt_chars`
- `excluded_domains` (merged and deduped)
- `llm_backend`

Override does not replace:

- `timeout_seconds`
- `searches_per_second`

So request-level overrides cannot currently change those two fields.

### Timeout Behavior

Method runtime timeout is enforced by the manager with an outer context:

- default: `900 seconds`
- if method options include `timeout_seconds > 0`, use that

The engine itself does not apply `Options.timeout_seconds`.

### Backend Selection

Manager method runner currently requires `options.llm_backend` to be set to:

- `gemini`
- `claude`

If it is empty, the runner returns an error.

This is another important integration quirk:

- manual manager test runs need `llm_backend` in method options or request override
- broker tool runs inject it automatically based on the agent model provider

### Provider Construction Behavior

For `llm_backend = gemini`:

- searcher is required; fatal if unavailable
- fetcher is required; fatal if unavailable
- planner/checker/synthesizer build errors are captured and appended as warnings
- engine still runs with search+fetch only if those optional stages fail to build
- if a constructor helper panics because model env vars are missing, that panic is not converted into a warning

For `llm_backend = claude`:

- searcher is required; fatal if unavailable
- fetcher is required; fatal if unavailable
- planner/checker/synthesizer are always constructed as Claude clients
- build step does not return an error
- later model-call failures happen during execution instead

Provider warnings appended after the run:

- `search_provider: gemini_grounded_search` or `claude_web_search`
- `fetch_provider: read_webpage_tool_fetcher`

If Gemini optional reasoning phases fail to build:

- `planner unavailable: ...`
- `completeness_checker unavailable: ...`
- `synthesizer unavailable: ...`

### Manager Test Endpoints

#### Non-Streaming Test

`POST /api/deep-research-methods/{method}/test`

Behavior:

- rejects disabled methods
- parses optional body:
  - `query`
  - `input`
  - `options`
- runs method
- updates `last_tested_at`
- returns:
  - `ok`
  - `method`
  - `test`

Where `test` contains:

- `method`
- `query`
- `input`
- `result`
- `duration_ms`

#### Streaming Test

`POST /api/deep-research-methods/{method}/test-stream`

Behavior:

- content type: `application/x-ndjson`
- emits one JSON object per line
- event types:
  - `start`
  - `progress`
  - `done`
  - `error`

Envelope shape:

- `type`
- `method`
- optional `message`
- optional `event` (`deepresearch.ProgressEvent`)
- optional `test`
- optional `error`

This is the main product surface for live source/progress updates.

## Broker Tool Exposure

Deep research methods become agent tools through the broker.

### Tool Discovery

Data-access tool:

- `get_deep_research_method_tools`

Returned tool spec includes:

- `tool_name`
- `method`
- `description`
- `query_template`
- `provider = "deep_research"`
- `provider_source = "global_deep_research_methods"`
- optional `input_schema`
- optional `research_schema`
- optional `options`

Only methods with `enabled = true` are included.

### Agent Enablement Rules

Dynamic deep research method tools are opt-in.

They are available to an agent only when:

- the enabled tool set contains `deep_research`
- or it contains the exact tool name

If the agent has no enabled-tools config (`nil`), dynamic deep research methods are considered disabled.

### Agent Tool Descriptions

Broker-added tool descriptions include a cost warning:

- expensive
- multi-step
- 1 to 3 minutes
- multiple API calls
- do not call more than once on the same topic

This warning exists in the tool description and in the default system prompt. It is not the only enforcement mechanism, but it is part of the user-visible behavior.

### Broker Tool Execution Result

When an agent actually calls a deep research method tool through the broker, the response is:

```json
{
  "ok": true,
  "method": "<method name>",
  "result": <structured output only>,
  "duration_ms": <integer>
}
```

Important:

- broker tool calls intentionally return only `out.Result.Output`
- they do not return the full findings/sources/summary payload
- this is to avoid blowing up the agent context window

### Backend Resolution From Agent Model Provider

Before broker execution, the manager maps the agent's `model_provider` to the deep research backend:

- `anthropic` -> `claude`
- `gemini` -> `gemini`
- empty or unsupported provider -> error

The broker path therefore overrides method options and forces `llm_backend` from the agent record.

## Agent Loop Guardrails

There is a default cap on deep research tool calls inside the agentic loop:

- `DefaultMaxToolCalls = 10`

But the detection rule is prefix-based:

```text
tool name starts with "deep_research_"
```

This creates an important naming caveat:

- the platform allows deep research method names that do not start with `deep_research_`
- those still work as dynamic tools
- but they do not automatically get the default deep-research tool-call cap

For behavioral parity with current guardrails, method names should use the `deep_research_` prefix.

## Agent-Node Integration

`gowild_agent_node` has a separate integration path.

### Config Surface

Node type:

- `deep_research`

Research config fields:

- `max_depth`
- `objectives`
- `guidance`
- `timeout_seconds`

### Runtime Behavior

Node integration always builds the Gemini stack directly:

- `deepresearch.NewSearcher()` -> Gemini grounded search
- `deepresearch.NewFetcher()` -> webpage fetcher
- `deepresearch.NewGeminiPlanner()`
- `deepresearch.NewGeminiCompletenessChecker()`
- `deepresearch.NewGeminiSynthesizer()`

If any of those constructors fail, the node fails immediately.

This is stricter than the manager method runtime, which tolerates missing Gemini optional reasoning phases.

### Request Construction

Node integration:

- sets `query = prompt`
- adds one required objective per `research_config.objectives`
- if no objectives are supplied, the engine falls back to the single `"research"` objective
- passes `guidance`
- uses an outer context timeout

### Output Mapping

`NodeResult` mapping:

- `turn_count = result.rounds`
- if `result.output` exists:
  - JSON-encode it into `NodeResult.output`
- `NodeResult.text = result.summary`
- if the run was canceled but partial rounds exist:
  - `NodeResult.text` is prefixed with `[partial] `

### Progress Bridging

Engine progress is bridged into `DeepResearchProgressEvent` with:

- `node_id`
- `stage`
- `round`
- `query`
- `url`
- `objective_key`
- `warning`

## Known Behavioral Mismatches to Older Docs

These are the biggest places where the running code does not match older design docs.

1. The engine does not automatically derive objectives from the research schema.
2. Default behavior is one required objective: `"research"`.
3. The actual score formula has no recency term.
4. There is no Google Custom Search fallback implementation.
5. Gemini search does not use structured output schema enforcement, even though a helper exists.
6. The completeness checker prompt says it needs excerpt evidence, but the actual prompt payload omits excerpts.
7. Planner and checker prompts are effectively single-objective, even though some types suggest multi-objective support.
8. `timeout_seconds` exists in options, but the engine itself does not enforce it.
9. Request-level override merge ignores `timeout_seconds` and `searches_per_second`.
10. Manager method runtime requires `llm_backend`; empty backend is rejected.
11. Agent loop deep-research call caps only apply to tool names prefixed with `deep_research_`.
12. Claude reasoning phases in manager runtime do not use the same model-env fallback chain as the library helpers.

If the Ruby port aims for strict parity, preserve these. If the goal is a cleanup pass, treat each one as an intentional product decision and document the deviation.

## Recommended Ruby Module Breakdown

To keep parity while staying maintainable, mirror the current seams:

1. `DeepResearch::Engine`
2. `DeepResearch::Types`
3. `DeepResearch::Scratchpad`
4. `DeepResearch::Scoring`
5. `DeepResearch::Schema`
6. `DeepResearch::Searchers::Gemini`
7. `DeepResearch::Searchers::Claude`
8. `DeepResearch::Fetchers::Webpage`
9. `DeepResearch::Planner`
10. `DeepResearch::CompletenessChecker`
11. `DeepResearch::Synthesizer`
12. `DeepResearch::Manager::MethodRegistry`
13. `DeepResearch::Manager::MethodRunner`
14. `DeepResearch::Broker::ToolAdapter`
15. `DeepResearch::NodeIntegration`

## Recommended Parity Test Matrix

At minimum, the Ruby port should have tests for:

1. Single-objective default fallback when no objectives are supplied.
2. Inclusive depth semantics: `max_depth = 2` means 3 rounds.
3. Planner-provided queries overriding fallback queries.
4. Retry round when an objective remains below evidence threshold.
5. Deduplication by objective+URL.
6. Source deduplication across objectives.
7. Domain exclusion exact and subdomain matches.
8. Fetch success converting snippet evidence into excerpt evidence.
9. Fetch failure discarding findings by default.
10. Fetch 404 preserving snippet-only findings when snippet is non-empty.
11. Long-snippet promotion to excerpt on fetch error.
12. Partial result return on context cancellation.
13. Warning dedupe and 404 warning collapse.
14. Gemini search JSON extraction plus grounding URL merge.
15. Claude search JSON extraction through prose/code-fence wrappers.
16. Checker fallback when `complete=false` and no missing objectives are returned.
17. Synthesizer fallback from structured output to plain JSON.
18. JSON Schema conversion for nested objects, arrays, nullable anyOf, type arrays with null, enums, and depth cap.
19. Query template rendering with nested `{{a.b}}` placeholders.
20. Broker tool execution returning only structured output, not full findings.

## Implementation Priorities

If the Ruby team wants to phase the work:

Phase 1:

- method registry
- query resolution
- Gemini searcher
- webpage fetcher
- engine loop without planner/checker/synthesizer

Phase 2:

- planner
- completeness checker
- synthesizer
- schema conversion

Phase 3:

- broker tool exposure
- streaming progress endpoint
- agent-node integration

Phase 4:

- Claude backend parity
- guardrail parity
- warning and logging parity

## Bottom Line

The current product is best understood as a single-objective, schema-guided, iterative research engine with optional reasoning stages, wrapped inside a configurable manager-managed method system and exposed to agents as dynamic broker tools.

If the Ruby port reproduces:

- the exact engine loop semantics
- the search/fetch rules
- the scratchpad/scoring/dedup logic
- the method runner behavior
- the broker and node integration contracts

it will match current functionality closely enough to replace the Go implementation without surprising downstream callers.
