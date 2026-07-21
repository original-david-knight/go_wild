package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/original-david-knight/go_wild/tools/broker"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/agentic_loop/mcp"
	"github.com/fatih/color"
	"google.golang.org/genai"
)

type mcpServerInfo struct {
	ID          string            `json:"id"`
	Scope       string            `json:"scope"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Enabled     bool              `json:"enabled"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

func addMCPTools(ctx context.Context, agent *loop.AgenticLoop, brokerClient *broker.Client) {
	// Add MCP management tools (broker-proxied).
	adminTools := broker.NewMCPAdminTools(brokerClient)
	agent.AddTools(loop.WrapToolsWithDescriptions(adminTools)...)

	servers, err := fetchMCPServers(ctx, brokerClient)
	if err != nil {
		fmt.Println(color.HiBlackString("MCP: failed to list servers (%v)", err))
		return
	}

	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		switch server.Scope {
		case "local":
			addLocalMCPServer(ctx, agent, server)
		case "host":
			addHostMCPServer(ctx, agent, brokerClient, server)
		}
	}
}

func fetchMCPServers(ctx context.Context, brokerClient *broker.Client) ([]mcpServerInfo, error) {
	result, err := brokerClient.CallTool(ctx, "mcp_list_servers", map[string]any{})
	if err != nil {
		return nil, err
	}
	raw, ok := result["servers"]
	if !ok {
		return nil, fmt.Errorf("missing servers in response")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var servers []mcpServerInfo
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func addLocalMCPServer(ctx context.Context, agent *loop.AgenticLoop, server mcpServerInfo) {
	if server.Command == "" {
		fmt.Println(color.HiBlackString("MCP: local server %s missing command", server.ID))
		return
	}
	client, err := mcp.NewStdioClientWithOptions(ctx, server.Command, server.Args, mcp.StdioClientOptions{
		Env: server.Env,
		Dir: server.WorkingDir,
	})
	if err != nil {
		fmt.Println(color.HiBlackString("MCP: local server %s failed to start (%v)", server.ID, err))
		return
	}
	guarded := &lockedMCPClient{inner: client}
	registerMCPClient(guarded)

	tools, err := guarded.ListTools(ctx)
	if err != nil {
		fmt.Println(color.HiBlackString("MCP: local server %s failed to list tools (%v)", server.ID, err))
		return
	}
	prefixed := prefixMCPTools("local", server, tools)
	agent.AddTools(prefixed...)
	fmt.Println(color.HiBlackString("MCP: local %s (%d tools)", server.ID, len(prefixed)))
}

func addHostMCPServer(ctx context.Context, agent *loop.AgenticLoop, brokerClient *broker.Client, server mcpServerInfo) {
	client := &brokerMCPClient{client: brokerClient, serverID: server.ID}
	tools, err := client.ListTools(ctx)
	if err != nil {
		fmt.Println(color.HiBlackString("MCP: host server %s failed to list tools (%v)", server.ID, err))
		return
	}
	prefixed := prefixMCPTools("host", server, tools)
	agent.AddTools(prefixed...)
	fmt.Println(color.HiBlackString("MCP: host %s (%d tools)", server.ID, len(prefixed)))
}

type prefixedTool struct {
	name        string
	description string
	base        loop.Tool
}

func (t *prefixedTool) Name() string               { return t.name }
func (t *prefixedTool) Description() string        { return t.description }
func (t *prefixedTool) InputSchema() *genai.Schema { return t.base.InputSchema() }
func (t *prefixedTool) Execute(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
	return t.base.Execute(ctx, input)
}

func prefixMCPTools(scope string, server mcpServerInfo, tools []loop.Tool) []loop.Tool {
	prefix := fmt.Sprintf("mcp_%s_%s__", scope, sanitizeToolID(server.ID))
	label := server.Name
	if label == "" {
		label = server.ID
	}
	descriptionPrefix := fmt.Sprintf("[MCP %s: %s] ", scope, label)

	prefixed := make([]loop.Tool, 0, len(tools))
	for _, tool := range tools {
		desc := tool.Description()
		if desc == "" {
			desc = descriptionPrefix + "MCP tool"
		} else {
			desc = descriptionPrefix + desc
		}
		prefixed = append(prefixed, &prefixedTool{
			name:        prefix + tool.Name(),
			description: desc,
			base:        tool,
		})
	}
	return prefixed
}

func sanitizeToolID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "server"
	}
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func registerMCPClient(client interface{ Close() error }) {
	globalMCPClientsMu.Lock()
	defer globalMCPClientsMu.Unlock()
	globalMCPClients = append(globalMCPClients, client)
}

func closeMCPClients() {
	globalMCPClientsMu.Lock()
	clients := globalMCPClients
	globalMCPClients = nil
	globalMCPClientsMu.Unlock()

	for _, c := range clients {
		_ = c.Close()
	}
}

type brokerMCPClient struct {
	client   *broker.Client
	serverID string
}

func (c *brokerMCPClient) Initialize(ctx context.Context) error { return nil }

func (c *brokerMCPClient) ListTools(ctx context.Context) ([]loop.Tool, error) {
	result, err := c.client.CallTool(ctx, "mcp_list_tools", map[string]any{
		"server_id": c.serverID,
	})
	if err != nil {
		return nil, err
	}
	raw, ok := result["tools"]
	if !ok {
		return nil, fmt.Errorf("missing tools in response")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var tools []mcp.MCPTool
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	return mcp.WrapMCPTools(c, tools), nil
}

func (c *brokerMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*loop.ToolResult, error) {
	result, err := c.client.CallTool(ctx, "mcp_call_tool", map[string]any{
		"server_id": c.serverID,
		"tool_name": name,
		"arguments": args,
	})
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (c *brokerMCPClient) Close() error { return nil }

type lockedMCPClient struct {
	inner *mcp.StdioClient
	mu    sync.Mutex
}

func (c *lockedMCPClient) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Initialize(ctx)
}

func (c *lockedMCPClient) ListTools(ctx context.Context) ([]loop.Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tools, err := c.inner.ListMCPTools(ctx)
	if err != nil {
		return nil, err
	}
	return mcp.WrapMCPTools(c, tools), nil
}

func (c *lockedMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*loop.ToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.CallTool(ctx, name, args)
}

func (c *lockedMCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Close()
}
