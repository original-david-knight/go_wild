package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/original-david-knight/go_wild/claudellm"
)

type claudeDeepResearchSynthesizer struct {
	client *claudellm.Client
}

func NewClaudeSynthesizer(client ...*claudellm.Client) *claudeDeepResearchSynthesizer {
	c := &claudellm.Client{
		Model:           deepResearchClaudeModelFromEnv("DEEP_RESEARCH_SYNTHESIZER_MODEL", "CLAUDE_SMART_MODEL"),
		OutputStylePath: claudellm.ResearchOutputStylePath(),
	}
	if len(client) > 0 && client[0] != nil {
		c = client[0]
	}
	return &claudeDeepResearchSynthesizer{client: c}
}

func (s *claudeDeepResearchSynthesizer) Synthesize(ctx context.Context, req SynthesisRequest) (SynthesisResult, error) {
	if len(req.Schema) == 0 {
		return SynthesisResult{}, nil
	}

	prompt := deepResearchSynthesisPrompt(req) + claudeJSONSuffix
	raw, err := s.client.Generate(ctx, prompt, "")
	if err != nil {
		return SynthesisResult{}, fmt.Errorf("claude synthesizer: %w", err)
	}

	raw = extractJSON(raw)

	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		if outputRaw, ok := wrapped["output"]; ok {
			var output any
			if err := json.Unmarshal(outputRaw, &output); err != nil {
				return SynthesisResult{}, fmt.Errorf("claude synthesizer output field is invalid JSON: %w", err)
			}
			result := SynthesisResult{Output: output}
			if summaryRaw, ok := wrapped["summary"]; ok {
				var summary string
				if err := json.Unmarshal(summaryRaw, &summary); err == nil {
					result.Summary = strings.TrimSpace(summary)
				}
			}
			return result, nil
		}
	}

	var output any
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return SynthesisResult{}, fmt.Errorf("claude synthesizer returned invalid JSON: %w", err)
	}
	return SynthesisResult{Output: output}, nil
}
