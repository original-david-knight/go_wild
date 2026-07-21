# GoWild

GoWild is a Go monorepo with random experimental apps and libraries.  You probably do not want to use this for anything at all.

## Repository Layout

This repository is a multi-module Go workspace (`go.work`). Most apps live under `apps/`, libraries live at the repo root. One legacy executable (`agent_node/cmd/agentnode/`) still sits inside a root-level library module and has not been promoted to `apps/` yet.

Apps (`apps/`):

| Module | Purpose |
|---|---|
| `apps/agent_manager/` | Web UI + broker + Docker orchestration for multiple agents (default API/UI `127.0.0.1:8888`, ingress `127.0.0.1:8890`) |
| `apps/agent/` | Agent runtime binary (sandbox-first CLI, broker-aware mode, REPL) |
| `apps/agent_net_server/` | Standalone agent-net protocol server |
| `apps/agent_net_admin/` | Admin CLI for the agent-net server |
| `apps/objectives/` | Objectives CLI |
| `apps/mcp-broker-server/` | MCP broker server binary |

Libraries (repo root):

| Module | Purpose |
|---|---|
| `agentic_loop/` | Event-streaming LLM loop with tool auto-discovery and MCP support |
| `data/` | PostgreSQL/SQLite abstraction with auto-registered tables and user-scoped data |
| `crypto/` | Wallet/key/signing/encryption utilities |
| `knowledge_graph/` | Persistent graph + traversal/search tools |
| `agent_net/` | Agent-net protocol library (consumed by `apps/agent_net_server` and `apps/agent_net_admin`) |
| `polymarket/` | Polymarket CLOB/Gamma/Data API client and order-signing utilities |
| `my/` | Shared helpers (`.env` loading, env accessors) |

Other top-level folders: `docs/`, `devdocs/`, and `scripts/`.

## Architecture

```
Browser UI
   |
   v
apps/agent_manager (127.0.0.1:8888)
  - Agent CRUD + lifecycle
  - Docker container orchestration
  - Broker endpoints (LLM/tools/wallet/search/etc.)
  - WebSocket terminal relay
   |
   v
apps/agent containers
  - Agent REPL + tool execution
  - Persistent state in Docker volumes
  - Broker mode (no raw API keys in container)

Shared libs: agentic_loop, data, crypto,
             knowledge_graph, my, polymarket

Optional external service:
apps/agent_net_server (decentralized feed + messaging protocol server,
                       built on the agent_net library)
```

## Quick Start

### 1. Configure environment

From repo root:

```bash
cp .env.example .env
```

Key settings depend on what you run:
- `GOWILD_DATABASE_URL` (or `-agent-db`) for `apps/agent_manager` Postgres access.
  If unset, manager falls back to `postgres://gowild_agent:gowild_agent@localhost:5432/gowild_agent`.
- `GEMINI_API_KEY` for direct agent runs (when not using manager broker mode).
- `DATABASE_URL` for `apps/agent_net_server`.

### 2. Build all modules

```bash
# Walks every workspace module listed in go.work
./scripts/workspace_go.sh build ./...

# Or via the Makefile (separates libs from apps)
make build
```

There is no root `go.mod`, so `go build ./...` from the repo root errors with `directory prefix . does not contain modules listed in go.work` — always go through `workspace_go.sh`, `make`, or `cd` into a specific module.

### 3. Run the manager (recommended path)

```bash
cd apps/agent_manager
go build .
./agent_manager
```

Manager API/UI listens on `127.0.0.1:8888` by default (open `http://localhost:8888` locally).
Ingress callbacks/webhooks listen on `127.0.0.1:8890` by default.

For public webhook/callback ingress, run the manager with a separate ingress port and expose only that port (for example via ngrok). Keep the manager API/UI bind address private:

```bash
cd apps/agent_manager
go build .
./agent_manager -addr 127.0.0.1:8888 -ingress-addr 127.0.0.1:8890
```

Then set `INGRESS_PUBLIC_URL` (for example `https://your-reserved-subdomain.ngrok.app`) in the manager environment.

### 4. Run the agent directly (sandbox mode)

```bash
cd apps/agent
go build .
./agent
```

By default this runs in a Docker sandbox. In direct mode (without broker env), it requires `GEMINI_API_KEY`; `GOWILD_DATABASE_URL` is optional and enables DB-backed features.

### 5. Run the agent-net server

```bash
# The .env template still lives with the library module for now
cp agent_net/.env.example apps/agent_net_server/.env

cd apps/agent_net_server
go build .
./agent_net_server
```

`DATABASE_URL` is required. The `apps/agent_net_admin` binary under `apps/agent_net_admin/` provides admin operations against the same database.

