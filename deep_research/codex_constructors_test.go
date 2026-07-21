package deepresearch

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/codexllm"
)

// clearCodexProfileEnv unsets every Codex profile env var that any of the four
// deep-research Codex constructors consult, so the "missing env" path is hit
// regardless of developer shell state. Restores the original values on cleanup.
func clearCodexProfileEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"DEEP_RESEARCH_CODEX_PLANNER_PROFILE",
		"DEEP_RESEARCH_CODEX_SEARCH_PROFILE",
		"DEEP_RESEARCH_CODEX_CHECKER_PROFILE",
		"DEEP_RESEARCH_CODEX_SYNTHESIZER_PROFILE",
		"CODEX_FAST_PROFILE",
		"CODEX_SMART_PROFILE",
	}
	for _, v := range vars {
		orig, had := os.LookupEnv(v)
		_ = os.Unsetenv(v)
		v, orig, had := v, orig, had
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(v, orig)
			} else {
				_ = os.Unsetenv(v)
			}
		})
	}
}

func TestDefaultCodexPlannerReturnsErrorWhenProfileMissing(t *testing.T) {
	clearCodexProfileEnv(t)
	planner, err := DefaultCodexPlanner()
	if err == nil {
		t.Fatalf("expected error when CODEX profile env vars are unset")
	}
	if planner != nil {
		t.Fatalf("expected nil planner on error, got %#v", planner)
	}
	if !strings.Contains(err.Error(), "CODEX_SMART_PROFILE") {
		t.Fatalf("error should name the tier env var, got %v", err)
	}
}

func TestNewCodexPlannerWrapsExplicitClient(t *testing.T) {
	clearCodexProfileEnv(t)
	explicit := &codexllm.Client{Profile: "injected", Label: "test"}
	planner := newCodexPlanner(explicit)
	if planner == nil || planner.client != explicit {
		t.Fatalf("expected planner to hold explicit client, got %#v", planner)
	}
	// The refactor added an injectable `generate` function; verify the
	// constructor wires it. A nil `generate` would nil-panic the first time
	// Plan() runs, and every unit test in planner_codex_test.go bypasses the
	// constructor by using struct literals, so this is the only place that
	// catches a broken constructor.
	if planner.generate == nil {
		t.Fatalf("expected generate to be wired from explicit client")
	}
}

func TestDefaultCodexPlannerUsesPhaseEnv(t *testing.T) {
	clearCodexProfileEnv(t)
	t.Setenv("DEEP_RESEARCH_CODEX_PLANNER_PROFILE", "phase-planner")
	planner, err := DefaultCodexPlanner()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if planner.client.Profile != "phase-planner" {
		t.Fatalf("profile = %q, want %q", planner.client.Profile, "phase-planner")
	}
}

func TestDefaultCodexSearcherReturnsErrorWhenProfileMissing(t *testing.T) {
	clearCodexProfileEnv(t)
	searcher, err := DefaultCodexSearcher()
	if err == nil {
		t.Fatalf("expected error when CODEX profile env vars are unset")
	}
	if searcher != nil {
		t.Fatalf("expected nil searcher on error, got %#v", searcher)
	}
	if !strings.Contains(err.Error(), "CODEX_FAST_PROFILE") {
		t.Fatalf("error should name the tier env var, got %v", err)
	}
}

func TestDefaultCodexSearcherUsesPhaseEnv(t *testing.T) {
	clearCodexProfileEnv(t)
	t.Setenv("DEEP_RESEARCH_CODEX_SEARCH_PROFILE", "phase-search")
	searcher, err := DefaultCodexSearcher()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if searcher.client.Profile != "phase-search" {
		t.Fatalf("profile = %q, want %q", searcher.client.Profile, "phase-search")
	}
	if searcher.generate == nil {
		t.Fatalf("expected generate to be wired")
	}
}

func TestNewCodexSearcherWrapsExplicitClient(t *testing.T) {
	clearCodexProfileEnv(t)
	explicit := &codexllm.Client{Profile: "injected", Label: "test"}
	searcher := newCodexSearcher(explicit)
	// Searcher takes a shallow copy of the explicit client rather than
	// aliasing the caller's pointer — see TestNewCodexSearcherForcesWeb
	// SearchOnInjectedClient for the full rationale. What we verify here is
	// only that the caller's fields flowed through to the searcher's client.
	if searcher == nil {
		t.Fatalf("expected non-nil searcher")
	}
	if searcher.client == explicit {
		t.Fatalf("searcher must hold a copy, not the caller's pointer; got same address %p", explicit)
	}
	if searcher.client.Profile != "injected" || searcher.client.Label != "test" {
		t.Fatalf("explicit client fields did not flow through; got %#v", searcher.client)
	}
	if searcher.generate == nil {
		t.Fatalf("expected generate to be wired from explicit client")
	}
}

