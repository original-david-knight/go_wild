# Context Compaction Implementation Plan

> NOTE: The context summary feature described later in this document has been removed. This document is retained for historical reference.

**Goal**: Implement the "Cognitive OS" pattern from agent_compaction.md to eliminate forgetfulness and reduce costs while maintaining agent coherence.

**Key Insight**: The current LLM summarization approach causes "trajectory elongation" - when the model summarizes failed tool executions, it smooths over specifics. The agent, reading the summary instead of raw output, may fail to realize how stuck it is. **Observation masking** outperforms summarization.

---

## Phase 1: Observation Masking (Priority: Critical)

Replace `compactHistory()` with observation masking that preserves the agent's reasoning trajectory while discarding verbose tool outputs.

### Current State
- `compaction.go` uses LLM summarization of entire history
- Tool results truncated to 500 chars in the summarization prompt
- All messages treated equally

### Target State
- Keep all user messages intact
- Keep all model messages (reasoning, tool calls) intact
- Keep last N tool outputs in full (default: 3)
- Replace older tool outputs with one-line placeholder summaries
- No LLM call required for compaction

### Implementation

#### 1.1 New `maskObservations()` function in `compaction.go`

```go
// ObservationMaskResult holds the result of observation masking.
type ObservationMaskResult struct {
    MaskedMessages []loop.Message
    MaskedCount    int
    TokensSaved    int // Estimated
}

// maskObservations replaces older tool outputs with placeholders while
// preserving the full reasoning trajectory.
func maskObservations(
    history []loop.Message,
    keepRecentOutputs int, // Number of recent tool outputs to keep in full
) *ObservationMaskResult {
    // 1. Identify all tool output messages (RoleTool)
    // 2. Keep the last `keepRecentOutputs` in full
    // 3. Replace older ones with: "[Output of {tool_name}({args}): {brief_summary}]"
    // 4. Keep all user and model messages intact
}
```

#### 1.2 Create placeholder summaries for tool outputs

```go
// createObservationMask creates a brief placeholder for a tool output.
// Examples:
//   - "[read_file('main.go'): 500 lines, Go source code]"
//   - "[run_python: executed successfully, returned dict with 3 keys]"
//   - "[web_search('AI agents'): 10 results returned]"
//   - "[run_shell('ls -la'): listed 15 files]"
func createObservationMask(toolName string, args map[string]any, response map[string]any) string
```

#### 1.3 Update `main.go` compaction trigger

```go
// In main.go REPL loop, replace:
if contextTokens >= *compactAt {
    compactResult, err := compactHistory(...)
}

// With:
if contextTokens >= *compactAt {
    originalTokens := contextTokens
    originalMsgCount := len(history)

    maskResult := maskObservations(history, *keepRecentOutputs)
    history = maskResult.MaskedMessages

    // Display prominent compaction notice (cyan box)
    fmt.Println(color.CyanString("┌─ Context Compacted ─────────────────────────┐"))
    fmt.Println(color.CyanString("│") + fmt.Sprintf(" Messages: %d → %d", originalMsgCount, len(history)) + ...)
    fmt.Println(color.CyanString("│") + fmt.Sprintf(" Tokens:   ~%dk → ~%dk", ...) + ...)
    fmt.Println(color.CyanString("│") + fmt.Sprintf(" Masked:   %d tool outputs", maskResult.MaskedCount) + ...)
    fmt.Println(color.CyanString("└─────────────────────────────────────────────┘"))
}
```

#### 1.4 Add CLI flag

```go
keepRecentOutputs = flag.Int("keep-outputs", 3, "Number of recent tool outputs to keep in full during masking")
```

### Migration
- No data migration needed
- Old `compactHistory()` can be kept as fallback (flag: `-use-summarization`)
- Default behavior changes from summarization to masking

---

## Phase 2: Context Summary Block (Priority: High)

Add an always-visible "Context Summary" that's rendered into the system prompt for every turn, ensuring critical session state survives any compaction.

### Current State
- Memory is file-based (`memory.md`) or database (`MemoryEntry`)
- Agent must manually read/write memory
- Memory not automatically visible in context
- Tasks exist but aren't rendered into system prompt
- Knowledge Graph exists for permanent world knowledge

### Target State
- Context Summary block always visible in system prompt
- Contains: Session Summary + Current Tasks
- Agent has one tool to update the summary
- Clear separation: Context Summary (this session) vs Knowledge Graph (forever)

### Design Principles

Keep it simple - one text field, one tool:
- **Context Summary**: Brief text about current session state, constraints, key identifiers
- **Tasks**: Already exist, just render them alongside
- **No separate facts table**: The summary IS the facts, written in prose

This avoids confusion with existing systems:
| System | Purpose | Retrieval |
|--------|---------|-----------|
| Context Summary | Current session state | Always visible |
| Memory (memory.md) | Detailed working notes | Must read |
| Knowledge Graph | Permanent world knowledge | Must query |
| Archive | Historical records | Must search |

### New Data Model

#### 2.1 Add `ContextSummary` table in `data/models.go`

