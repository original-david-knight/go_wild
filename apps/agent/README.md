# GoWild Agent

A terminal-based AI agent powered by Google Gemini.

## Setup

1. Copy the example environment file:
   ```bash
   cp .env.example .env
   ```

2. Add your Gemini API key to `.env`:
   ```
   GEMINI_API_KEY=your-api-key-here
   ```

3. Build and run:
   ```bash
   go build .
   ./gowild_agent
   ```

## Usage

```bash
# Run with defaults
./gowild_agent

# Use a specific model
./gowild_agent -model gemini-2.0-flash

# Custom system prompt
./gowild_agent -system "You are a coding assistant specializing in Go."

# Limit agentic turns
./gowild_agent -max-turns 5
```

## Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/clear` | Clear conversation history |
| `/history` | Show conversation history |
| `/exit` | Exit the agent |

## Tools

Tools are automatically loaded based on environment variables.

### Web Search

Search the web using Gemini Grounding with Google Search. No separate API key needed — uses your existing `GEMINI_API_KEY`.

**Usage:**
```
you> Search for the latest news about Golang
assistant> [calling web_search...]
[web_search completed]
Here's what I found...
```

## Features

- **Interactive REPL** with readline support (history, line editing)
- **Streaming responses** - see text as it's generated
- **Conversation history** - maintains context across messages
- **Tool support** - extensible with custom tools
- **Colored output** - easy to distinguish roles and events
- **Auto .env loading** - searches parent directories for `.env`

## Project Structure

```
gowild_agent/
├── main.go           # CLI entry point
├── tools/
│   └── web_search.go  # Web search tool
├── .env.example      # Environment template
└── README.md
```

## Adding New Tools

Create a new file in `tools/` following the pattern:

```go
func CreateMyTool() loop.Tool {
    return loop.NewFuncTool(
        "my_tool",
        "Description of what the tool does",
        &genai.Schema{...},
        func(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
            // Tool implementation
            return loop.NewSuccessResult(result), nil
        },
    )
}
```

Then add it in `main.go`:
```go
func addTools(agent *loop.AgenticLoop) {
    agent.AddTools(tools.CreateMyTool())
}
```

## Roadmap

- [x] Web Search tool
- [ ] MCP server integration
- [ ] Web interface
- [ ] Configuration file support
