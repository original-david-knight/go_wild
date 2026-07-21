package main

import (
	"context"

	kg "github.com/original-david-knight/go_wild/knowledge_graph"
)

type knowledgeGraphToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error)

var knowledgeGraphToolHandlers = map[string]knowledgeGraphToolHandlerFunc{
	"kg_search": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[kg.KgSearchInput](inputJSON, func(input kg.KgSearchInput) (any, error) {
			kgTools := h.kgTools(agentID)
			r, err := kgTools.KgSearchTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"kg_add": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[kg.KgAddInput](inputJSON, func(input kg.KgAddInput) (any, error) {
			kgTools := h.kgTools(agentID)
			r, err := kgTools.KgAddTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"kg_get": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[kg.KgGetInput](inputJSON, func(input kg.KgGetInput) (any, error) {
			kgTools := h.kgTools(agentID)
			r, err := kgTools.KgGetTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"kg_update": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[kg.KgUpdateInput](inputJSON, func(input kg.KgUpdateInput) (any, error) {
			kgTools := h.kgTools(agentID)
			r, err := kgTools.KgUpdateTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"kg_delete": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[kg.KgDeleteInput](inputJSON, func(input kg.KgDeleteInput) (any, error) {
			kgTools := h.kgTools(agentID)
			r, err := kgTools.KgDeleteTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
	"kg_explore": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[kg.KgExploreInput](inputJSON, func(input kg.KgExploreInput) (any, error) {
			kgTools := h.kgTools(agentID)
			r, err := kgTools.KgExploreTool(ctx, input)
			return toolResultContent(r, err)
		})
	},
}

func isKnowledgeGraphTool(toolName string) bool {
	_, ok := knowledgeGraphToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callKnowledgeGraphTools(ctx context.Context, agentID string, toolName string, inputJSON []byte) (bool, any, error) {
	if !isKnowledgeGraphTool(toolName) {
		return false, nil, nil
	}

	handler, ok := knowledgeGraphToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, agentID, inputJSON)
	return true, result, err
}

// kgTools creates knowledge graph tools for the given agent.
func (h *BrokerToolsHandler) kgTools(agentID string) *kg.Tools {
	kgTools := kg.NewTools(h.db, agentID)
	// Set up embedding service for semantic search
	ctx := context.Background()
	es, err := kg.NewEmbeddingService(ctx, "")
	if err == nil {
		kgTools.SetEmbeddingService(es)
	}
	return kgTools
}