```go
// ContextSummary represents the always-visible session context for an agent.
// This is rendered into the system prompt for every API call.
type ContextSummary struct {
    ID        string    `json:"id"`
    AgentID   string    `json:"agent_id"`
    Summary   string    `json:"summary"` // 2-5 sentence overview of current session
    UpdatedAt time.Time `json:"updated_at"`
}
```

#### 2.2 Add service methods in `data/service.go`

```go
// Context Summary operations
func (s *AgentService) GetContextSummary(ctx context.Context) (*ContextSummary, error)
func (s *AgentService) SaveContextSummary(ctx context.Context, summary string) error
```

#### 2.3 Render Context Summary into system prompt

In `main.go` `loadSystemPrompt()`:

```go
func renderContextBlock(ctx context.Context, service *data.AgentService) string {
    var sb strings.Builder
    sb.WriteString("\n\n# Current Context (Always Visible)\n\n")

    // Session Summary
    if cs, _ := service.GetContextSummary(ctx); cs != nil && cs.Summary != "" {
        sb.WriteString("## Session Summary\n")
        sb.WriteString(cs.Summary + "\n\n")
    }

    // Current Tasks (from existing task system)
    if tasks, _ := service.GetPendingTasks(ctx); len(tasks) > 0 {
        sb.WriteString("## Current Tasks\n")
        for i, t := range tasks {
            status := "⬜"
            if t.Blocked { status = "🚫" }
            if t.SleepUntil != nil && t.SleepUntil.After(time.Now()) { status = "💤" }
            sb.WriteString(fmt.Sprintf("%d. %s %s\n", i+1, status, t.Description))
        }
    }

    return sb.String()
}
```

### Migration

No migration needed - Context Summary starts empty, agent populates it as needed.
Existing memory.md and knowledge graph continue to work as before.

---

## Phase 3: Context Summary Tool (Priority: Medium)

Give the agent a single tool to update its context summary.

### New Tool in `tools/context.go`

```go
// UpdateContextSummaryInput for updating the always-visible context summary
type UpdateContextSummaryInput struct {
    Summary string `json:"summary" description:"Brief summary of current session state, goals, and critical context (2-5 sentences). Include key identifiers, constraints, and decisions." required:"true"`
}

// ContextTools provides tools for managing the always-visible context
type ContextTools struct {
    service *data.AgentService
}

func (t *ContextTools) UpdateContextSummaryTool(ctx context.Context, input UpdateContextSummaryInput) (*loop.ToolResult, error) {
    if input.Summary == "" {
        return loop.NewErrorResult("summary is required"), nil
    }

    if err := t.service.SaveContextSummary(ctx, input.Summary); err != nil {
        return loop.NewErrorResult(fmt.Sprintf("failed to save context summary: %v", err)), nil
    }

    return loop.NewSuccessResult(map[string]any{
        "success": true,
        "message": "Context summary updated. It will be visible at the start of every turn.",
    }), nil
}

func (t *ContextTools) DescribeTool(name string) string {
    return map[string]string{
        "update_context_summary": "Update the always-visible context summary. Use this to preserve critical session state (current goals, key identifiers, constraints, decisions) that must survive context compaction. This is different from memory (detailed notes) and knowledge graph (permanent world knowledge).",
    }[name]
}
```

### Update System Prompt

Add guidance in `default_system_prompt.md`:

```markdown
## Context Summary vs Other Memory Systems

**update_context_summary** - What you need RIGHT NOW (always visible):
- Current goals and session state
- Key identifiers (API keys, URLs, IDs)
- Active constraints and decisions
- Update when context shifts or approaching token limits
- Example: "Working on payment API integration. Endpoint: api.example.com/v2,
  auth token: sk-abc123. Rate limit 100/min. Using exponential backoff for retries."

**write_memory** - Detailed working notes (must read to see):
- Research findings, exploration logs
- Step-by-step procedures
- Debugging attempts

**Knowledge Graph** - Permanent world knowledge (must query):
- Facts about the world that are always true
- Relationships between concepts
- Verified information with sources

Simple rule: Context Summary = this session's critical state.
Memory = detailed notes. Knowledge Graph = permanent truth.
```

---

## Phase 4: Gemini Context Caching (Priority: Medium)

Use Gemini's explicit caching to reduce costs on the immutable prefix (system prompt, tool schemas).

### Current State
- Every API call sends full system prompt + tool schemas
- No caching of repeated content

### Target State
- System prompt + tool schemas cached with 1-hour TTL
- Subsequent calls only send new conversation turns
- Up to 90% cost reduction on prefix tokens

### Implementation

#### 4.1 Add caching to `gowild_agentic_loop`

