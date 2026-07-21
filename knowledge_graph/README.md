# gowild_knowledge_graph

A knowledge graph implementation for Go with persistent storage support via the `gowild_data` layer.

## Features

- **Nodes**: Represent entities with type, name, free-form notes, and arbitrary JSON properties
- **Edges**: Directed relationships between nodes with type, weight, and properties
- **Multi-tenant**: User-scoped data isolation via `gowild_data.ForUser()`
- **Graph Traversal**: BFS-based traversal with configurable depth and filters
- **Path Finding**: Shortest path discovery between nodes
- **Agent Tools**: Ready-to-use tools for AI agent integration

## Installation

```go
import "github.com/anthropics/wilder/golang/gowild_knowledge_graph"
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/anthropics/wilder/golang/gowild_data"
    kg "github.com/anthropics/wilder/golang/gowild_knowledge_graph"
)

func main() {
    // Create database
    db, _ := gowild_data.NewSqliteDatabase("knowledge.db")
    defer db.Close()

    // Register tables (auto-discovery via init())
    gowild_data.AddAllTables(db)

    // Create service for a user
    ctx := context.Background()
    service := kg.NewService(db, "user-123")

    // Create nodes
    alice, _ := service.CreateNode(ctx, "Alice", kg.NodeTypePerson, "Senior backend engineer, leads infrastructure team", map[string]any{
        "role": "engineer",
    })

    project, _ := service.CreateNode(ctx, "Project X", kg.NodeTypeEntity, "Internal platform rewrite, targeting Q3 launch", map[string]any{
        "status": "active",
    })

    // Create relationship
    service.CreateEdge(ctx, alice.ID, project.ID, kg.RelationTypeOwnedBy, nil, 1.0)

    // Traverse the graph
    result, _ := service.Traverse(ctx, alice.ID, kg.TraversalOptions{MaxDepth: 2})
    log.Printf("Found %d nodes", len(result.Nodes))
}
```

## Node Types

Built-in constants for common node types:

- `NodeTypeConcept` - Abstract concepts
- `NodeTypeEntity` - General entities
- `NodeTypePerson` - People
- `NodeTypeOrganization` - Organizations
- `NodeTypeEvent` - Events
- `NodeTypeLocation` - Places
- `NodeTypeDocument` - Documents

## Relation Types

Built-in constants for common relationships:

- `RelationTypeRelatedTo` - General relation
- `RelationTypePartOf` - Part-whole
- `RelationTypeHasA` - Ownership
- `RelationTypeIsA` - Classification
- `RelationTypeDependsOn` - Dependency
- `RelationTypeCreatedBy` - Authorship
- `RelationTypeOwnedBy` - Ownership
- `RelationTypeReferences` - Reference
- `RelationTypePrecedes` / `RelationTypeFollows` - Temporal
- `RelationTypeSimilarTo` - Similarity
- `RelationTypeContradicts` - Opposition

## Service API

### Node Operations

```go
// Create a new node
node, err := service.CreateNode(ctx, name, nodeType, notes, properties)

// Get node by ID
node, err := service.GetNode(ctx, nodeID)

// Update node
err := service.UpdateNode(ctx, node)

// Delete node (and connected edges)
err := service.DeleteNode(ctx, nodeID)

// List all nodes (optionally filtered by type)
nodes, err := service.ListNodes(ctx, nodeType)

// Search nodes by name
nodes, err := service.SearchNodes(ctx, "pattern")
```

### Edge Operations

```go
// Create relationship
edge, err := service.CreateEdge(ctx, sourceID, targetID, relationType, properties, weight)

// Get edge by ID
edge, err := service.GetEdge(ctx, edgeID)

// Update edge
err := service.UpdateEdge(ctx, edge)

// Delete edge
err := service.DeleteEdge(ctx, edgeID)

// Get edges from/to a node
outgoing, err := service.GetOutgoingEdges(ctx, nodeID, relationType)
incoming, err := service.GetIncomingEdges(ctx, nodeID, relationType)
```

### Graph Queries

```go
// Get immediate neighbors
opts := kg.TraversalOptions{
    RelationTypes:  []string{kg.RelationTypePartOf},
    NodeTypes:      []string{kg.NodeTypePerson},
    IncludeReverse: true,
}
result, err := service.GetNeighbors(ctx, nodeID, opts)

// Traverse from a starting node
opts := kg.TraversalOptions{MaxDepth: 3}
result, err := service.Traverse(ctx, startNodeID, opts)

// Find shortest path
opts := kg.TraversalOptions{MaxDepth: 5}
path, err := service.FindPath(ctx, startID, endID, opts)
```

## Agent Tools

The package provides agent tools for integration with `gowild_agentic_loop`. Tools implement the `ToolProvider` interface for descriptions and use the auto-discovery pattern (methods ending in "Tool"):

```go
import (
    "github.com/anthropics/wilder/golang/gowild_agentic_loop"
    kg "github.com/anthropics/wilder/golang/gowild_knowledge_graph"
)

// Create tools instance
kgTools := kg.NewTools(db, userID)

// Wrap for use with AgenticLoop (auto-discovers all *Tool methods)
tools := gowild_agentic_loop.WrapToolsWithDescriptions(kgTools)

// Add to your agentic loop
loop := gowild_agentic_loop.NewAgenticLoop(client, model, tools)
```

**Available Tools (12 total):**

| Tool Name | Description |
|-----------|-------------|
| `create_node` | Create a new node in the knowledge graph |
| `get_node` | Retrieve a node by its ID |
| `update_node` | Update an existing node's properties |
| `delete_node` | Delete a node and all connected edges |
| `list_nodes` | List all nodes, optionally filtered by type |
| `search_nodes` | Search for nodes by name pattern |
| `create_edge` | Create a relationship between two nodes |
| `get_edge` | Retrieve an edge by its ID |
| `delete_edge` | Delete an edge |
| `get_neighbors` | Get directly connected nodes |
| `traverse` | BFS traversal from a starting node |
| `find_path` | Find shortest path between two nodes |

## Testing

```bash
go test -v ./...
```
