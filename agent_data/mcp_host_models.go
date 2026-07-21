package data

import "time"

// MCPServer is a registry entry for a host-side MCP server.
// Per-agent configuration is stored in AgentMCPServer.
type MCPServer struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	DefaultEnv  map[string]string `json:"default_env,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// AgentMCPServer stores per-agent configuration for a host-side MCP server.
type AgentMCPServer struct {
	ID         string            `json:"id"`
	AgentID    string            `json:"agent_id"`
	ServerID   string            `json:"server_id"`
	Enabled    bool              `json:"enabled"`
	Args       []string          `json:"args,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}
