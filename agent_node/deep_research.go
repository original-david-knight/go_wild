package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	deepresearch "github.com/original-david-knight/go_wild/deep_research"
)

// executeDeepResearch runs the deep research engine for a node.
func (e *GraphExecutor) executeDeepResearch(ctx context.Context, def *NodeDef, prompt string) *NodeResult {
	searcher, _, err := deepresearch.NewSearcher()
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("failed to create searcher: %v", err),
		}
	}

	fetcher, _, err := deepresearch.NewFetcher()
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("failed to create fetcher: %v", err),
		}
	}

	planner, err := deepresearch.NewGeminiPlanner()
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("failed to create research planner: %v", err),
		}
	}

	checker, err := deepresearch.NewGeminiCompletenessChecker()
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("failed to create completeness checker: %v", err),
		}
	}

	synthesizer, err := deepresearch.NewGeminiSynthesizer()
	if err != nil {
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("failed to create synthesizer: %v", err),
		}
	}

	engine := deepresearch.NewEngineWithReasoning(searcher, fetcher, planner, checker, synthesizer)

	// Build request from node config
	req := deepresearch.Request{
		Query: prompt,
	}

	if cfg := def.ResearchCfg; cfg != nil {
		if cfg.MaxDepth > 0 {
			req.Options.MaxDepth = cfg.MaxDepth
		}
		if cfg.TimeoutSeconds > 0 {
			req.Options.TimeoutSeconds = cfg.TimeoutSeconds
		}
		if cfg.Guidance != "" {
			req.Guidance = cfg.Guidance
		}
		for _, key := range cfg.Objectives {
			req.Objectives = append(req.Objectives, deepresearch.Objective{
				Key:      key,
				Required: true,
			})
		}
	}

	// Bridge progress events to graph event stream
	if e.config.Events != nil {
		req.Progress = func(ev deepresearch.ProgressEvent) {
			e.config.Events <- DeepResearchProgressEvent{
				NodeID:       def.ID,
				Stage:        ev.Stage,
				Round:        ev.Round,
				Query:        ev.Query,
				URL:          ev.URL,
				ObjectiveKey: ev.ObjectiveKey,
				Warning:      ev.Warning,
			}
		}
	}

	// Apply timeout
	timeout := 300 * time.Second
	if def.ResearchCfg != nil && def.ResearchCfg.TimeoutSeconds > 0 {
		timeout = time.Duration(def.ResearchCfg.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := engine.Run(ctx, req)
	if err != nil {
		// On context cancellation, return partial results if available
		if ctx.Err() != nil && result.Rounds > 0 {
			return buildResearchResult(def.ID, result, true)
		}
		return &NodeResult{
			NodeID: def.ID,
			Status: NodeFailed,
			Error:  fmt.Sprintf("deep research error: %v", err),
		}
	}

	return buildResearchResult(def.ID, result, false)
}

func buildResearchResult(id NodeID, result deepresearch.Result, partial bool) *NodeResult {
	nr := &NodeResult{
		NodeID:    id,
		Status:    NodeDone,
		TurnCount: result.Rounds,
	}

	// Use synthesized output if available, otherwise summary
	if result.Output != nil {
		b, err := json.Marshal(result.Output)
		if err == nil {
			nr.Output = json.RawMessage(b)
		}
	}

	text := result.Summary
	if partial {
		text = "[partial] " + text
	}
	nr.Text = text

	return nr
}
