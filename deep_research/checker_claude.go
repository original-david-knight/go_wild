package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/claudellm"
)

type claudeDeepResearchCompletenessChecker struct {
	client *claudellm.Client
}

func NewClaudeCompletenessChecker(client ...*claudellm.Client) *claudeDeepResearchCompletenessChecker {
	c := &claudellm.Client{
		Model:           deepResearchClaudeModelFromEnv("DEEP_RESEARCH_CHECKER_MODEL", "CLAUDE_FAST_MODEL"),
		OutputStylePath: claudellm.ResearchOutputStylePath(),
	}
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}
	return &claudeDeepResearchCompletenessChecker{client: c}
}

func (c *claudeDeepResearchCompletenessChecker) Check(ctx context.Context, req CompletenessRequest) (CompletenessResult, error) {
	if len(req.Objectives) == 0 {
		return CompletenessResult{Complete: true}, nil
	}

	prompt := deepResearchCompletenessPrompt(req) + claudeJSONSuffix
	raw, err := c.client.Generate(ctx, prompt, "")
	if err != nil {
		return CompletenessResult{}, fmt.Errorf("claude completeness checker: %w", err)
	}

	raw = extractJSON(raw)

	var parsed struct {
		Complete          bool   `json:"complete"`
		Reasoning         string `json:"reasoning"`
		MissingObjectives []struct {
			ObjectiveKey string `json:"objective_key"`
			Question     string `json:"question"`
		} `json:"missing_objectives"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		snippet := raw
		if len(snippet) > 280 {
			snippet = snippet[:280] + "..."
		}
		return CompletenessResult{}, fmt.Errorf("claude completeness checker returned invalid JSON: %w (raw=%q)", err, snippet)
	}

	out := CompletenessResult{
		Complete:  parsed.Complete,
		Reasoning: strings.TrimSpace(parsed.Reasoning),
	}
	for _, item := range parsed.MissingObjectives {
		key := strings.TrimSpace(item.ObjectiveKey)
		if key == "" {
			continue
		}
		out.MissingObjectives = append(out.MissingObjectives, MissingObjective{
			ObjectiveKey: key,
			Question:     strings.TrimSpace(item.Question),
		})
	}
	if !out.Complete && len(out.MissingObjectives) == 0 {
		for _, item := range req.ObjectiveResults {
			if item.Status == ObjectiveStatusSatisfied {
				continue
			}
			key := strings.TrimSpace(item.Objective.Key)
			if key == "" {
				continue
			}
			out.MissingObjectives = append(out.MissingObjectives, MissingObjective{
				ObjectiveKey: key,
				Question:     "Need stronger evidence for " + key,
			})
		}
	}
	return out, nil
}
