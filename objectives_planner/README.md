# objectives_planner

`objectives_planner` is the autonomous mission planner that used to live inside
`objectives`. It turns an objective into a tree of work, runs the leaves, judges the
results, and decides what to do next.

- `StrategicPlanner` (`planner.go`) — decomposes an objective into tree mutations, or asks
  clarifying questions when it cannot.
- `ExecutionEngine` (`executor.go`) — runs a leaf task through `agent_node` under the
  objective's tool allowlist and autonomy level.
- `PostExecutionEvaluator` (`evaluator.go`) — scores what came back and writes what was
  learned.
- `Scheduler` (`scheduler.go`) — the work queue, cron/event/continuous triggers, cooldowns
  and escalation handling.
- `MemoryStore` (`memory.go`) — knowledge, decisions and learnings, in its own tables
  registered at `init()` so a consumer that only wants objectives never materialises them.
- `Config` (`config.go`) — database URL, Gemini API key, the fast/smart model pair,
  concurrency and listen address. `NewConfig` for an embedding process, `LoadConfig` for the
  environment.

## Why it is a separate module

`objectives` is consumed by a local desktop dashboard that needs the data model, the store
and the REST/WS surface — not an LLM planning loop. The planner was the only thing dragging
`genai`, `agent_node` and `go-ethereum` into that cone, so it moved out rather than being
trimmed down. `objectives` guards the boundary with `dep_cone_test.go`.

The move was a boundary change, not a rewrite. The planner code was written against
`Objective`, `ObjectiveStore`, `ActivityStore` and friends as unqualified local names;
`alias.go` re-declares those as type aliases, constants and function values pointing back at
`github.com/original-david-knight/go_wild/objectives`, so every moved file stayed
byte-identical. Behaviour is unchanged.

## Consumers

- `apps/agent_manager` — imports both modules: `objectives` for the mission HTTP handlers,
  this one for the scheduler it starts in-process (`startObjectivesScheduler`).
- `apps/objectives` — the CLI: `run "<mission>"` plans and executes once, `daemon` runs the
  scheduler long-lived.

Both wire the two halves together themselves: build the store with
`objectives.NewObjectiveStore`, then hand it to `NewScheduler` here.
