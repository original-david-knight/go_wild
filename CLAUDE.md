# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Workflow

- When the user asks for a plan or architecture design, produce the plan document first and get approval before implementing.
- When the user asks for implementation, proceed directly to code — do not spend excessive time exploring unless the codebase is unfamiliar.
- Always verify Go code compiles (`go build ./...`) and tests pass (`go test ./...`) before committing.
- After making Go backend or JS frontend changes that affect containerized agents, rebuild the Docker image and restart containers before considering the task complete.

## Repository Layout

Libraries live at the repo root; runnable programs live under `apps/`.

```
my/, data/, tools/, knowledge_graph/, deep_research/, polymarket/,
crypto/, agentic_loop/, codexllm/, claudellm/, agent_data/,
agent_node/, agent_net/, objectives/              # libraries
apps/agent/, apps/agent_manager/,
apps/agent_net_server/, apps/agent_net_admin/,
apps/objectives/, apps/mcp-broker-server/         # runnable apps
```

All Go modules use the path prefix `github.com/original-david-knight/go_wild/...`. Modules are stitched together by `go.work` at the repo root. There is no root `go.mod`, and no `replace` directives — each `go.mod` records real pseudo-versions resolved against `github.com/original-david-knight/go_wild@main`. Consumers in other repos need `GOPRIVATE=github.com/original-david-knight/*`.

## Build Commands

```bash
# Build every workspace module (there is no root go.mod, so `go build ./...`
# at the repo root errors; use the wrapper or make targets instead)
./scripts/workspace_go.sh build ./...
make build

# Build specific apps
cd apps/agent && go build .
cd apps/agent_manager && go build .
cd apps/agent_net_server && go build -o server .

# Docker builds (from workspace root)
docker build -f apps/agent/Dockerfile -t gowild-agent:latest .
docker build -f apps/agent_net_server/Dockerfile -t agent-net-server .
```

## Test Commands

```bash
# All tests across every workspace module
./scripts/workspace_go.sh test ./...
make test

# Module tests
(cd agent_net && go test ./... -v)
(cd agentic_loop && go test ./... -v)
(cd data && go test ./... -v)

# Single test (inside the owning module)
(cd agent_net && go test -run TestServiceIsPremium -v)

# Tests use in-memory SQLite — no external DB needed
db, _ := gowild_data.NewSqliteDatabase(":memory:")
```

Go package identifiers are still `gowild_<name>` (e.g. `gowild_data`, `gowild_agent_net`) even though the directory and module paths dropped the prefix.

## Run Commands

```bash
# Agent manager (serves web UI on :8888, manages Docker containers)
cd apps/agent_manager && go build . && ./agent_manager

# Agent standalone (requires GEMINI_API_KEY in .env)
cd apps/agent && go build . && ./agent

# Agent net server (requires DATABASE_URL and SOLANA_RPC_URL in .env)
cd apps/agent_net_server && go run .

# Agent net admin CLI
cd apps/agent_net_admin && go run .
```

## Architecture

This is a Go monorepo for building autonomous AI agents. The repo uses `go.work` to stitch modules together for local dev. Outside the workspace, each `go.mod` resolves its siblings via pseudo-versions against `github.com/original-david-knight/go_wild@main` — no `replace` directives remain.

### Dependency Graph

```
my (base — .env loading)
    ↓
data (DB abstraction — PostgreSQL + SQLite)
    ↓
agentic_loop (Gemini LLM framework — streaming, tools, MCP)
    ↓
┌────────────────┼──────────────────┐
↓                ↓                  ↓
apps/agent     knowledge_graph    agent_net (library + apps/agent_net_server)
    ↓
crypto (ETH/SOL wallet)
    ↓
apps/agent_manager (web UI, broker service, Docker orchestration)
```

### Three Main Entry Points

1. **apps/agent_manager** — Web UI on port 8888 that manages containerized agents. Contains an embedded broker service (`/broker/v1/*`) that proxies LLM calls and DB operations for isolated agent containers. Agents get `BROKER_URL` + `BROKER_TOKEN` instead of raw API keys.

