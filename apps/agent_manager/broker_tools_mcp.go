package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

type mcpListToolsInput struct {
	ServerID string `json:"server_id"`
}

type mcpCallToolInput struct {
	ServerID  string         `json:"server_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

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

type mcpToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, inputJSON []byte) (any, error)

var mcpToolHandlers = map[string]mcpToolHandlerFunc{
	"mcp_list_servers": func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, _ []byte) (any, error) {
		return h.listMCPServers(ctx, agentID, svc)
	},
	"mcp_set_local_server": func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[tools.MCPSetLocalServerInput](inputJSON, func(input tools.MCPSetLocalServerInput) (any, error) {
			return h.setLocalMCPServer(ctx, svc, input)
		})
	},
	"mcp_remove_local_server": func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[tools.MCPRemoveLocalServerInput](inputJSON, func(input tools.MCPRemoveLocalServerInput) (any, error) {
			return h.removeLocalMCPServer(ctx, svc, input.ID)
		})
	},
	"mcp_set_server_enabled": func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[tools.MCPSetServerEnabledInput](inputJSON, func(input tools.MCPSetServerEnabledInput) (any, error) {
			return h.setMCPServerEnabled(ctx, agentID, svc, input)
		})
	},
	"mcp_list_tools": func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[mcpListToolsInput](inputJSON, func(input mcpListToolsInput) (any, error) {
			if input.ServerID == "" {
				return nil, fmt.Errorf("server_id is required")
			}
			if h.mcpHost == nil {
				return nil, fmt.Errorf("mcp host manager unavailable")
			}
			toolsList, err := h.mcpHost.ListTools(ctx, agentID, input.ServerID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"tools": toolsList}, nil
		})
	},
	"mcp_call_tool": func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[mcpCallToolInput](inputJSON, func(input mcpCallToolInput) (any, error) {
			if input.ServerID == "" {
				return nil, fmt.Errorf("server_id is required")
			}
			if input.ToolName == "" {
				return nil, fmt.Errorf("tool_name is required")
			}
			if h.mcpHost == nil {
				return nil, fmt.Errorf("mcp host manager unavailable")
			}
			r, err := h.mcpHost.CallTool(ctx, agentID, input.ServerID, input.ToolName, input.Arguments)
			return toolResultContent(r, err)
		})
	},
}

func isMCPTool(toolName string) bool {
	_, ok := mcpToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callMCPTools(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isMCPTool(toolName) {
		return false, nil, nil
	}

	handler, ok := mcpToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, agentID, svc, inputJSON)
	return true, result, err
}

func (h *BrokerToolsHandler) listMCPServers(ctx context.Context, agentID string, svc *data.AgentService) (any, error) {
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}
	local, err := agent.GetMCPServers()
	if err != nil {
		return nil, err
	}

	registry, err := data.ListMCPServers(ctx, h.db)
	if err != nil {
		return nil, err
	}
	agentCfgs, err := data.ListAgentMCPServers(ctx, h.db, agentID)
	if err != nil {
		return nil, err
	}
	agentCfgByServer := make(map[string]*data.AgentMCPServer)
	for _, cfg := range agentCfgs {
		agentCfgByServer[cfg.ServerID] = cfg
	}

	servers := make([]mcpServerInfo, 0, len(local)+len(registry))
	for _, s := range local {
		servers = append(servers, mcpServerInfo{
			ID:          s.ID,
			Scope:       "local",
			Name:        s.Name,
			Description: s.Description,
			Enabled:     s.Enabled,
			Command:     s.Command,
			Args:        s.Args,
			WorkingDir:  s.WorkingDir,
			Env:         s.Env,
		})
	}
	for _, s := range registry {
		cfg := agentCfgByServer[s.ID]
		enabled := cfg != nil && cfg.Enabled
		servers = append(servers, mcpServerInfo{
			ID:          s.ID,
			Scope:       "host",
			Name:        s.Name,
			Description: s.Description,
			Enabled:     enabled,
		})
	}

	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Scope == servers[j].Scope {
			return servers[i].ID < servers[j].ID
		}
		return servers[i].Scope < servers[j].Scope
	})

	return map[string]any{"servers": servers}, nil
}

func (h *BrokerToolsHandler) setLocalMCPServer(ctx context.Context, svc *data.AgentService, input tools.MCPSetLocalServerInput) (any, error) {
	if input.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if input.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}

	servers, err := agent.GetMCPServers()
	if err != nil {
		return nil, err
	}

	updated := false
	for i := range servers {
		if servers[i].ID == input.ID {
			servers[i] = data.MCPServerConfig{
				ID:          input.ID,
				Name:        input.Name,
				Description: input.Description,
				Command:     input.Command,
				Args:        input.Args,
				WorkingDir:  input.WorkingDir,
				Env:         input.Env,
				Enabled:     input.Enabled,
			}
			updated = true
			break
		}
	}
	if !updated {
		servers = append(servers, data.MCPServerConfig{
			ID:          input.ID,
			Name:        input.Name,
			Description: input.Description,
			Command:     input.Command,
			Args:        input.Args,
			WorkingDir:  input.WorkingDir,
			Env:         input.Env,
			Enabled:     input.Enabled,
		})
	}

	agent.SetMCPServers(servers)
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		return nil, err
	}

	return map[string]any{"status": "ok"}, nil
}

func (h *BrokerToolsHandler) removeLocalMCPServer(ctx context.Context, svc *data.AgentService, serverID string) (any, error) {
	if serverID == "" {
		return nil, fmt.Errorf("id is required")
	}
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := agent.GetMCPServers()
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return map[string]any{"status": "ok"}, nil
	}
	filtered := servers[:0]
	for _, s := range servers {
		if s.ID != serverID {
			filtered = append(filtered, s)
		}
	}
	servers = filtered
	agent.SetMCPServers(servers)
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

type mcpSetServerEnabledScopeHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, input tools.MCPSetServerEnabledInput) error

var mcpSetServerEnabledScopeHandlers = map[string]mcpSetServerEnabledScopeHandlerFunc{
	"local": func(h *BrokerToolsHandler, ctx context.Context, _ string, svc *data.AgentService, input tools.MCPSetServerEnabledInput) error {
		return h.setLocalMCPServerEnabled(ctx, svc, input)
	},
	"host": func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, input tools.MCPSetServerEnabledInput) error {
		return h.setHostMCPServerEnabled(ctx, agentID, input)
	},
}

func isMCPSetServerEnabledScope(scope string) bool {
	_, ok := mcpSetServerEnabledScopeHandlers[scope]
	return ok
}

func (h *BrokerToolsHandler) setMCPServerEnabled(ctx context.Context, agentID string, svc *data.AgentService, input tools.MCPSetServerEnabledInput) (any, error) {
	if !isMCPSetServerEnabledScope(input.Scope) {
		return nil, fmt.Errorf("scope must be 'local' or 'host'")
	}
	handler, ok := mcpSetServerEnabledScopeHandlers[input.Scope]
	if !ok {
		return nil, fmt.Errorf("scope must be 'local' or 'host'")
	}
	if err := handler(h, ctx, agentID, svc, input); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

func (h *BrokerToolsHandler) setLocalMCPServerEnabled(ctx context.Context, svc *data.AgentService, input tools.MCPSetServerEnabledInput) error {
	if input.ServerID == "" {
		return fmt.Errorf("server_id is required")
	}
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		return err
	}
	servers, err := agent.GetMCPServers()
	if err != nil {
		return err
	}
	updated := false
	for i := range servers {
		if servers[i].ID == input.ServerID {
			servers[i].Enabled = input.Enabled
			updated = true
			break
		}
	}
	if !updated {
		return fmt.Errorf("local server %q not found", input.ServerID)
	}
	agent.SetMCPServers(servers)
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		return err
	}
	return nil
}

func (h *BrokerToolsHandler) setHostMCPServerEnabled(ctx context.Context, agentID string, input tools.MCPSetServerEnabledInput) error {
	if input.ServerID == "" {
		return fmt.Errorf("server_id is required")
	}
	server, err := data.GetMCPServer(ctx, h.db, input.ServerID)
	if err != nil || server == nil {
		return fmt.Errorf("host server %q not found", input.ServerID)
	}
	existing, err := data.GetAgentMCPServer(ctx, h.db, agentID, input.ServerID)
	if err != nil {
		return err
	}
	cfg := &data.AgentMCPServer{
		AgentID:  agentID,
		ServerID: input.ServerID,
		Enabled:  input.Enabled,
	}
	if existing != nil {
		cfg.Args = existing.Args
		cfg.Env = existing.Env
		cfg.WorkingDir = existing.WorkingDir
	}
	if err := data.UpsertAgentMCPServer(ctx, h.db, cfg); err != nil {
		return err
	}
	if !input.Enabled && h.mcpHost != nil {
		h.mcpHost.CloseClient(agentID, input.ServerID)
	}
	return nil
}
