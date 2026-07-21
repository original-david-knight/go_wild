# Knowledge Graph Architecture: Implementation Plan

This document describes enhancements to `gowild_knowledge_graph` to transform it from a static store into a provenance-aware, self-maintaining belief state. Based on a feasibility analysis against the current codebase (~2,155 lines), the plan prioritizes practical wins and avoids over-engineering.

## Current State

The KG already has:
- **Node/Edge model** with `Properties map[string]any` for arbitrary metadata
- **`ConfidenceScore *float64`** on edges (0.0-1.0) — already wired into `CreateEdgeTool`
- **`ValidFrom *time.Time`** on edges — temporal validity for time-bound facts
- **BFS traversal** (`Traverse`, `FindPath`, `GetNeighbors`) up to configurable depth
- **Semantic search** via Gemini `gemini-embedding-001` embeddings + cosine similarity
- **15 agent tools** auto-discovered via struct method convention
- **Dual DB support** (SQLite + PostgreSQL) via `gowild_data` with auto-migration

## 1. Provenance (Isnad/Evidence Links)

**Status: New — implement as column additions to Edge**

Every edge gains first-class provenance fields so the agent can trace where a fact came from and what extracted it.

### Schema Changes (models.go)

Add to `Edge` struct:

```go
Source      string     `json:"source,omitempty"`       // URL or document identifier
ExtractedBy string    `json:"extracted_by,omitempty"` // tool or agent that created this fact
ExtractedAt *time.Time `json:"extracted_at,omitempty"` // when the fact was extracted from source
```

These are nullable/omitempty so existing edges are unaffected. `gowild_data.ensureColumns()` auto-migrates the new columns.

### Tool Changes (tools.go)

Update `CreateEdgeInput` and `UpdateEdgeInput` DTOs:

```go
type CreateEdgeInput struct {
    // ... existing fields ...
    Source      string `json:"source,omitempty" description:"URL or document the fact was extracted from"`
    ExtractedBy string `json:"extracted_by,omitempty" description:"Tool or agent that generated this fact"`
    ExtractedAt string `json:"extracted_at,omitempty" description:"RFC3339 timestamp of extraction"`
}
```

### Agent Behavior

The agent's system prompt should instruct it to populate provenance fields when creating edges from external sources (web search results, documents, tool outputs). This enables "Deep Verification" — the agent can later cross-check facts by revisiting their source URLs.

### Estimated Effort

~50-100 lines across `models.go`, `tools.go`. No architectural changes.

## 2. Confidence Scores

**Status: Already implemented — leverage via prompting**

`Edge.ConfidenceScore *float64` already exists and is accepted by `CreateEdgeTool`. No code changes needed.

### Agent-Side Strategy: Active Foraging

The agent's system prompt and heartbeat logic should implement Active Foraging:

1. During heartbeat, query edges with low confidence scores (< 0.5)
2. Prioritize re-verification of uncertain facts over discovering new ones
3. When re-verifying, update the confidence score based on new evidence
4. Use confidence as a tiebreaker when traversal returns multiple paths

This is a prompt engineering and heartbeat logic task, not a KG module change.

## 3. Lightweight Consistency Checks

**Status: New — implement as a focused duplicate/contradiction detector**

Full transitive consistency checking (A* pathfinding, OWL reasoners, async conflict agents) is not justified at current scale. The current traversal loads all nodes from DB with no lazy evaluation, and semantic contradiction detection would require LLM calls on every edge creation.

### What We Build Instead

A synchronous `CheckConsistency()` method that runs on `CreateEdge` and catches the most common problems:

```go
func (s *Service) CheckConsistency(ctx context.Context, edge *Edge) (*ConsistencyResult, error)
```

**Checks performed:**

1. **Duplicate detection** — Does an edge with the same source, target, and relation type already exist? If so, return the existing edge ID instead of creating a duplicate.