### 6. Configure VPN/proxy for Polymarket tools (optional)

Polymarket tool calls are executed by the manager broker, not by agent containers directly.
If you need VPN/proxy routing for Polymarket CLOB access, configure it on the host where `apps/agent_manager` runs.
The exact VPN approach is system/provider-specific; the commands below are one Linux network-namespace example.

Example namespace-bridge flow (from your working setup):

```bash
# 1) Find the VPN namespace (example: names containing "vo_pr")
NS=$(ip netns list | grep vo_pr | awk '{print $1}')

# 2) Bridge localhost:1080 -> SOCKS5 listener inside that namespace
sudo socat TCP-LISTEN:1080,fork,reuseaddr,bind=127.0.0.1 EXEC:"ip netns exec $NS socat STDIO TCP\:127.0.0.1\:1080"

# 3) Quick connectivity test through SOCKS5
curl --socks5-hostname 127.0.0.1:1080 https://clob.polymarket.com/time

# 4) Point manager/broker at the local SOCKS5 bridge
# Option A: shell export
export POLYMARKET_PROXY_URL=socks5://127.0.0.1:1080
# Option B: add to .env
# POLYMARKET_PROXY_URL=socks5://127.0.0.1:1080
```

Then:

1. Start or restart `apps/agent_manager` so the broker picks up `POLYMARKET_PROXY_URL`.
2. Ensure the target agent has a wallet seed phrase configured (required for Polymarket client auth/signing).

Notes:
- `POLYMARKET_PROXY_URL` is consumed by the manager broker (`/broker/v1/polymarket/*`).
- In the Polymarket client, CLOB calls are routed through the proxy; public Gamma/Data calls are direct.
- On-chain wallet calls (for example `contract_call` to redeem/claim) use `WALLET_ETH_RPC_URL`.
  Set this to a Polygon RPC endpoint (for example `https://polygon-rpc.com`) for Polymarket settlement transactions.
- The dedicated `polymarket_redeem_winnings` tool settles via Polygon automatically (default `https://polygon-rpc.com`).
  Optional override: `POLYMARKET_RPC_URL` (agent env first, then manager env).

## Build, Test, and Local Dev Commands

From repo root:

```bash
# Build all workspace modules from go.work
./scripts/workspace_go.sh build ./...

# Run tests across all workspace modules from go.work
./scripts/workspace_go.sh test ./...

# Run vet across all workspace modules from go.work
./scripts/workspace_go.sh vet ./...

# Makefile targets (iterate over modules listed in go.work)
make build       # build libs, then apps
make build-libs  # libraries only
make build-apps  # apps under apps/ only
make test        # test libs, then apps
make test-libs
make test-apps
```

Docker builds (run from repo root so the build context includes sibling library modules):

```bash
# Agent-net server image
docker build -f apps/agent_net_server/Dockerfile -t agent-net-server:latest .
```

The agent runtime Dockerfile at `apps/agent/Dockerfile` has not been updated for the `apps/` layout yet — its `COPY` paths still assume the pre-reorg root layout and will fail. Build the agent binary directly (`cd apps/agent && go build .`) until the Dockerfile is reworked.

Module-local examples:

```bash
cd apps/agent_manager && go test ./...
cd apps/agent && go test ./...
cd apps/agent_net_server && go test ./...
cd agent_net && go test ./...   # agent-net library tests
```

## Agent Tool Groups

Current configurable tool groups (manager-controlled):

- `skills`
- `web_search`
- `web_reader`
- `http`
- `report`
- `soul`
- `knowledge_graph`
- `company_admin`
- `company_knowledge`
- `company_finance`
- `company_commerce`
- `wallet`
- `polymarket_read`
- `polymarket_buy`
- `polymarket_sell`
- `shell`
- `file`
- `tasks`
- `telegram`
- `email`
- `reuters`
- `messaging`
- `a2a`
- `mcp`
- `shopify`
- `supplier`
- `ads`
- `ecommerce`

The agent runtime also includes content output helpers (`show_image`, `show_svg`, `show_audio`).

## Environment Variables

The root `.env.example` covers common defaults (`GEMINI_API_KEY`, search keys, wallet/chain settings, `DATABASE_URL`, `INGRESS_PUBLIC_URL`, supplier defaults, retention/model helpers).

Additional module-specific variables used in practice:

- `GOWILD_DATABASE_URL`: optional Postgres URL for `apps/agent_manager` and direct-mode agent DB access (manager has a built-in fallback DSN)
- `DATABASE_URL`: required by `apps/agent_net_server`
- `INGRESS_PUBLIC_URL`: public HTTPS base URL used in generated webhook/callback URLs
- `A2A_CALLBACK_SIGNING_KEY`: optional signing key for `apps/agent_net_server` A2A callback signatures
- `POW_DIFFICULTY`, `UPGRADE_FEE_SOL`: `apps/agent_net_server` tuning
- `SOLANA_RPC_URL`, `SOLANA_TREASURY`: protocol/crypto chain settings
- `POLYMARKET_PROXY_URL`: optional SOCKS5 proxy URL for Polymarket CLOB requests (manager host)
- `POLYMARKET_RPC_URL`: optional Polygon RPC fallback for Polymarket settlement tools

## Security Notes

- In manager-orchestrated broker mode, containers receive `BROKER_URL`/`BROKER_TOKEN`; manager filters `GEMINI_API_KEY`, Google search keys, and `GOWILD_DATABASE_URL` from container env.
- Agent state is stored in Docker volumes named `gowild-agent-<id>-data`.
- Volume purge is blocked unless `ALLOW_VOLUME_PURGE=1`.
- Keep secrets out of git; use `.env.example` as the template.

## Module Dependency Diagram

Generated from `go.mod` files (direct module dependencies only). Excludes `vendor/` and `examples/`.
Regenerate with `./scripts/gen_module_deps.sh`.

<!-- MODULE_DEPS_START -->
```mermaid
flowchart LR
  agent --> agent_data
  agent --> agentic_loop
  agent --> crypto
  agent --> data
  agent --> knowledge_graph
  agent --> my
  agent --> tools
  agent_auth["agent_auth"]
  agent_data --> data
  agent_data --> knowledge_graph
  agent_manager --> agent_auth
  agent_manager --> agent_data
  agent_manager --> agent_net
  agent_manager --> agent_node
  agent_manager --> agentic_loop
  agent_manager --> claudellm
  agent_manager --> codexllm
  agent_manager --> crypto
  agent_manager --> data
  agent_manager --> deep_research
  agent_manager --> knowledge_graph
  agent_manager --> my
  agent_manager --> objectives
  agent_manager --> polymarket
  agent_manager --> tools
  agent_net --> agent_data
  agent_net --> data
  agent_net_admin --> agent_net
  agent_net_admin --> data
  agent_net_admin --> my
  agent_net_server --> agent_net
  agent_net_server --> data
  agent_net_server --> my
  agent_node --> agentic_loop
  agent_node --> deep_research
  agent_node --> my
  agent_node --> tools
  agentic_loop --> my
  claudellm["claudellm"]
  codexllm["codexllm"]
  crypto["crypto"]
  data["data"]
  deep_research --> agentic_loop
  deep_research --> claudellm
  deep_research --> codexllm
  deep_research --> tools
  knowledge_graph --> agentic_loop
  knowledge_graph --> data
  mcp-broker-server["mcp-broker-server"]
  my["my"]
  objectives --> agent_node
  objectives --> data
  objectives --> my
  polymarket --> crypto
  polymarket --> my
  polymarket_no_buyer --> crypto
  polymarket_no_buyer --> my
  polymarket_no_buyer --> polymarket
  screen_agent --> agentic_loop
  tools --> agent_auth
  tools --> agent_data
  tools --> agentic_loop
  tools --> crypto
  tools --> data
  tools --> knowledge_graph
  tools --> my
```
<!-- MODULE_DEPS_END -->

## Consuming these libraries in another project

The GitHub repo is private, so `go get` needs both a Go proxy bypass and Git
credentials that can read it.

```bash
# Route the go_wild namespace around the public proxy and sumdb
go env -w GOPRIVATE=github.com/original-david-knight/*

# Make sure `git` can reach the private repo. Either:
#   - SSH: have an ssh key loaded that can clone the repo, and
#     git config --global url."git@github.com:".insteadOf "https://github.com/"
#   - HTTPS: drop a token into ~/.netrc, e.g.
#     machine github.com login <username> password <personal-access-token>
```

Then pull any library at `@main`:

```bash
go get github.com/original-david-knight/go_wild/agent_net@main
go get github.com/original-david-knight/go_wild/agentic_loop@main
go get github.com/original-david-knight/go_wild/data@main
```

`go get` records a pseudo-version (`v0.0.0-<timestamp>-<sha>`) against the
current `main` commit. If you depend on several libraries from this repo, fetch
them in one `go get A@main B@main C@main` invocation so they pick up the same
commit — separate invocations can drift if new commits land in between.

## Documentation

- `docs/` and `devdocs/` contain architecture and protocol notes.
- Module-level design notes are in each module’s `CLAUDE.md`.
- `devdocs/maintainability_plan.md` tracks the current maintainability roadmap and refactor status.
- `docs/archive/` holds completed design docs (e.g. the April 2026 reorganization).

## License

[MIT](LICENSE)
