package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// CompanyKnowledgeTools proxies company-shared knowledge operations through the broker API.
type CompanyKnowledgeTools struct {
	client *Client
}

func NewCompanyKnowledgeTools(client *Client) *CompanyKnowledgeTools {
	return &CompanyKnowledgeTools{client: client}
}

func (t *CompanyKnowledgeTools) CompanyKnowledgeSearchTool(ctx context.Context, input tools.CompanyKnowledgeSearchInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_knowledge_search", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyKnowledgeTools) CompanyKnowledgeAddTool(ctx context.Context, input tools.CompanyKnowledgeAddInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_knowledge_add", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyKnowledgeTools) CompanyKnowledgeGetTool(ctx context.Context, input tools.CompanyKnowledgeGetInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_knowledge_get", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyKnowledgeTools) CompanyKnowledgeUpdateTool(ctx context.Context, input tools.CompanyKnowledgeUpdateInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_knowledge_update", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyKnowledgeTools) CompanyKnowledgeDeleteTool(ctx context.Context, input tools.CompanyKnowledgeDeleteInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_knowledge_delete", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyKnowledgeTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"company_knowledge_search": "Search shared company knowledge by query and optional kind.",
		"company_knowledge_add":    "Add a new shared company knowledge entry.",
		"company_knowledge_get":    "Get one shared company knowledge entry by ID.",
		"company_knowledge_update": "Update a shared company knowledge entry.",
		"company_knowledge_delete": "Delete a shared company knowledge entry.",
	}
	return descriptions[name]
}
