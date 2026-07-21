# GoWild Objectives — Product Requirements Document

## Problem Statement

Current agent systems (gowild_agent via gowild_manager) fail at sustained, long-running objectives because of three compounding problems:

1. **Context window loss**: As conversations grow, earlier instructions and goals fall out of the LLM's context window. The agent literally forgets what it was supposed to be doing.
2. **Goal drift**: Agents get absorbed in immediate subtasks and lose sight of higher-level objectives. A task like "optimize product listings" devolves into endless tweaks to one product.
3. **No persistence**: When a session ends, crashes, or restarts, all progress, learned knowledge, and decision context is lost. The agent starts from zero.

The result: agents that work for minutes but not hours, hours but not days. They cannot maintain a coherent strategy across the time scales that real business operations require.

## Vision

A **fully autonomous, long-running objective management system** that:

- Maintains a persistent, hierarchical tree of objectives that survives restarts
- Plans, executes, evaluates, and replans continuously — like an operations team, not a chatbot
- Remembers everything it learns: facts about the domain, what worked, what failed, and why
- Manages its own attention — deciding what to work on, when, and for how long
- Asks humans for help when it's stuck, not when it's confused about its own goals

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                     GoWild Objectives                        │
│                                                              │
│  ┌─────────────┐   ┌──────────────┐   ┌─────────────────┐  │
│  │  Objective   │   │   Strategic  │   │    Persistent   │  │
│  │    Store     │◄──│   Planner    │──►│     Memory      │  │
│  │  (Postgres)  │   │              │   │   (Knowledge,   │  │
│  └──────┬───────┘   └──────┬───────┘   │    Decisions,   │  │
│         │                  │           │    Learnings)   │  │
│         ▼                  ▼           └────────┬────────┘  │
│  ┌──────────────────────────────────┐           │           │
│  │          Scheduler               │◄──────────┘           │
│  │  (Cron + Events + Continuous)    │                       │
│  └──────────────┬───────────────────┘                       │
│                 ▼                                            │
│  ┌──────────────────────────────────┐                       │
│  │     Execution Engine             │                       │
│  │  (gowild_agent_node DAGs)       │                       │
│  │  ┌────────┐ ┌────────┐ ┌─────┐  │                       │
│  │  │agentic │ │single  │ │deep │  │                       │
│  │  │ nodes  │ │ shot   │ │res. │  │                       │
│  │  └────────┘ └────────┘ └─────┘  │                       │
│  └──────────────┬───────────────────┘                       │
│                 ▼                                            │
│  ┌──────────────────────────────────┐                       │
│  │     Evaluation & Replan          │                       │
│  │  (Sufficiency + Learning Loop)   │                       │
│  └──────────────────────────────────┘                       │
│                                                              │
│  ┌──────────────────────────────────┐                       │
│  │     Human Escalation             │                       │
│  │  (Dashboard + Push Notifications)│                       │
│  └──────────────────────────────────┘                       │
│                                                              │
│  Reuses: gowild_agentic_loop, gowild_agent_node,           │
│          gowild_agent tools, gowild_data                    │
└──────────────────────────────────────────────────────────────┘
```

## Core Concepts

### 1. Objective Tree (Persistent, Arbitrary Depth)

The central data structure is a **tree of objectives** stored in PostgreSQL. Each node in the tree represents a goal at some level of abstraction, from a high-level mission down to a concrete executable task.

```
"Run a profitable Shopify store"                    [MISSION]
├── "Optimize product catalog for conversions"      [OBJECTIVE]
│   ├── "Audit current product descriptions"        [GOAL]
│   │   ├── "Scrape competitor listings"            [TASK]
│   │   └── "Analyze conversion data per product"   [TASK]
│   ├── "Rewrite underperforming listings"          [GOAL]
│   │   └── ...                                     [TASK]
│   └── "A/B test new vs old descriptions"          [GOAL]
├── "Maintain healthy inventory levels"             [OBJECTIVE]
│   ├── "Monitor stock levels daily"                [GOAL, recurring]
│   └── "Forecast demand for next 30 days"          [GOAL, weekly]
└── "Grow organic traffic"                          [OBJECTIVE]
    ├── "SEO audit"                                 [GOAL]
    └── "Content calendar for blog posts"           [GOAL]
