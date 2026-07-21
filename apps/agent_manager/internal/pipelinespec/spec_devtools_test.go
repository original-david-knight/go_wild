//go:build devtools

package pipelinespec

import "testing"

// TestBuiltinTestSeedRegisteredWithDevtoolsTag pins the devtools-build
// contract: with the `devtools` tag set, the BuiltinTestSeed constant is
// declared, the three method names (canonical + two aliases) normalize to
// the canonical form, IsBuiltinMethod accepts all three, and a pipeline that
// references the method passes validation. Inverse is pinned by
// TestBuiltinTestSeedExcludedFromProductionBuilds (//go:build !devtools).
func TestBuiltinTestSeedRegisteredWithDevtoolsTag(t *testing.T) {
	if BuiltinTestSeed != "builtin_test_seed" {
		t.Fatalf("BuiltinTestSeed = %q, want %q", BuiltinTestSeed, "builtin_test_seed")
	}

	cases := map[string]string{
		"builtin_test_seed": BuiltinTestSeed,
		"test_seed":         BuiltinTestSeed,
		"/test_seed":        BuiltinTestSeed,
	}
	for raw, want := range cases {
		if got := NormalizeBuiltinMethod(raw); got != want {
			t.Errorf("NormalizeBuiltinMethod(%q) = %q, want %q", raw, got, want)
		}
		if !IsBuiltinMethod(raw) {
			t.Errorf("IsBuiltinMethod(%q) = false, want true under devtools tag", raw)
		}
	}

	valid := Definition{
		ID:   "pipeline_test_seed",
		Name: "Test Seed",
		Steps: []Step{
			{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: BuiltinTestSeed},
		},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate(valid test_seed pipeline) unexpected error: %v", err)
	}
}
