package main

import (
	data "github.com/original-david-knight/go_wild/agent_data"
	spec "github.com/original-david-knight/go_wild/apps/agent_manager/internal/pipelinespec"
)

type Pipeline = spec.Definition
type PipelineStep = spec.Step

const (
	pipelineStepRunnerAgent      = spec.RunnerAgent
	pipelineStepRunnerBuiltin    = spec.RunnerBuiltin
	pipelineStepRunnerClaudeCode = spec.RunnerClaudeCode
	pipelineStepRunnerCodex      = spec.RunnerCodex
)

func clonePipelines(in []Pipeline) []Pipeline {
	return spec.CloneAll(in)
}

func clonePipeline(in Pipeline) Pipeline {
	return spec.Clone(in)
}

func normalizePipeline(in Pipeline) Pipeline {
	return spec.Normalize(in)
}

func validatePipeline(p Pipeline) error {
	return spec.Validate(p)
}

func pipelineFromDefinition(def data.PipelineDefinition) (Pipeline, error) {
	return spec.FromDefinition(def)
}

func normalizePipelineStepRunner(raw string) string {
	return spec.NormalizeRunner(raw)
}

func normalizeBuiltinPipelineMethod(raw string) string {
	return spec.NormalizeBuiltinMethod(raw)
}

func isBuiltinPipelineMethod(method string) bool {
	return spec.IsBuiltinMethod(method)
}
