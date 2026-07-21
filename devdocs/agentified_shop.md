# Agentified Shop: Agent-Operated Shopify via Drop-Shipping

**Status:** Design Document
**Depends on:** A2A queue system, broker isolation model, agent manager

---

## 1. Overview

An autonomous Shopify store operated by a team of specialized agents. Each agent owns a slice of the e-commerce pipeline: product discovery, curation, listing creation, pricing, order fulfillment, customer service, marketing, and analytics. Drop-shipping removes physical inventory risk, making the entire value chain digital and agent-operable.

### Why drop-shipping for validation

Drop-shipping is the ideal bootstrap because:
1. Zero inventory risk --- agents only need to be competent at digital tasks.
2. Clean validation signal --- profitable after ad spend means the system works.
3. The entire pipeline is API-driven --- suppliers, Shopify, ad platforms all have programmatic interfaces.
4. Low capital requirement --- ad spend is the only cost, and it's tuneable.

### Agent team

| Role | Agent ID | Mode | Heartbeat | Tools | Rollout |
|------|----------|------|-----------|-------|---------|
| Product Scout | `scout` | worker | 1h | supplier, web_search | v1 |
| Curator | `curator` | worker | 30s | shopify (read), A2A | v1 |
| Listing Agent | `lister` | worker | 30s | shopify (write), supplier (images) | v1 |
| Fulfillment Agent | `fulfiller` | worker | 10s | shopify (orders), supplier (ordering) | v1 |
| Pricing Agent | `pricer` | worker | 5m | shopify (pricing), supplier (cost) | v2 |
| Customer Service | `support` | worker | 30s | shopify (customers, orders) | v2 |
| Marketing Agent | `marketer` | worker | 10m | ads (meta, google), shopify (analytics) | v2 |
| Analytics Agent | `analyst` | worker | 1h | shopify (analytics), spend ledger | v2 |

Initial scope is the 4 v1 agents (`scout`, `curator`, `lister`, `fulfiller`). The other 4 agents are added after v1 stabilizes.

---

## 2. Architecture

### 2.1 System diagram

```
                External                        Agent Manager (host)
                ───────                         ────────────────────
                                               ┌──────────────────────────────────┐
  Shopify ──webhook──► Webhook Router ──A2A──► │                                  │
                       (manager)               │  Pipeline Engine                 │
                                               │  - watches A2A completions       │
  Supplier APIs ◄──────────────────────────────│  - chains steps                  │
                                               │  - routes by role                │
  Ad Platforms ◄───────────────────────────────│                                  │
                                               │  Spend Governor                  │
                                               │  - per-agent daily limits        │
                                               │  - enforced at broker dispatch   │
                                               │                                  │
                                               │  Broker Service                  │
                                               │  - existing tool dispatch        │
                                               │  - + shopify/supplier/ads tools  │
                                               │  - + spend checks                │
                                               └──────────┬───────────────────────┘
                                                          │
                                    ┌─────────┬───────────┼───────────┬─────────┐
                                    ▼         ▼           ▼           ▼         ▼
                                 scout    curator      lister    fulfiller   marketer
                                 (worker)  (worker)    (worker)   (worker)   (worker)
                                    │         │           │           │         │
                                    └─────────┴───────────┴───────────┴─────────┘
                                              All via broker (A2A + tools)
```

### 2.2 How it maps to existing code

| Existing | Role in this design |
|----------|-------------------|
| `gowild_agentic_loop` | Unchanged. Drives each agent's LLM reasoning. |
| `gowild_agent` (REPL) | Extended with worker mode (new session type). |
| `gowild_agent_manager` | Extended with webhook router, pipeline engine, spend governor. |
| `gowild_agent/data` | New tables for spend ledger, pipelines, capabilities, webhooks. |
| A2A system | Used as-is for inter-agent job dispatch. Pipeline engine automates chaining. |
| Broker isolation | Used as-is. Shopify/supplier/ad API keys live on manager, never in containers. |

---

## 3. Core Changes (Non-Tool Work)

### 3.1 Worker mode (`gowild_agent/main_worker.go`)

A new agent session type that replaces the interactive REPL for autonomous agents. No readline, no human input. The agent continuously pulls and processes work.

**Activation:** CLI flag `-mode worker` (default remains `interactive`).

**Loop structure:**

```go
func runWorkerSession(ctx context.Context, agent *loop.AgenticLoop, agentID string) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-pollTimer.C:
            // 1. Claim A2A jobs
            jobs := claimA2AJobs(ctx, maxJobs, leaseSeconds)

            // 2. Process each job
            for _, job := range jobs {
                result := processJob(ctx, agent, job)
                completeJob(ctx, job.ID, result)
            }

            // 3. Check recurring tasks (same as heartbeat)
            processRecurringTasks(ctx, agent)

            pollTimer.Reset(heartbeatInterval)
        }
    }
}
```

**Key differences from REPL mode:**

| Aspect | Interactive (current) | Worker (new) |
|--------|----------------------|--------------|
| Input source | readline + heartbeat timer | A2A job queue |
| Heartbeat default | 15 minutes | Per-agent config (10s--1h) |
| Human in loop | Yes, always available | No, fully autonomous |
| Interjection | User can interrupt agent mid-run | Not applicable |
| Context management | Persistent across conversation | Fresh per job (or configurable) |

**Context strategy for workers:**

Workers can operate in two context modes:
- **Stateless** (default): Fresh history per job. Simple, no compaction needed, each job is independent.
- **Persistent**: Carry history across jobs. Useful for agents that need accumulated context (e.g., Curator remembering past decisions). Uses existing compaction when context grows.

Configured per agent in `data.Agent`:
```go
type Agent struct {
    // ... existing fields ...
    Mode              string        `json:"mode"`               // "interactive" or "worker"
    HeartbeatInterval time.Duration `json:"heartbeat_interval"` // per-agent override
    WorkerContextMode string        `json:"worker_context"`     // "stateless" or "persistent"
}
```

### 3.2 Method handlers (`gowild_agent/methods.go`)

Agents declare which A2A methods they handle. This enables:
1. The pipeline engine to route by capability, not hardcoded public keys.
2. Input validation before hitting the LLM.
3. Optional direct handlers that bypass LLM for mechanical tasks.

```go
type MethodHandler struct {
    Method      string
    Description string
    InputSchema map[string]any  // JSON Schema for param validation
    // If non-nil, executes directly without LLM.
    // If nil, builds a prompt from method+params and runs agentic loop.
    DirectFunc  func(ctx context.Context, params map[string]any) (map[string]any, error)
}

type MethodRegistry struct {
    handlers map[string]*MethodHandler
}
```

**Job processing with method dispatch:**

```go
func processJob(ctx context.Context, agent *loop.AgenticLoop, job A2AJob) JobResult {
    handler, exists := methodRegistry.Get(job.Method)

    if exists && handler.DirectFunc != nil {
        // Direct execution - no LLM, fast and cheap
        result, err := handler.DirectFunc(ctx, job.Params)
        return JobResult{Status: "succeeded", Result: result, Error: err}
    }

    // LLM-driven execution - build prompt from job
    prompt := buildJobPrompt(job, handler)
    history := []loop.Message{loop.NewUserMessage(prompt)}
    events := agent.Run(ctx, history)
    return consumeEventsToResult(events)
}
```

