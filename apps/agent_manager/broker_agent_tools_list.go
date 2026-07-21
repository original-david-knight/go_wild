package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/original-david-knight/go_wild/agent_data"
)

// agentToolEntry describes a tool available to an agent via the broker.
type agentToolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`

	// Route indicates how to call this tool:
	//   "broker" → POST /broker/v1/tools/{name}
	//   "mcp"    → POST /broker/v1/tools/mcp_call_tool with server_id + tool_name
	Route string `json:"route"`

	// MCP-routed tools carry the server and original tool name.
	MCPServerID string `json:"mcp_server_id,omitempty"`
	MCPToolName string `json:"mcp_tool_name,omitempty"`
}

// HandleListAgentTools returns all dynamically available tools for the authenticated agent.
// POST /broker/v1/mcp-tools/list
func (h *BrokerToolsHandler) HandleListAgentTools(w http.ResponseWriter, r *http.Request) {
	agentID := BrokerAgentID(r.Context())
	if agentID == "" {
		writeError(w, http.StatusUnauthorized, "no agent ID in context")
		return
	}

	tools := h.listAgentDynamicTools(r.Context(), agentID)
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

// listAgentDynamicTools collects deep research methods and host-side MCP server
// tools that are enabled for the given agent.
func (h *BrokerToolsHandler) listAgentDynamicTools(ctx context.Context, agentID string) []agentToolEntry {
	var tools []agentToolEntry

	// Deep research method tools (filtered by agent's enabled tools).
	tools = append(tools, h.listDeepResearchTools(ctx, agentID)...)

	// Host-side MCP server tools.
	tools = append(tools, h.listHostMCPServerTools(ctx, agentID)...)

	return tools
}

func (h *BrokerToolsHandler) listDeepResearchTools(ctx context.Context, agentID string) []agentToolEntry {
	if h.db == nil {
		return nil
	}

	specs, err := listDeepResearchMethodTools(ctx, h.db)
	if err != nil {
		log.Printf("broker: failed to list deep research tools: %v", err)
		return nil
	}

	// Load the agent's enabled tool set to filter deep research tools.
	var enabled map[string]bool
	if agentID != "" {
		svc := data.NewAgentService(h.db, agentID)
		enabled, err = agentEnabledToolSet(ctx, svc)
		if err != nil {
			log.Printf("broker: failed to load enabled tools for agent %s: %v", agentID, err)
			return nil
		}
	}

	tools := make([]agentToolEntry, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.ToolName)
		if name == "" {
			continue
		}

		if !deepResearchToolEnabled(enabled, spec) {
			continue
		}

		entry := agentToolEntry{
			Name:        name,
			Description: strings.TrimSpace(spec.Description),
			Route:       "broker",
		}

		// Build an input schema from the spec's InputSchema if available.
		if spec.InputSchema != nil {
			entry.InputSchema = spec.InputSchema
		}

		tools = append(tools, entry)
	}
	return tools
}

func (h *BrokerToolsHandler) listHostMCPServerTools(ctx context.Context, agentID string) []agentToolEntry {
	if h.db == nil || h.mcpHost == nil {
		return nil
	}

	agentCfgs, err := data.ListAgentMCPServers(ctx, h.db, agentID)
	if err != nil {
		log.Printf("broker: failed to list agent MCP servers for %s: %v", agentID, err)
		return nil
	}

	var tools []agentToolEntry

	for _, cfg := range agentCfgs {
		if !cfg.Enabled {
			continue
		}

		serverID := strings.TrimSpace(cfg.ServerID)
		if serverID == "" {
			continue
		}

		// Look up server metadata for naming.
		server, err := data.GetMCPServer(ctx, h.db, serverID)
		if err != nil {
			log.Printf("broker: failed to get MCP server %s: %v", serverID, err)
			continue
		}

		mcpTools, err := h.mcpHost.ListTools(ctx, agentID, serverID)
		if err != nil {
			log.Printf("broker: failed to list tools for MCP server %s (agent %s): %v", serverID, agentID, err)
			continue
		}

		serverPrefix := strings.TrimSpace(server.Name)
		if serverPrefix == "" {
			serverPrefix = serverID
		}
		// Normalize to snake_case prefix.
		serverPrefix = normalizeToolPrefix(serverPrefix)

		for _, t := range mcpTools {
			toolName := strings.TrimSpace(t.Name)
			if toolName == "" {
				continue
			}

			entry := agentToolEntry{
				Name:        serverPrefix + "__" + toolName,
				Description: strings.TrimSpace(t.Description),
				Route:       "mcp",
				MCPServerID: serverID,
				MCPToolName: toolName,
			}
			if t.InputSchema != nil {
				entry.InputSchema = t.InputSchema
			}

			tools = append(tools, entry)
		}
	}

	return tools
}

// normalizeToolPrefix converts a display name to a snake_case prefix.
func normalizeToolPrefix(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, name)

	// Collapse multiple underscores.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return strings.Trim(name, "_")
}