func TestDefaultCodexCompletenessCheckerReturnsErrorWhenProfileMissing(t *testing.T) {
	clearCodexProfileEnv(t)
	checker, err := DefaultCodexCompletenessChecker()
	if err == nil {
		t.Fatalf("expected error when CODEX profile env vars are unset")
	}
	if checker != nil {
		t.Fatalf("expected nil checker on error, got %#v", checker)
	}
	if !strings.Contains(err.Error(), "CODEX_FAST_PROFILE") {
		t.Fatalf("error should name the tier env var, got %v", err)
	}
}

func TestDefaultCodexCompletenessCheckerUsesPhaseEnv(t *testing.T) {
	clearCodexProfileEnv(t)
	t.Setenv("DEEP_RESEARCH_CODEX_CHECKER_PROFILE", "phase-checker")
	checker, err := DefaultCodexCompletenessChecker()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checker.client.Profile != "phase-checker" {
		t.Fatalf("profile = %q, want %q", checker.client.Profile, "phase-checker")
	}
}

func TestNewCodexCompletenessCheckerWrapsExplicitClient(t *testing.T) {
	clearCodexProfileEnv(t)
	explicit := &codexllm.Client{Profile: "injected", Label: "test"}
	checker := newCodexCompletenessChecker(explicit)
	if checker == nil || checker.client != explicit {
		t.Fatalf("expected checker to hold explicit client, got %#v", checker)
	}
	// See newCodexPlanner wiring note: struct-literal tests bypass the
	// constructor, so the generate-field check has to live here.
	if checker.generate == nil {
		t.Fatalf("expected generate to be wired from explicit client")
	}
}

func TestDefaultCodexSynthesizerReturnsErrorWhenProfileMissing(t *testing.T) {
	clearCodexProfileEnv(t)
	synth, err := DefaultCodexSynthesizer()
	if err == nil {
		t.Fatalf("expected error when CODEX profile env vars are unset")
	}
	if synth != nil {
		t.Fatalf("expected nil synthesizer on error, got %#v", synth)
	}
	if !strings.Contains(err.Error(), "CODEX_SMART_PROFILE") {
		t.Fatalf("error should name the tier env var, got %v", err)
	}
}

func TestDefaultCodexSynthesizerUsesPhaseEnv(t *testing.T) {
	clearCodexProfileEnv(t)
	t.Setenv("DEEP_RESEARCH_CODEX_SYNTHESIZER_PROFILE", "phase-synth")
	synth, err := DefaultCodexSynthesizer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth.client.Profile != "phase-synth" {
		t.Fatalf("profile = %q, want %q", synth.client.Profile, "phase-synth")
	}
}

func TestNewCodexSynthesizerWrapsExplicitClient(t *testing.T) {
	clearCodexProfileEnv(t)
	explicit := &codexllm.Client{Profile: "injected", Label: "test"}
	synth := newCodexSynthesizer(explicit)
	if synth == nil || synth.client != explicit {
		t.Fatalf("expected synthesizer to hold explicit client, got %#v", synth)
	}
	// See newCodexPlanner wiring note: struct-literal tests bypass the
	// constructor, so the generate-field check has to live here.
	if synth.generate == nil {
		t.Fatalf("expected generate to be wired from explicit client")
	}
}

