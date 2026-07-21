package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/codexllm"
)

type codexDeepResearchPlanner struct {
	client *codexllm.Client
	// generate is the injection point used by tests to avoid shelling out to
	// the codex CLI. Defaults to client.Generate in newCodexPlanner; tests
	// construct the struct literal directly with a stub function.
	generate func(context.Context, string, string) (string, error)
}

func newCodexPlanner(client *codexllm.Client) *codexDeepResearchPlanner {
	return &codexDeepResearchPlanner{client: client, generate: client.Generate}
}

// DefaultCodexPlanner constructs a planner with a client built from
// DEEP_RESEARCH_CODEX_PLANNER_PROFILE (or the CODEX_SMART_PROFILE tier
// fallback). Returns an error if neither env var is set.
func DefaultCodexPlanner() (*codexDeepResearchPlanner, error) {
	client, err := buildCodexClient("DEEP_RESEARCH_CODEX_PLANNER_PROFILE", "CODEX_SMART_PROFILE", "deep-research-planner", 3*time.Minute)
	if err != nil {
		return nil, err
	}
	return newCodexPlanner(client), nil
}

func (p *codexDeepResearchPlanner) Plan(ctx context.Context, req PlanningRequest) (PlanningResult, error) {
	if len(req.MissingObjectives) == 0 {
		return PlanningResult{}, nil
	}

	prompt := deepResearchPlannerPrompt(req) + codexJSONSuffix
	raw, err := p.generate(ctx, prompt, "")
	if err != nil {
		return PlanningResult{}, fmt.Errorf("codex planner: %w", err)
	}

	raw = extractJSON(raw)

	var parsed struct {
		Queries []struct {
			ObjectiveKey string `json:"objective_key"`
			Query        string `json:"query"`
			Rationale    string `json:"rationale"`
		} `json:"queries"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return PlanningResult{}, fmt.Errorf("codex planner returned invalid JSON: %w", err)
	}

	allowed := make(map[string]struct{}, len(req.MissingObjectives))
	for _, objective := range req.MissingObjectives {
		key := strings.TrimSpace(objective.Key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}

	out := PlanningResult{
		Queries:   make([]PlannedQuery, 0, len(parsed.Queries)),
		Reasoning: strings.TrimSpace(parsed.Reasoning),
	}
	for _, row := range parsed.Queries {
		key := strings.TrimSpace(row.ObjectiveKey)
		query := strings.TrimSpace(row.Query)
		if key == "" || query == "" {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		out.Queries = append(out.Queries, PlannedQuery{
			ObjectiveKey: key,
			Query:        query,
			Rationale:    strings.TrimSpace(row.Rationale),
		})
	}
	return out, nil
}
