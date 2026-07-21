package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// RegisterCapability registers a capability for the agent.
//
// The method must already exist as a global A2A method (see CreateA2AMethod).
func (s *AgentService) RegisterCapability(ctx context.Context, role, method string) error {
	_, err := s.CreateCapability(ctx, role, method)
	return err
}

// CreateCapability inserts and returns a capability record.
//
// Capabilities are per-agent and are used for routing (role+method -> agent).
// Payload contracts (schemas/description) live on the global A2A method.
func (s *AgentService) CreateCapability(ctx context.Context, role, method string) (*AgentCapability, error) {
	role = strings.TrimSpace(role)
	method = strings.TrimSpace(method)
	if role == "" {
		return nil, fmt.Errorf("role is required")
	}
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	// Enforce: method must exist globally.
	if _, err := s.GetA2AMethod(ctx, method); err != nil {
		return nil, fmt.Errorf("unknown method %q", method)
	}

	dao := s.db.Table(AgentCapability{})

	// Avoid duplicates for the same agent.
	existing, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"agent_id": s.agentID,
			"role":     role,
			"method":   method,
		},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("capability already exists for %s/%s", role, method)
	}

	now := time.Now()
	cap := &AgentCapability{
		ID:               newID(),
		AgentID:          s.agentID,
		Role:             role,
		Method:           method,
		Description:      "",
		InputSchemaJSON:  "",
		OutputSchemaJSON: "",
		RegisteredAt:     now,
	}
	if err := dao.Insert(ctx, cap); err != nil {
		return nil, err
	}
	return cap, nil
}

// GetCapabilities retrieves all capabilities registered by the agent.
func (s *AgentService) GetCapabilities(ctx context.Context) ([]AgentCapability, error) {
	dao := s.db.Table(AgentCapability{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return nil, err
	}

	caps := make([]AgentCapability, len(results))
	for i, r := range results {
		caps[i] = *r.(*AgentCapability)
	}
	return caps, nil
}

// FindAgentByCapability finds the agent ID that provides the given role and method.
// Searches across all agents (not scoped to s.agentID).
func (s *AgentService) FindAgentByCapability(ctx context.Context, role, method string) (string, error) {
	dao := s.db.Table(AgentCapability{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"role": role, "method": method},
		Limit: 1,
	})
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", fmt.Errorf("no agent found with capability %s/%s", role, method)
	}
	return results[0].(*AgentCapability).AgentID, nil
}

// FindAgentByCapabilityInCompany finds an agent ID for role/method restricted to a company.
func (s *AgentService) FindAgentByCapabilityInCompany(ctx context.Context, role, method, companyID string) (string, error) {
	role = strings.TrimSpace(role)
	method = strings.TrimSpace(method)
	companyID = strings.TrimSpace(companyID)
	if role == "" || method == "" {
		return "", fmt.Errorf("role and method are required")
	}
	if companyID == "" {
		return "", fmt.Errorf("company_id is required")
	}

	capDAO := s.db.Table(AgentCapability{})
	caps, err := capDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"role": role, "method": method},
	})
	if err != nil {
		return "", err
	}
	if len(caps) == 0 {
		return "", fmt.Errorf("no agent found with capability %s/%s", role, method)
	}

	memberDAO := s.db.Table(CompanyMember{})
	for _, row := range caps {
		cap := row.(*AgentCapability)
		memberRows, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID, "agent_id": cap.AgentID},
			Limit: 1,
		})
		if err != nil {
			return "", err
		}
		if len(memberRows) > 0 {
			return cap.AgentID, nil
		}
	}

	return "", fmt.Errorf("no agent found with capability %s/%s in company %s", role, method, companyID)
}

// FindAllAgentsByCapability returns all agent IDs that provide the given role and method.
// Searches across all agents (not scoped to s.agentID).
func (s *AgentService) FindAllAgentsByCapability(ctx context.Context, role, method string) ([]string, error) {
	dao := s.db.Table(AgentCapability{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"role": role, "method": method},
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no agents found with capability %s/%s", role, method)
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.(*AgentCapability).AgentID)
	}
	return ids, nil
}

// FindAllAgentsByCapabilityInCompany returns all agent IDs for role/method restricted to a company.
func (s *AgentService) FindAllAgentsByCapabilityInCompany(ctx context.Context, role, method, companyID string) ([]string, error) {
	role = strings.TrimSpace(role)
	method = strings.TrimSpace(method)
	companyID = strings.TrimSpace(companyID)
	if role == "" || method == "" {
		return nil, fmt.Errorf("role and method are required")
	}
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}

	capDAO := s.db.Table(AgentCapability{})
	caps, err := capDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"role": role, "method": method},
	})
	if err != nil {
		return nil, err
	}
	if len(caps) == 0 {
		return nil, fmt.Errorf("no agents found with capability %s/%s", role, method)
	}

	memberDAO := s.db.Table(CompanyMember{})
	var ids []string
	for _, row := range caps {
		cap := row.(*AgentCapability)
		memberRows, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID, "agent_id": cap.AgentID},
			Limit: 1,
		})
		if err != nil {
			return nil, err
		}
		if len(memberRows) > 0 {
			ids = append(ids, cap.AgentID)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no agents found with capability %s/%s in company %s", role, method, companyID)
	}
	return ids, nil
}

// ClearCapabilities removes all capabilities for the agent (for re-registration on startup).
func (s *AgentService) ClearCapabilities(ctx context.Context) error {
	dao := s.db.Table(AgentCapability{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		cap := r.(*AgentCapability)
		if err := dao.Delete(ctx, cap.ID); err != nil {
			return err
		}
	}
	return nil
}