// TestDefaultCodexConstructorsWireClientFields pins the per-role Label and
// Timeout that each DefaultCodex* constructor passes through buildCodexClient.
// Before the shared-helper refactor those values were inline literals, so an
// accidental swap (planner timeout on the checker, etc.) would have been
// caught in code review. Now they're positional args to a shared helper and
// a swap would silently compile. This test makes swaps fail loudly.
func TestDefaultCodexConstructorsWireClientFields(t *testing.T) {
	cases := []struct {
		name        string
		phaseEnvVar string
		phaseValue  string
		build       func(t *testing.T) *codexllm.Client
		wantLabel   string
		wantTimeout time.Duration
	}{
		{
			name:        "planner",
			phaseEnvVar: "DEEP_RESEARCH_CODEX_PLANNER_PROFILE",
			phaseValue:  "p-planner",
			build: func(t *testing.T) *codexllm.Client {
				p, err := DefaultCodexPlanner()
				if err != nil {
					t.Fatalf("DefaultCodexPlanner: %v", err)
				}
				return p.client
			},
			wantLabel:   "deep-research-planner",
			wantTimeout: 3 * time.Minute,
		},
		{
			name:        "searcher",
			phaseEnvVar: "DEEP_RESEARCH_CODEX_SEARCH_PROFILE",
			phaseValue:  "p-search",
			build: func(t *testing.T) *codexllm.Client {
				s, err := DefaultCodexSearcher()
				if err != nil {
					t.Fatalf("DefaultCodexSearcher: %v", err)
				}
				return s.client
			},
			wantLabel:   "deep-research-searcher",
			wantTimeout: 2 * time.Minute,
		},
		{
			name:        "checker",
			phaseEnvVar: "DEEP_RESEARCH_CODEX_CHECKER_PROFILE",
			phaseValue:  "p-checker",
			build: func(t *testing.T) *codexllm.Client {
				c, err := DefaultCodexCompletenessChecker()
				if err != nil {
					t.Fatalf("DefaultCodexCompletenessChecker: %v", err)
				}
				return c.client
			},
			wantLabel:   "deep-research-checker",
			wantTimeout: 2 * time.Minute,
		},
		{
			name:        "synthesizer",
			phaseEnvVar: "DEEP_RESEARCH_CODEX_SYNTHESIZER_PROFILE",
			phaseValue:  "p-synth",
			build: func(t *testing.T) *codexllm.Client {
				s, err := DefaultCodexSynthesizer()
				if err != nil {
					t.Fatalf("DefaultCodexSynthesizer: %v", err)
				}
				return s.client
			},
			wantLabel:   "deep-research-synthesizer",
			wantTimeout: 5 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearCodexProfileEnv(t)
			t.Setenv(tc.phaseEnvVar, tc.phaseValue)
			c := tc.build(t)
			if c.Profile != tc.phaseValue {
				t.Errorf("Profile = %q, want %q", c.Profile, tc.phaseValue)
			}
			if c.Label != tc.wantLabel {
				t.Errorf("Label = %q, want %q", c.Label, tc.wantLabel)
			}
			if c.Timeout != tc.wantTimeout {
				t.Errorf("Timeout = %v, want %v", c.Timeout, tc.wantTimeout)
			}
		})
	}
}

// TestBuildCodexClient directly exercises the shared helper's contract:
// phase-env wins over tier-env for Profile, Label/Timeout flow through, and
// missing-env returns an ErrMissingConfig-wrapped error. The DefaultCodex*
// tests cover this transitively, but calling buildCodexClient directly pins
// its contract for callers that might add a fifth role later.
func TestBuildCodexClient(t *testing.T) {
	t.Run("phase_env_resolves_and_fields_flow_through", func(t *testing.T) {
		clearCodexProfileEnv(t)
		t.Setenv("DEEP_RESEARCH_CODEX_TEST_PHASE", "phase-val")
		t.Setenv("CODEX_FAST_PROFILE", "tier-val")
		c, err := buildCodexClient("DEEP_RESEARCH_CODEX_TEST_PHASE", "CODEX_FAST_PROFILE", "test-label", 7*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Profile != "phase-val" {
			t.Errorf("phase env must win over tier env; got Profile = %q", c.Profile)
		}
		if c.Label != "test-label" {
			t.Errorf("Label = %q, want %q", c.Label, "test-label")
		}
		if c.Timeout != 7*time.Second {
			t.Errorf("Timeout = %v, want 7s", c.Timeout)
		}
	})

	t.Run("falls_back_to_tier_env", func(t *testing.T) {
		clearCodexProfileEnv(t)
		t.Setenv("CODEX_FAST_PROFILE", "tier-val")
		c, err := buildCodexClient("DEEP_RESEARCH_CODEX_TEST_PHASE", "CODEX_FAST_PROFILE", "l", time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Profile != "tier-val" {
			t.Errorf("Profile = %q, want fallback tier value", c.Profile)
		}
	})

	t.Run("returns_error_when_both_env_vars_missing", func(t *testing.T) {
		clearCodexProfileEnv(t)
		c, err := buildCodexClient("DEEP_RESEARCH_CODEX_TEST_PHASE", "CODEX_FAST_PROFILE", "l", time.Second)
		if err == nil {
			t.Fatalf("expected error when both env vars unset")
		}
		if c != nil {
			t.Errorf("expected nil client on error, got %#v", c)
		}
		if !strings.Contains(err.Error(), "CODEX_FAST_PROFILE") {
			t.Errorf("error should name the tier env var, got %v", err)
		}
	})
}
