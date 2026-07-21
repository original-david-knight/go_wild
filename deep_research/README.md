# deep_research

`deepresearch` is a Go library for schema-guided deep research runs used inside this workspace.

It provides:
- Iterative deep-research rounds with depth/worker controls
- Pluggable search and fetch interfaces (`Searcher`, `Fetcher`)
- Optional planner / completeness-checker / synthesizer reasoning hooks
- Thread-safe evidence scratchpad and deduplicated source output

## Quick usage

```go
searcher, _, err := deepresearch.NewSearcher()
if err != nil {
    return err
}
fetcher, _, err := deepresearch.NewFetcher()
if err != nil {
    return err
}
planner, err := deepresearch.NewGeminiPlanner()
if err != nil {
    return err
}
checker, err := deepresearch.NewGeminiCompletenessChecker()
if err != nil {
    return err
}
synth, err := deepresearch.NewGeminiSynthesizer()
if err != nil {
    return err
}

engine := deepresearch.NewEngineWithReasoning(searcher, fetcher, planner, checker, synth)
result, err := engine.Run(ctx, deepresearch.Request{
    Query: "competitive landscape for X",
    Objectives: []deepresearch.Objective{
        {Key: "overview", Required: true},
    },
    Options: deepresearch.Options{
        MaxDepth:                3,
        MaxWorkers:              8,
        MinEvidencePerObjective: 2,
    },
})
```

`searcher` and `fetcher` are intentionally provider-agnostic so manager tools, A2A methods, and other runtimes can reuse this package.
