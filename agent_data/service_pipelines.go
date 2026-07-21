package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// ListPipelineDefinitions returns all persisted pipeline definitions.
func (s *AgentService) ListPipelineDefinitions(ctx context.Context) ([]PipelineDefinition, error) {
	dao := s.db.Table(PipelineDefinition{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	defs := make([]PipelineDefinition, len(results))
	for i, r := range results {
		defs[i] = *r.(*PipelineDefinition)
	}
	return defs, nil
}

// GetPipelineDefinition retrieves a pipeline definition by ID.
func (s *AgentService) GetPipelineDefinition(ctx context.Context, pipelineID string) (*PipelineDefinition, error) {
	dao := s.db.Table(PipelineDefinition{})
	var def PipelineDefinition
	if err := dao.Get(ctx, pipelineID, &def); err != nil {
		return nil, fmt.Errorf("pipeline definition not found: %w", err)
	}
	return &def, nil
}

// UpsertPipelineDefinition inserts or updates a pipeline definition.
func (s *AgentService) UpsertPipelineDefinition(ctx context.Context, def *PipelineDefinition) error {
	if def == nil {
		return fmt.Errorf("pipeline definition is nil")
	}
	if def.ID == "" {
		return fmt.Errorf("pipeline id is required")
	}
	if def.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}
	if def.StepsJSON == "" {
		return fmt.Errorf("steps_json is required")
	}

	def.ScopeMode = strings.TrimSpace(def.ScopeMode)
	def.ScopeCompanyID = strings.TrimSpace(def.ScopeCompanyID)
	if def.ScopeMode == "" {
		def.ScopeMode = "global"
	}
	switch def.ScopeMode {
	case "global":
		def.ScopeCompanyID = ""
	case "company":
		if def.ScopeCompanyID == "" {
			return fmt.Errorf("scope_company_id is required when scope_mode=company")
		}
		var company Company
		if err := s.db.Table(Company{}).Get(ctx, def.ScopeCompanyID, &company); err != nil {
			return fmt.Errorf("scope_company_id not found: %w", err)
		}
	default:
		return fmt.Errorf("invalid scope_mode %q (expected global or company)", def.ScopeMode)
	}

	dao := s.db.Table(PipelineDefinition{})
	now := time.Now()

	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"id": def.ID},
		Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(results) > 0 {
		existing := results[0].(*PipelineDefinition)
		existing.Name = def.Name
		existing.StepsJSON = def.StepsJSON
		existing.ScopeMode = def.ScopeMode
		existing.ScopeCompanyID = def.ScopeCompanyID
		existing.Schedule = def.Schedule
		existing.Enabled = def.Enabled
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	insert := &PipelineDefinition{
		ID:             def.ID,
		Name:           def.Name,
		StepsJSON:      def.StepsJSON,
		ScopeMode:      def.ScopeMode,
		ScopeCompanyID: def.ScopeCompanyID,
		Schedule:       def.Schedule,
		Enabled:        def.Enabled,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return dao.Insert(ctx, insert)
}

// DeletePipelineDefinition removes a pipeline definition by ID.
func (s *AgentService) DeletePipelineDefinition(ctx context.Context, pipelineID string) error {
	return s.db.Table(PipelineDefinition{}).Delete(ctx, pipelineID)
}

// CreatePipelineRun inserts a new pipeline run record.
func (s *AgentService) CreatePipelineRun(ctx context.Context, run *PipelineRun) error {
	return s.db.Table(PipelineRun{}).Insert(ctx, run)
}

// UpdatePipelineRun updates an existing pipeline run record.
func (s *AgentService) UpdatePipelineRun(ctx context.Context, run *PipelineRun) error {
	return s.db.Table(PipelineRun{}).Update(ctx, run)
}

// GetPipelineRun retrieves a pipeline run by ID.
func (s *AgentService) GetPipelineRun(ctx context.Context, runID string) (*PipelineRun, error) {
	dao := s.db.Table(PipelineRun{})
	var run PipelineRun
	if err := dao.Get(ctx, runID, &run); err != nil {
		return nil, fmt.Errorf("pipeline run not found: %w", err)
	}
	return &run, nil
}

// CreateStepRun inserts a new pipeline step run record.
func (s *AgentService) CreateStepRun(ctx context.Context, step *PipelineStepRun) error {
	return s.db.Table(PipelineStepRun{}).Insert(ctx, step)
}

// UpdateStepRun updates an existing pipeline step run record.
func (s *AgentService) UpdateStepRun(ctx context.Context, step *PipelineStepRun) error {
	return s.db.Table(PipelineStepRun{}).Update(ctx, step)
}

// ListStepRunsForRun returns all step runs belonging to a pipeline run.
func (s *AgentService) ListStepRunsForRun(ctx context.Context, runID string) ([]PipelineStepRun, error) {
	dao := s.db.Table(PipelineStepRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"run_id": runID},
		OrderBy: "step_index",
	})
	if err != nil {
		return nil, err
	}
	steps := make([]PipelineStepRun, len(results))
	for i, r := range results {
		steps[i] = *r.(*PipelineStepRun)
	}
	return steps, nil
}

// GetActivePipelineRuns retrieves all pipeline runs with status "running".
func (s *AgentService) GetActivePipelineRuns(ctx context.Context) ([]PipelineRun, error) {
	dao := s.db.Table(PipelineRun{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"status": "running"},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}

	runs := make([]PipelineRun, len(results))
	for i, r := range results {
		runs[i] = *r.(*PipelineRun)
	}
	return runs, nil
}
