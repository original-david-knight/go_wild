package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/codexllm"
)

type codexDeepResearchCompletenessChecker struct {
	client *codexllm.Client
	// generate is the injection point used by tests to avoid shelling out to
	// the codex CLI. Defaults to client.Generate in newCodexCompletenessChecker;
	// tests construct the struct literal directly with a stub function.
	generate func(context.Context, string, string) (string, error)
}

func newCodexCompletenessChecker(client *codexllm.Client) *codexDeepResearchCompletenessChecker {
	return &codexDeepResearchCompletenessChecker{client: client, generate: client.Generate}
}

// DefaultCodexCompletenessChecker constructs a checker with a client built
// from DEEP_RESEARCH_CODEX_CHECKER_PROFILE (or the CODEX_FAST_PROFILE tier
// fallback). Returns an error if neither env var is set.
func DefaultCodexCompletenessChecker() (*codexDeepResearchCompletenessChecker, error) {
	client, err := buildCodexClient("DEEP_RESEARCH_CODEX_CHECKER_PROFILE", "CODEX_FAST_PROFILE", "deep-research-checker", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	return newCodexCompletenessChecker(client), nil
}

func (c *codexDeepResearchCompletenessChecker) Check(ctx context.Context, req CompletenessRequest) (CompletenessResult, error) {
	if len(req.Objectives) == 0 {
		return CompletenessResult{Complete: true}, nil
	}

	prompt := deepResearchCompletenessPrompt(req) + codexJSONSuffix
	raw, err := c.generate(ctx, prompt, "")
	if err != nil {
		return CompletenessResult{}, fmt.Errorf("codex completeness checker: %w", err)
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
		return CompletenessResult{}, fmt.Errorf("codex completeness checker returned invalid JSON: %w (raw=%q)", err, snippet)
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
