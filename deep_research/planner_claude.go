package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/claudellm"
)

type claudeDeepResearchPlanner struct {
	client *claudellm.Client
}

func NewClaudePlanner(client ...*claudellm.Client) *claudeDeepResearchPlanner {
	c := &claudellm.Client{
		Model:           deepResearchClaudeModelFromEnv("DEEP_RESEARCH_PLANNER_MODEL", "CLAUDE_SMART_MODEL"),
		OutputStylePath: claudellm.ResearchOutputStylePath(),
	}
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}
	return &claudeDeepResearchPlanner{client: c}
}

func (p *claudeDeepResearchPlanner) Plan(ctx context.Context, req PlanningRequest) (PlanningResult, error) {
	if len(req.MissingObjectives) == 0 {
		return PlanningResult{}, nil
	}

	prompt := deepResearchPlannerPrompt(req) + claudeJSONSuffix
	raw, err := p.client.Generate(ctx, prompt, "")
	if err != nil {
		return PlanningResult{}, fmt.Errorf("claude planner: %w", err)
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
		return PlanningResult{}, fmt.Errorf("claude planner returned invalid JSON: %w", err)
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