2. **Inverse contradiction** — Does an edge exist from target→source with a contradicting relation type? Maintain a small map of known contradictions:
   ```go
   var contradictions = map[string]string{
       "is_a":       "is_not_a",
       "supports":   "contradicts",
       "causes":     "prevents",
       "enables":    "disables",
       "agrees_with": "disagrees_with",
   }
   ```

3. **Self-loop detection** — Reject edges where source == target.

### ConsistencyResult

```go
type ConsistencyResult struct {
    OK          bool   `json:"ok"`
    Issue       string `json:"issue,omitempty"`       // "duplicate", "contradiction", "self_loop"
    ConflictID  string `json:"conflict_id,omitempty"` // ID of the conflicting edge
    Suggestion  string `json:"suggestion,omitempty"`  // what the agent should do
}
```

When a conflict is detected, the tool returns the `ConsistencyResult` in the tool output so the agent can decide how to resolve it (update the existing edge, delete the old one, or abandon the new one). The KG does not auto-resolve — it reports and lets the agent decide.

### What We Skip

- Async conflict resolution agents
- Multi-hop transitive reasoning on every write
- OWL/RDFS reasoner integration
- LLM-based semantic contradiction detection

These can be revisited if the graph grows beyond ~10K edges per user.

### Estimated Effort

~200-300 lines: `ConsistencyResult` struct, `CheckConsistency()` method, integration into `CreateEdgeTool`, contradiction map, tests.

## 4. Synaptic Pruning (TTL / Expiration)

**Status: New — implement via ValidUntil + background ticker**

Facts decay. A prediction about next week's weather shouldn't persist indefinitely. The KG needs an expiration mechanism that marks stale facts as invalid without deleting them (preserving audit history).

### Schema Changes (models.go)

Add to `Edge` struct:

```go
ValidUntil *time.Time `json:"valid_until,omitempty"` // when this fact expires
Status     string     `json:"status,omitempty"`      // "active" (default), "expired", "invalid"
VerifiedAt *time.Time `json:"verified_at,omitempty"` // last re-verification timestamp
```

Add to `Node` struct:

```go
Status     string     `json:"status,omitempty"`      // "active" (default), "expired", "invalid"
LastUsedAt *time.Time `json:"last_used_at,omitempty"` // updated on traversal/query hits
```

### Background Pruning Worker

```go
func (s *Service) StartPruningWorker(ctx context.Context, interval time.Duration)
```

Runs on a `time.Ticker` (default interval: 1 hour). Each tick:

1. Find all edges where `valid_until < now()` AND `status = "active"`
2. Set `status = "expired"` (soft delete — preserves audit trail)
3. Find all nodes with no remaining active edges and `last_used_at` older than a configurable threshold
4. Mark orphaned nodes as `"expired"`

The worker is started by the `Service` owner (agent manager or standalone agent). It accepts a `context.Context` for clean shutdown.

### Query-Time Filtering

All traversal and query methods (`Traverse`, `FindPath`, `GetNeighbors`, `SemanticSearch`) gain an option:

```go
type QueryOptions struct {
    IncludeExpired bool // default false — only return active nodes/edges
}
```

By default, expired facts are invisible to the agent. Setting `IncludeExpired: true` allows the agent to explicitly review historical data when needed.

### Tool Changes

Update `CreateEdgeTool` to accept `valid_until` (RFC3339 string). The agent decides TTL based on fact type:
- Predictions/forecasts: hours to days
- Current events: days to weeks
- Stable facts (definitions, identities): no expiration

### Estimated Effort

~150-200 lines: schema fields, `StartPruningWorker()`, query filtering, tool DTO updates, tests.

## 5. Tool Consolidation (15 → 6)

**Status: New — reduce agent-facing tool count by 60%**

The current 15 tools pollute the agent's context window with schema definitions and descriptions, increasing the chance of tool confusion. Most tools are thin CRUD wrappers that can be merged by intent.

