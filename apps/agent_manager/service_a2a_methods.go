package main

import (
	"context"

	"github.com/original-david-knight/go_wild/agent_data"
)

func (s *AgentService) ListA2AMethods(ctx context.Context) ([]data.A2AMethod, error) {
	return data.NewAgentService(s.db, "system").ListA2AMethods(ctx)
}

func (s *AgentService) GetA2AMethod(ctx context.Context, method string) (*data.A2AMethod, error) {
	return data.NewAgentService(s.db, "system").GetA2AMethod(ctx, method)
}

func (s *AgentService) CreateA2AMethod(ctx context.Context, method, description, inputSchemaJSON, outputSchemaJSON string) (*data.A2AMethod, error) {
	return data.NewAgentService(s.db, "system").CreateA2AMethod(ctx, method, description, inputSchemaJSON, outputSchemaJSON)
}

func (s *AgentService) CreateA2AMethodWithInstructions(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string) (*data.A2AMethod, error) {
	return data.NewAgentService(s.db, "system").CreateA2AMethodWithInstructions(ctx, method, description, instructions, inputSchemaJSON, outputSchemaJSON)
}

func (s *AgentService) CreateA2AMethodWithConfig(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string, autoMarketNote bool, freshContext bool, redactMarketPrices bool, disableMarketNotes bool, disablePolymarketNoteAugmentation bool, opts ...data.A2AMethodOption) (*data.A2AMethod, error) {
	return data.NewAgentService(s.db, "system").CreateA2AMethodWithConfig(ctx, method, description, instructions, inputSchemaJSON, outputSchemaJSON, autoMarketNote, freshContext, redactMarketPrices, disableMarketNotes, disablePolymarketNoteAugmentation, opts...)
}

func (s *AgentService) UpdateA2AMethodWithConfig(ctx context.Context, method, description, instructions, inputSchemaJSON, outputSchemaJSON string, autoMarketNote bool, freshContext bool, redactMarketPrices bool, disableMarketNotes bool, disablePolymarketNoteAugmentation bool, opts ...data.A2AMethodOption) (*data.A2AMethod, error) {
	return data.NewAgentService(s.db, "system").UpdateA2AMethodWithConfig(ctx, method, description, instructions, inputSchemaJSON, outputSchemaJSON, autoMarketNote, freshContext, redactMarketPrices, disableMarketNotes, disablePolymarketNoteAugmentation, opts...)
}

func (s *AgentService) DeleteA2AMethod(ctx context.Context, method string) error {
	return data.NewAgentService(s.db, "system").DeleteA2AMethod(ctx, method)
}
