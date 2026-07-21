package tools

// MCPListServersInput lists local and host-side MCP servers for the agent.
type MCPListServersInput struct{}

// MCPSetLocalServerInput creates or updates a local MCP server config.
type MCPSetLocalServerInput struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Enabled     bool              `json:"enabled"`
}

// MCPRemoveLocalServerInput removes a local MCP server config.
type MCPRemoveLocalServerInput struct {
	ID string `json:"id"`
}

// MCPSetServerEnabledInput enables or disables a server (local or host).
type MCPSetServerEnabledInput struct {
	Scope    string `json:"scope" enum:"local,host"`
	ServerID string `json:"server_id"`
	Enabled  bool   `json:"enabled"`
}