### Current Tool Inventory

| Category | Tools | Count |
|----------|-------|-------|
| Node CRUD | `create_node`, `get_node`, `update_node`, `delete_node`, `list_nodes`, `search_nodes` | 6 |
| Edge CRUD | `create_edge`, `get_edge`, `update_edge`, `delete_edge` | 4 |
| Traversal | `get_neighbors`, `traverse`, `find_path` | 3 |
| Semantic | `semantic_search`, `find_similar_nodes` | 2 |

### Consolidated Tools

#### `kg_search` — Find knowledge (replaces 4 tools)

Replaces: `search_nodes`, `list_nodes`, `semantic_search`, `find_similar_nodes`

```go
type KGSearchInput struct {
    Query    string `json:"query,omitempty" description:"Text or natural language query"`
    Mode     string `json:"mode,omitempty" description:"Search mode: text (default), semantic, similar, list" enum:"text,semantic,similar,list"`
    NodeID   string `json:"node_id,omitempty" description:"Node ID for 'similar' mode"`
    NodeType string `json:"node_type,omitempty" description:"Filter results by node type"`
    Limit    int    `json:"limit,omitempty" description:"Max results (default 10)"`
}
```

Dispatch logic:
- `mode=list` → `ListNodes(type)`
- `mode=semantic` → `SemanticSearch(query, limit)`
- `mode=similar` → `FindSimilarNodes(nodeID, limit)`
- `mode=text` (default) → `SearchNodes(query)`, filtered by `node_type`

#### `kg_add` — Add knowledge (replaces 2 tools)

Replaces: `create_node`, `create_edge`

```go
type KGAddInput struct {
    // Node fields
    Name       string         `json:"name,omitempty" description:"Node name (creates a node)"`
    Type       string         `json:"type,omitempty" description:"Node type or edge relation type"`
    Properties map[string]any `json:"properties,omitempty" description:"Additional properties"`

    // Edge fields (presence of these triggers edge creation)
    SourceNodeID    string   `json:"source_node_id,omitempty" description:"Source node ID (creates an edge)"`
    TargetNodeID    string   `json:"target_node_id,omitempty" description:"Target node ID (creates an edge)"`
    Weight          float64  `json:"weight,omitempty" description:"Edge weight (default 1.0)"`
    ValidFrom       string   `json:"valid_from,omitempty" description:"RFC3339 timestamp for time-bound facts"`
    ConfidenceScore *float64 `json:"confidence_score,omitempty" description:"Certainty 0.0-1.0"`

    // Provenance (new fields from section 1)
    Source      string `json:"source,omitempty" description:"URL or document the fact came from"`
    ExtractedBy string `json:"extracted_by,omitempty" description:"Tool or agent that generated this"`
}
```

Dispatch logic:
- If `source_node_id` + `target_node_id` present → create edge (with consistency check from section 3)
- Else → search for duplicate by name first, then create node

The built-in duplicate check eliminates the need for the current tool description hack ("ALWAYS use search_nodes first").

#### `kg_get` — Get by ID (replaces 2 tools)

Replaces: `get_node`, `get_edge`

```go
type KGGetInput struct {
    ID string `json:"id" description:"Node or edge ID to retrieve" required:"true"`
}
```

Try node lookup first. If not found, try edge lookup. Return whichever matches.

#### `kg_update` — Update by ID (replaces 2 tools)

Replaces: `update_node`, `update_edge`

```go
type KGUpdateInput struct {
    ID              string         `json:"id" description:"Node or edge ID to update" required:"true"`
    Name            string         `json:"name,omitempty" description:"New name (nodes only)"`
    Type            string         `json:"type,omitempty" description:"New type (nodes) or relation type (edges)"`
    Properties      map[string]any `json:"properties,omitempty" description:"New properties (replaces existing)"`
    Weight          *float64       `json:"weight,omitempty" description:"New weight (edges only)"`
    ValidFrom       string         `json:"valid_from,omitempty" description:"RFC3339 timestamp (edges only)"`
    ConfidenceScore *float64       `json:"confidence_score,omitempty" description:"Certainty 0.0-1.0 (edges only)"`
}
```

