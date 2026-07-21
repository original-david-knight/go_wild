# gowild_agent_node

`gowild_agent_node` is a DAG-based orchestration package that runs:

1. plan (`Planner`)
2. execute (`GraphExecutor`)
3. sufficiency check (`SufficiencyChecker`)

The CLI entrypoint is `cmd/agentnode`.

## Environment

Required:

- `GEMINI_API_KEY` for planner/checker/synthesis LLM calls.

Optional (enables `web_search` tool in agentic nodes):

Web search uses Gemini Grounding with Google Search via the `GEMINI_API_KEY`.

If the Gemini API key is missing, `web_search` is not registered.
`ToolCatalog` now reports this explicitly as:

- `web_search unavailable: set GEMINI_API_KEY`

## Run CLI

From repo root:

```bash
go run ./gowild_agent_node/cmd/agentnode --rounds 7 "your question here"
```

Optional flags:

- `--rounds` maximum planning rounds (default `7`)
- `--model` model override (default executor model)

## Integration Notes

- `NewOrchestrator` requires non-nil `Planner`, `SufficiencyChecker`, and `GraphExecutor`.
- Orchestrator guards against a planner returning `nil` results.
- `GraphExecutor` supports:
  - `single_shot` nodes (one LLM call)
  - `agentic` nodes (multi-turn tool use)
  - `deep_research` nodes (iterative research engine)
