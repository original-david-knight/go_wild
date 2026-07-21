# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the package
go build ./...

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -run TestSerializeDeserializeHistory_RoundTrip -v

# Run tests for a specific package
go test ./mcp -v
```

## Architecture

This is a Go package for building agentic loops with Google Gemini. The design is stateless - callers manage conversation history and pass it to each `Run()` call.

### Core Components

**AgenticLoop** (`loop_*.go`) - Main orchestrator split across focused files:
- `loop_init.go` - Constructor, configuration, client setup
- `loop_run.go` - Core run loop: API calls, tool execution, event streaming
- `loop_retry.go` - Transient error retry with exponential backoff
- `loop_prompt.go` - Single-turn convenience wrapper
- `loop_options.go` - Functional options (WithSystemPrompt, WithTools, etc.)
- `loop_types.go` - Loop-specific type definitions

Key capabilities:
- Calls Gemini API with conversation history
- Executes tools when requested by the model
- Streams events via Go channels
- Runs multiple tool calls in parallel
- Supports extended thinking with configurable token budget
- Retries transient errors with exponential backoff

**GeminiClient** (`gemini.go`) - Low-level Gemini API wrapper:
- Supports both synchronous and streaming generation
- Configurable temperature, max tokens, and thinking budget
- Extracts text while filtering out thought parts

**Event System** (`types.go`) - Events streamed from `Run()`:
- `TextDeltaEvent` - Generated text chunks
- `ToolCallEvent` - Tool invocation requests
- `ToolResultEvent` - Tool execution results
- `DoneEvent` - Completion with usage stats, turn count, stop reason
- `ErrorEvent` - Errors

**Message Types** (`types.go`) - Conversation building blocks:
- `NewUserMessage(text)` - Simple text message
- `NewUserMessageWithImage(text, imageData, mimeType)` - Message with image
- `NewModelTextMessage(text)` - Simple model text response
- `NewToolResultMessage(name, result)` - Tool execution result

**Tool Results** (`types.go`):
- `NewSuccessResult(content)` - Successful execution
- `NewSuccessResultWithImage(content, imageData, mimeType)` - Success with image
- `NewErrorResult(errString)` - Error result

**Tool Interface** (`tool.go`) - Tools must implement:
```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() *genai.Schema
    Execute(ctx context.Context, input map[string]any) (*ToolResult, error)
}
```

### Tool Registration Methods

Three ways to create tools, in order of preference:

1. **Struct-based with auto-discovery** (`tool_registry.go`) - Methods ending in "Tool" with signature `func(ctx context.Context, InputStruct) (*ToolResult, error)` are discovered via `WrapTools()` or `WrapToolsWithDescriptions()`

2. **Functional tools** - Use `NewFuncTool(name, description, schema, fn)` for simple cases

3. **Explicit definitions** - Use `WrapToolsWithDefinitions(provider, definitions)` for full control

### Tool Provider Interface

Implement `ToolProvider` to provide descriptions:
```go
type ToolProvider interface {
    DescribeTool(name string) string
}
```

### Schema Generation

`schema.go` generates Gemini JSON schemas from Go structs using reflection. Supported tags:
- `json:"name"` - Field name
- `description:"..."` - Field description
- `enum:"a,b,c"` - Allowed values
- `required:"true"` - Mark required (default: required unless `omitempty`)

Pointer fields are optional by default. Schemas are generated internally by `schemaFromType(t)`.

### Serialization

`serialization.go` provides JSON serialization for persisting conversation history to databases:
- `SerializeHistory()` / `DeserializeHistory()` - Convert message slices
- `SerializeContent()` / `DeserializeContent()` / `DeserializeContents()` - broker-path content serialization
- Preserves Gemini's `ThoughtSignature` fields (base64 encoded)
- Handles function calls and function responses

### MCP Integration

`mcp/` package provides Model Context Protocol client support:
- `Client` interface - Initialize, ListTools, CallTool, Close
- `StdioClient` - Connect to MCP servers via stdio
- `WrapMCPTools(client, mcpTools)` - Convert MCP tools to the Tool interface

### Convenience Methods

- `loop.Run(ctx, history)` - Async streaming, returns event channel
- `loop.RunSync(ctx, history)` - Blocking, returns final `DoneEvent`
- `loop.Prompt(ctx, text)` - Single-turn convenience wrapper

### Configuration Options

```go
loop, err := gowild_agentic_loop.New(ctx, apiKey, model,
    WithSystemPrompt("You are..."),
    WithMaxTurns(10),
    WithTools(tool1, tool2),
)

// Runtime configuration
loop.SetModel("gemini-3-flash-preview")
loop.SetThinkingBudget(8192) // Enable extended thinking
loop.AddTools(moreTool)
```

## Key Dependencies

- `google.golang.org/genai` v1.44.0 - Gemini API client (supports Gemini 3)
- `github.com/joho/godotenv` - .env file loading (via the `my` module)

## Environment Variables

- `GEMINI_API_KEY` - Required for API access (auto-loaded from .env)

The package searches current directory and up to 5 parent directories for `.env` files.

## Default Values

- Model: `gemini-3-flash-preview`
- Max turns: 10
- System prompt: Generic helpful assistant message