Auto-detects node vs edge by attempting node lookup first, falling back to edge.

#### `kg_delete` — Delete by ID (replaces 2 tools)

Replaces: `delete_node`, `delete_edge`

```go
type KGDeleteInput struct {
    ID string `json:"id" description:"Node or edge ID to delete" required:"true"`
}
```

Same auto-detect pattern. Node deletion cascades edges (existing behavior preserved).

#### `kg_explore` — Explore relationships (replaces 3 tools)

Replaces: `get_neighbors`, `traverse`, `find_path`

```go
type KGExploreInput struct {
    StartNodeID    string   `json:"start_node_id" description:"Starting node ID" required:"true"`
    EndNodeID      string   `json:"end_node_id,omitempty" description:"Target node ID (triggers shortest path search)"`
    MaxDepth       int      `json:"max_depth,omitempty" description:"Max traversal depth (default 1 for neighbors, 3 for traverse, 5 for path)"`
    RelationTypes  []string `json:"relation_types,omitempty" description:"Filter by relation types"`
    NodeTypes      []string `json:"node_types,omitempty" description:"Filter by node types"`
    IncludeReverse *bool    `json:"include_reverse,omitempty" description:"Include incoming edges (default true)"`
}
```

Dispatch logic:
- If `end_node_id` present → `FindPath(start, end, maxDepth=5)`
- If `max_depth` > 1 → `Traverse(start, maxDepth)`
- Else → `GetNeighbors(start)` (single-hop, the most common case)

### Implementation Notes from Code Review

**Return type normalization for `kg_search`:** The four merged tools currently return different types — `search_nodes`/`list_nodes` return `[]NodeDTO` while `semantic_search`/`find_similar_nodes` return `[]ScoredNodeDTO` (with float32 similarity scores). The consolidated tool must return a uniform type. Solution: always return `[]ScoredNodeDTO`. For `text` and `list` modes, set `Score: 1.0` on all results. This keeps the agent's response parsing consistent across modes.

