package data

import "encoding/json"

// AgentConfigMCPServersKey stores per-agent MCP server configs in Agent.Config.
const AgentConfigMCPServersKey = "mcp_servers"

// MCPServerConfig describes an MCP server accessible to an agent.
// These are stored per-agent in Agent.Config.
type MCPServerConfig struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Enabled     bool              `json:"enabled"`
}

// DecodeMCPServers converts a loosely-typed value into a slice of MCPServerConfig.
func DecodeMCPServers(v any) ([]MCPServerConfig, error) {
	if v == nil {
		return nil, nil
	}
	if servers, ok := v.([]MCPServerConfig); ok {
		return servers, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var servers []MCPServerConfig
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

// GetMCPServers returns the MCP server configs stored on the agent.
func (a *Agent) GetMCPServers() ([]MCPServerConfig, error) {
	if a == nil || a.Config == nil {
		return nil, nil
	}
	return DecodeMCPServers(a.Config[AgentConfigMCPServersKey])
}

// SetMCPServers stores the MCP server configs on the agent.
func (a *Agent) SetMCPServers(servers []MCPServerConfig) {
	if a == nil {
		return
	}
	if len(servers) == 0 {
		if a.Config != nil {
			delete(a.Config, AgentConfigMCPServersKey)
			if len(a.Config) == 0 {
				a.Config = nil
			}
		}
		return
	}
	if a.Config == nil {
		a.Config = make(map[string]any)
	}
	a.Config[AgentConfigMCPServersKey] = servers
}