2. **apps/agent** — Terminal AI agent that runs inside Docker containers (managed by #1) or standalone. Uses structured JSON output for frontend parsing. 18 tool categories split into local (shell, python, files) and broker-proxied (memory, tasks, KG, soul).

3. **apps/agent_net_server** — Sybil-resistant social protocol server built on the `agent_net` library. Two-tier access: free (Argon2id PoW) and premium (0.005 SOL burn). Isnad protocol for claims/endorsements/verifications/bounties. E2E encrypted DMs via NaCl box.

### Key Architectural Patterns

**Event-driven streaming**: `agentic_loop` emits events (TextDelta, ToolCall, ToolResult, Done, Error) via Go channels. Callers manage conversation history — the loop is stateless.

**Struct-based tool auto-discovery**: Methods ending in `Tool` with signature `func(ctx, InputStruct) (*ToolResult, error)` are discovered via `WrapTools()` / `WrapToolsWithDescriptions()`. Input schemas are generated from struct tags (`json`, `description`, `enum`, `required`).

**Auto-discovery data registry**: Packages register tables in `init()` via `gowild_data.RegisterFunc()`. Applications call `gowild_data.AddAllTables(db)` to create all schemas. Schema auto-migrates new columns via `ensureColumns()`.

**Broker isolation model**: Agent containers have no API keys or database URLs. All sensitive operations (LLM, DB, secrets) go through the broker. Auth uses HMAC-SHA256 tokens: `base64url(agentID).base64url(HMAC(secret,agentID))`.

**Dual-layer signing** (agent_net): Transport signature (HTTP auth via `X-Agent-Sig`) + data signature (content provenance in Isnad claims/endorsements). Uses `json.RawMessage` for `target_object` to prevent signature breakage on re-marshalling.

## Module Details

Each module has its own `CLAUDE.md` with detailed docs:
- `apps/agent/CLAUDE.md` — Tools, broker architecture, structured output, slash commands
- `apps/agent_manager/CLAUDE.md` — Web UI, broker service, Docker orchestration, WebSocket relay
- `agentic_loop/CLAUDE.md` — Tool registration, schema generation, events, serialization
- `agent_net/CLAUDE.md` — API endpoints, Isnad protocol, middleware chain, crypto
- `crypto/CLAUDE.md` — Multi-chain wallet, key derivation, NaCl encryption
- `data/CLAUDE.md` — ORM patterns, type mappings, transactions, user scoping
- `knowledge_graph/CLAUDE.md` — Graph algorithms, tool integration, embeddings

Protocol specs live in `devdocs/`.

## Docker / Sandbox

When making changes to sandbox/Docker mode, always verify that CLI flags, environment variables, and file paths are correctly forwarded to the container. Never assume a flag available on the host is automatically available inside a Docker container. Specifically:
- Check that new CLI flags are included in the `buildContainerCmd()` args in `sandbox.go`
- Check that file writes target host-mounted paths, not container-local paths that vanish on restart
- After rebuilding the Docker image, verify bind mounts aren't overriding the new binary (stop container, rebuild image, then start fresh)
- When restarting agents or containers, use exact case-sensitive IDs — run a list/status command first to confirm

## Inter-Component Communication

Always use structured JSON messages for communication between agent, manager, and frontend. Never parse raw text or terminal output. The protocol:
- Agent emits `OutputMessage` JSON lines to stdout (`output.go`)
- Manager's `ws.go` parses these into `AgentMessage` and forwards as `WSMessage` over WebSocket
- Frontend's `app.js` handles each `agent_type` in `handleAgentMessage()`

When adding a new message type: add a `Msg*` constant + emitter method in `output.go`, and a case in `handleAgentMessage()` in `app.js`. The manager passthrough (`tryParseAgentJSON`) is generic and needs no changes.

When fixing bugs, prefer proper architectural solutions over hacky heuristics. Do not propose timeout increases, pattern matching on output, or string-guessing approaches when a structured protocol or clean abstraction is the right fix.

## Configuration

Never hardcode fallback/default values for configuration like model names, API keys, or database URLs. If a required env var is missing, fail loudly (panic or fatal error). Use a `requireEnv()` pattern that accepts multiple env var names to check in priority order, but always fails if none are set.

## Common Pitfalls

- **Container restart vs start**: Restart must remove the old container and recreate it to pick up config changes. Stop+start reuses the same container with old cmd/env.
- **Cross-module changes are not local-only**: there are no `replace` directives. A change in a library module only reaches consumers after it lands on `main` — commit + push, then `go get <module>@main` (or bump via `go mod tidy`) in each consumer. `go.work` papers over this for local dev, but CI and external consumers see the pinned pseudo-version.
- **Manager uses PostgreSQL only**: Same database as the agent — `data.Agent` struct in `apps/agent/data/models.go` is the single source of truth for both.
- **Isnad float formatting**: Canonical strings require exactly 4 decimal places (`fmt.Sprintf("%.4f", v)`) — mismatch breaks signature verification.
- **Timestamps must be UTC**: Agent net server rejects non-UTC timestamps with `INVALID_TIMESTAMP (400)`.
- **`GOWILD_DATABASE_URL` filtered from containers**: The manager strips this env var so containers cannot access the database directly.
- Do not code fallbacks.  if a configuration is missing or a dependency is unreachable, the code should fail.
## gstack

For all web browsing, use the `/browse` skill from gstack. Never use `mcp__claude-in-chrome__*` tools.

Available gstack skills:
- `/plan-ceo-review` — CEO/founder-mode plan review
- `/plan-eng-review` — Eng manager-mode plan review
- `/review` — Pre-landing PR review
- `/ship` — Ship workflow (merge, test, review, bump, commit, push, PR)
- `/browse` — Fast headless web browsing
- `/retro` — Weekly engineering retrospective