```

**Each node has:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | Unique identifier |
| `parent_id` | UUID | Parent node (null for root missions) |
| `title` | string | Short description |
| `description` | text | Detailed description, acceptance criteria, context |
| `status` | enum | `pending`, `active`, `blocked`, `completed`, `failed`, `paused` |
| `priority` | int | Relative priority among siblings |
| `schedule` | JSON | Scheduling config: one-shot, cron, event-trigger, or continuous |
| `tool_allowlist` | []string | Which tools this node's execution can use (inherited from parent if empty) |
| `autonomy_level` | enum | `full`, `approve_plan`, `approve_actions` — per-node override |
| `deadline` | timestamp | Optional deadline |
| `created_at` | timestamp | When this node was created |
| `updated_at` | timestamp | Last modification |
| `completed_at` | timestamp | When marked complete |
| `metadata` | JSONB | Extensible key-value data |

**Key properties:**
- **Arbitrary depth**: The LLM decides how deep to decompose. A simple task might be a single leaf. A complex mission might be 6 levels deep.
- **Dynamic**: New children can be added, existing ones can be removed/restructured as the planner learns more.
- **Inherited defaults**: Tool allowlists, autonomy levels propagate down from parent unless overridden.

### 2. Strategic Planner

The planner is the brain of the system. Unlike `gowild_agent_node`'s single-round planner that produces one DAG, the Strategic Planner operates at **multiple time horizons** and performs **context-dependent replanning**.

**Planning levels:**

| Level | Trigger | Scope | Output |
|-------|---------|-------|--------|
| **Strategic** | New mission created, or major learning | Entire mission subtree | Restructure objectives/goals |
| **Tactical** | Goal becomes active, or evaluation fails | Single goal and its tasks | Decompose into executable task DAG |
| **Reactive** | External event or schedule trigger | Specific task or small subtree | Adjust/create tasks in response |

**How replanning works:**

1. **Minor learning** (e.g., "this product's description is already good"): Skip the task, mark complete, move on. Minimal plan change.
2. **Moderate learning** (e.g., "competitor prices dropped 20%"): Tactical replan — add new tasks, reprioritize existing goals.
3. **Major learning** (e.g., "Shopify store is being migrated to new platform"): Strategic replan — potentially restructure entire objective tree.

**The planner receives:**
- The current objective tree (with statuses)
- Persistent memory (knowledge base, decision log, learnings)
- Results from recent executions
- External events (if event-triggered)

**The planner can:**
- Add new nodes at any level
- Remove or archive nodes that are no longer relevant
- Change priorities
- Modify schedules
- Restructure the tree (move nodes, merge goals)
- Produce execution DAGs (via `gowild_agent_node.NodeGraph`) for leaf-level tasks

### 3. Persistent Memory System

Three categories of persistent memory, all stored in PostgreSQL:

#### a) Knowledge Base
Facts discovered during execution. Structured as key-value with tags and source attribution.

```
{
  "fact": "Product 'Organic Mango Butter' has 4.2% conversion rate",
  "source": "shopify_analytics_scrape_2026-02-28",
  "tags": ["product_performance", "organic_mango_butter"],
  "confidence": 0.95,
  "discovered_at": "2026-02-28T14:30:00Z",
  "expires_at": "2026-03-28T14:30:00Z"  // stale after 30 days
}
```

Key feature: **expiration**. Facts about dynamic data (prices, rankings, stock levels) expire and must be re-verified. The system knows when its knowledge is stale.

#### b) Decision Log
Records of decisions made, with reasoning and outcomes. Enables the system to avoid repeating mistakes and build on successes.

```
{
  "decision": "Rewrote product description for Organic Mango Butter",
  "reasoning": "Conversion rate was 2.1%, below category average of 3.5%",
  "action_taken": "Emphasized benefits over features, added social proof",
  "outcome": "Conversion rate increased to 4.2% after 7 days",
  "objective_id": "uuid-of-parent-goal",
  "created_at": "2026-02-21T10:00:00Z"
}
```

#### c) Learnings
Higher-level patterns extracted from multiple decisions/observations. These inform future planning.

```
{
  "learning": "Product descriptions that lead with benefits convert 2x better than feature-first descriptions in this store",
  "evidence": ["decision-uuid-1", "decision-uuid-2", "decision-uuid-3"],
  "confidence": 0.8,
  "applicable_to": ["product_optimization", "copywriting"],
  "created_at": "2026-02-28T10:00:00Z"
}
```

**Memory retrieval for planning:**
When the planner runs, it receives a curated subset of memory relevant to the current objective. This is filtered by tags, recency, and relevance (potentially using embeddings via `gowild_knowledge_graph`).

### 4. Scheduler

The scheduler is a long-running daemon that determines **what to work on and when**. It combines three scheduling modes:

#### a) Cron-based
Objectives/tasks with cron schedules (e.g., `"0 9 * * *"` for daily at 9am). Standard recurring work like inventory checks, report generation, analytics review.

#### b) Event-driven
External triggers that activate objectives. Events come from:
- Webhooks (Shopify order created, inventory low)
- Polling (check competitor prices every 4 hours)
- Internal events (a task completion triggers a dependent objective)

#### c) Continuous evaluation
A periodic "what should I work on?" sweep. The scheduler asks the planner:
- "Given the current objective tree, memory, and priorities — what's the highest-value work right now?"
- This handles the case where no cron or event fires, but there's still useful proactive work to do.

**Attention management:**
The scheduler maintains a **work queue** of activated objectives. It respects:
- Priority ordering
- Concurrency limits (don't run 10 expensive LLM tasks simultaneously)
- Deadlines (urgent items jump the queue)
- Cooldowns (don't re-evaluate the same objective every minute)

### 5. Execution Engine

Execution is delegated to `gowild_agent_node`. When the scheduler picks a task (or small subtree of tasks), it:

1. Calls the **Tactical Planner** to produce a `NodeGraph` (DAG of executable nodes)
2. Passes the DAG to `gowild_agent_node.Orchestrator` for execution
3. Collects results, stores them in the objective tree and memory system
4. Triggers evaluation

**Tool access is scoped per-node** via the `tool_allowlist` field. A monitoring task might only get `web_search` and `http_request`, while a content creation task gets `file_write` and `shell_exec` too.

The existing `gowild_agent` tool suite is available:
- Web tools: search, fetch, browser automation
- File tools: read, write, edit
- Shell tools: execute commands
- Research tools: deep research
- Memory tools (rewired to use the persistent memory system instead of per-session memory)

### 6. Evaluation & Learning Loop

After each execution:

1. **Sufficiency check** (via `gowild_agent_node` sufficiency checker): Did the task produce adequate results?
2. **Outcome recording**: Store the result in the decision log with context
3. **Knowledge extraction**: An LLM pass extracts new facts from the results and adds them to the knowledge base
4. **Learning synthesis**: Periodically (not every execution), synthesize learnings from recent decisions
5. **Replan trigger**: If the evaluation reveals something that changes the plan, trigger the appropriate level of replanning

### 7. Observability & Status Visibility

Visibility into what the system is doing, why, and how it's progressing is a **first-class priority** — not an afterthought bolted onto a dashboard.

#### a) Activity Stream
A persistent, append-only log of every significant action the system takes. Stored in PostgreSQL, queryable by objective, time range, severity, and type.

Event types:
- `plan_created` — A planner produced a new plan/decomposition
- `plan_modified` — An existing plan was restructured
- `task_started` — Execution began on a task
- `task_completed` — Task finished (with summary of result)
- `task_failed` — Task failed (with error context)
- `knowledge_acquired` — New fact added to knowledge base
- `decision_made` — A non-trivial decision was recorded
- `learning_extracted` — A pattern was synthesized from multiple observations
- `escalation_raised` — Human help requested
- `replan_triggered` — Something caused a replan (with reasoning)
- `schedule_fired` — A cron/event/continuous trigger activated

Each event includes: timestamp, objective_id, severity, summary (human-readable), details (structured JSON), and the LLM reasoning that led to the action (when applicable).

#### b) Objective Tree Status Aggregation
Every node in the objective tree carries a **computed status summary** that rolls up from its children:

```
"Optimize product catalog" [ACTIVE]
  Progress: 3/7 goals complete
  Last activity: 2m ago — "Analyzing conversion data for product #42"
  Next scheduled: in 4h — "Rewrite descriptions for bottom 10 products"
  Blockers: None
  Cost so far: $2.14 (47 LLM calls)
