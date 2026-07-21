package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/agentic_loop/mcp"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

type mcpResolvedConfig struct {
	command    string
	args       []string
	workingDir string
	env        map[string]string
}

type hostMCPClient struct {
	configHash string
	client     *mcp.StdioClient
	mu         sync.Mutex
}

// MCPHostManager manages host-side MCP server processes.
type MCPHostManager struct {
	db      gowild_data.Database
	mu      sync.Mutex
	clients map[string]*hostMCPClient
}

func NewMCPHostManager(db gowild_data.Database) *MCPHostManager {
	return &MCPHostManager{
		db:      db,
		clients: make(map[string]*hostMCPClient),
	}
}

func (m *MCPHostManager) CloseClient(agentID, serverID string) {
	key := agentID + ":" + serverID

	m.mu.Lock()
	client, ok := m.clients[key]
	if ok {
		delete(m.clients, key)
	}
	m.mu.Unlock()

	if ok && client != nil {
		client.mu.Lock()
		_ = client.client.Close()
		client.mu.Unlock()
	}
}

func (m *MCPHostManager) ListTools(ctx context.Context, agentID, serverID string) ([]mcp.MCPTool, error) {
	client, err := m.getClient(ctx, agentID, serverID)
	if err != nil {
		return nil, err
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.client.ListMCPTools(ctx)
}

func (m *MCPHostManager) CallTool(ctx context.Context, agentID, serverID, toolName string, args map[string]any) (*loop.ToolResult, error) {
	client, err := m.getClient(ctx, agentID, serverID)
	if err != nil {
		return nil, err
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.client.CallTool(ctx, toolName, args)
}

func (m *MCPHostManager) getClient(ctx context.Context, agentID, serverID string) (*hostMCPClient, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}

	cfg, enabled, err := m.resolveConfig(ctx, agentID, serverID)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, fmt.Errorf("mcp server %q is disabled for agent %q", serverID, agentID)
	}

	key := agentID + ":" + serverID
	hash := hashMCPConfig(cfg)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.clients[key]; ok {
		if existing.configHash == hash {
			return existing, nil
		}
		_ = existing.client.Close()
		delete(m.clients, key)
	}

	client, err := mcp.NewStdioClientWithOptions(context.Background(), cfg.command, cfg.args, mcp.StdioClientOptions{
		Env: cfg.env,
		Dir: cfg.workingDir,
	})
	if err != nil {
		return nil, err
	}

	hostClient := &hostMCPClient{
		configHash: hash,
		client:     client,
	}
	m.clients[key] = hostClient
	return hostClient, nil
}

func (m *MCPHostManager) resolveConfig(ctx context.Context, agentID, serverID string) (mcpResolvedConfig, bool, error) {
	server, err := data.GetMCPServer(ctx, m.db, serverID)
	if err != nil {
		return mcpResolvedConfig{}, false, fmt.Errorf("mcp server %q not found: %w", serverID, err)
	}
	if server.Command == "" {
		return mcpResolvedConfig{}, false, fmt.Errorf("mcp server %q has no command configured", serverID)
	}
	agentCfg, err := data.GetAgentMCPServer(ctx, m.db, agentID, serverID)
	if err != nil {
		return mcpResolvedConfig{}, false, err
	}
	if agentCfg == nil || !agentCfg.Enabled {
		return mcpResolvedConfig{}, false, nil
	}

	args := server.Args
	if len(agentCfg.Args) > 0 {
		args = agentCfg.Args
	}
	workingDir := server.WorkingDir
	if agentCfg.WorkingDir != "" {
		workingDir = agentCfg.WorkingDir
	}

	env := make(map[string]string)
	for k, v := range server.DefaultEnv {
		env[k] = v
	}
	for k, v := range agentCfg.Env {
		env[k] = v
	}
	env["GOWILD_AGENT_ID"] = agentID
	env["MCP_AGENT_ID"] = agentID

	return mcpResolvedConfig{
		command:    server.Command,
		args:       args,
		workingDir: workingDir,
		env:        env,
	}, true, nil
}

func hashMCPConfig(cfg mcpResolvedConfig) string {
	var sb strings.Builder
	sb.WriteString(cfg.command)
	sb.WriteString("|")
	sb.WriteString(cfg.workingDir)
	sb.WriteString("|")
	sb.WriteString(strings.Join(cfg.args, "\x1f"))
	sb.WriteString("|")

	if len(cfg.env) > 0 {
		keys := make([]string, 0, len(cfg.env))
		for k := range cfg.env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(cfg.env[k])
			sb.WriteString(";")
		}
	}

	return sb.String()
}
