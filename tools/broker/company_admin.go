package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// CompanyAdminTools proxies company admin operations through the broker API.
type CompanyAdminTools struct {
	client *Client
}

func NewCompanyAdminTools(client *Client) *CompanyAdminTools {
	return &CompanyAdminTools{client: client}
}

func (t *CompanyAdminTools) CompanyAdminGetContextTool(ctx context.Context, input tools.CompanyAdminGetContextInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_get_context", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) CompanyAdminUpdateCompanyTool(ctx context.Context, input tools.CompanyAdminUpdateCompanyInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_update_company", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) CompanyAdminListMembersTool(ctx context.Context, input tools.CompanyAdminListMembersInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_list_members", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) CompanyAdminAddMemberTool(ctx context.Context, input tools.CompanyAdminAddMemberInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_add_member", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) CompanyAdminRemoveMemberTool(ctx context.Context, input tools.CompanyAdminRemoveMemberInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_remove_member", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) CompanyAdminSetCEOTool(ctx context.Context, input tools.CompanyAdminSetCEOInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_set_ceo", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) CompanyAdminSendHeartbeatTool(ctx context.Context, input tools.CompanyAdminSendHeartbeatInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "company_admin_send_heartbeat", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) SendCompanyHeartbeatTool(ctx context.Context, input tools.SendCompanyHeartbeatInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "send_company_heartbeat", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *CompanyAdminTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"company_admin_get_context":    "Return the caller's company context and governance role.",
		"company_admin_update_company": "Update company metadata such as name and description (CEO only).",
		"company_admin_list_members":   "List members in the caller's company.",
		"company_admin_add_member":     "Add an agent to the caller's company (CEO only).",
		"company_admin_remove_member":  "Remove an agent from the caller's company (CEO only).",
		"company_admin_set_ceo":        "Assign a new CEO for the caller's company (CEO only).",
		"company_admin_send_heartbeat": "Fan out a heartbeat message to company members (CEO only).",
		"send_company_heartbeat":       "Fan out a heartbeat message to members in the caller's company (CEO only).",
	}
	return descriptions[name]
}
