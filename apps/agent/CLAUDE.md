# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the agent
cd apps/agent && go build .

# Build Docker image (from workspace root)
cd /path/to/golang && docker build -f apps/agent/Dockerfile -t gowild-agent:latest .

# Run tests (always run after significant changes)
go test ./tools/...

# The agent is typically run via the manager, not directly
# See "Running with Manager" section below
```

**Important**: Always run `go test ./tools/...` after significant changes to broker client, tool wiring, or KG tools. The broker client tests (`tools/broker/client_test.go`) cover JSON response handling for objects, arrays, and strings — these catch marshaling issues that only surface at runtime.

## Architecture Overview

GoWild Agent is an AI agent that runs inside a Docker container, managed by `apps/agent_manager`. It communicates with the outside world through a **broker service** that proxies all database operations and LLM calls.

```
┌─────────────────────────────────────────────────────────────┐
│  Agent Manager (host)                                       │
│  ├── Web UI (port 8888)                                     │
│  ├── Broker Service (/broker/v1/*)                          │
│  │   ├── LLM Proxy (Gemini API)                             │
│  │   ├── Tool Proxy (DB operations)                         │
│  │   └── Auth (Ethereum challenge-response + session JWT)   │
│  └── Docker Manager                                         │
│       └── Container lifecycle                               │
└─────────────────────────────────────────────────────────────┘
         │ WebSocket              │ Unix socket (broker)
         ▼                        ▼
┌─────────────────────────────────────────────────────────────┐
│  Agent Container (Docker)                                   │
│  ├── REPL (main_session.go)                                  │
│  ├── Structured Output (output.go)                          │
│  ├── Tools (local: shell, file, python)                     │
│  └── Broker Client (tools/broker/)                          │
│       └── Proxied tools: memory, soul, KG, tasks, etc.      │
└─────────────────────────────────────────────────────────────┘
```

### Key Principles

1. **Broker-Only Mode**: Agent always uses broker for DB/LLM operations
2. **No Direct Secrets**: Container has no API keys or database URLs
3. **Structured Output**: All output is JSON for frontend parsing
4. **Container Isolation**: Agent runs in sandboxed Docker container

## Running with Manager

The agent is designed to run via the manager:

```bash
# Build and run the manager
cd apps/agent_manager && go build . && ./agent_manager

# The manager:
# - Serves web UI on port 8888
# - Provides broker service for agents
# - Manages Docker containers
# - Handles WebSocket relay to containers
```

Agents are started/stopped via the manager's web UI or API:
```bash
curl -X POST http://localhost:8888/api/agents/jake/start
curl -X POST http://localhost:8888/api/agents/jake/stop
```

## Core Components

### Structured Output (`output.go`)

All agent output goes through a JSON emitter for frontend parsing:

```go
type OutputMessage struct {
    Type    string `json:"type"`              // Message type
    Content string `json:"content,omitempty"` // Text content
    Name    string `json:"name,omitempty"`    // Tool name
    Detail  string `json:"detail,omitempty"`  // Tool detail/args
    Status  string `json:"status,omitempty"`  // Tool status
    Tokens  int    `json:"tokens,omitempty"`  // Token count
}
```

Message types:
| Type | Purpose | Example |
|------|---------|---------|
| `prompt` | Ready for input | `{"type":"prompt"}` |
| `system` | System messages | `{"type":"system","content":"Pending Tasks (4):"}` |
| `thinking` | Agent thinking | `{"type":"thinking"}` |
| `response` | Response chunk | `{"type":"response","content":"Hello..."}` |
| `response_end` | Response done | `{"type":"response_end","tokens":1234}` |
| `tool_call` | Tool starting | `{"type":"tool_call","name":"read_file","detail":"path=/data/foo.txt"}` |
| `tool_result` | Tool finished | `{"type":"tool_result","name":"read_file","status":"completed"}` |
| `error` | Error occurred | `{"type":"error","content":"Failed to..."}` |
| `compaction` | Context compacted | `{"type":"compaction","content":"masked=5 kept=3..."}` |

Usage in code:
```go
output.System("Pending Tasks (%d):", count)     // System message
output.SystemSuccess("Task completed!")          // Success (with checkmark)
output.SystemWarning("Token limit approaching")  // Warning
output.Error("Failed to load: %v", err)          // Error
output.Response("Hello ")                        // Response chunk
output.ToolCall("read_file", "path=/data/x.txt") // Tool starting
output.ToolResult("read_file", true, "")         // Tool succeeded
output.Prompt()                                  // Ready for input
```

### Main Entry & Session (`main_*.go`)

The main entry point is split across focused files:

| File | Purpose |
|------|---------|
| `main_entry.go` | CLI entry point, flag parsing, agent creation, .env loading, signal handling |
| `main_globals.go` | Global variables: readline, tool tracking, broker/Telegram/email/MCP clients |
| `main_session.go` | Interactive REPL session with readline, heartbeat loop, input processing |
| `main_interjection.go` | User interruption handling during agent execution |

The REPL loop in `main_session.go` handles:
1. Reading input from readline (stdin)
2. Processing slash commands locally
3. Sending user messages to the LLM via broker
4. Streaming responses back via structured output
5. Heartbeat-triggered task processing

### Command Processing (`commands_*.go`)

Slash commands are split across focused files:

| File | Purpose |
|------|---------|
| `commands.go` | Base infrastructure: `CommandResult` enum, context struct, handler map |
| `commands_dispatch.go` | Text parser converting `/addtask` → structured `CommandMessage` |
| `commands_basic.go` | `/help`, `/clear`, `/restart`, `/history`, `/report`, `/context` |
| `commands_io.go` | `/image`, `/file`, `/paste` for media attachments |
| `commands_exit.go` | `/exit` with email queue warning and cleanup |
| `commands_messaging.go` | `/telegram`, `/email` configuration |
| `commands_recurring.go` | `/addrecurring`, `/deleterecurring`, `/listrecurring` |
| `commands_smart.go` | `/smart` toggle for pro model + extended thinking |
| `commands_tasks.go` | `/tasks`, `/addtask`, `/finished`, `/worktasks`, `/stoptasks` |

Return values control the main loop:
| Value | Behavior |
|-------|----------|
| `cmdContinue` | Wait for next input |
| `cmdRestart` | Warn agent, then clear history |
| `cmdWorkTasks` | Generate task prompt, send to LLM |
| `cmdProcessMessage` | Treat as normal user message |

### Broker Client (`tools/broker/`)

The broker client proxies all DB operations:

```go
// tools/broker/client.go
type Client struct {
    baseURL string
    token   string
}

func (c *Client) CallTool(ctx context.Context, name string, input any) (map[string]any, error) {
    // POST /broker/v1/tools/{name} with JSON body
}

// Usage in slash commands:
result, err := globalBrokerClient.CallTool(ctx, "get_pending_tasks", map[string]any{})
tasks := result["tasks"].([]any)
```

Tools proxied through broker:
- Memory: `read_memory`, `write_memory`, `archive_memory`, `search_archive`, `read_archive`
- Soul: `read_soul`, `update_soul`
- Tasks: `add_task`, `mark_task_done`, `list_tasks`, `get_pending_tasks`, `get_workable_tasks`, etc.
- Knowledge Graph: 15+ node/edge/traversal tools
- Skills: `save_skill`, `list_skills`, `get_skill`, `delete_skill` (CRUD only - execution is local)

### LLM Client (`tools/broker/llm.go`)

LLM calls go through the broker, not direct Gemini API:

```go
type LLMClient struct {
    client *Client
}

func (c *LLMClient) GenerateContentStream(ctx context.Context, req *loop.GenerateRequest) (<-chan loop.StreamEvent, error) {
    // POST /broker/v1/llm/stream
    // Returns channel of streaming events
}
```

## Slash Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/clear` | Clear conversation history (immediate) |
| `/restart` | Restart session (warns agent first, then clears) |
| `/history` | Show conversation history |
| `/context` | Show short-term memory |
| `/report` | Show tool call statistics |
| `/tasks` | Show pending tasks |
| `/addtask <desc>` | Add a task for the agent |
| `/finished` | Show done and deprecated tasks |
| `/worktasks` | Start working through pending tasks |
| `/stoptasks` | Stop work tasks mode |
| `/recurring` | List recurring tasks |
| `/addrecurring` | Add a recurring task (prompts for interval) |
| `/deleterecurring` | Delete a recurring task (interactive) |
| `/smart` | Toggle smart mode (pro model + extended thinking) |
| `/outbox` | Show pending outgoing emails |
| `/approve <n>` | Approve and send a pending email |
| `/reject <n>` | Reject a pending email |
| `/image <path>` | Attach an image file to next message |
| `/paste` | Attach image from clipboard |
| `/file <path>` | Attach any file (PDF, code, text) |
| `/exit` | Exit the agent |

**Disabled in container mode** (use manager web UI instead):
- `/telegram <token>` - Configure via manager
- `/email apikey <key>` - Configure via manager
- `/whitelist` - Configure via manager

## Tools

### Local Tools (run in container)

**Python Sandbox** (`tools/python.go`):
- `run_python` - Execute Python code
- Runs in the container's Python environment
- Has pip, network access

**Shell** (`tools/shell.go`):
- `run_shell` - Execute shell commands
- Working directory: /data
- Tools: curl, wget, git, jq, grep, sed, awk, file, tree

**File** (`tools/file.go`):
- `read_file`, `write_file`, `edit_file`, `list_files`
- Paths relative to /data

**Skills Execution** (`tools/skills.go`):
- `execute_skill`, `test_skill` - Run locally
- CRUD operations go through broker

**Web/HTTP** (`tools/web_reader.go`, `tools/http.go`):
- `read_webpage`, `fetch_image`, `crop_image`
- `http_request` - Full HTTP client

**Browser** (`tools/browser.go`):
- Playwright-based browser automation
- `browse`, `browser_click`, `browser_type`, `browser_screenshot`, etc.

### Broker-Proxied Tools

**Memory** - Short/long-term memory via broker
**Soul** - Agent identity via broker
**Tasks** - Task management via broker
**Knowledge Graph** - 15 graph tools via broker
**Telegram** - Messaging via broker
**Email** - AgentMail integration via broker
**Google Search** - Search via broker (API key on manager)

## Context Management

**Heartbeat System**:
- Default: every 15 minutes
- Checks recurring tasks, creates instances
- Checks pending Telegram messages
- Triggers work tasks mode if tasks available

**Context Compaction** (`compaction_*.go`):
- `compaction_masking.go` - **Preferred**: Observation masking replaces verbose tool outputs with placeholders while preserving reasoning trajectory
- `compaction_legacy.go` - **Deprecated**: LLM-based history summarization (causes trajectory elongation)
- Triggered when context exceeds token threshold
- Preserves all user/model messages and reasoning
- Recent tool outputs kept in full

**Work Tasks Mode**:
- Enabled by `/worktasks` or heartbeat
- Agent works through pending tasks automatically
- 4-minute timeout between tasks
- Disabled when all tasks complete

## Project Structure

```
apps/agent/
├── main_entry.go        # CLI entry point, flag parsing, signal handling
├── main_globals.go      # Global variables (readline, broker clients, etc.)
├── main_session.go      # Interactive REPL session, heartbeat loop
├── main_interjection.go # User interruption handling
├── commands.go          # Command infrastructure (result enum, context, registry)
├── commands_dispatch.go # Slash command parser
├── commands_basic.go    # /help, /clear, /restart, /history, /report, /context
├── commands_io.go       # /image, /file, /paste
├── commands_exit.go     # /exit with cleanup
├── commands_messaging.go # /telegram, /email configuration
├── commands_recurring.go # /addrecurring, /deleterecurring
├── commands_smart.go    # /smart toggle
├── commands_tasks.go    # /tasks, /addtask, /worktasks, /stoptasks
├── output.go            # Structured JSON output emitter
├── compaction_masking.go # Context compaction via observation masking (preferred)
├── compaction_legacy.go # Legacy LLM-based summarization (deprecated)
├── tools_setup.go       # Tool registration and system prompt embedding
├── mcp_setup.go         # MCP server setup (local + host-side)
├── mermaid.go           # Mermaid diagram rendering
├── session_logger.go    # Session logging
├── sandbox_types.go     # Sandbox type definitions
├── sandbox_container.go # Docker container management
├── sandbox_image.go     # Docker image building
├── sandbox_ops.go       # Sandbox operations
├── Dockerfile           # Container image definition
├── data/
│   ├── data.go          # Table registration
│   ├── models.go        # Agent data model (shared with manager)
│   ├── tool_groups.go   # 17 tool category definitions
│   ├── mcp.go           # MCP configuration storage
│   ├── mcp_host_models.go # MCP registry models (global + per-agent)
│   ├── service_core.go  # Core service (CRUD, agent lookup)
│   ├── service_memory.go # Short-term memory operations
│   ├── service_archive.go # Memory archive
│   ├── service_soul.go  # Agent identity/personality
│   ├── service_context.go # Context summary management
│   ├── service_context_cleanup.go # Context cleanup
│   ├── service_tasks.go # Task management
│   ├── service_recurring.go # Recurring tasks
│   ├── service_skills.go # Skills CRUD
│   ├── service_wallet.go # Wallet data
│   ├── service_chat.go  # Chat history
│   ├── service_report.go # Report HTML persistence
│   ├── service_email_whitelist.go # Email whitelist
│   ├── service_pending_emails.go # Pending email approval
│   ├── service_peer_messages.go # Inter-agent messaging
│   ├── service_peer_groups.go # Peer group management
│   ├── service_mcp_registry.go # MCP server registry queries
│   ├── service_agents_list.go # Multi-agent listing
│   ├── service_cleanup.go # Data cleanup
│   └── service_helpers.go # Shared helper methods
├── tools/
│   ├── broker/          # Broker client for proxied operations
│   │   ├── client.go    # HTTP client for broker API
│   │   ├── llm.go       # LLM client (implements loop.LLMClient)
│   │   ├── memory.go    # Memory tools via broker
│   │   ├── soul.go      # Soul tools via broker
│   │   ├── context.go   # Context tools via broker
│   │   ├── tasks.go     # Task tools via broker
│   │   ├── kg.go        # Knowledge graph tools via broker
│   │   ├── skills.go    # Skills CRUD via broker
│   │   ├── telegram.go  # Telegram tools via broker
│   │   ├── email.go     # Email tools via broker
│   │   ├── search.go    # Google search via broker
│   │   ├── wallet.go    # Wallet tools via broker
│   │   ├── messaging.go # Peer messaging via broker
│   │   ├── mcp_admin.go # MCP admin tools via broker
│   │   └── report.go    # Report tools via broker
│   ├── browser_tools.go # Browser automation tool definitions
│   ├── browser_describe.go # Browser tool descriptions
│   ├── browser_server.go # Browser server management
│   ├── email.go         # Email tool definitions
│   ├── email_outbox.go  # Email outbox/approval tools
│   ├── file.go          # File operations (local)
│   ├── web_search.go    # Web search tool definition
│   ├── http.go          # HTTP request tool (local)
│   ├── content.go       # Content extraction tools
│   ├── context.go       # Context management tools
│   ├── messaging.go     # Inter-agent messaging tools
│   ├── python.go        # Python sandbox (local)
│   ├── shell.go         # Shell commands (local)
│   ├── skills.go        # Skills tool definitions
│   ├── soul.go          # Soul tool definitions
│   ├── report.go        # Report HTML tools
│   ├── mcp_admin_types.go # MCP admin tool types
│   ├── tasks_types.go   # Task type definitions
│   ├── tasks_tools_core.go # Core task tools (add, update, complete)
│   ├── tasks_tools_list.go # Task listing/query tools
│   ├── tasks_tools_plan.go # Task planning tools
│   ├── tasks_describe.go # Task tool descriptions
│   ├── tasks_helpers.go # Task formatting helpers
│   ├── tasks_format.go  # Task output formatting
│   ├── telegram_new.go  # Telegram tool constructors
│   ├── telegram_api.go  # Telegram API client
│   ├── telegram_inputs.go # Telegram input structs
│   ├── telegram_tools.go # Telegram tool methods
│   ├── telegram_describe.go # Telegram tool descriptions
│   ├── telegram_poll.go # Telegram message polling
│   ├── telegram_types.go # Telegram type definitions
│   ├── wallet_new.go    # Wallet tool constructors
│   ├── wallet_types.go  # Wallet type definitions
│   ├── wallet_describe.go # Wallet tool descriptions
│   ├── wallet_log.go    # Wallet transaction logging
│   ├── wallet_tools_address.go # Address tools
│   ├── wallet_tools_balance.go # Balance query tools
│   ├── wallet_tools_send.go # Token send tools
│   ├── wallet_tools_sign.go # Message signing tools
│   ├── wallet_tools_swap.go # Token swap tools
│   ├── wallet_tools_contract.go # Smart contract tools
│   ├── wallet_tools_history.go # Transaction history tools
│   ├── wallet_tools_crypto.go # Encryption tools
│   ├── reuters_tools.go # Reuters news tools
│   ├── reuters_fetch.go # Reuters article fetching
│   ├── reuters_helpers.go # Reuters parsing helpers
│   ├── reuters_types.go # Reuters type definitions
│   ├── time.go          # Time tool (local)
│   └── web_reader.go    # Web/image fetching (local)
├── system_prompt.md     # Default system prompt template
└── .env.example         # Environment template
```

## Environment Variables (Container)

The container receives these from the manager:

```bash
# Required - provided by manager
BROKER_SOCKET_PATH=/tmp/gowild-broker/broker.sock
GOWILD_AGENT_ETH_PRIVATE_KEY=<auth-only-ethereum-private-key>
GOWILD_BROKER_ONLY=1
GOWILD_AGENT_ID=jake

# NOT in container (filtered by manager)
# GEMINI_API_KEY - manager handles LLM calls
# GOWILD_DATABASE_URL - manager handles DB
# Web search uses Gemini Grounding (no separate API key needed)
```

## Key Dependencies

- `agentic_loop` - LLM framework with tool support
- `data` - Database abstraction (used by manager, not agent)
- `knowledge_graph` - Graph tools (proxied via broker)
- `crypto` - Wallet operations (proxied via broker)
- `my` - Shared utilities (.env loading)
- `github.com/chzyer/readline` - Interactive line editing

## Adding New Tools

For **local tools** (run in container):
1. Create file in `tools/` directory
2. Define input struct with JSON/description tags
3. Implement tool method ending in "Tool"
4. Add to `main.go` in appropriate `add*Tools()` function

For **broker-proxied tools** (need DB/secrets):
1. Add endpoint in `apps/agent_manager/broker_tools.go`
2. Create wrapper in `tools/broker/` that calls the endpoint
3. Add to `main.go` using the broker wrapper

## CLI Admin Commands

These run on the host (not in container) for agent management:

```bash
# Create agent
./agent -create-agent <name> [-seed-phrase "..."] [-telegram-token "..."]

# Configure agent
./agent -agent <id> -telegram-token <token>
./agent -agent <id> -email-apikey <key>
./agent -agent <id> -email-inbox <inbox_id>

# List/delete agents
./agent -list-agents
./agent -delete-agent <id>
```

These commands use direct database access and are not available inside containers.