**Completion ownership --- worker loop only:**

The worker loop is the **sole owner** of A2A job completion. The LLM must never call
`a2a_complete_job` directly. If it did, the worker's unconditional `completeJob()` call
after `processJob` returns would attempt a second completion on an already-terminal job,
hitting `ErrA2AInvalidState`.

To enforce this:

1. **Strip `a2a_complete_job` from the LLM's tool set in worker mode.** When building
   the agentic loop for a worker, filter out `a2a_complete_job` (and `a2a_claim_jobs`,
   `a2a_extend_lease`) from the registered tools. The worker manages the full job
   lifecycle; the LLM only does reasoning and calls domain tools (shopify, supplier, etc.).

2. **Use a `submit_result` tool instead.** Register a synthetic local tool that the LLM
   calls to declare its structured output. The worker captures this and uses it as the
   completion payload:

```go
// Registered only in worker mode. Not a broker tool — runs locally.
type SubmitResultInput struct {
    Result map[string]any `json:"result" description:"Structured result for this job" required:"true"`
}

func (w *WorkerTools) SubmitResultTool(ctx context.Context, input SubmitResultInput) (*loop.ToolResult, error) {
    // Store result for the worker loop to pick up after the agentic loop finishes.
    w.pendingResult = &input.Result
    return loop.NewSuccessResult("Result recorded. You may provide a final summary."), nil
}
```

3. **`consumeEventsToResult` extracts the pending result.** After the agentic loop emits
   `DoneEvent`, the worker checks `workerTools.pendingResult`. If set, that becomes the
   completion payload. If the LLM finished without calling `submit_result` (e.g., it only
   produced text), the worker wraps the final text as `{"text": finalText}`.

4. **`buildJobPrompt` instructs the LLM to use `submit_result`.** The generated prompt
   ends with: "When done, call `submit_result` with a structured JSON object containing
   your output. Do NOT call `a2a_complete_job`."

This gives the worker a clean single-writer path:
```
claim → processJob (LLM calls domain tools + submit_result) → completeJob (worker sends result to A2A)
```

**Why both execution modes matter:**

- `create_shopify_listing` with fully structured input --- direct handler, no LLM needed, costs nothing.
- `review_product_candidate` --- needs LLM reasoning about market fit, competition, margins.
- `handle_customer_inquiry` --- needs LLM for natural language, but structured input (order data, customer message).

### 3.3 Agent capabilities (UI-configured, system-prompt-injected)

Capabilities define the A2A methods an agent can handle. They are configured per-agent through the manager web UI and propagated to the agent at startup via the broker.

#### 3.3.1 Manager Config Tab UI

The web UI's config tab provides an inline form for managing capabilities per agent. Each capability has three fields: **role** (e.g. "scout", "fulfiller"), **method** (e.g. "find_product_candidates", "fulfill_order"), and **description** (human-readable). Add and delete operations are immediate.

#### 3.3.2 Manager REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/agents/{id}/capabilities` | GET | List all capabilities for agent |
| `/api/agents/{id}/capabilities` | POST | Add a capability (`{role, method, description}`) |
| `/api/agents/{id}/capabilities/{capID}` | DELETE | Remove a specific capability |

Handlers: `gowild_agent_manager/handlers_capabilities.go`. Service layer: `gowild_agent_manager/service_capabilities.go` (delegates to `data.AgentService`).

#### 3.3.3 Database model

```go
// gowild_agent/data/models_shop.go
type AgentCapability struct {
    ID           string    `json:"id"`
    AgentID      string    `json:"agent_id"`
    Role         string    `json:"role"`
    Method       string    `json:"method"`
    Description  string    `json:"description"`
    RegisteredAt time.Time `json:"registered_at"`
}
```

**Table: `agent_capabilities`**

| Column | Type | Description |
|--------|------|-------------|
| id | text | Primary key |
| agent_id | text | FK to agents |
| role | text | Agent role name |
| method | text | A2A method this agent handles |
| description | text | Human-readable description |
| registered_at | timestamp | Creation time |

Service methods in `gowild_agent/data/service_capabilities.go`: `RegisterCapability`, `GetCapabilities`, `FindAgentByCapability`, `ClearCapabilities`.

#### 3.3.4 Broker tool

The `get_capabilities` tool is registered in `broker_tools_data_access.go`. Agents call it via the broker client to fetch their own capabilities. Returns `{capabilities: [{role, method, description}, ...]}`.

#### 3.3.5 System prompt injection

At agent startup (`tools_setup.go` `loadSystemPrompt`), capabilities are fetched via `get_capabilities` and appended to the system prompt as an "A2A Capabilities" section. Each method is listed with its role and description, telling the agent what jobs it should handle:

```
## A2A Capabilities

You are a worker agent. You handle the following A2A methods:

### find_product_candidates (role: scout)
Search for trending products using market analysis tools...
```

#### 3.3.6 Worker mode registration

In `main_worker.go`, the worker loop fetches capabilities via broker at startup and registers each one in the `MethodRegistry`. This enables the worker poll loop to dispatch incoming A2A jobs to the correct handler by method name.

#### 3.3.7 Pipeline engine routing

`FindAgentByCapability(ctx, role, method)` queries the capabilities table across all agents (not scoped to one agent) to resolve a target agent ID. The pipeline engine uses this to route pipeline steps by role+method rather than hardcoded agent IDs.

### 3.4 Pipeline engine (`gowild_agent_manager/pipelines.go`)

The pipeline engine watches A2A job completions and automatically chains the next step. It lives in the manager, not in any agent.

**Pipeline definition:**

```go
type Pipeline struct {
    ID    string
    Name  string
    Steps []PipelineStep
}

type PipelineStep struct {
    // Trigger: which completed method activates this step
    OnMethod string // e.g. "find_product_candidates"
    OnStatus string // "succeeded" (default), "failed", or "*"
    FromRole string // source agent role, or "*" for any

    // Action: what to do next
    ToRole     string // target agent role (resolved via capabilities)
    NextMethod string // method to submit
    // Transform the completed job's result into the next job's params.
    // Keys are dot-paths into the result; values are param names.
    // Special key "$" passes entire result as-is.
    ParamMap map[string]string
}
```

**Example pipeline definitions:**

```go
var ProductPipeline = Pipeline{
    ID:   "new_product",
    Name: "Product Discovery to Listing",
    Steps: []PipelineStep{
        {
            OnMethod:   "find_product_candidates",
            OnStatus:   "succeeded",
            FromRole:   "scout",
            ToRole:     "curator",
            NextMethod: "review_product_candidate",
            ParamMap:   map[string]string{"$": "candidate"},
        },
        {
            OnMethod:   "review_product_candidate",
            OnStatus:   "succeeded",
            FromRole:   "curator",
            ToRole:     "lister",
            NextMethod: "create_listing",
            ParamMap: map[string]string{
                "product":     "product",
                "supplier_id": "supplier_id",
                "target_price": "target_price",
            },
        },
        {
            OnMethod:   "create_listing",
            OnStatus:   "succeeded",
            FromRole:   "lister",
            ToRole:     "marketer",
            NextMethod: "create_initial_campaign",
            ParamMap: map[string]string{
                "shopify_product_id": "shopify_product_id",
                "product_title":      "title",
                "target_audience":    "audience",
            },
        },
    },
}

var OrderPipeline = Pipeline{
    ID:   "order_fulfillment",
    Name: "Order to Delivery",
    Steps: []PipelineStep{
        {
            OnMethod:   "shopify_order_created",  // from webhook
            OnStatus:   "succeeded",
            FromRole:   "*",                       // webhook-injected
            ToRole:     "fulfiller",
            NextMethod: "fulfill_order",
            ParamMap:   map[string]string{"$": "order"},
        },
    },
}
```

