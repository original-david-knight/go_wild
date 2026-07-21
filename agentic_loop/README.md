# gowild_agentic_loop

A Go package for building agentic loops with LLM tool support. Uses Google's Gemini API and supports MCP (Model Context Protocol) for external tool integration.

## Features

- **Streaming events** via Go channels
- **Parallel tool execution** for multiple simultaneous tool calls
- **Struct-based tool definitions** with automatic JSON Schema generation
- **MCP client support** for external tool servers
- **Stateless design** - caller manages conversation history
- **Automatic .env loading** - loads `GEMINI_API_KEY` from `.env` files

## Installation

```bash
go get github.com/anthropics/wilder/golang/gowild_agentic_loop
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    loop "github.com/anthropics/wilder/golang/gowild_agentic_loop"
)

func main() {
    ctx := context.Background()

    // Create agent (uses GEMINI_API_KEY env var)
    agent, _ := loop.New(ctx, "", "",
        loop.WithSystemPrompt("You are a helpful assistant."),
    )
    defer agent.Close()

    // Run and stream events
    for event := range agent.Run(ctx, []loop.Message{loop.NewUserMessage("Hello!")}) {
        switch e := event.(type) {
        case loop.TextDeltaEvent:
            fmt.Print(e.Text)
        case loop.DoneEvent:
            fmt.Printf("\n(tokens: %d)\n", e.Usage.TotalTokens)
        }
    }
}
```

## Adding Tools

### Method 1: Struct-based tools (recommended)

```go
// Define input struct with tags for schema generation
type WeatherInput struct {
    Location string `json:"location" description:"City and state"`
    Unit     string `json:"unit,omitempty" enum:"celsius,fahrenheit"`
}

// Tool provider with methods ending in "Tool"
type MyTools struct{}

func (t *MyTools) GetWeatherTool(ctx context.Context, input WeatherInput) (*loop.ToolResult, error) {
    return loop.NewSuccessResult(map[string]any{
        "temperature": 72,
        "condition":   "Sunny",
    }), nil
}

// Implement ToolProvider for descriptions
func (t *MyTools) DescribeTool(name string) string {
    return map[string]string{
        "get_weather": "Get weather for a location",
    }[name]
}

// Register tools
agent.AddTools(loop.WrapToolsWithDescriptions(&MyTools{})...)
```

### Method 2: Functional tools

```go
agent.AddTools(loop.NewFuncTool(
    "get_time",
    "Get the current time",
    &genai.Schema{Type: genai.TypeObject},
    func(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
        return loop.NewSuccessResult("3:42 PM"), nil
    },
))
```

### Method 3: Explicit definitions

```go
agent.AddTools(loop.WrapToolsWithDefinitions(&MyTools{}, []loop.ToolDefinition{
    {Name: "weather", Description: "Get weather", Method: "GetWeatherTool"},
})...)
```

## MCP Integration

Connect to external MCP servers:

```go
import "github.com/anthropics/wilder/golang/gowild_agentic_loop/mcp"

// Connect to MCP server via stdio
client, _ := mcp.NewStdioClient(ctx, "node", "path/to/mcp-server.js")
defer client.Close()

// Get tools from MCP server
tools, _ := client.ListTools(ctx)
agent.AddTools(tools...)
```

## Events

The `Run()` method returns a channel that emits:

- `TextDeltaEvent` - Streamed text chunks
- `ToolCallEvent` - Tool invocation requests
- `ToolResultEvent` - Tool execution results
- `DoneEvent` - Loop completion with usage stats
- `ErrorEvent` - Errors during execution

## Configuration

```go
loop.New(ctx, apiKey, model,
    loop.WithSystemPrompt("Custom system prompt"),
    loop.WithMaxTurns(10),           // Max agentic turns
    loop.WithTools(myTools...),      // Add tools during creation
)
```

## Environment Variables

The package automatically loads environment variables from `.env` files when creating an agent. It searches the current directory and up to 5 parent directories.

### Supported Variables

- `GEMINI_API_KEY` - Gemini API key (used if apiKey parameter is empty)

### Example .env file

```env
GEMINI_API_KEY=your-api-key-here
```

### Manual Loading

You can also load environment variables manually via the `my` package:

```go
import "github.com/original-david-knight/go_wild/my"

// Load from default locations (.env in current or parent directories)
gowild_my.LoadEnv()
```
