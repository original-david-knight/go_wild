//go:build devtools

package main

import (
	"context"

	data "github.com/original-david-knight/go_wild/agent_data"
	spec "github.com/original-david-knight/go_wild/apps/agent_manager/internal/pipelinespec"
)

func init() {
	builtinPipelineMethodHandlers[spec.BuiltinTestSeed] = pipelineBuiltinTestSeed
}

// pipelineBuiltinTestSeed returns a fixed list of test items for fan-out testing.
// Compiled in only with the `devtools` build tag; production builds exclude
// both this handler and the `spec.BuiltinTestSeed` constant entirely.
func pipelineBuiltinTestSeed(_ context.Context, _ *PipelineEngine, _ *data.PipelineRun, _ PipelineStep, _ map[string]any) (map[string]any, error) {
	return map[string]any{
		"items": []map[string]any{
			{"topic": "population of Tokyo"},
			{"topic": "height of Mount Everest"},
			{"topic": "speed of light in vacuum"},
			{"topic": "boiling point of water at sea level"},
		},
	}, nil
}
