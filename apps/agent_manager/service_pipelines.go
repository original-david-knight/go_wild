package main

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
)

// ListPipelineDefinitions returns persisted pipeline definitions.
func (s *AgentService) ListPipelineDefinitions(ctx context.Context) ([]data.PipelineDefinition, error) {
	agentSvc := data.NewAgentService(s.db, "system")
	return agentSvc.ListPipelineDefinitions(ctx)
}

// UpsertPipelineDefinition inserts or updates a persisted pipeline definition.
func (s *AgentService) UpsertPipelineDefinition(ctx context.Context, def *data.PipelineDefinition) error {
	agentSvc := data.NewAgentService(s.db, "system")
	return agentSvc.UpsertPipelineDefinition(ctx, def)
}

// DeletePipelineDefinition removes a persisted pipeline definition by ID.
func (s *AgentService) DeletePipelineDefinition(ctx context.Context, pipelineID string) error {
	agentSvc := data.NewAgentService(s.db, "system")
	return agentSvc.DeletePipelineDefinition(ctx, pipelineID)
}

// GetPipelineDefinition returns a persisted pipeline definition by ID.
func (s *AgentService) GetPipelineDefinition(ctx context.Context, pipelineID string) (*data.PipelineDefinition, error) {
	agentSvc := data.NewAgentService(s.db, "system")
	return agentSvc.GetPipelineDefinition(ctx, pipelineID)
}
