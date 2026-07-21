package broker

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	kg "github.com/original-david-knight/go_wild/knowledge_graph"
)

// KGTools proxies knowledge graph operations through the broker API.
type KGTools struct {
	client *Client
}

func NewKGTools(client *Client) *KGTools {
	return &KGTools{client: client}
}

func (k *KGTools) KgSearchTool(ctx context.Context, input kg.KgSearchInput) (*loop.ToolResult, error) {
	result, err := k.client.CallTool(ctx, "kg_search", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (k *KGTools) KgAddTool(ctx context.Context, input kg.KgAddInput) (*loop.ToolResult, error) {
	result, err := k.client.CallTool(ctx, "kg_add", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (k *KGTools) KgGetTool(ctx context.Context, input kg.KgGetInput) (*loop.ToolResult, error) {
	result, err := k.client.CallTool(ctx, "kg_get", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (k *KGTools) KgUpdateTool(ctx context.Context, input kg.KgUpdateInput) (*loop.ToolResult, error) {
	result, err := k.client.CallTool(ctx, "kg_update", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (k *KGTools) KgDeleteTool(ctx context.Context, input kg.KgDeleteInput) (*loop.ToolResult, error) {
	result, err := k.client.CallTool(ctx, "kg_delete", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (k *KGTools) KgExploreTool(ctx context.Context, input kg.KgExploreInput) (*loop.ToolResult, error) {
	result, err := k.client.CallTool(ctx, "kg_explore", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (k *KGTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"kg_search":  "Search the knowledge graph by text, semantic meaning, or similarity",
		"kg_add":     "Add a node or edge to the knowledge graph",
		"kg_get":     "Retrieve a node or edge by ID",
		"kg_update":  "Update a node or edge by ID",
		"kg_delete":  "Delete a node or edge by ID",
		"kg_explore": "Explore graph relationships from a starting node",
	}
	return descriptions[name]
}