```

This propagates up the tree — the root mission shows aggregate progress across all branches.

#### c) Real-time Process Visibility
When a task is actively executing (an `agent_node` DAG is running), the system exposes:
- Which nodes in the DAG are running / completed / pending
- Live streaming of LLM output (text deltas) for active nodes
- Tool calls being made and their results
- Sufficiency check verdicts

This uses the existing `gowild_agent_node` event stream, piped to the dashboard via WebSocket.

#### d) "Why did it do that?" Audit Trail
Every action is traceable back to:
1. Which objective triggered it
2. Which plan decomposed it
3. What the planner's reasoning was
4. What memory/knowledge informed the decision
5. What the evaluation said afterward

This chain is stored in the activity stream and cross-referenced via objective IDs. The dashboard provides a drill-down view: click any action → see the full reasoning chain.

#### e) Status API
A REST API that exposes all observability data programmatically:
- `GET /api/objectives` — Full tree with status summaries
- `GET /api/objectives/:id` — Single objective with children and activity
- `GET /api/activity` — Activity stream (filterable)
- `GET /api/activity/:id` — Single event with full details
- `GET /api/memory/knowledge` — Knowledge base entries
- `GET /api/memory/decisions` — Decision log
- `GET /api/memory/learnings` — Synthesized learnings
- `GET /api/status` — System health: active tasks, queue depth, last activity, uptime
- `WS /api/stream` — Real-time event stream via WebSocket

This API powers the dashboard and can be consumed by external tools (monitoring, Telegram bots, custom integrations).

### 8. Human Escalation System

The agent operates autonomously but can request human help via:

#### a) Web Dashboard
A simple web UI showing:
- Objective tree with statuses (collapsible tree view)
- Recent activity log
- Pending questions / escalations
- Memory browser (knowledge, decisions, learnings)
- Manual controls: pause/resume objectives, add new missions, override plans

#### b) Push Notifications
For time-sensitive escalations:
- Configurable channel (Telegram, Slack webhook, email, ntfy.sh, etc.)
- Severity levels: `info`, `question`, `warning`, `critical`
- Questions include context and suggested options so the human can reply quickly

**Escalation triggers:**
- Task failed after max retries
- Confidence too low to proceed autonomously
- Budget/cost threshold exceeded
- External service requires human auth (CAPTCHA, 2FA)
- A major learning that might warrant strategic replan

## Execution Environment

- **Runtime**: Go binary running as a Docker container
- **Database**: PostgreSQL (via `gowild_data`)
- **LLM**: Gemini (via `gowild_agentic_loop`) — same as current infrastructure
- **Tool execution**: Inside the same container (or spawning child containers for sandboxed tools)
- **Deployment**: Docker Compose for local dev, single container + Postgres for prod

## Module Structure

```
gowild_objectives/
├── cmd/
│   └── objectives/          # Main binary entry point
│       └── main.go
├── objective.go             # Objective tree types and DB operations
├── planner.go               # Strategic + Tactical planner
├── scheduler.go             # Cron + Event + Continuous scheduling
├── memory.go                # Knowledge base, decision log, learnings
├── evaluator.go             # Post-execution evaluation and learning extraction
├── activity.go              # Activity stream (event logging, queries)
├── status.go                # Status aggregation, objective tree rollups
├── api.go                   # REST + WebSocket API for observability
├── escalation.go            # Human escalation (dashboard + notifications)
├── dashboard/               # Web dashboard (HTML/JS, embedded via go:embed)
│   ├── index.html
│   ├── app.js
│   └── components/          # Tree view, activity feed, live execution view
├── notifier.go              # Push notification integrations
├── config.go                # Configuration (YAML/env)
├── go.mod
└── go.sum
```

**Dependencies:**
- `gowild_data` — PostgreSQL access
- `gowild_agentic_loop` — LLM calls
- `gowild_agent_node` — DAG execution engine
- `gowild_agent` (tools only) — tool registry
- `gowild_my` — env loading
- `gowild_knowledge_graph` — (optional) embedding-based memory retrieval

## Key Design Decisions

### Why a tree, not a flat list or DAG?
- Trees naturally represent goal decomposition (mission → objectives → goals → tasks)
- Inherited properties (tool access, autonomy) flow naturally down a tree
- Easier to reason about "what's the status of this high-level objective?" by aggregating children
- DAGs are used at the *execution* level (within `gowild_agent_node`), trees at the *planning* level

### Why PostgreSQL, not SQLite?
- Long-running daemon needs robust crash recovery
- Concurrent access from scheduler, executor, evaluator, and dashboard
- JSONB for flexible metadata without schema migrations
- Consistent with existing `gowild_data` Postgres backend

### Why not use gowild_agent_manager?
- Manager is designed for interactive, session-based agent use (chat UI, WebSocket relay)
- Objectives system is autonomous and continuous — no human session driving it
- Different lifecycle: manager spawns/kills containers per chat session; objectives runs indefinitely
- The manager's broker model adds unnecessary indirection for a single-user system

### Why persistent memory with expiration?
- LLM context windows are finite — can't load all history into every prompt
- Expiration prevents stale facts from poisoning decisions
- Structured memory (not just raw text) enables targeted retrieval
- Decision logs prevent repeating failed strategies

### Why context-dependent replanning?
- Rigid plans break when reality diverges from expectations
- But constant restructuring wastes compute and loses momentum
- The magnitude of replanning should match the magnitude of the learning
- This is how human operations teams work: small adjustments daily, big pivots rarely

## Non-Goals (v1)

- **Multi-user / multi-tenant**: This is a personal tool. Single user, single instance.
- **Distributed execution**: Everything runs in one process (or one container). No distributed task queues.
- **Custom tool development UI**: New tools are added by writing Go code, not through a UI.
- **Mobile app**: Dashboard is web-only.
- **Cost optimization**: v1 doesn't track or optimize LLM spend (though the memory system naturally reduces redundant calls).

## Success Criteria

1. **Persistence**: System can be stopped and restarted without losing any objective state, memory, or progress.
2. **Multi-day execution**: A mission like "optimize Shopify store" runs across multiple days with coherent strategy.
3. **No goal drift**: After 100+ task executions, the system is still working toward the original mission objectives.
4. **Adaptive planning**: When conditions change (e.g., a product goes out of stock), the system adjusts its plan without human intervention.
5. **Human escalation works**: When stuck, the system sends a clear, actionable notification and pauses the affected branch until a human responds.
6. **Full visibility**: At any moment, you can see what the system is doing, why it's doing it, what it plans to do next, and trace any past action back to the reasoning that produced it.

## Open Questions

1. **LLM cost management**: Long-running autonomous agents can burn through API credits fast. Should v1 include budget limits per objective/per day?
2. **Concurrency model**: How many objectives should execute simultaneously? Should this be configurable or auto-tuned?
3. **Rollback**: If a plan goes badly wrong (agent publishes bad content, makes a bad API call), should there be an undo mechanism?
4. **Testing strategy**: How do we test a system that's designed to run for days? Simulated time? Mock LLM responses?
5. **Migration path**: Should existing gowild_agent_node be modified to support the objectives system, or should objectives wrap it as-is?
