# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the manager
cd apps/agent_manager && go build .

# Run tests
go test ./apps/agent_manager/... -v

# Run specific test
go test -run TestBrokerAuth -v

# Run the manager (serves web UI on :8888)
cd apps/agent_manager && go build . && ./agent_manager
```

Tests use in-memory SQLite (`:memory:`) — no external DB or Docker needed.

## Architecture Overview

GoWild Agent Manager is the control plane for managing containerized AI agents. It provides a web UI, Docker orchestration, a broker service that proxies all sensitive operations for isolated agent containers, and a WebSocket relay for terminal I/O.

```
┌─────────────────────────────────────────────────────────────┐
│  Agent Manager (host)                                       │
│  ├── Web UI (static/)           → port 8888                 │
│  ├── REST API (/api/*)          → agent CRUD, state, config │
│  ├── Broker Service (/broker/v1/*)                          │
│  │   ├── LLM Proxy              → Gemini API                │
│  │   ├── Tool Proxy             → DB operations              │
│  │   ├── Wallet Proxy           → crypto operations          │
│  │   ├── Email/Search/Telegram  → external APIs              │
│  │   ├── MCP Host               → host-side MCP servers      │
│  │   └── Auth                   → HMAC-SHA256 tokens         │
│  ├── Docker Manager             → container lifecycle        │
│  ├── WebSocket Hub              → terminal relay             │
│  └── Worker Manager             → background tasks           │
└─────────────────────────────────────────────────────────────┘
```

## CLI Flags

```bash
-addr     # Listen address (default: ":8888")
-agent-db # PostgreSQL URL (overrides GOWILD_DATABASE_URL env)
```

## Environment Variables

```bash
# Required
GOWILD_DATABASE_URL=postgres://user:pass@localhost:5432/gowild_agent

# Required for LLM broker
GEMINI_API_KEY=<your-key>

# Auto-generated if missing (stored in DB)
BROKER_SECRET=<base64-encoded-32-byte-secret>

# Optional
# Web search uses Gemini Grounding with Google Search (via GEMINI_API_KEY)
```

## Core Components

### Server & Routing (`server.go`, `handlers_routes.go`)

HTTP server with CORS, logging, and recovery middleware.

**Timeouts**: Read 60s, Write 5min (LLM generation can be slow), Idle 120s.

Route structure:
```
/                                    Static index page
/health                              Health check
/static/*                            CSS, JS assets

/api/agents                          GET (list), POST (create)
/api/agents/{id}                     GET, PUT
  /start, /stop, /restart            Container lifecycle
  /refresh-image                     Rebuild Docker image
  /logs                              Container logs
  /terminal                          WebSocket terminal relay
  /memory, /archive                  Agent state
  /report, /soul, /tasks             Agent state
  /chat-history, /runtime-status     Agent state
  /email-whitelist                    GET, POST, DELETE
  /pending-emails                    GET, POST (approve/reject)
  /recurring-tasks                   GET, POST, PUT, DELETE
  /kg/nodes, /kg/edges               Knowledge graph
  /kg/node/{nodeId}, /kg/search      Knowledge graph
  /mcp-servers                       Per-agent MCP config
  /upload                            File upload

/api/peer-groups                     Multi-agent messaging groups
/api/mcp-servers                     Global MCP server registry
/api/docker/build, /api/docker/status Docker management

/broker/v1/*                         Agent broker API (HMAC auth)
```

### Agent Service (`service.go`)

Database queries wrapping `apps/agent/data.AgentService` for agent-centric operations:
- Agent CRUD and configuration
- State retrieval (memory, archive, context, soul, tasks, chat history)
- Knowledge graph queries (nodes, edges, neighbors, search)
- Recurring task management
- MCP server registry (global + per-agent)
- Peer group management
- Email approval workflow (pending emails, whitelist)

### Docker Management (`docker_*.go`)

| File | Purpose |
|------|---------|
| `docker_manager.go` | DockerManager struct, client initialization |
| `docker_container.go` | Container CRUD (create, start, stop, remove, attach) |
| `docker_image.go` | Image build, staleness detection via build labels |
| `docker_lifecycle.go` | High-level lifecycle (start = ensure volume + image + create + start) |
| `docker_helpers.go` | Container naming, label parsing utilities |

Container configuration:
- `BROKER_SOCKET_PATH`, `GOWILD_AGENT_ID`, `GOWILD_BROKER_ONLY`, and an auth-only Ethereum key are injected (no API keys or DB URLs)
- Agent volume mounted at `/root/.gowild` for persistent storage
- Docker socket mounted for nested container support
- Memory & CPU limits from agent config

### Broker API (`broker_*.go`)

Agents authenticate with a short-lived Ethereum challenge-response flow modeled on SIWE, then use the returned session token for broker calls.

| File | Purpose |
|------|---------|
| `broker_auth.go` | Token generation, validation, middleware |
| `broker_llm.go` | Gemini API proxy (`/broker/v1/llm/generate`) |
| `broker_wallet_handlers.go` | Wallet operations (address, balance, sign, send, swap, encrypt) |
| `broker_email.go` | Email operations (list, read, send with approval) |
| `broker_search.go` | Google Search API proxy |
| `broker_telegram.go` | Telegram API proxy (send, updates, chats) |
| `broker_tools_handler.go` | Generic tool request handler |
| `broker_tools_dispatch.go` | Routes tool calls to implementations |
| `broker_tools_soul.go` | Soul (identity) tools |
| `broker_tools_context.go` | Context summary tools |
| `broker_tools_skills.go` | Skills CRUD tools |
| `broker_tools_tasks.go` | Task management tools |
| `broker_tools_kg.go` | Knowledge graph tools |
| `broker_tools_recurring.go` | Recurring task tools |
| `broker_tools_data_access.go` | Chat history, data access |
| `broker_tools_report.go` | Report generation tools |
| `broker_tools_mcp.go` | Host-side MCP server calls |
| `broker_tools_cache.go` | Cache operations |
| `broker_tools_compress.go` | Web content compression via local LLM |
| `broker_secret.go` | Broker secret loading/generation |

### WebSocket Relay (`ws_*.go`)

Terminal I/O relay between browser clients and Docker containers.

| File | Purpose |
|------|---------|
| `ws_hub.go` | SessionHub: registry of active relay sessions (one per agent) |
| `ws_session.go` | RelaySession: Docker attach + multi-client broadcast |
| `ws_output.go` | Container stdout → WebSocket broadcast (line-buffered JSON parsing) |
| `ws_input.go` | WebSocket client → container stdin |
| `ws_types.go` | WSMessage format definitions |

Multiple browser clients can connect to the same agent's terminal session.

### HTTP Handlers (`handlers_*.go`)

| File | Purpose |
|------|---------|
| `handlers_core.go` | Handlers struct, shared utilities |
| `handlers_agents.go` | Agent CRUD (list, create, get, update) |
| `handlers_lifecycle.go` | Container start/stop/restart/refresh-image |
| `handlers_state.go` | Agent state (memory, archive, context, report, soul, tasks) |
| `handlers_docker.go` | Docker build/status |
| `handlers_terminal.go` | WebSocket terminal upgrade |
| `handlers_kg.go` | Knowledge graph queries |
| `handlers_recurring.go` | Recurring task CRUD |
| `handlers_pending_emails.go` | Email approval workflow |
| `handlers_peer_groups.go` | Peer group CRUD + member management |
| `handlers_upload.go` | File upload handling |
| `handlers_mcp.go` | MCP server CRUD + per-agent config + connection testing |

### Background Workers (`workers.go`, `worker_telegram.go`)

Extensible worker system for per-agent background tasks. Currently supports Telegram polling. Workers start when agent containers start and stop when containers stop.

### MCP Host Manager (`mcp_host.go`)

Manages host-side MCP (Model Context Protocol) server processes. One StdioClient per (agent, server) pair, lazy-loaded and restarted on config changes (hash-based detection).

## Frontend (`static/`)

Single-page web UI with multi-column agent layout:
- `index.html` - Page structure, modals (peer groups, MCP, Docker, email approval)
- `app.js` - Agent management, WebSocket terminal, state tabs, `handleAgentMessage()` for JSON parsing
- `app.css` - Styling

## Startup Sequence

1. Load `.env`, parse CLI flags
2. Connect to PostgreSQL
3. Initialize Docker manager
4. Create service, session hub, worker manager
5. Load/generate broker secret
6. Initialize broker handlers
7. Auto-start agents with `auto_start=true` that have volumes
8. Start background workers for running agents
9. Start HTTP server
10. Block on SIGINT/SIGTERM for graceful shutdown

## Key Dependencies

- `github.com/docker/docker` v27.5.1 - Docker SDK for container management
- `github.com/gorilla/websocket` - WebSocket relay
- `google.golang.org/genai` v1.44.0 - Gemini API (LLM broker)
- Libraries (`my`, `data`, `agentic_loop`, `knowledge_graph`, `crypto`, `agent_data`) via `go.work` + replace directives

## Common Pitfalls

- **Container restart vs start**: Restart must remove old container and recreate to pick up config changes. Stop+start reuses same container with old cmd/env.
- **Image staleness**: Build labels (ID, SHA, dirty, time) are compared to detect stale images. Force rebuild with `/api/docker/build`.
- **Broker secret**: Auto-generated on first run and stored in DB. Set `BROKER_SECRET` env var for deterministic tokens.
- **Write timeout**: Set to 5 minutes for LLM streaming responses. Don't reduce without considering generation time.
- **`GOWILD_DATABASE_URL` filtered**: Manager strips this env var from container environments so agents cannot access the database directly.
