# objectives

`objectives` is a store for goals with measurable progress, plus a status rollup and a
REST/WebSocket surface over them. It is a persistence and transport layer only — nothing in
this module decides what to do next.

## What is here

- `Objective` — a node in the objective tree: title, description, status, priority, parent,
  schedule, tool allowlist, autonomy level, deadline. `Target`, `Current` and `Unit` carry
  measurable progress, so `198 → 180 lb` is a `Target` of `180`, a `Current` of `197.4` and a
  `Unit` of `"lb"`. Objectives with no measurable target leave all three zero.
- `ObjectiveStore` — CRUD, tree traversal (`GetRoots`, `GetChildren`, `GetTree`,
  `GetLeafTasks`), `GetByStatus`, `GetByScheduleType`, transactional `ApplyMutations`, and
  escalation read/resolve. Constructed with a company ID; a non-empty one scopes every read
  and write.
- `ActivityStore` / `ActivityEvent` — the append-only log of what happened to an objective.
- `StatusRollup` (`status.go`) — child counts by status plus the last activity event, per node
  or across a subtree.
- `APIServer` (`api.go`, `api_handlers.go`, `api_ws.go`) — the REST endpoints under
  `/api/objectives`, `/api/activity`, `/api/escalations`, `/api/status`, the `/api/stream`
  WebSocket, and the embedded dashboard at `/`.

Tables register themselves through `gowild_data.RegisterFunc` at `init()`, so importing the
package is enough; the caller runs `gowild_data.AddAllTables`.

## What moved out

The autonomous mission planner — `planner.go`, `executor.go`, `scheduler.go`, `evaluator.go`
and the planner's memory store — now lives in the sibling module
`github.com/original-david-knight/go_wild/objectives_planner`. It was the only thing pulling
`genai`, `agent_node` and (behind `agent_node`) `go-ethereum` into this module. The life
dashboard wants the data model and the store, not an LLM planning loop, and a desktop binary
should not link an Ethereum client to render a progress bar.

The split is a boundary change, not a rewrite: the planner code moved byte-identical and
aliases the shared types back out of this module in `objectives_planner/alias.go`. Consumers
that want both — `apps/agent_manager` and `apps/objectives` — import both modules.

## The rule

**`objectives` must not grow a dependency on `genai`, `agent_node` or `go-ethereum` again.**
Anything that needs them belongs in `objectives_planner` or further out. `dep_cone_test.go`
runs `go list -deps ./...` and fails if any of the three reappears, so a violation shows up as
a red test rather than as a build-size surprise downstream.