**`kg_explore` gains a capability:** `find_path` currently hides the `NodeTypes` filter from its input (the underlying `TraversalOptions` supports it, but `FindPathInput` doesn't expose it). The consolidated `KGExploreInput` includes `NodeTypes` for all modes, which is a minor improvement.

**Auto-detect via try-node-then-edge is safe:** Both nodes and edges use `uuid.New()` for IDs — globally unique, no collisions. The `kg_get`/`kg_update`/`kg_delete` pattern of trying node lookup first, then edge, costs at most one extra `Get` by primary key for edge operations. Negligible.

### Files Changed (3 files, atomic deploy)

| File | Current | After |
|------|---------|-------|
| `gowild_knowledge_graph/tools.go` | 15 methods, 15 input structs, 502 lines | 6 methods, 6 input structs |
| `gowild_agent/tools/broker/kg.go` | 15 proxy methods, 160 lines | 6 proxy methods |
| `gowild_agent_manager/broker_tools.go` | 15 `case` branches (lines 202-307) | 6 `case` branches |

All three layers route by tool name string. Changing names across all three is atomic — the agent gets its tool list at startup from `WrapToolsWithDescriptions()`, so agents see 6 tools on next container restart.

### Data Migration

**None required.** The tool consolidation is purely an API layer change. The `Node` and `Edge` tables, their columns, and all stored data remain identical. The new tools call the same `Service.*` methods underneath.

The schema additions from earlier sections (provenance, TTL, status) are auto-migrated by `gowild_data.ensureColumns()` — new nullable columns with zero-value defaults. Existing rows are unaffected and need no backfill.

## Implementation Order

| Phase | Feature | Dependencies | Lines (est.) |
|-------|---------|-------------|-------------|
| 1 | Provenance fields on Edge | None | ~75 |
| 2 | TTL / ValidUntil / Status fields | None | ~100 |
| 3 | Query-time status filtering | Phase 2 | ~50 |
| 4 | Pruning background worker | Phase 2, 3 | ~100 |
| 5 | Consistency checks | None | ~250 |
| 6 | Tool consolidation (15 → 6) | Phases 1-5 | ~400 |
| **Total** | | | **~975** |

Phases 1, 2, and 5 can be done in parallel (independent changes). Phase 6 should be last since it rewrites the tool layer that phases 1-5 modify.

## What This Does NOT Include

- **External frameworks** (Graphiti/Zep, Eino, Lattice, KARMA) — these are Python/JS ecosystems irrelevant to this Go codebase
- **Full transitive reasoning** — better handled by the LLM at query time
- **Async conflict agents** — unnecessary infrastructure at current scale
- **Redis** — the PostgreSQL/SQLite backend is sufficient for TTL queries
- **Lateral inhibition / SYNAPSE** — academic concept; query-time filtering achieves the same practical result

## Works Cited

1. Beyond Short-term Memory: The 3 Types of Long-term Memory AI Agents Need - MachineLearningMastery.com, accessed February 4, 2026, [https://machinelearningmastery.com/beyond-short-term-memory-the-3-types-of-long-term-memory-ai-agents-need/](https://machinelearningmastery.com/beyond-short-term-memory-the-3-types-of-long-term-memory-ai-agents-need/)
2. Building AI Agents That Actually Remember: A Deep Dive Into Memory Architectures, accessed February 4, 2026, [https://pub.towardsai.net/building-ai-agents-that-actually-remember-a-deep-dive-into-memory-architectures-db79a15dba70](https://pub.towardsai.net/building-ai-agents-that-actually-remember-a-deep-dive-into-memory-architectures-db79a15dba70)
3. AGENTiGraph: An Interactive Knowledge Graph Platform for LLM-based Chatbots Utilizing Private Data - arXiv, accessed February 4, 2026, [https://arxiv.org/html/2410.11531v1](https://arxiv.org/html/2410.11531v1)
4. Graphiti: Knowledge Graph Memory for an Agentic World - Graph, accessed February 4, 2026, [https://neo4j.com/blog/developer/graphiti-knowledge-graph-memory/](https://neo4j.com/blog/developer/graphiti-knowledge-graph-memory/)
5. How do I stop LLM from calling the same tool calls each iteration? : r/AI\_Agents - Reddit, accessed February 4, 2026, [https://www.reddit.com/r/AI_Agents/comments/1pumehs/how_do_i_stop_llm_from_calling_the_same_tool/](https://www.reddit.com/r/AI_Agents/comments/1pumehs/how_do_i_stop_llm_from_calling_the_same_tool/)
6. Beyond Pipelines: A Survey of the Paradigm Shift toward Model-Native Agentic AI - arXiv, accessed February 4, 2026, [https://arxiv.org/html/2510.16720v1](https://arxiv.org/html/2510.16720v1)
7. Constructing coherent spatial memory in LLM agents through graph rectification - arXiv, accessed February 4, 2026, [https://arxiv.org/html/2510.04195v1](https://arxiv.org/html/2510.04195v1)
8. The ultimate LLM agent build guide - Vellum AI, accessed February 4, 2026, [https://www.vellum.ai/blog/the-ultimate-llm-agent-build-guide](https://www.vellum.ai/blog/the-ultimate-llm-agent-build-guide)
9. NeurIPS Poster BeliefMapNav: 3D Voxel-Based Belief Map for Zero-Shot Object Navigation, accessed February 4, 2026, [https://neurips.cc/virtual/2025/poster/119733](https://neurips.cc/virtual/2025/poster/119733)
