package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/codexllm"
)

type codexDeepResearchSynthesizer struct {
	client *codexllm.Client
	// generate is the injection point used by tests to avoid shelling out to
	// the codex CLI. Defaults to client.Generate in newCodexSynthesizer; tests
	// construct the struct literal directly with a stub function.
	generate func(context.Context, string, string) (string, error)
}

func newCodexSynthesizer(client *codexllm.Client) *codexDeepResearchSynthesizer {
	return &codexDeepResearchSynthesizer{client: client, generate: client.Generate}
}

// DefaultCodexSynthesizer constructs a synthesizer with a client built from
// DEEP_RESEARCH_CODEX_SYNTHESIZER_PROFILE (or the CODEX_SMART_PROFILE tier
// fallback). Returns an error if neither env var is set.
func DefaultCodexSynthesizer() (*codexDeepResearchSynthesizer, error) {
	client, err := buildCodexClient("DEEP_RESEARCH_CODEX_SYNTHESIZER_PROFILE", "CODEX_SMART_PROFILE", "deep-research-synthesizer", 5*time.Minute)
	if err != nil {
		return nil, err
	}
	return newCodexSynthesizer(client), nil
}

func (s *codexDeepResearchSynthesizer) Synthesize(ctx context.Context, req SynthesisRequest) (SynthesisResult, error) {
	if len(req.Schema) == 0 {
		return SynthesisResult{}, nil
	}

	prompt := deepResearchSynthesisPrompt(req) + codexJSONSuffix
	raw, err := s.generate(ctx, prompt, "")
	if err != nil {
		return SynthesisResult{}, fmt.Errorf("codex synthesizer: %w", err)
	}

	raw = extractJSON(raw)

	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		if outputRaw, ok := wrapped["output"]; ok {
			var output any
			if err := json.Unmarshal(outputRaw, &output); err != nil {
				return SynthesisResult{}, fmt.Errorf("codex synthesizer output field is invalid JSON: %w", err)
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
		return SynthesisResult{}, fmt.Errorf("codex synthesizer returned invalid JSON: %w", err)
	}
	return SynthesisResult{Output: output}, nil
}
