# CLAUDE.md

This file provides guidance to Claude Code when working with the `knowledge_graph` library (Go package `gowild_knowledge_graph`).

## Build and Test Commands

```bash
# Build the package
go build ./...

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -run TestCreateAndGetNode -v
```

## Architecture

This package implements a knowledge graph with persistent storage via the `data` module.

### Core Components

**Models** (`models.go`):
- `Node` - Graph vertices with ID, name, type, and JSON properties
- `Edge` - Directed relationships with source/target, type, weight, and properties
- `QueryResult` - Container for graph query results
- `TraversalOptions` - Configuration for graph traversal

**Data Layer** (`data.go`):
- Auto-registers `Node` and `Edge` tables via `init()`
- Uses `gowild_data.RegisterFunc()` for auto-discovery

**Service** (`service.go`):
- `Service` - Main API for graph operations
- CRUD operations for nodes and edges
- Graph traversal and path finding algorithms
- User-scoped via `gowild_data.ForUser()`

**Tools** (`tools.go`):
- `Tools` - Agent tool wrapper for `gowild_agentic_loop`
- Methods ending in "Tool" are auto-discovered
- Input structs define tool schemas via tags

### Data Model

```
Node:
- ID (string, primary key)
- UserID (string, for multi-tenant)
- Name (string)
- Type (string)
- Notes (string, free-form text — agents should store observations, context, and learned details here)
- Properties (map[string]any, JSON)
- CreatedAt, UpdatedAt (time.Time)

Edge:
- ID (string, primary key)
- UserID (string)
- SourceNodeID (string, references Node)
- TargetNodeID (string, references Node)
- RelationType (string)
- Properties (map[string]any, JSON)
- Weight (float64)
- CreatedAt, UpdatedAt (time.Time)
```

### Graph Algorithms

**GetNeighbors**: Returns immediately connected nodes
- Filters by relation type and node type
- Optional reverse edge traversal

**Traverse**: BFS from starting node
- Configurable max depth
- Collects all reachable nodes/edges
- Deduplicates visited nodes

**FindPath**: BFS shortest path
- Returns path nodes and edges
- Fails if no path exists within max depth

## Key Dependencies

- `github.com/original-david-knight/go_wild/agentic_loop` - Agent tool interface
- `github.com/original-david-knight/go_wild/data` - Database abstraction
- `github.com/google/uuid` - ID generation

## Usage Patterns

### Creating a Service

```go
db, _ := gowild_data.NewSqliteDatabase(":memory:")
db.AddTable(kg.Node{})
db.AddTable(kg.Edge{})

service := kg.NewService(db, "user-id")
```

### Testing

Use in-memory database:

```go
db, _ := gowild_data.NewSqliteDatabase(":memory:")
defer db.Close()
db.AddTable(kg.Node{})
db.AddTable(kg.Edge{})

service := kg.NewService(db, "test-user")
// ... run tests
```

### Query Result Handling

```go
// Query returns []any, convert to typed slice
results, _ := dao.Query(ctx, opts)
nodes := convertNodes(results)  // Uses type assertion
```

## Agent Tool Integration

Tools integrate with `gowild_agentic_loop` via the auto-discovery pattern:

```go
// Tools implements ToolProvider interface for descriptions
kgTools := kg.NewTools(db, userID)

// WrapToolsWithDescriptions discovers all *Tool methods and populates descriptions
tools := gowild_agentic_loop.WrapToolsWithDescriptions(kgTools)

// tools is []gowild_agentic_loop.Tool, ready for AgenticLoop
```

**Tool Method Signature**: All tool methods follow this pattern:
```go
func (t *Tools) CreateNodeTool(ctx context.Context, input CreateNodeInput) (*gowild_agentic_loop.ToolResult, error)
```

**Input Structs**: Define tool schemas via struct tags:
- `json:"name"` - Field name in JSON
- `description:"..."` - Field description for schema
- `required:"true"` - Mark field as required
- `json:"field,omitempty"` - Mark field as optional

## Common Patterns

1. **User Isolation**: Always use `db.ForUser(userID)` for user-scoped operations
2. **Transaction Safety**: `DeleteNode` uses `RunInTransaction` to delete edges atomically
3. **ID Generation**: Uses `github.com/google/uuid` via `newID()` helper
4. **Search**: In-memory filtering since data layer doesn't support LIKE queries
5. **Tool Descriptions**: Implement `DescribeTool(name string) string` for ToolProvider interface
