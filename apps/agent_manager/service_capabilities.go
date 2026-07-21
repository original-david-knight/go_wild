package main

import (
	"context"
	"fmt"

	"github.com/original-david-knight/go_wild/agent_data"
)

// GetCapabilities returns all capabilities for an agent.
func (s *AgentService) GetCapabilities(ctx context.Context, agentID string) ([]data.AgentCapability, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetCapabilities(ctx)
}

// AddCapability registers a new capability for an agent and returns it.
func (s *AgentService) AddCapability(ctx context.Context, agentID, role, method string) (*data.AgentCapability, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	cap, err := agentSvc.CreateCapability(ctx, role, method)
	if err != nil {
		return nil, err
	}
	if cap == nil {
		return nil, fmt.Errorf("capability created but missing response record")
	}
	return cap, nil
}

// DeleteCapability deletes a specific capability by ID.
func (s *AgentService) DeleteCapability(ctx context.Context, agentID, capID string) error {
	return s.db.Table(data.AgentCapability{}).Delete(ctx, capID)
}