```go
// In loop/gemini.go or new cache.go

type CacheConfig struct {
    Enabled bool
    TTL     time.Duration // Default 1 hour
}

// CacheManager handles Gemini context caching
type CacheManager struct {
    client    *genai.Client
    cacheName string
    expiresAt time.Time
}

func (m *CacheManager) EnsureCache(ctx context.Context, systemPrompt string, tools []*genai.Tool) error {
    // Check if cache exists and is valid
    if m.cacheName != "" && time.Now().Before(m.expiresAt) {
        return nil
    }

    // Create new cache
    config := &genai.CreateCachedContentConfig{
        Model:             m.model,
        SystemInstruction: genai.Text(systemPrompt),
        Tools:             tools,
        TTL:               m.ttl.String(),
    }
    cache, err := m.client.Caches.Create(ctx, m.model, config)
    if err != nil {
        return err
    }

    m.cacheName = cache.Name
    m.expiresAt = time.Now().Add(m.ttl)
    return nil
}
```

#### 4.2 Update AgenticLoop to use cache

```go
// In loop.go GenerateContent
func (l *AgenticLoop) GenerateContent(ctx context.Context, messages []Message) (*Response, error) {
    // Ensure cache is valid
    if l.cacheManager != nil {
        if err := l.cacheManager.EnsureCache(ctx, l.systemPrompt, l.tools); err != nil {
            // Log warning, continue without cache
        }
    }

    // Use cached content name in request
    // ...
}
```

#### 4.3 CLI flag

```go
enableCache = flag.Bool("cache", true, "Enable Gemini context caching for system prompt and tools")
cacheTTL    = flag.Duration("cache-ttl", 1*time.Hour, "TTL for context cache")
```

### Constraints
- Minimum token threshold: 1024 (Flash), 4096 (Pro)
- Cannot redefine system_instructions, tools, tool_config if already in cache
- Cache invalidation needed when tools change

---

---

## Migration Strategy

### Existing Data Preservation

1. **Memory files** (`memory.md`, `memory_archive.md`)
   - Keep as-is for backward compatibility
   - Run migration to extract facts to Core Memory
   - Memory tools continue to work

2. **Database entries** (`MemoryEntry`, `ArchiveEntry`)
   - Keep as-is
   - Run migration to create initial Core Memory
   - Existing tools continue to work

3. **Soul** (`soul.md`, `Soul` table)
   - No changes needed
   - Already appended to system prompt

4. **Tasks** (`Task` table)
   - No changes needed
   - Now also rendered in Core Memory block

### Migration Command

Add CLI flag for one-time migration:

```bash
./gowild_agent -migrate-core-memory
```

This will:
1. Read existing memory (file or DB)
2. Extract facts using patterns
3. Create initial CoreFact entries
4. Set initial SessionSummary
5. Print migration report

### Rollback

If issues arise:
1. Core Memory tables can be dropped
2. Original memory files/entries unchanged
3. Set `-use-summarization` flag to revert to old compaction

---

## Implementation Order

| Phase | Status | Deliverable |
|-------|--------|-------------|
| 1 | ✅ DONE | Observation masking replaces summarization |
| 2 | ✅ DONE | Context Summary data model + system prompt rendering |
| 3 | ✅ DONE | `update_context_summary` tool |
| 4 | TODO | Gemini context caching (optional optimization) |

---

## Testing Strategy

### Unit Tests

```go
// compaction_test.go
func TestMaskObservations_KeepsRecentOutputs(t *testing.T)
func TestMaskObservations_PreservesUserMessages(t *testing.T)
func TestMaskObservations_PreservesModelReasoning(t *testing.T)
func TestCreateObservationMask_VariousTools(t *testing.T)

// core_memory_test.go
func TestRenderCoreMemoryBlock_WithFacts(t *testing.T)
func TestRenderCoreMemoryBlock_WithTasks(t *testing.T)
func TestMigrateMemoryToCoreMemory(t *testing.T)
```

### Integration Tests

1. Run agent through multi-turn conversation
2. Trigger compaction threshold
3. Verify agent doesn't lose track of important facts
4. Verify agent doesn't repeat failed approaches

### Metrics to Track

- Token usage before/after masking
- Compaction frequency
- Fact extraction accuracy (manual review)
- Agent task completion rate
- "Lost in the middle" incidents

---

## File Changes Summary

| File | Status | Changes |
|------|--------|---------|
| `compaction.go` | ✅ DONE | Added `maskObservations()`, kept `compactHistory()` as fallback |
| `compaction_test.go` | ✅ DONE | Added masking tests |
| `main.go` | ✅ DONE | Updated compaction trigger, flags, `loadSystemPrompt()`, `renderContextBlock()`, `addContextTools()` |
| `data/models.go` | ✅ DONE | Added `ContextSummary` model |
| `data/service.go` | ✅ DONE | Added Context Summary CRUD methods |
| `data/data.go` | ✅ DONE | Registered new table |
| `tools/context.go` | ✅ DONE | New file with `update_context_summary` tool |
| `default_system_prompt.md` | ✅ DONE | Added guidance on memory systems |
| `CLAUDE.md` | ✅ DONE | Documented observation masking and context summary |

---

## References

- `devdocs/agent_compaction.md` - Research document with detailed analysis
- JetBrains Research: "Cutting Through the Noise" - Observation masking study
- MemGPT Paper - Virtual context management paradigm
- Gemini API Docs - Context caching
