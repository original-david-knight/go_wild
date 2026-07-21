package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// MCPAdminTools proxies MCP server management through the broker API.
type MCPAdminTools struct {
	client *Client
}

func NewMCPAdminTools(client *Client) *MCPAdminTools {
	return &MCPAdminTools{client: client}
}

func (t *MCPAdminTools) McpListServersTool(ctx context.Context, input tools.MCPListServersInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "mcp_list_servers", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *MCPAdminTools) McpSetLocalServerTool(ctx context.Context, input tools.MCPSetLocalServerInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "mcp_set_local_server", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *MCPAdminTools) McpRemoveLocalServerTool(ctx context.Context, input tools.MCPRemoveLocalServerInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "mcp_remove_local_server", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *MCPAdminTools) McpSetServerEnabledTool(ctx context.Context, input tools.MCPSetServerEnabledInput) (*loop.ToolResult, error) {
	result, err := t.client.CallTool(ctx, "mcp_set_server_enabled", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *MCPAdminTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"mcp_list_servers":        "List MCP servers available to this agent, including local and host-side servers, along with enabled state.",
		"mcp_set_local_server":    "Create or update a local MCP server configuration for this agent.",
		"mcp_remove_local_server": "Remove a local MCP server configuration for this agent.",
		"mcp_set_server_enabled":  "Enable or disable an MCP server for this agent. Scope must be 'local' or 'host'.",
	}
	return descriptions[name]
}
