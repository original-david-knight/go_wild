# objectives

`objectives` is a store for goals with measurable progress, plus a status rollup over them. It
is a persistence layer only — nothing in this module decides what to do next, and nothing in it
serves HTTP.

## What is here

- `Objective` — a node in the objective tree: identity (`ID`, `CompanyID`), tree position
  (`ParentID`, `Depth`), content (`Title`, `Description`), lifecycle (`Status`, `Priority`,
  `Deadline`), `Metadata` and timestamps. `Target`, `Current` and `Unit` carry measurable
  progress, so `198 → 180 lb` is a `Target` of `180`, a `Current` of `197.4` and a `Unit` of
  `"lb"`. Objectives with no measurable target leave all three zero.
- `ObjectiveStore` — CRUD, tree traversal (`GetRoots`, `GetChildren`, `GetTree`,
  `GetLeafTasks`), `GetByStatus` and transactional `ApplyMutations`. Constructed with a company
  ID; a non-empty one scopes every read and write.
- `ActivityStore` / `ActivityEvent` — the append-only log of what happened to an objective.
- `StatusRollup` (`status.go`) — child counts by status plus the last activity event, per node
  or across a subtree.

Tables register themselves through `gowild_data.RegisterFunc` at `init()`, so importing the
package is enough; the caller runs `gowild_data.AddAllTables`.

## What left

**The mission planner (earlier split).** `planner.go`, `executor.go`, `scheduler.go`,
`evaluator.go` and the planner's memory store moved to a sibling module,
`objectives_planner`. It was the only thing pulling `genai`, `agent_node` and (behind
`agent_node`) `go-ethereum` into this module.

**The planner legacy (2026-08-07, lifedash M23).** With the planner gone and unrepaired, the
node fields that only served it went too: `ScheduleType` / `ScheduleCron` / `ScheduleEvent`,
`ToolAllowlist`, `AutonomyLevel`, `CooldownUntil`, `LastResult`. `Escalation` and its store
went with them — asking a human to unblock an autonomous run is a planner concern.

**The embedded HTTP surface (same change).** `APIServer`, the REST handlers, the `/api/stream`
WebSocket and the embedded dashboard are deleted. Management is this store's Go API; HTTP is a
consumer concern, and the life dashboard serves its own.

`objectives_planner` aliased the dropped types, so it breaks; it and its runners
(`apps/objectives`, `apps/agent_manager`) are not repaired and no longer sit in `go.work`.

Dropped struct fields leave orphan columns in Postgres. `data/` migrations are additive-only
and orphan columns are inert, so the strip ships zero DDL.

## The rule

**`objectives` must not grow a dependency on `genai`, `agent_node` or `go-ethereum` again.**
Anything that needs them belongs further out. `dep_cone_test.go` runs `go list -deps ./...` and
fails if any of the three reappears, so a violation shows up as a red test rather than as a
build-size surprise downstream.
