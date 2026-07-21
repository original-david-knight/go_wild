package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// ListMCPServers returns all registered host-side MCP servers.
func ListMCPServers(ctx context.Context, db gowild_data.Database) ([]*MCPServer, error) {
	results, err := db.Table(MCPServer{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	servers := make([]*MCPServer, 0, len(results))
	for _, r := range results {
		if s, ok := r.(*MCPServer); ok {
			servers = append(servers, s)
		}
	}
	return servers, nil
}

// GetMCPServer retrieves a registry entry by ID.
func GetMCPServer(ctx context.Context, db gowild_data.Database, serverID string) (*MCPServer, error) {
	var server MCPServer
	if err := db.Table(MCPServer{}).Get(ctx, serverID, &server); err != nil {
		return nil, err
	}
	return &server, nil
}

// ListAgentMCPServers returns all per-agent MCP configs.
func ListAgentMCPServers(ctx context.Context, db gowild_data.Database, agentID string) ([]*AgentMCPServer, error) {
	results, err := db.Table(AgentMCPServer{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": agentID},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	configs := make([]*AgentMCPServer, 0, len(results))
	for _, r := range results {
		if c, ok := r.(*AgentMCPServer); ok {
			configs = append(configs, c)
		}
	}
	return configs, nil
}

// GetAgentMCPServer retrieves the per-agent config for a registry server.
func GetAgentMCPServer(ctx context.Context, db gowild_data.Database, agentID, serverID string) (*AgentMCPServer, error) {
	results, err := db.Table(AgentMCPServer{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": agentID, "server_id": serverID},
	})
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if c, ok := r.(*AgentMCPServer); ok {
			return c, nil
		}
	}
	return nil, nil
}

// UpsertAgentMCPServer creates or updates a per-agent MCP config.
func UpsertAgentMCPServer(ctx context.Context, db gowild_data.Database, config *AgentMCPServer) error {
	if config == nil {
		return nil
	}
	dao := db.Table(AgentMCPServer{})
	existing, err := GetAgentMCPServer(ctx, db, config.AgentID, config.ServerID)
	if err == nil && existing != nil {
		config.ID = existing.ID
		config.CreatedAt = existing.CreatedAt
		config.UpdatedAt = time.Now()
		return dao.Update(ctx, config)
	}
	if err != nil {
		return err
	}
	config.ID = newID()
	now := time.Now()
	config.CreatedAt = now
	config.UpdatedAt = now
	return dao.Insert(ctx, config)
}

// DeleteAgentMCPServer removes a per-agent config.
func DeleteAgentMCPServer(ctx context.Context, db gowild_data.Database, agentID, serverID string) error {
	config, err := GetAgentMCPServer(ctx, db, agentID, serverID)
	if err != nil {
		return err
	}
	if config == nil {
		return nil
	}
	return db.Table(AgentMCPServer{}).Delete(ctx, config.ID)
}
