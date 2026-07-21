package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

func (s *AgentService) ListDeepResearchMethods(ctx context.Context) ([]DeepResearchMethod, error) {
	dao := s.db.Table(DeepResearchMethod{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	methods := make([]DeepResearchMethod, len(results))
	for i, r := range results {
		methods[i] = *r.(*DeepResearchMethod)
	}
	return methods, nil
}

func (s *AgentService) GetDeepResearchMethod(ctx context.Context, method string) (*DeepResearchMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	dao := s.db.Table(DeepResearchMethod{})
	var m DeepResearchMethod
	if err := dao.Get(ctx, method, &m); err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}
	return &m, nil
}

func (s *AgentService) CreateDeepResearchMethod(
	ctx context.Context,
	method, description, instructions, queryTemplate, inputSchemaJSON, researchSchemaJSON, optionsJSON string,
	enabled bool,
) (*DeepResearchMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	dao := s.db.Table(DeepResearchMethod{})
	existing, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"method": method},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("method %q already exists", method)
	}

	now := time.Now()
	entry := &DeepResearchMethod{
		Method:             method,
		Description:        strings.TrimSpace(description),
		Instructions:       strings.TrimSpace(instructions),
		QueryTemplate:      strings.TrimSpace(queryTemplate),
		InputSchemaJSON:    strings.TrimSpace(inputSchemaJSON),
		ResearchSchemaJSON: strings.TrimSpace(researchSchemaJSON),
		OptionsJSON:        strings.TrimSpace(optionsJSON),
		Enabled:            enabled,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := dao.Insert(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *AgentService) UpdateDeepResearchMethod(
	ctx context.Context,
	method, description, instructions, queryTemplate, inputSchemaJSON, researchSchemaJSON, optionsJSON string,
	enabled bool,
) (*DeepResearchMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	dao := s.db.Table(DeepResearchMethod{})
	var existing DeepResearchMethod
	if err := dao.Get(ctx, method, &existing); err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}

	existing.Description = strings.TrimSpace(description)
	existing.Instructions = strings.TrimSpace(instructions)
	existing.QueryTemplate = strings.TrimSpace(queryTemplate)
	existing.InputSchemaJSON = strings.TrimSpace(inputSchemaJSON)
	existing.ResearchSchemaJSON = strings.TrimSpace(researchSchemaJSON)
	existing.OptionsJSON = strings.TrimSpace(optionsJSON)
	existing.Enabled = enabled
	existing.UpdatedAt = time.Now()

	if err := dao.Update(ctx, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *AgentService) DeleteDeepResearchMethod(ctx context.Context, method string) error {
	method = strings.TrimSpace(method)
	if method == "" {
		return fmt.Errorf("method is required")
	}
	dao := s.db.Table(DeepResearchMethod{})
	var existing DeepResearchMethod
	if err := dao.Get(ctx, method, &existing); err != nil {
		return fmt.Errorf("method not found: %w", err)
	}
	return dao.Delete(ctx, method)
}

func (s *AgentService) MarkDeepResearchMethodTested(ctx context.Context, method string) error {
	method = strings.TrimSpace(method)
	if method == "" {
		return fmt.Errorf("method is required")
	}
	dao := s.db.Table(DeepResearchMethod{})
	var existing DeepResearchMethod
	if err := dao.Get(ctx, method, &existing); err != nil {
		return fmt.Errorf("method not found: %w", err)
	}
	existing.LastTestedAt = time.Now()
	existing.UpdatedAt = time.Now()
	return dao.Update(ctx, &existing)
}
