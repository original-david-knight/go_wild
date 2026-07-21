package deepresearch

import (
	"time"

	"github.com/original-david-knight/go_wild/codexllm"
)

const codexJSONSuffix = `

IMPORTANT: Respond with ONLY the raw JSON object. Do not wrap the JSON in markdown code fences (no ` + "```" + `json or ` + "```" + `). Do not include any text before or after the JSON.`

// buildCodexClient resolves the Codex profile from phaseEnv (falling back to
// tierEnv) and returns a client preconfigured with the given label and
// per-phase timeout. Returns ErrMissingConfig-wrapped error if neither env
// var is set. Used by every DefaultCodex* role constructor so that adding a
// new phase is one line instead of a copy of the resolve+literal pattern.
func buildCodexClient(phaseEnv, tierEnv, label string, timeout time.Duration) (*codexllm.Client, error) {
	profile, err := deepResearchCodexProfileFromEnv(phaseEnv, tierEnv)
	if err != nil {
		return nil, err
	}
	return &codexllm.Client{
		Profile: profile,
		Timeout: timeout,
		Label:   label,
	}, nil
}
