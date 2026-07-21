//go:build devtools

package main

import (
	"context"
	"testing"

	spec "github.com/original-david-knight/go_wild/apps/agent_manager/internal/pipelinespec"
)

// TestPipelineBuiltinTestSeedRegisteredAndEmitsFixedTopics pins the
// devtools-build behavior of the `builtin_test_seed` handler: it must be
// registered in builtinPipelineMethodHandlers under spec.BuiltinTestSeed and
// must return the canonical 4-topic seed used for fan-out plumbing
// validation. A silent drift in the topic list would bypass any downstream
// test that relies on this exact fan-out shape.
func TestPipelineBuiltinTestSeedRegisteredAndEmitsFixedTopics(t *testing.T) {
	handler, ok := builtinPipelineMethodHandlers[spec.BuiltinTestSeed]
	if !ok {
		t.Fatalf("builtinPipelineMethodHandlers[%q] not registered under devtools tag", spec.BuiltinTestSeed)
	}

	result, err := handler(context.Background(), nil, nil, PipelineStep{}, nil)
	if err != nil {
		t.Fatalf("pipelineBuiltinTestSeed returned unexpected error: %v", err)
	}
	items, ok := result["items"].([]map[string]any)
	if !ok {
		t.Fatalf("result[\"items\"] has wrong type %T, want []map[string]any", result["items"])
	}
	wantTopics := []string{
		"population of Tokyo",
		"height of Mount Everest",
		"speed of light in vacuum",
		"boiling point of water at sea level",
	}
	if len(items) != len(wantTopics) {
		t.Fatalf("len(items) = %d, want %d", len(items), len(wantTopics))
	}
	for i, want := range wantTopics {
		got, _ := items[i]["topic"].(string)
		if got != want {
			t.Errorf("items[%d].topic = %q, want %q", i, got, want)
		}
	}
}
