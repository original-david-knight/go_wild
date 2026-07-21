package deepresearch

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ErrMissingConfig marks errors caused by missing server-side configuration
// (e.g. unset model/profile env vars). Callers should treat these as server
// misconfiguration — not client input errors — so HTTP handlers can map them
// to a 5xx status instead of a 4xx.
var ErrMissingConfig = errors.New("deep research: missing configuration")

// deepResearchModelFromEnv resolves the model for a deep-research phase.
// Priority: phaseEnv → DEEP_RESEARCH_MODEL → tierEnv (FAST_MODEL or SMART_MODEL).
func deepResearchModelFromEnv(phaseEnv, tierEnv string) string {
	if v := strings.TrimSpace(os.Getenv(strings.TrimSpace(phaseEnv))); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("DEEP_RESEARCH_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(tierEnv)); v != "" {
		return v
	}
	panic(fmt.Sprintf("deep research: no model configured (set %s, DEEP_RESEARCH_MODEL, or %s)", phaseEnv, tierEnv))
}

// deepResearchClaudeModelFromEnv resolves the Claude model for a deep-research phase.
// Priority: phaseEnv → claudeTierEnv (CLAUDE_FAST_MODEL or CLAUDE_SMART_MODEL).
func deepResearchClaudeModelFromEnv(phaseEnv, claudeTierEnv string) string {
	if v := strings.TrimSpace(os.Getenv(strings.TrimSpace(phaseEnv))); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(claudeTierEnv)); v != "" {
		return v
	}
	panic(fmt.Sprintf("deep research: no Claude model configured (set %s or %s)", phaseEnv, claudeTierEnv))
}

// deepResearchCodexProfileFromEnv resolves the Codex config profile for a deep-research phase.
// Priority: phaseEnv → tierEnv (CODEX_FAST_PROFILE or CODEX_SMART_PROFILE).
// Codex profiles are defined in ~/.codex/config.toml and bundle model, sandbox, etc.
// Returns an error instead of panicking so config problems surface as normal
// per-request errors with a specific env-var name, rather than as a generic 500
// from the HTTP recovery middleware.
func deepResearchCodexProfileFromEnv(phaseEnv, tierEnv string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(strings.TrimSpace(phaseEnv))); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(os.Getenv(tierEnv)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%w: no Codex profile configured (set %s or %s)", ErrMissingConfig, phaseEnv, tierEnv)
}

func deepResearchCurrentTime() string {
	utc := time.Now().UTC()
	eastern, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Sprintf("UTC: %s (%s)", utc.Format("2006-01-02 3:04 PM"), utc.Format("Monday"))
	}
	et := utc.In(eastern)
	return fmt.Sprintf("UTC: %s (%s) / Eastern: %s (%s)",
		utc.Format("2006-01-02 3:04 PM"), utc.Format("Monday"),
		et.Format("2006-01-02 3:04 PM MST"), et.Format("Monday"))
}
