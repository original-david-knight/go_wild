//go:build !devtools

package pipelinespec

import (
	"strings"
	"testing"
)

// TestBuiltinTestSeedExcludedFromProductionBuilds pins the production-build
// contract: with the `devtools` tag absent, the `builtin_test_seed` method
// and its two aliases are not compiled in, so NormalizeBuiltinMethod returns
// the raw input unchanged, IsBuiltinMethod returns false, and a pipeline
// that references the method fails validation. Inverse is pinned by
// TestBuiltinTestSeedRegisteredWithDevtoolsTag (//go:build devtools).
func TestBuiltinTestSeedExcludedFromProductionBuilds(t *testing.T) {
	for _, name := range []string{"builtin_test_seed", "test_seed", "/test_seed"} {
		if got := NormalizeBuiltinMethod(name); got != name {
			t.Errorf("NormalizeBuiltinMethod(%q) = %q, want raw input (no production alias)", name, got)
		}
		if IsBuiltinMethod(name) {
			t.Errorf("IsBuiltinMethod(%q) = true, want false in non-devtools build", name)
		}
	}

	err := Validate(Definition{
		ID:   "pipeline_test_seed",
		Name: "Test Seed",
		Steps: []Step{
			{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: "builtin_test_seed"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown builtin method") {
		t.Fatalf("expected builtin_test_seed to be rejected in production build, got %v", err)
	}
}
