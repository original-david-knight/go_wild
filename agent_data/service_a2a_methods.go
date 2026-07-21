package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// A2AMethodOption is a functional option for A2A method create/update.
type A2AMethodOption func(m *A2AMethod)

// WithModelTier sets the model tier ("fast" or "smart") for the method.
func WithModelTier(tier string) A2AMethodOption {
	return func(m *A2AMethod) {
		m.ModelTier = strings.TrimSpace(tier)
	}
}

// WithCompletionKeys configures market property keys to set when the method completes.
// timestampKey is written as a datetime (current UTC time).
// successKey is written as a bool ("true"/"false" based on succeeded/failed).
func WithCompletionKeys(timestampKey, successKey string) A2AMethodOption {
	return func(m *A2AMethod) {
		m.CompletionTimestampKey = strings.TrimSpace(timestampKey)
		m.CompletionSuccessKey = strings.TrimSpace(successKey)
	}
}

// WithDisabledToolGroups sets the list of tool group IDs disabled for this method.
func WithDisabledToolGroups(groups []string) A2AMethodOption {
	return func(m *A2AMethod) {
		if len(groups) == 0 {
			m.DisabledToolGroupsJSON = ""
			return
		}
		b, err := json.Marshal(groups)
		if err != nil {
			return
		}
		m.DisabledToolGroupsJSON = string(b)
	}
}

// ListA2AMethods returns all globally-configured A2A methods.
func (s *AgentService) ListA2AMethods(ctx context.Context) ([]A2AMethod, error) {
	dao := s.db.Table(A2AMethod{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	methods := make([]A2AMethod, len(results))
	for i, r := range results {
		methods[i] = *r.(*A2AMethod)
	}
	return methods, nil
}

// GetA2AMethod retrieves an A2A method by name.
func (s *AgentService) GetA2AMethod(ctx context.Context, method string) (*A2AMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	dao := s.db.Table(A2AMethod{})
	var m A2AMethod
	if err := dao.Get(ctx, method, &m); err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}
	return &m, nil
}

// CreateA2AMethod inserts and returns a new A2A method. The method name is the
// primary key and must be unique.
func (s *AgentService) CreateA2AMethod(ctx context.Context, method, description, inputSchemaJSON, outputSchemaJSON string) (*A2AMethod, error) {
	return s.CreateA2AMethodWithInstructions(ctx, method, description, "", inputSchemaJSON, outputSchemaJSON)
}

// CreateA2AMethodWithInstructions inserts and returns a new A2A method with
// method-specific execution instructions for the implementing agent.
func (s *AgentService) CreateA2AMethodWithInstructions(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string) (*A2AMethod, error) {
	return s.CreateA2AMethodWithConfig(ctx, method, description, instructions, inputSchemaJSON, outputSchemaJSON, false, false, false, false, false)
}

// CreateA2AMethodWithConfig inserts and returns a new A2A method with
// method-specific execution instructions and manager-side behavior flags.
func (s *AgentService) CreateA2AMethodWithConfig(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string, autoMarketNote bool, freshContext bool, redactMarketPrices bool, disableMarketNotes bool, disablePolymarketNoteAugmentation bool, opts ...A2AMethodOption) (*A2AMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	dao := s.db.Table(A2AMethod{})
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
	m := &A2AMethod{
		Method:                            method,
		Description:                       strings.TrimSpace(description),
		Instructions:                      strings.TrimSpace(instructions),
		InputSchemaJSON:                   strings.TrimSpace(inputSchemaJSON),
		OutputSchemaJSON:                  strings.TrimSpace(outputSchemaJSON),
		AutoMarketNote:                    autoMarketNote,
		FreshContext:                      freshContext,
		RedactMarketPrices:                redactMarketPrices,
		DisableMarketNotes:                disableMarketNotes,
		DisablePolymarketNoteAugmentation: disablePolymarketNoteAugmentation,
		CreatedAt:                         now,
		UpdatedAt:                         now,
	}
	for _, opt := range opts {
		opt(m)
	}
	if err := dao.Insert(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateA2AMethod updates an existing method.
func (s *AgentService) UpdateA2AMethod(ctx context.Context, method, description, inputSchemaJSON, outputSchemaJSON string) (*A2AMethod, error) {
	existing, err := s.GetA2AMethod(ctx, method)
	if err != nil {
		return nil, err
	}
	return s.UpdateA2AMethodWithInstructions(ctx, method, description, existing.Instructions, inputSchemaJSON, outputSchemaJSON)
}

// UpdateA2AMethodWithInstructions updates an existing method, including
// method-specific execution instructions for the implementing agent.
func (s *AgentService) UpdateA2AMethodWithInstructions(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string) (*A2AMethod, error) {
	existing, err := s.GetA2AMethod(ctx, method)
	if err != nil {
		return nil, err
	}
	return s.UpdateA2AMethodWithConfig(ctx, method, description, instructions, inputSchemaJSON, outputSchemaJSON, existing.AutoMarketNote, existing.FreshContext, existing.RedactMarketPrices, existing.DisableMarketNotes, existing.DisablePolymarketNoteAugmentation)
}

// UpdateA2AMethodWithConfig updates an existing method, including
// method-specific execution instructions and manager-side behavior flags.
func (s *AgentService) UpdateA2AMethodWithConfig(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string, autoMarketNote bool, freshContext bool, redactMarketPrices bool, disableMarketNotes bool, disablePolymarketNoteAugmentation bool, opts ...A2AMethodOption) (*A2AMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}

	dao := s.db.Table(A2AMethod{})
	var existing A2AMethod
	if err := dao.Get(ctx, method, &existing); err != nil {
		return nil, fmt.Errorf("method not found: %w", err)
	}

	existing.Description = strings.TrimSpace(description)
	existing.Instructions = strings.TrimSpace(instructions)
	existing.InputSchemaJSON = strings.TrimSpace(inputSchemaJSON)
	existing.OutputSchemaJSON = strings.TrimSpace(outputSchemaJSON)
	existing.AutoMarketNote = autoMarketNote
	existing.FreshContext = freshContext
	existing.RedactMarketPrices = redactMarketPrices
	existing.DisableMarketNotes = disableMarketNotes
	existing.DisablePolymarketNoteAugmentation = disablePolymarketNoteAugmentation
	for _, opt := range opts {
		opt(&existing)
	}
	existing.UpdatedAt = time.Now()

	if err := dao.Update(ctx, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

// DeleteA2AMethod deletes an A2A method by name.
func (s *AgentService) DeleteA2AMethod(ctx context.Context, method string) error {
	method = strings.TrimSpace(method)
	if method == "" {
		return fmt.Errorf("method is required")
	}

	dao := s.db.Table(A2AMethod{})
	var existing A2AMethod
	if err := dao.Get(ctx, method, &existing); err != nil {
		return fmt.Errorf("method not found: %w", err)
	}
	return dao.Delete(ctx, method)
}