**Execution model --- callback-driven, not polling:**

The current A2A API has no list/query endpoint (`GET /api/v1/a2a/jobs` only supports POST for submission; `server.go:285-293`). The only way to retrieve a job is by exact ID (`GET /api/v1/a2a/jobs/{id}`). Polling for completions would require a new endpoint.

Instead, the pipeline engine uses the existing **callback mechanism** (`A2AJob.CallbackURL`). When the pipeline engine submits a chained A2A job, it sets the callback URL to the manager's own pipeline endpoint. When the agent completes the job, `agent_net` fires the callback (with retries; `a2a_callback.go`), and the manager receives the completion event push-style.

```go
// Pipeline engine receives completions via callback, not polling.
// Route: POST /pipeline/callbacks/a2a
func (pe *PipelineEngine) HandleA2ACallback(w http.ResponseWriter, r *http.Request) {
    // 1. Parse callback payload (A2A job response with status, result, error)
    // 2. Look up pipeline_step_run by a2a_job_id
    // 3. If no matching step, ignore (not a pipeline job)
    // 4. Extract result from response --- API returns decoded objects:
    //      response.request  → A2ARequestEnvelope (with .method, .params)
    //      response.result   → map[string]any (decoded, not raw JSON string)
    //      response.error    → *A2AErrorEnvelope (decoded)
    //    NOT the DB-level fields (request_json, result_json, error_json)
    // 5. Match against pipeline steps (on_method, on_status, from_role)
    // 6. If match: resolve target agent, map params, submit next A2A job
    //    (with callback_url pointing back to this handler)
    // 7. Update pipeline_run / pipeline_step_run state
    w.WriteHeader(http.StatusOK)
}
```

**Why callbacks over polling:**
- The A2A callback system already exists with retry, exponential backoff, and dead-letter handling (`service_a2a.go:417-473`).
- No new agent_net endpoint needed for the critical path.
- Push-based: pipeline steps chain within seconds of completion, not on a poll interval.
- The pipeline engine's callback URL is HTTPS (required by `service_a2a.go:88-89`), so the manager must be reachable via HTTPS from agent_net.

**Bootstrapping pipeline runs:**

The pipeline engine does NOT poll for ad-hoc completions. Pipelines are triggered in two ways:

1. **Webhook-initiated** (e.g., Shopify order → webhook router → A2A job with callback → pipeline tracks it).
2. **Recurring-task-initiated** (e.g., scout's daily `find_product_candidates` completes → agent calls `a2a_complete_job` → agent_net fires callback → pipeline chains to curator).

For recurring-task-initiated pipelines, the scout agent's system prompt instructs it to submit its own output as a self-A2A completion. The pipeline engine registers a "seed" callback URL when it creates the initial recurring task configuration.

**A2A response field mapping (critical for correct implementation):**

The `a2aJobToResponse` function in `handlers_a2a.go:268-307` decodes raw JSON columns into structured objects before returning them over HTTP:

| DB column (`A2AJob` struct) | HTTP response field (`a2aJobResponse`) | Type |
|-----------------------------|---------------------------------------|------|
| `RequestJSON` (string) | `request` | `A2ARequestEnvelope` with `.method`, `.params` |
| `ResultJSON` (string) | `result` | `map[string]any` (decoded object) |
| `ErrorJSON` (string) | `error` | `*A2AErrorEnvelope` with `.code`, `.message` |

The pipeline engine works with the **HTTP-level decoded objects**, not the raw JSON strings. When extracting params for the next step, it reads `response.result["candidates"]` (decoded map), not `response.result_json` (raw string).

**New agent_net endpoint (observability, not critical path):**

For dashboard visibility and debugging, add a query endpoint to agent_net:

```
GET /api/v1/a2a/jobs?status=succeeded&since=2026-02-09T00:00:00Z&limit=50
```

Query params: `status`, `since` (completed_at filter), `to_public_key`, `from_public_key`, `limit`, `offset`. This is used by the manager's Pipeline Monitor dashboard, not by the pipeline engine itself.

**Pipeline run tracking:**

**Table: `pipeline_runs`**

| Column | Type | Description |
|--------|------|-------------|
| id | text | Pipeline run ID (UUID) |
| pipeline_id | text | Which pipeline definition |
| trigger_job_id | text | A2A job that started this run |
| current_step | int | Index into pipeline steps |
| status | text | running, completed, failed |
| created_at | timestamp | When the run started |
| updated_at | timestamp | Last state change |

**Table: `pipeline_step_runs`**

| Column | Type | Description |
|--------|------|-------------|
| id | text | Step run ID |
| run_id | text | FK to pipeline_runs |
| step_index | int | Which step |
| a2a_job_id | text | A2A job created for this step |
| status | text | pending, running, succeeded, failed |
| started_at | timestamp | When the job was submitted |
| completed_at | timestamp | When the job completed |

### 3.5 Webhook router (`gowild_agent_manager/webhooks.go`)

An HTTP handler in the manager that receives external webhooks and converts them to A2A jobs.

**Route:** `POST /webhooks/:source/:event`

```go
func (h *WebhookHandler) HandleShopify(w http.ResponseWriter, r *http.Request) {
    // 1. HMAC verification (MUST reject before any other work)
    //    a. Read X-Shopify-Hmac-Sha256 header.
    //    b. If header is MISSING or EMPTY → 401 immediately. Do not fall through.
    //    c. Read raw body, compute HMAC-SHA256 with webhook secret, constant-time compare.
    //    d. If mismatch → 401 immediately.
    // 2. Parse webhook topic from X-Shopify-Topic header
    // 3. Extract X-Shopify-Event-Id for dedupe and idempotency
    // 4. Persist event to webhook queue table (pending)
    // 5. Return 200 immediately (fast ACK, async processing)
    // 6. Worker resolves target role/method and submits A2A job
    // 7. Mark event delivered/failed with retry metadata
}
```

**HMAC verification implementation (`webhooks_verify.go`):**

```go
func verifyShopifyHMAC(secret string, body []byte, r *http.Request) error {
    // The header MUST be present. A missing header is not "no signature to check",
    // it is "unsigned request" and MUST be rejected.
    sigHeader := r.Header.Get("X-Shopify-Hmac-Sha256")
    if sigHeader == "" {
        return fmt.Errorf("missing X-Shopify-Hmac-Sha256 header")
    }

    expected, err := base64.StdEncoding.DecodeString(sigHeader)
    if err != nil {
        return fmt.Errorf("malformed HMAC header: %w", err)
    }

    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    computed := mac.Sum(nil)

    if !hmac.Equal(computed, expected) {
        return fmt.Errorf("HMAC verification failed")
    }
    return nil
}
```

The webhook secret itself is loaded from `webhook_configs.hmac_secret` at startup. If the
secret is empty or unconfigured, the handler MUST refuse all requests for that source
(fail-closed), not silently skip verification.

**Webhook reliability and idempotency requirements (Shopify):**

1. HMAC verification is mandatory and fail-closed: reject if header is absent, reject if secret is unconfigured, reject if signature mismatches. No request reaches persistence or processing without passing HMAC.
2. Acknowledge webhook within 5 seconds; process asynchronously.
3. Deduplicate by `X-Shopify-Event-Id` using a unique DB constraint.
4. Make queue processing idempotent: retries must not create duplicate A2A jobs, and downstream write tools must pass idempotency keys.
5. Do not assume ordered delivery; always fetch latest Shopify object state before write actions.
6. Keep dead-letter records after max retry attempts and alert on backlog growth.
7. Run scheduled reconciliation for missed events (orders/inventory/fulfillment drift).

**Webhook config table: `webhook_configs`**

| Column | Type | Description |
|--------|------|-------------|
| id | text | Config ID |
| source | text | "shopify", "stripe", etc. |
| event | text | "orders/create", "products/update", etc. |
| target_role | text | Agent role to route to |
| target_method | text | A2A method to invoke |
| enabled | bool | Active flag |
| hmac_secret | text | Webhook verification secret |

**Webhook event table: `webhook_events`**

| Column | Type | Description |
|--------|------|-------------|
| event_id | text | `X-Shopify-Event-Id` (unique) |
| source | text | "shopify" |
| topic | text | Webhook topic |
| payload | jsonb | Raw webhook payload |
| status | text | pending, delivered, failed, dead_letter |
| attempts | int | Delivery attempts |
| next_retry_at | timestamp | Next retry schedule |
| created_at | timestamp | Received time |
| updated_at | timestamp | Last state change |

**Shopify webhook events we handle:**

| Shopify Topic | Target Role | A2A Method | Rollout |
|--------------|-------------|------------|---------|
| `orders/create` | fulfiller | `fulfill_order` | v1 |
| `orders/cancelled` | fulfiller | `cancel_order` | v1 |
| `products/update` | pricer | `check_price_change` | v2 |
| `inventory_levels/update` | pricer | `check_inventory` | v2 |
| `customers/create` | support | `welcome_customer` | v2 |
| `refunds/create` | support | `process_refund` | v2 |

### 3.6 Spend governor (`gowild_agent_manager/spend_governor.go`)

Middleware in the broker's tool dispatch that enforces per-agent, per-category budget limits. This is a hard enforcement layer --- agents cannot bypass it.

**Where it hooks in:**

```go
// In broker_tools_dispatch.go, before executing the tool:
func (h *BrokerToolsHandler) callTool(ctx context.Context, agentID string,
    svc *data.AgentService, toolName string, inputJSON []byte) (any, error) {

    // NEW: Check spend limits before execution
    if cost, category := h.spendGovernor.EstimateCost(toolName, inputJSON); cost > 0 {
        if err := h.spendGovernor.CheckBudget(agentID, category, cost); err != nil {
            return nil, err  // returns error to agent
        }
        defer h.spendGovernor.RecordSpend(agentID, category, cost, toolName)
    }

    // ... existing dispatch logic ...
}
```

**Spend categories and limits:**

| Category | Tools | Default Daily Limit | Notes |
|----------|-------|-------------------|-------|
| `ads` | `create_campaign`, `update_budget`, `create_ad` | $100 | Marketing Agent only |
| `orders` | `place_supplier_order` | $500 | Fulfillment Agent only |
| `shopify` | `create_product`, `update_product` | 100 calls | Listing Agent |
| `llm` | (implicit, all agents) | 1000 calls | Prevent runaway loops |

Limits are configurable per agent in the agent config. The spend governor reads them from the database.

**Spend ledger table: `agent_spend_ledger`**

| Column | Type | Description |
|--------|------|-------------|
| id | serial | Auto-increment |
| agent_id | text | Which agent |
| category | text | "ads", "orders", "shopify", "llm" |
| amount | numeric | Dollar amount or call count |
| tool_name | text | Which tool was called |
| created_at | timestamp | When the spend occurred |

**Querying spend:**

```go
func (g *SpendGovernor) GetTodaySpend(agentID, category string) float64 {
    // SELECT COALESCE(SUM(amount), 0) FROM agent_spend_ledger
    // WHERE agent_id = ? AND category = ? AND created_at >= today_utc_start
}

func (g *SpendGovernor) CheckBudget(agentID, category string, amount float64) error {
    spent := g.GetTodaySpend(agentID, category)
    limit := g.GetDailyLimit(agentID, category)
    if spent+amount > limit {
        return fmt.Errorf("BUDGET_EXCEEDED: %s daily limit for %s is $%.2f, already spent $%.2f",
            category, agentID, limit, spent)
    }
    return nil
}
```

### 3.7 Configurable heartbeat interval

Currently hardcoded as a CLI flag with a 15-minute default. Needs to be per-agent and stored in config.

**Change in `data.Agent`:**

Add `HeartbeatInterval` to the agent struct. The manager passes it as a CLI flag when starting the container:

```go
// In docker_lifecycle.go, when building container cmd:
if agent.HeartbeatInterval > 0 {
    args = append(args, "-heartbeat", agent.HeartbeatInterval.String())
}
if agent.Mode == "worker" {
    args = append(args, "-mode", "worker")
}
```

---

## 4. New Tool Packages

### 4.1 Shopify tools (`gowild_agent/tools/shopify/`)

All Shopify operations go through the Shopify Admin GraphQL API first. REST is fallback-only for endpoints not yet available in GraphQL. API credentials live on the manager; agents access via broker-proxied tools.

**Files:**

| File | Tools | Description |
|------|-------|-------------|
| `products.go` | `shopify_create_product`, `shopify_update_product`, `shopify_get_product`, `shopify_list_products`, `shopify_delete_product` | Product CRUD |
| `variants.go` | `shopify_update_variant`, `shopify_list_variants` | Variant pricing and inventory |
| `orders.go` | `shopify_list_orders`, `shopify_get_order`, `shopify_update_order`, `shopify_create_fulfillment` | Order management |
| `customers.go` | `shopify_get_customer`, `shopify_list_customers`, `shopify_search_customers` | Customer lookup |
| `inventory.go` | `shopify_get_inventory_level`, `shopify_set_inventory_level` | Stock management |
| `analytics.go` | `shopify_get_reports`, `shopify_get_orders_summary` | Sales data |
| `images.go` | `shopify_upload_image`, `shopify_list_images` | Product images |

**Example tool struct (products.go):**

```go
type ShopifyProductTools struct {
    client *ShopifyClient
}

type CreateProductInput struct {
    Title       string   `json:"title" description:"Product title" required:"true"`
    BodyHTML    string   `json:"body_html" description:"Product description in HTML" required:"true"`
    Vendor      string   `json:"vendor" description:"Product vendor/brand" required:"true"`
    ProductType string   `json:"product_type" description:"Product category" required:"true"`
    Tags        []string `json:"tags" description:"Product tags for organization"`
    Price       string   `json:"price" description:"Price in store currency" required:"true"`
    CompareAt   string   `json:"compare_at_price" description:"Original price for showing discount"`
    SKU         string   `json:"sku" description:"Stock keeping unit identifier"`
    ImageURLs   []string `json:"image_urls" description:"URLs of product images to upload"`
}

func (s *ShopifyProductTools) ShopifyCreateProductTool(
    ctx context.Context, input CreateProductInput,
) (*loop.ToolResult, error) {
    product, err := s.client.CreateProduct(ctx, input)
    if err != nil {
        return loop.NewErrorResult(err.Error()), nil
    }
    return loop.NewSuccessResult(map[string]any{
        "product_id": product.ID,
        "handle":     product.Handle,
        "status":     product.Status,
        "url":        product.URL,
    }), nil
}
```

**Shopify client (`shopify/client.go`):**

```go
type ShopifyClient struct {
    shopURL    string // e.g. "my-store.myshopify.com"
    apiVersion string // e.g. "2025-01"
    token      string // Admin API access token
    http       *http.Client
}
```

The client is instantiated in the manager's broker, not in agent containers.

### 4.2 Supplier tools (`gowild_agent/tools/supplier/`)

Starting with US/nearshore suppliers. TopDawg is the v1 primary provider. Keep the supplier abstraction so a backup US provider can be added without changing agent logic.

**Files:**

| File | Tools | Description |
|------|-------|-------------|
| `catalog.go` | `supplier_search_products`, `supplier_get_product`, `supplier_get_shipping` | Product discovery |
| `orders.go` | `supplier_place_order`, `supplier_get_order`, `supplier_cancel_order` | Order placement and lifecycle |
| `tracking.go` | `supplier_get_tracking` | Tracking updates |
| `providers/topdawg.go` | --- | TopDawg provider adapter (v1 primary) |
| `providers/us_*.go` | --- | Additional US supplier adapters (v2+) |
| `types.go` | --- | Common types across suppliers |

**Example tool struct:**

```go
type SupplierProductTools struct {
    client Supplier
}

type SearchProductsInput struct {
    Query            string  `json:"query" description:"Search keywords" required:"true"`
    Category         string  `json:"category" description:"Product category filter"`
    MinPrice         float64 `json:"min_price" description:"Minimum price in USD"`
    MaxPrice         float64 `json:"max_price" description:"Maximum price in USD"`
    MinRating        float64 `json:"min_rating" description:"Minimum product rating (0-5)"`
    MaxDeliveryDays  int     `json:"max_delivery_days" description:"Maximum estimated delivery days"`
    ShipsFromCountry string  `json:"ships_from_country" description:"Preferred origin country (default US)"`
    SortBy           string  `json:"sort_by" description:"Sort order" enum:"price_asc,price_desc,rating,orders"`
    Page             int     `json:"page" description:"Page number for pagination"`
}

type PlaceOrderInput struct {
    SupplierName    string `json:"supplier_name" description:"Configured supplier adapter key" required:"true"`
    ProductID       string `json:"product_id" description:"Supplier product ID" required:"true"`
    VariantID       string `json:"variant_id" description:"Supplier variant ID" required:"true"`
    Quantity        int    `json:"quantity" description:"Order quantity" required:"true"`
    ShippingName    string `json:"shipping_name" description:"Recipient name" required:"true"`
    ShippingAddress string `json:"shipping_address" description:"Full shipping address" required:"true"`
    ShippingCity    string `json:"shipping_city" required:"true"`
    ShippingState   string `json:"shipping_state" required:"true"`
    ShippingZip     string `json:"shipping_zip" required:"true"`
    ShippingCountry string `json:"shipping_country" description:"ISO country code" required:"true"`
    ShippingPhone   string `json:"shipping_phone" required:"true"`
    ShopifyOrderID  string `json:"shopify_order_id" description:"Linked Shopify order for tracking"`
}
```

**Supplier interface (required from day one):**

```go
type Supplier interface {
    SearchProducts(ctx context.Context, query string, opts SearchOpts) ([]Product, error)
    GetProduct(ctx context.Context, productID string) (*Product, error)
    GetShippingEstimate(ctx context.Context, productID, country string) (*ShippingEstimate, error)
    PlaceOrder(ctx context.Context, order OrderRequest) (*OrderConfirmation, error)
    GetOrder(ctx context.Context, orderID string) (*OrderStatus, error)
    GetTracking(ctx context.Context, orderID string) (*TrackingInfo, error)
}
```

### 4.3 Ads tools (`gowild_agent/tools/ads/`)

Meta (Facebook/Instagram) Ads and Google Ads. These are the highest-risk tools --- spend governor is mandatory.

**Files:**

| File | Tools | Description |
|------|-------|-------------|
| `meta_campaigns.go` | `ads_meta_create_campaign`, `ads_meta_update_campaign`, `ads_meta_get_campaign`, `ads_meta_pause_campaign` | Campaign management |
| `meta_adsets.go` | `ads_meta_create_adset`, `ads_meta_update_adset` | Audience targeting |
| `meta_creatives.go` | `ads_meta_create_ad`, `ads_meta_get_ad_performance` | Ad creation and stats |
| `google_campaigns.go` | `ads_google_create_campaign`, `ads_google_update_campaign` | Google Ads campaigns |
| `budget.go` | `ads_get_daily_spend`, `ads_get_campaign_roas` | Cross-platform spend tracking |

**Spend cap enforcement:**

Every ads tool is tagged with the `ads` spend category. The broker's spend governor intercepts all calls:

```go
// In spend_governor.go
var toolCategoryMap = map[string]string{
    "ads_meta_create_campaign":  "ads",
    "ads_meta_update_campaign":  "ads",
    "ads_meta_create_adset":     "ads",
    "ads_meta_create_ad":        "ads",
    "ads_google_create_campaign": "ads",
    "ads_google_update_campaign": "ads",
    "supplier_place_order":       "orders",
}

func (g *SpendGovernor) EstimateCost(toolName string, inputJSON []byte) (float64, string) {
    category, ok := toolCategoryMap[toolName]
    if !ok {
        return 0, ""
    }
    // For ads: extract budget from input params
    // For orders: extract order total from input params
    // For shopify: count-based (cost = 1.0 per call)
    ...
}
```

### 4.4 E-commerce analytics tools (`gowild_agent/tools/ecommerce/`)

Cross-cutting tools that combine data from Shopify, suppliers, and ad platforms.

**Files:**

| File | Tools | Description |
|------|-------|-------------|
| `pnl.go` | `ecommerce_product_pnl`, `ecommerce_daily_pnl` | Per-product and daily P&L |
| `pricing.go` | `ecommerce_calculate_margin`, `ecommerce_suggest_price` | Margin calculation |

**P&L computation:**

```go
type ProductPnLInput struct {
    ShopifyProductID string `json:"shopify_product_id" required:"true"`
    DateFrom         string `json:"date_from" description:"Start date (YYYY-MM-DD)" required:"true"`
    DateTo           string `json:"date_to" description:"End date (YYYY-MM-DD)" required:"true"`
}

// Output structure
type ProductPnL struct {
    ProductID     string  `json:"product_id"`
    Title         string  `json:"title"`
    Revenue       float64 `json:"revenue"`        // Shopify sales
    COGS          float64 `json:"cogs"`            // Supplier cost * units
    AdSpend       float64 `json:"ad_spend"`        // Attributed ad spend
    ShopifyFees   float64 `json:"shopify_fees"`    // ~2.9% + $0.30 per txn
    NetProfit     float64 `json:"net_profit"`      // Revenue - COGS - AdSpend - Fees
    Margin        float64 `json:"margin_pct"`      // NetProfit / Revenue
    ROAS          float64 `json:"roas"`            // Revenue / AdSpend
    UnitsSold     int     `json:"units_sold"`
    ReturnRate    float64 `json:"return_rate_pct"`
}
```

---

## 5. Broker Integration

All new tools are broker-proxied. The pattern is identical to existing tools.

### 5.1 Manager-side: tool dispatch

New handler files in `gowild_agent_manager/`:

| File | Handles |
|------|---------|
| `broker_tools_shopify.go` | All `shopify_*` tools |
| `broker_tools_supplier.go` | All `supplier_*` tools |
| `broker_tools_ads.go` | All `ads_*` tools |
| `broker_tools_ecommerce.go` | All `ecommerce_*` tools |

Each follows the existing pattern:

```go
func (h *BrokerToolsHandler) callShopifyTools(ctx context.Context, agentID string,
    toolName string, inputJSON []byte) (handled bool, result any, err error) {

    switch toolName {
    case "shopify_create_product":
        var input shopify.CreateProductInput
        if err := json.Unmarshal(inputJSON, &input); err != nil {
            return true, nil, err
        }
        return true, h.shopifyClient.CreateProduct(ctx, input)
    // ... other cases
    default:
        return false, nil, nil
    }
}
```

### 5.2 Agent-side: broker wrapper

New files in `gowild_agent/tools/broker/`:

| File | Wraps |
|------|-------|
| `shopify.go` | Shopify tools via broker |
| `supplier.go` | Supplier tools via broker |
| `ads.go` | Ads tools via broker |
| `ecommerce.go` | E-commerce analytics via broker |

Each follows the existing pattern (same as `broker/memory.go`, `broker/kg.go`, etc.):

```go
type ShopifyTools struct {
    client *Client
}

func (s *ShopifyTools) ShopifyCreateProductTool(
    ctx context.Context, input shopify.CreateProductInput,
) (*loop.ToolResult, error) {
    result, err := s.client.CallTool(ctx, "shopify_create_product", input)
    if err != nil {
        return loop.NewErrorResult(err.Error()), nil
    }
    return loop.NewSuccessResult(result), nil
}
```

### 5.3 Tool group configuration

New tool groups in `gowild_agent/data/tool_groups.go`:

```go
var ToolGroups = map[string][]string{
    // ... existing groups ...
    "shopify":   {"shopify_create_product", "shopify_update_product", "shopify_get_product", ...},
    "supplier":  {"supplier_search_products", "supplier_get_product", "supplier_place_order", ...},
    "ads":       {"ads_meta_create_campaign", "ads_meta_create_ad", "ads_google_create_campaign", ...},
    "ecommerce": {"ecommerce_product_pnl", "ecommerce_daily_pnl", "ecommerce_calculate_margin", ...},
}
```

Each agent is configured with the tool groups it needs:
- Scout: `["supplier", "web_search"]`
- Curator: `["shopify", "a2a"]`
- Lister: `["shopify", "supplier"]`
- Fulfiller: `["shopify", "supplier"]`
- Marketer: `["ads", "shopify", "ecommerce"]`
- Analyst: `["shopify", "ecommerce", "ads"]`
- Support: `["shopify"]`
- Pricer: `["shopify", "supplier"]`

v1 deployment enables only `scout`, `curator`, `lister`, and `fulfiller` tool groups. The other role mappings are staged for v2.

### 5.4 Environment variables (manager-side)

```bash
# Shopify
SHOPIFY_STORE_URL=my-store.myshopify.com
SHOPIFY_API_VERSION=2025-01
SHOPIFY_ACCESS_TOKEN=shpat_xxxxx
SHOPIFY_WEBHOOK_SECRET=whsec_xxxxx

# US suppliers
SUPPLIER_DEFAULT_PROVIDER=topdawg
TOPDAWG_API_KEY=xxxxx
TOPDAWG_SUPPLIER_ID=xxxxx

# Meta Ads
META_ADS_ACCESS_TOKEN=xxxxx
META_ADS_ACCOUNT_ID=act_xxxxx
META_ADS_PIXEL_ID=xxxxx

# Google Ads
GOOGLE_ADS_DEVELOPER_TOKEN=xxxxx
GOOGLE_ADS_CUSTOMER_ID=xxxxx
GOOGLE_ADS_REFRESH_TOKEN=xxxxx
```

All stored on the manager host. Never exposed to agent containers.

---

## 6. Agent A2A Methods

v1 activates methods for `scout`, `curator`, `lister`, and `fulfiller`. Methods for `pricer`, `support`, `marketer`, and `analyst` are defined now but enabled in v2.

### 6.1 Product Scout

| Method | Trigger | Output |
|--------|---------|--------|
| `find_product_candidates` | Recurring task (daily) | List of scored product candidates |

**`find_product_candidates` output:**

```json
{
    "candidates": [
        {
            "supplier_id": "us_12345",
            "title": "Portable Blender USB Rechargeable",
            "supplier_cost": 8.50,
            "suggested_retail": 24.99,
            "estimated_margin": 0.45,
            "shipping_days": 7,
            "supplier_rating": 4.6,
            "competition_score": 0.3,
            "trend_score": 0.8,
            "category": "kitchen",
            "image_urls": ["https://..."],
            "reasoning": "High trend score on TikTok, low competition..."
        }
    ]
}
```

### 6.2 Curator

| Method | Trigger | Output |
|--------|---------|--------|
| `review_product_candidate` | Pipeline (from scout) | Approved/rejected with reasoning |
| `review_underperformers` | Pipeline (from analyst) | Kill/keep/adjust decisions |

**`review_product_candidate` output:**

```json
{
    "decision": "approved",
    "supplier_id": "us_12345",
    "target_price": 24.99,
    "target_audience": "health-conscious millennials",
    "product": { "...enriched product data..." },
    "reasoning": "Good margin, aligns with store niche, acceptable shipping time"
}
```

### 6.3 Listing Agent

| Method | Trigger | Output |
|--------|---------|--------|
| `create_listing` | Pipeline (from curator) | Shopify product ID |
| `update_listing` | Pipeline (from pricer or curator) | Updated product ID |

### 6.4 Pricing Agent

| Method | Trigger | Output |
|--------|---------|--------|
| `check_price_change` | Webhook (product update) | Price adjustment action |
| `check_inventory` | Webhook (inventory update) | Stock action (disable/enable) |
| `daily_price_review` | Recurring task | Batch price adjustments |

### 6.5 Fulfillment Agent

| Method | Trigger | Output |
|--------|---------|--------|
| `fulfill_order` | Webhook (order created) | Supplier order confirmation |
| `cancel_order` | Webhook (order cancelled) | Cancellation confirmation |
| `check_tracking` | Recurring task (hourly) | Updated tracking for open orders |

### 6.6 Customer Service Agent

| Method | Trigger | Output |
|--------|---------|--------|
| `handle_inquiry` | Webhook or scheduled | Response to customer |
| `process_refund` | Webhook (refund created) | Refund handling confirmation |
| `welcome_customer` | Webhook (customer created) | Welcome message |

### 6.7 Marketing Agent

| Method | Trigger | Output |
|--------|---------|--------|
| `create_initial_campaign` | Pipeline (from lister) | Campaign IDs |
| `optimize_campaigns` | Recurring task (daily) | Budget adjustments, paused ads |
| `kill_campaign` | Pipeline (from curator) | Paused campaign confirmation |

### 6.8 Analytics Agent

| Method | Trigger | Output |
|--------|---------|--------|
| `daily_report` | Recurring task (daily) | Full P&L report |
| `product_performance` | On-demand | Per-product performance data |
| `identify_underperformers` | Recurring task (weekly) | Products below margin threshold |

---

## 7. Pipeline Definitions

### 7.1 Product discovery pipeline

v1 pipeline ends at listing creation. Marketing handoff is enabled in v2.

```
Scout (recurring daily)
  │ find_product_candidates
  │ output: { candidates: [...] }
  │
  ▼ (for each candidate)
Curator
  │ review_product_candidate
  │ input: { candidate: {...} }
  │ output: { decision: "approved", product: {...}, target_price: ... }
  │
  ▼ (if approved)
Lister
  │ create_listing
  │ input: { product: {...}, supplier_id: ..., target_price: ... }
  │ output: { shopify_product_id: ..., title: ..., audience: ... }
  │
  ▼
  Done (v1). Product is live in Shopify and ready for manual or v2 marketing.

  v2 extension:
  Lister -> Marketer(create_initial_campaign) -> campaign live
```

### 7.2 Order fulfillment pipeline

```
Shopify Webhook (orders/create)
  │
  ▼
Webhook Router
  │ converts to A2A job: fulfill_order
  │
  ▼
Fulfiller
  │ fulfill_order
  │ input: { order: { id, items, shipping_address, ... } }
  │ 1. Match items to supplier products
  │ 2. Place supplier order(s)
  │ 3. Update Shopify order with supplier reference
  │ output: { supplier_order_id: ..., estimated_delivery: ... }
  │
  ▼
  Done. Tracking updates handled by recurring task.
```

### 7.3 Daily review pipeline (v2)

```
Analyst (recurring daily)
  │ daily_report
  │ output: { report: {...}, underperformers: [...] }
  │
  ▼ (if underperformers exist)
Curator
  │ review_underperformers
  │ input: { underperformers: [...], report: {...} }
  │ output: { decisions: [{ product_id, action: "kill"|"reduce_price"|"keep" }] }
  │
  ▼ (for each "kill" decision)
Marketer
  │ kill_campaign
  │ input: { shopify_product_id: ... }
  │
  ▼ (for each "reduce_price" decision)
Pricer
  │ adjust_price
  │ input: { shopify_product_id: ..., new_price: ... }
```

### 7.4 Fan-out handling

The Product Scout returns multiple candidates. The pipeline engine needs to handle fan-out:

```go
type PipelineStep struct {
    // ... existing fields ...
    // If true, iterate over result array and submit one job per element.
    FanOut    bool   // e.g., true for "candidates" array
    FanOutKey string // JSON key containing the array, e.g., "candidates"
}
```

When `FanOut` is true, the engine iterates over `result[FanOutKey]` and submits a separate A2A job for each element. Each becomes an independent pipeline run from that point forward.

---

## 8. Data Model Additions

### 8.1 New tables

All registered via `init()` + `gowild_data.RegisterFunc()`:

```go
// agent_capabilities
type AgentCapability struct {
    AgentID      string    `db:"agent_id"`
    Role         string    `db:"role"`
    Method       string    `db:"method"`
    Description  string    `db:"description"`
    RegisteredAt time.Time `db:"registered_at"`
}

// agent_spend_ledger
type SpendEntry struct {
    ID        int64     `db:"id"`
    AgentID   string    `db:"agent_id"`
    Category  string    `db:"category"`
    Amount    float64   `db:"amount"`
    ToolName  string    `db:"tool_name"`
    Detail    string    `db:"detail"`
    CreatedAt time.Time `db:"created_at"`
}

// agent_spend_limits
type SpendLimit struct {
    AgentID    string  `db:"agent_id"`
    Category   string  `db:"category"`
    DailyLimit float64 `db:"daily_limit"`
}

// pipeline_definitions (stored as JSON, loaded at startup)
// pipeline_runs
// pipeline_step_runs
// webhook_configs
// webhook_events (dedupe + retry + dead-letter state)
```

### 8.2 Modifications to existing tables

**`agents` table** --- new columns:

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| mode | text | "interactive" | "interactive" or "worker" |
| heartbeat_interval | text | "15m" | Go duration string |
| worker_context_mode | text | "stateless" | "stateless" or "persistent" |
| roles | text[] | [] | Agent roles for capability routing |

---

## 9. Bootstrap Sequence

### Phase 0: Infrastructure (weeks 1--2)

Build the core platform changes, no e-commerce yet.

1. Worker mode in `gowild_agent`
2. Method handler registry
3. Capabilities table + registration
4. Pipeline engine with callback-driven chaining:
   a. `POST /pipeline/callbacks/a2a` endpoint on the manager
   b. Pipeline step matching on callback receipt (method, status, role)
   c. Basic linear chains (fan-out deferred to Phase 2)
5. Spend governor (framework + LLM category)
6. Webhook event queue table with dedupe/idempotency state
7. `GET /api/v1/a2a/jobs` query endpoint on agent_net (for dashboard/debug)

**Validate:** Two test agents in worker mode. Agent A submits a job to Agent B with callback URL pointing at the pipeline endpoint. Agent B completes the job. Callback fires. Pipeline engine receives the completion and chains the next step to Agent A. Verify the full round-trip works and pipeline_run / pipeline_step_run state is recorded correctly.

### Phase 1: Shopify + Supplier tools (weeks 3--4)

Build the tool packages and broker integration.

1. Shopify GraphQL-first tool package (products, orders, inventory, fulfillment)
2. TopDawg tool adapter + supplier interface (for backup provider support)
3. Broker dispatch for both
4. Webhook router for Shopify with async queue
5. Dedupe by `X-Shopify-Event-Id`, retry policy, dead-letter handling

**Validate:** Manually trigger: search TopDawg products, create Shopify listing, receive test order webhook, place supplier order, replay same webhook and confirm no duplicate job.

### Phase 2: Core v1 agents (weeks 5--6)

Stand up the agent team with a real Shopify dev store.

1. Create agents: scout, curator, lister, fulfiller
2. Define product discovery + order fulfillment pipelines for only those roles
3. System prompts for each agent role (what it does, how it decides)
4. Fan-out support in pipeline engine
5. Wire up Shopify webhooks to the dev store

**Validate:** End-to-end: scout finds products, curator approves, lister creates listings, test order triggers fulfillment.

### Phase 3: Ops expansion (weeks 7--8)

Add operational agents after v1 fulfillment quality is stable.

1. Add Pricing agent + price/inventory adjustment methods
2. Add Support agent + inquiry/refund methods
3. Add escalation queues and response templates
4. Expand pipeline definitions for underperformers and refund workflows

**Validate:** Price adjustment and support flows operate autonomously under guardrails with no fulfillment regression.

### Phase 4: Marketing + Analytics (weeks 9--10)

Add the revenue-generating and measurement agents.

1. Meta Ads tool package
2. Google Ads tool package (optional, can start Meta-only)
3. E-commerce analytics tools (P&L)
4. Spend governor with ads category + hard limits
5. Marketing and Analytics agents
6. Daily review pipeline

**Validate:** Marketing agent creates campaign with $10/day budget. Spend governor prevents exceeding limit. Analytics agent produces accurate P&L.

### Phase 5: Live validation (weeks 11--14)

Run with real money, small scale.

1. Pick a niche, go live on real Shopify store
2. 10 products, $20/day ad budget
3. Monitor daily: P&L, customer satisfaction, fulfillment accuracy
4. Iterate on agent prompts, pipeline logic, spend limits
5. Customer service agent goes live

**Success criteria:** Positive ROAS (>1.5x) sustained over 2 weeks.

### Phase 6: Scale (weeks 15+)

If unit economics validate:

1. Increase to 50+ products
2. Scale ad budget to $100--500/day
3. Add Google Ads alongside Meta
4. Second supplier integration for redundancy
5. Reduce human oversight cadence from daily to weekly

---

## 10. Observability

### 10.1 Dashboard (agent manager web UI)

New dashboard panels:

| Panel | Shows |
|-------|-------|
| Pipeline Monitor | Active pipeline runs, step status, bottlenecks |
| Spend Dashboard | Per-agent daily/weekly spend vs limits |
| P&L Summary | Revenue, COGS, ad spend, profit by product and aggregate |
| Agent Health | Last heartbeat, jobs processed, error rate per agent |
| Order Tracker | Open orders, fulfillment status, shipping ETAs |
| Product Funnel | Scouted, approved, listed, selling, killed |

### 10.2 Alerts

Conditions that escalate to human attention:

| Condition | Severity | Action |
|-----------|----------|--------|
| Spend limit hit | Warning | Notification, agent paused |
| Fulfillment failure | High | Notification, manual review queue |
| Customer complaint | Medium | Queued for human review |
| Agent error rate >10% | High | Notification, agent inspection |
| Daily loss >$50 | Critical | Pause all ad campaigns |
| Pipeline stuck (>1h) | Medium | Notification, lease expiry handles recovery |

### 10.3 Logging

All A2A jobs are already persisted. Pipeline runs add a second layer of traceability. Combined, you can answer:
- "Why did this product get listed?" --- trace pipeline run back to scout output + curator approval.
- "Why did this order fail?" --- trace A2A job for `fulfill_order`, see supplier error.
- "Where did the $50 in ad spend go?" --- query spend ledger by agent + date.

---

## 11. Risk Mitigations

### 11.1 Financial controls

| Control | Mechanism |
|---------|-----------|
| Daily ad spend cap | Spend governor, enforced at broker |
| Per-order cost limit | Spend governor rejects orders above threshold |
| Global daily loss limit | Analytics agent triggers emergency pause pipeline |
| No direct API access | Broker isolation model --- agents cannot bypass controls |

### 11.2 Quality controls

| Control | Mechanism |
|---------|-----------|
| Curator approval gate | Products require LLM-driven quality review |
| Template-based CS responses | Customer service starts with approved templates |
| Listing quality check | Lister validates required fields before Shopify API call |
| Return rate monitoring | Pricer auto-disables products above return threshold |

### 11.3 Operational controls

| Control | Mechanism |
|---------|-----------|
| Lease-based job processing | Stuck jobs auto-expire and can be reclaimed |
| Per-agent error budgets | >10% error rate triggers alert |
| Pipeline timeout | Runs stuck for >1 hour trigger notification |
| Heartbeat monitoring | Manager detects agents that stop checking in |
| Graceful degradation | If one agent is down, others continue independently |

### 11.4 Shopify TOS compliance

- No fake reviews or review manipulation.
- Product descriptions generated by AI are clearly original (not copied from competitors).
- Customer data handled per Shopify's privacy requirements.
- No misleading pricing or fake urgency tactics.
- Fulfillment timelines accurately reflect supplier shipping estimates.

---

## 12. Cost Estimates

### Per-agent LLM costs (Gemini)

| Agent | Calls/day | Avg tokens/call | Est. daily cost |
|-------|-----------|-----------------|-----------------|
| Scout | 5--10 | 5,000 | $0.10 |
| Curator | 10--50 | 3,000 | $0.15 |
| Lister | 10--50 | 4,000 | $0.20 |
| Pricer | 20--100 | 1,000 | $0.05 |
| Fulfiller | 5--50 | 2,000 | $0.05 |
| Support | 5--20 | 3,000 | $0.10 |
| Marketer | 5--20 | 4,000 | $0.10 |
| Analyst | 2--5 | 5,000 | $0.05 |
| **Total** | | | **~$0.80/day** |

LLM costs are negligible compared to ad spend. Direct handlers for mechanical tasks (order placement, price updates) reduce this further.

### Infrastructure costs

| Item | Monthly cost |
|------|-------------|
| Shopify Basic plan | $39 |
| VPS (agent manager + 8 containers) | $20--40 |
| PostgreSQL (managed) | $15 |
| Domain + SSL | $1 |
| **Total infrastructure** | **~$75--95/month** |

### Break-even analysis

With $100/day ad spend and 2x ROAS:
- Daily revenue: $200
- Daily COGS (est. 40% of revenue): $80
- Daily ad spend: $100
- Daily Shopify fees (~3%): $6
- Daily infrastructure: $3
- Daily LLM: $1
- **Daily profit: ~$10**

At 3x ROAS: ~$60/day profit. At 1.5x ROAS: ~-$15/day loss (still validating).

---

## 13. Open Questions

1. **Niche selection:** Should the Product Scout be niche-constrained from day one, or start broad and let the Curator narrow?

2. **Backup supplier:** Which US supplier should be the first backup adapter in parallel with TopDawg?

3. **Customer service escalation:** Email-based human escalation queue, or integrate with an existing helpdesk (Zendesk, Freshdesk)?

4. **Ad creative generation:** Should the Marketing Agent generate ad images/copy, or use supplier images with templated copy?

5. **Multi-store:** Design for single store initially. Should the data model anticipate multi-store from the start (store_id scoping)?

6. **Pipeline definition storage:** Hardcoded in Go (as shown), or stored in the database and editable via manager UI?

7. **Agent model selection:** All agents on Gemini Flash (cheap, fast), or Curator/Marketing on Pro (better reasoning)?
