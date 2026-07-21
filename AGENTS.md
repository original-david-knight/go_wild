# Repository Guidelines

## Project Structure & Module Organization
This is a Go monorepo with multiple modules managed via `go.work`. Apps live under `apps/`; shared libraries live at the repo root.

Apps (`apps/`):
- `apps/agent_manager/`: web UI + broker + Docker orchestration (API/UI default `127.0.0.1:8888`, ingress default `127.0.0.1:8890`).
- `apps/agent/`: sandboxed agent runtime binary (container-first, broker-aware).
- `apps/agent_net_server/`: standalone agent-net protocol server (built on the `agent_net` library).
- `apps/agent_net_admin/`: admin CLI for the agent-net server.
- `apps/objectives/`: objectives CLI (built on the `objectives` library).
- `apps/mcp-broker-server/`: MCP broker server binary.

Core libraries (repo root):
- `agent_data/`: shared data models/services for agents, companies, pipelines, messaging, skills, paywalls, and more.
- `tools/`: tool implementations and broker-facing tool adapters (wallet, web, ecommerce, messaging, etc.).
- `agentic_loop/`: streaming LLM loop + tool wiring + MCP support.
- `data/`: PostgreSQL/SQLite abstraction + table auto-registration.
- `crypto/`: wallet/key/signing/encryption helpers.
- `knowledge_graph/`: persistent graph storage + traversal/search tools.
- `my/`: shared env/config/runtime helpers.
- `polymarket/`: Polymarket client + signing utilities.
- `claudellm/`, `codexllm/`: LLM client libraries.
- `deep_research/`: schema-guided deep research library.

Go package identifiers still use the `gowild_` prefix (e.g. `package gowild_data`) even though the directory and module paths dropped it.

Additional library modules with in-tree CLIs not yet promoted to `apps/`:
- `agent_net/`: agent-net protocol library (the server and admin CLIs now live under `apps/agent_net_server/` and `apps/agent_net_admin/`).
- `agent_node/`: DAG-based planning/execution/check orchestration + CLI (`cmd/agentnode`).
- `objectives/`: objective planning/scheduling engine + API/dashboard (the CLI now lives under `apps/objectives/`).

Other directories:
- `docs/` and `devdocs/`: architecture + protocol docs.
- `scripts/`: helper scripts (module graph, proxy helpers, setup scripts).

Tests generally live alongside packages (`*_test.go`).

## Build, Test, and Development Commands
From repo root (there is no root `go.mod`, so `go build ./...` at the root errors with `directory prefix . does not contain modules listed in go.work or their selected dependencies` — iterate via the workspace instead):
- `./scripts/workspace_go.sh build ./...` — build every module listed in `go.work`.
- `./scripts/workspace_go.sh test ./...` — test every module listed in `go.work`.
- `make build` — build libraries then apps (walks modules from `go.work`).
- `make build-libs` / `make build-apps` — build one half only.
- `make test` — test libraries then apps.
- `make test-libs` / `make test-apps` — test one half only.

Workspace-loop alternatives (safe for main-module binaries):
- `for dir in $(grep '\./' go.work | sed 's/[[:space:]]//g'); do (cd "$dir" && go build ./...) || exit 1; done`
- `for dir in $(grep '\./' go.work | sed 's/[[:space:]]//g'); do (cd "$dir" && go test ./...) || exit 1; done`

Important:
- Avoid `go list -m | xargs -I{} go build {}/...` (can fail with main-package output collisions).
- `go build ./...` and `go test ./...` from repo root fail (no root module) — use `make`, `scripts/workspace_go.sh`, or `cd` into a specific module.

From module dirs:
- `cd apps/agent_manager && go build . && ./agent_manager` — run manager (Postgres via `-agent-db` or `GOWILD_DATABASE_URL`).
- `cd apps/agent && go build . && ./agent` — run agent locally (`GEMINI_API_KEY` required outside broker mode).
- `cd apps/agent_net_server && go run .` — run protocol server (`DATABASE_URL` required).
- `cd apps/agent_net_admin && go run .` — run protocol admin CLI.
- `go run ./agent_node/cmd/agentnode --rounds 7 "<question>"` — run DAG orchestrator CLI.
- `cd apps/objectives && go run . daemon` — run objectives scheduler/API process.

## Coding Style & Naming Conventions
- Run `gofmt` on changed Go files.
- Follow standard Go naming (`CamelCase` for exported, `camelCase` for unexported).
- Use `snake_case` for JSON fields and tool names (for example `get_wallet_addresses`).

## Testing Guidelines
- Framework: Go's built-in `testing` package.
- Name tests `TestXxx` and keep them near the code they cover.
- Prefer module-local runs while iterating (for example `cd apps/agent_manager && go test ./...`).
- For cross-module/refactor validation, run `make test` or `./scripts/workspace_go.sh test ./...`.

## Commit & Pull Request Guidelines
Recent history uses short imperative summaries (for example "Add ...", "Fix ...", "Strengthen ...").
- Commits: concise present-tense subject lines; add scope/context in body when useful.
- PRs: include clear summary, test results, and UI screenshots for manager/dashboard changes.

## Security & Configuration Tips
- In broker mode, manager injects `BROKER_SOCKET_PATH` and per-agent `BROKER_TOKEN` into containers.
- In broker mode, manager filters these from container env: `GEMINI_API_KEY`, `GOWILD_DATABASE_URL`, `BROKER_URL`, `BROKER_TOKEN`, `BROKER_SOCKET_PATH`.
- Agent state lives in Docker volumes named `gowild-agent-<id>-data`; purge is blocked unless `ALLOW_VOLUME_PURGE=1`.
- Polymarket tools can use SOCKS5 via `POLYMARKET_PROXY_URL` (manager host env).
- `apps/agent_net_server` needs `DATABASE_URL`; premium-chain features additionally rely on settings like `SOLANA_RPC_URL` and `SOLANA_TREASURY`.
- Use `.env.example` as the template and never commit real secrets.
