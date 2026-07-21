//go:build !devtools

package main

import "testing"

// TestPipelineBuiltinTestSeedExcludedFromProductionBuild pins the
// production-build contract: without the `devtools` tag, the
// `builtin_test_seed` handler must not be registered in
// builtinPipelineMethodHandlers. The string literal is used directly because
// the corresponding spec.BuiltinTestSeed constant is also excluded from
// production.
func TestPipelineBuiltinTestSeedExcludedFromProductionBuild(t *testing.T) {
	if _, ok := builtinPipelineMethodHandlers["builtin_test_seed"]; ok {
		t.Fatalf("builtinPipelineMethodHandlers[%q] unexpectedly registered in non-devtools build", "builtin_test_seed")
	}
}
