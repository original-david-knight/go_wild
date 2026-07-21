package deepresearch

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestDeepResearchModelFromEnvPrecedence(t *testing.T) {
	const (
		phaseEnv = "DEEP_RESEARCH_TEST_PHASE_MODEL"
		global   = "DEEP_RESEARCH_MODEL"
		tierEnv  = "DEEP_RESEARCH_TEST_TIER"
	)

	origPhase := os.Getenv(phaseEnv)
	origGlobal := os.Getenv(global)
	origTier := os.Getenv(tierEnv)
	defer func() {
		_ = os.Setenv(phaseEnv, origPhase)
		_ = os.Setenv(global, origGlobal)
		_ = os.Setenv(tierEnv, origTier)
	}()

	// No env vars set → panics
	_ = os.Unsetenv(phaseEnv)
	_ = os.Unsetenv(global)
	_ = os.Unsetenv(tierEnv)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic when no model env vars set")
			}
		}()
		deepResearchModelFromEnv(phaseEnv, tierEnv)
	}()

	// Tier env set → uses tier model
	_ = os.Setenv(tierEnv, "tier-model")
	if got := deepResearchModelFromEnv(phaseEnv, tierEnv); got != "tier-model" {
		t.Fatalf("tier model = %q, want %q", got, "tier-model")
	}

	// Global overrides tier
	_ = os.Setenv(global, "global-model")
	if got := deepResearchModelFromEnv(phaseEnv, tierEnv); got != "global-model" {
		t.Fatalf("global model = %q, want %q", got, "global-model")
	}

	// Phase overrides global
	_ = os.Setenv(phaseEnv, "phase-model")
	if got := deepResearchModelFromEnv(phaseEnv, tierEnv); got != "phase-model" {
		t.Fatalf("phase model = %q, want %q", got, "phase-model")
	}
}

func TestDeepResearchCodexProfileFromEnvPrecedence(t *testing.T) {
	const (
		phaseEnv = "DEEP_RESEARCH_TEST_CODEX_PHASE"
		tierEnv  = "DEEP_RESEARCH_TEST_CODEX_TIER"
	)

	origPhase := os.Getenv(phaseEnv)
	origTier := os.Getenv(tierEnv)
	defer func() {
		_ = os.Setenv(phaseEnv, origPhase)
		_ = os.Setenv(tierEnv, origTier)
	}()

	_ = os.Unsetenv(phaseEnv)
	_ = os.Unsetenv(tierEnv)
	_, err := deepResearchCodexProfileFromEnv(phaseEnv, tierEnv)
	if err == nil {
		t.Fatal("expected error when no Codex profile env vars are set")
	}
	if !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("expected ErrMissingConfig, got %v", err)
	}

	_ = os.Setenv(tierEnv, "tier-profile")
	got, err := deepResearchCodexProfileFromEnv(phaseEnv, tierEnv)
	if err != nil {
		t.Fatalf("unexpected error with tier set: %v", err)
	}
	if got != "tier-profile" {
		t.Fatalf("tier profile = %q, want %q", got, "tier-profile")
	}

	_ = os.Setenv(phaseEnv, "phase-profile")
	got, err = deepResearchCodexProfileFromEnv(phaseEnv, tierEnv)
	if err != nil {
		t.Fatalf("unexpected error with phase set: %v", err)
	}
	if got != "phase-profile" {
		t.Fatalf("phase profile = %q, want %q", got, "phase-profile")
	}
}

func TestDeepResearchCurrentTimeIncludesUTC(t *testing.T) {
	out := deepResearchCurrentTime()
	if !strings.Contains(out, "UTC: ") {
		t.Fatalf("expected UTC prefix in %q", out)
	}
	if !strings.Contains(out, "(") || !strings.Contains(out, ")") {
		t.Fatalf("expected day-of-week formatting in %q", out)
	}
}
