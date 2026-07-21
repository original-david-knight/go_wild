package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	kg "github.com/original-david-knight/go_wild/knowledge_graph"
	"github.com/original-david-knight/go_wild/tools"
)

// AgentService handles queries against the agent database.
type AgentService struct {
	db gowild_data.Database
}

// NewAgentService creates a new agent service.
func NewAgentService(db gowild_data.Database) *AgentService {
	return &AgentService{db: db}
}

// ListAgents returns all agents from the database.
func (s *AgentService) ListAgents(ctx context.Context) ([]*data.Agent, error) {
	results, err := s.db.Table(data.Agent{}).GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	agents := make([]*data.Agent, 0, len(results))
	for _, r := range results {
		if a, ok := r.(*data.Agent); ok {
			agents = append(agents, a)
		}
	}
	return agents, nil
}

// CreateAgent creates a new agent with the given ID.
// Returns an error if the agent already exists.
func (s *AgentService) CreateAgent(ctx context.Context, id string) (*data.Agent, error) {
	// Check if agent already exists
	var existing data.Agent
	if err := s.db.Table(data.Agent{}).Get(ctx, id, &existing); err == nil {
		return nil, fmt.Errorf("agent %q already exists", id)
	}

	agentSvc := data.NewAgentService(s.db, id)
	return agentSvc.EnsureAgent(ctx)
}

// CloneAgent creates a copy of an existing agent with a new ID.
// It copies configuration fields and capabilities but skips sensitive fields
// (wallet seed phrase, telegram token, agentmail config, report data).
// If the source agent belongs to a company, the clone is added to the same company with the same role.
func (s *AgentService) CloneAgent(ctx context.Context, sourceID, newID string) (*data.Agent, error) {
	// Load source agent
	source, err := s.GetAgent(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("source agent not found: %w", err)
	}

	// Ensure target doesn't already exist
	var existing data.Agent
	if err := s.db.Table(data.Agent{}).Get(ctx, newID, &existing); err == nil {
		return nil, fmt.Errorf("agent %q already exists", newID)
	}

	// Create new agent (gets its own seed phrase)
	agentSvc := data.NewAgentService(s.db, newID)
	clone, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create clone: %w", err)
	}

	// Copy config fields from source
	clone.ModelProvider = source.ModelProvider
	clone.OpenAIAuthMode = source.OpenAIAuthMode
	clone.Model = source.Model
	clone.SmartModel = source.SmartModel
	clone.SmartDefault = source.SmartDefault
	clone.MaxTurns = source.MaxTurns
	clone.Heartbeat = source.Heartbeat
	clone.WorkTasksTimeout = source.WorkTasksTimeout
	clone.ExtraFlags = source.ExtraFlags
	clone.EnabledToolsJSON = source.EnabledToolsJSON
	clone.EnvVarsJSON = source.EnvVarsJSON
	clone.MemoryLimit = source.MemoryLimit
	clone.CPULimit = source.CPULimit
	clone.AutoStart = source.AutoStart
	clone.SystemPrompt = source.SystemPrompt
	clone.Mode = source.Mode
	clone.WorkerContextMode = source.WorkerContextMode
	clone.UpdatedAt = time.Now()

	if err := s.db.Table(data.Agent{}).Update(ctx, clone); err != nil {
		return nil, fmt.Errorf("failed to update clone config: %w", err)
	}

	// Copy capabilities
	srcSvc := data.NewAgentService(s.db, sourceID)
	caps, err := srcSvc.GetCapabilities(ctx)
	if err == nil {
		cloneSvc := data.NewAgentService(s.db, newID)
		for _, cap := range caps {
			_ = cloneSvc.RegisterCapability(ctx, cap.Role, cap.Method)
		}
	}

	// Copy company membership
	member, err := data.GetCompanyMemberForAgent(ctx, s.db, sourceID)
	if err == nil && member != nil {
		_ = data.AddAgentToCompany(ctx, s.db, member.CompanyID, newID, member.Role)
	}

	return clone, nil
}

// GetAgent returns an agent from the database.
func (s *AgentService) GetAgent(ctx context.Context, id string) (*data.Agent, error) {
	var agent data.Agent
	if err := s.db.Table(data.Agent{}).Get(ctx, id, &agent); err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	return &agent, nil
}

// UpdateAgent updates manager-specific fields on an agent.
func (s *AgentService) UpdateAgent(ctx context.Context, agent *data.Agent) error {
	if err := s.db.Table(data.Agent{}).Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}
	return nil
}

// ListAutoStartAgents returns agents with auto_start enabled.
func (s *AgentService) ListAutoStartAgents(ctx context.Context) ([]*data.Agent, error) {
	results, err := s.db.Table(data.Agent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"auto_start": true},
	})
	if err != nil {
		return nil, err
	}
	agents := make([]*data.Agent, 0, len(results))
	for _, r := range results {
		if a, ok := r.(*data.Agent); ok {
			agents = append(agents, a)
		}
	}
	return agents, nil
}

// GetRecurringTasks returns recurring tasks for a specific agent.
func (s *AgentService) GetRecurringTasks(ctx context.Context, agentID string) ([]*data.RecurringTask, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetRecurringTasks(ctx)
}

// AddRecurringTask creates a new recurring task for an agent.
func (s *AgentService) AddRecurringTask(ctx context.Context, agentID, description string, intervalMinutes int) (*data.RecurringTask, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.AddRecurringTask(ctx, description, intervalMinutes)
}

// UpdateRecurringTask updates a recurring task.
func (s *AgentService) UpdateRecurringTask(ctx context.Context, agentID, taskID, description string, intervalMinutes int) (*data.RecurringTask, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	task, err := agentSvc.GetRecurringTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	task.Description = description
	task.IntervalMinutes = intervalMinutes
	if err := s.db.Table(data.RecurringTask{}).Update(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to update recurring task: %w", err)
	}
	return task, nil
}

// DeleteRecurringTask deletes a recurring task.
func (s *AgentService) DeleteRecurringTask(ctx context.Context, agentID, taskID string) error {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.DeleteRecurringTask(ctx, taskID)
}

// GetPendingTasks returns all pending tasks for an agent.
func (s *AgentService) GetPendingTasks(ctx context.Context, agentID string) ([]*data.Task, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetPendingTasks(ctx)
}

// GetMemory returns the short-term memory for an agent.
func (s *AgentService) GetMemory(ctx context.Context, agentID string) (*data.MemoryEntry, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetMemory(ctx)
}

// GetArchiveEntries returns all archive entries for an agent.
func (s *AgentService) GetArchiveEntries(ctx context.Context, agentID string) ([]*data.ArchiveEntry, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetArchiveEntries(ctx, 100) // Return up to 100 entries
}

// GetReportHTML returns the report HTML for an agent.
func (s *AgentService) GetReportHTML(ctx context.Context, agentID string) (string, time.Time, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetReportHTML(ctx)
}

// GetSoul returns the soul for an agent.
func (s *AgentService) GetSoul(ctx context.Context, agentID string) (*data.Soul, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetSoul(ctx)
}

// Knowledge Graph methods

// ListKGNodes returns all nodes for an agent, optionally filtered by type.
func (s *AgentService) ListKGNodes(ctx context.Context, agentID, nodeType string) ([]kg.Node, error) {
	kgSvc := kg.NewService(s.db, agentID)
	return kgSvc.ListNodes(ctx, nodeType)
}

// ListKGEdges returns all edges for an agent.
func (s *AgentService) ListKGEdges(ctx context.Context, agentID string) ([]kg.Edge, error) {
	// Query edges directly since KG service doesn't have ListEdges
	results, err := s.db.ForUser(agentID).Table(kg.Edge{}).Query(ctx, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}
	edges := make([]kg.Edge, 0, len(results))
	for _, r := range results {
		if e, ok := r.(*kg.Edge); ok {
			edges = append(edges, *e)
		}
	}
	return edges, nil
}

// NeighborInfo represents a neighbor node with its connecting edge.
type NeighborInfo struct {
	Node      kg.Node
	Edge      kg.Edge
	Direction string // "outgoing" or "incoming"
}

// GetKGNodeWithNeighbors returns a node and its neighbors.
func (s *AgentService) GetKGNodeWithNeighbors(ctx context.Context, agentID, nodeID string) (*kg.Node, []NeighborInfo, error) {
	kgSvc := kg.NewService(s.db, agentID)

	node, err := kgSvc.GetNode(ctx, nodeID)
	if err != nil {
		return nil, nil, err
	}

	// Get neighbors using the traverse method with depth 1
	result, err := kgSvc.GetNeighbors(ctx, nodeID, kg.TraversalOptions{IncludeReverse: true})
	if err != nil {
		return node, nil, nil // Return node even if neighbors fail
	}

	neighbors := make([]NeighborInfo, 0)
	nodeMap := make(map[string]kg.Node)
	for _, n := range result.Nodes {
		if n.ID != nodeID {
			nodeMap[n.ID] = n
		}
	}

	for _, e := range result.Edges {
		var neighborNode kg.Node
		var direction string
		var found bool

		if e.SourceNodeID == nodeID {
			neighborNode, found = nodeMap[e.TargetNodeID]
			direction = "outgoing"
		} else if e.TargetNodeID == nodeID {
			neighborNode, found = nodeMap[e.SourceNodeID]
			direction = "incoming"
		}

		if found {
			neighbors = append(neighbors, NeighborInfo{
				Node:      neighborNode,
				Edge:      e,
				Direction: direction,
			})
		}
	}

	return node, neighbors, nil
}

// SaveChatMessage saves a chat message for an agent.
func (s *AgentService) SaveChatMessage(ctx context.Context, agentID, role, content string) error {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.SaveChatMessage(ctx, role, content)
}

// GetChatHistory returns recent chat messages for an agent.
func (s *AgentService) GetChatHistory(ctx context.Context, agentID string, limit int) ([]*data.ChatMessage, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetChatHistory(ctx, limit)
}

// MCP registry methods

func (s *AgentService) ListMCPServers(ctx context.Context) ([]*data.MCPServer, error) {
	return data.ListMCPServers(ctx, s.db)
}

func (s *AgentService) GetMCPServer(ctx context.Context, serverID string) (*data.MCPServer, error) {
	return data.GetMCPServer(ctx, s.db, serverID)
}

func (s *AgentService) CreateMCPServer(ctx context.Context, server *data.MCPServer) error {
	if server == nil {
		return fmt.Errorf("server is nil")
	}
	return s.db.Table(data.MCPServer{}).Insert(ctx, server)
}

func (s *AgentService) UpdateMCPServer(ctx context.Context, server *data.MCPServer) error {
	if server == nil {
		return fmt.Errorf("server is nil")
	}
	return s.db.Table(data.MCPServer{}).Update(ctx, server)
}

func (s *AgentService) DeleteMCPServer(ctx context.Context, serverID string) error {
	// Remove per-agent configs first.
	configDAO := s.db.Table(data.AgentMCPServer{})
	configs, err := configDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"server_id": serverID},
	})
	if err != nil {
		return err
	}
	for _, r := range configs {
		if cfg, ok := r.(*data.AgentMCPServer); ok {
			if err := configDAO.Delete(ctx, cfg.ID); err != nil {
				return err
			}
		}
	}
	return s.db.Table(data.MCPServer{}).Delete(ctx, serverID)
}

func (s *AgentService) ListAgentMCPServers(ctx context.Context, agentID string) ([]*data.AgentMCPServer, error) {
	return data.ListAgentMCPServers(ctx, s.db, agentID)
}

func (s *AgentService) GetAgentMCPServer(ctx context.Context, agentID, serverID string) (*data.AgentMCPServer, error) {
	return data.GetAgentMCPServer(ctx, s.db, agentID, serverID)
}

func (s *AgentService) UpsertAgentMCPServer(ctx context.Context, cfg *data.AgentMCPServer) error {
	return data.UpsertAgentMCPServer(ctx, s.db, cfg)
}

func (s *AgentService) DeleteAgentMCPServer(ctx context.Context, agentID, serverID string) error {
	return data.DeleteAgentMCPServer(ctx, s.db, agentID, serverID)
}

// SearchKGNodes searches nodes by name.
func (s *AgentService) SearchKGNodes(ctx context.Context, agentID, query string) ([]kg.Node, error) {
	kgSvc := kg.NewService(s.db, agentID)
	return kgSvc.SearchNodes(ctx, query)
}

// Peer Group methods

func (s *AgentService) ListPeerGroups(ctx context.Context) ([]data.PeerGroup, error) {
	return data.ListPeerGroups(ctx, s.db)
}

func (s *AgentService) CreatePeerGroup(ctx context.Context, name string) (*data.PeerGroup, error) {
	return data.CreatePeerGroup(ctx, s.db, name)
}

func (s *AgentService) DeletePeerGroup(ctx context.Context, groupID string) error {
	return data.DeletePeerGroup(ctx, s.db, groupID)
}

func (s *AgentService) AddAgentToGroup(ctx context.Context, groupID, agentID string) error {
	return data.AddAgentToGroup(ctx, s.db, groupID, agentID)
}

func (s *AgentService) RemoveAgentFromGroup(ctx context.Context, groupID, agentID string) error {
	return data.RemoveAgentFromGroup(ctx, s.db, groupID, agentID)
}

func (s *AgentService) GetGroupMembers(ctx context.Context, groupID string) ([]data.PeerGroupMember, error) {
	return data.GetGroupMembers(ctx, s.db, groupID)
}

func (s *AgentService) GetPeerGroupsForAgent(ctx context.Context, agentID string) ([]data.PeerGroup, error) {
	return data.GetPeerGroupsForAgent(ctx, s.db, agentID)
}

// Company methods

func (s *AgentService) ListCompanies(ctx context.Context) ([]data.Company, error) {
	return data.ListCompanies(ctx, s.db)
}

func (s *AgentService) CreateCompany(ctx context.Context, name, description, ceoAgentID string) (*data.Company, error) {
	return data.CreateCompany(ctx, s.db, name, description, ceoAgentID)
}

func (s *AgentService) GetCompany(ctx context.Context, companyID string) (*data.Company, error) {
	return data.GetCompany(ctx, s.db, companyID)
}

func (s *AgentService) UpdateCompany(ctx context.Context, company *data.Company) error {
	return data.UpdateCompany(ctx, s.db, company)
}

func (s *AgentService) DeleteCompany(ctx context.Context, companyID string) error {
	return data.DeleteCompany(ctx, s.db, companyID)
}

func (s *AgentService) AddAgentToCompany(ctx context.Context, companyID, agentID, role string) error {
	return data.AddAgentToCompany(ctx, s.db, companyID, agentID, role)
}

func (s *AgentService) RemoveAgentFromCompany(ctx context.Context, companyID, agentID string) error {
	return data.RemoveAgentFromCompany(ctx, s.db, companyID, agentID)
}

func (s *AgentService) ListCompanyMembers(ctx context.Context, companyID string) ([]data.CompanyMember, error) {
	return data.ListCompanyMembers(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyForAgent(ctx context.Context, agentID string) (*data.Company, error) {
	return data.GetCompanyForAgent(ctx, s.db, agentID)
}

func (s *AgentService) GetCompanyMemberForAgent(ctx context.Context, agentID string) (*data.CompanyMember, error) {
	return data.GetCompanyMemberForAgent(ctx, s.db, agentID)
}

func (s *AgentService) SetCompanyCEO(ctx context.Context, companyID, agentID string) error {
	return data.SetCompanyCEO(ctx, s.db, companyID, agentID)
}

func (s *AgentService) EnsureCompanyWalletSeedPhrase(ctx context.Context, companyID string) (string, error) {
	return data.EnsureCompanyWalletSeedPhrase(ctx, s.db, companyID)
}

func (s *AgentService) EnsureCompanyWebhookIngressKey(ctx context.Context, companyID string) (string, error) {
	return data.EnsureCompanyWebhookIngressKey(ctx, s.db, companyID)
}

func (s *AgentService) RotateCompanyWebhookIngressKey(ctx context.Context, companyID string) (string, error) {
	return data.RotateCompanyWebhookIngressKey(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyByWebhookIngressKey(ctx context.Context, key string) (*data.Company, error) {
	return data.GetCompanyByWebhookIngressKey(ctx, s.db, key)
}

func (s *AgentService) GetCompanyShopifyConnection(ctx context.Context, companyID string) (*data.CompanyShopifyConnection, error) {
	return data.GetCompanyShopifyConnection(ctx, s.db, companyID)
}

func (s *AgentService) UpsertCompanyShopifyConnection(ctx context.Context, conn *data.CompanyShopifyConnection) error {
	return data.UpsertCompanyShopifyConnection(ctx, s.db, conn)
}

func (s *AgentService) DeleteCompanyShopifyConnection(ctx context.Context, companyID string) error {
	return data.DeleteCompanyShopifyConnection(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyPolymarketConnection(ctx context.Context, companyID string) (*data.CompanyPolymarketConnection, error) {
	return data.GetCompanyPolymarketConnection(ctx, s.db, companyID)
}

func (s *AgentService) UpsertCompanyPolymarketConnection(ctx context.Context, conn *data.CompanyPolymarketConnection) error {
	return data.UpsertCompanyPolymarketConnection(ctx, s.db, conn)
}

func (s *AgentService) DeleteCompanyPolymarketConnection(ctx context.Context, companyID string) error {
	return data.DeleteCompanyPolymarketConnection(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyTopDawgConnection(ctx context.Context, companyID string) (*data.CompanyTopDawgConnection, error) {
	return data.GetCompanyTopDawgConnection(ctx, s.db, companyID)
}

func (s *AgentService) UpsertCompanyTopDawgConnection(ctx context.Context, conn *data.CompanyTopDawgConnection) error {
	return data.UpsertCompanyTopDawgConnection(ctx, s.db, conn)
}

func (s *AgentService) DeleteCompanyTopDawgConnection(ctx context.Context, companyID string) error {
	return data.DeleteCompanyTopDawgConnection(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyCJDropshippingConnection(ctx context.Context, companyID string) (*data.CompanyCJDropshippingConnection, error) {
	return data.GetCompanyCJDropshippingConnection(ctx, s.db, companyID)
}

func (s *AgentService) UpsertCompanyCJDropshippingConnection(ctx context.Context, conn *data.CompanyCJDropshippingConnection) error {
	return data.UpsertCompanyCJDropshippingConnection(ctx, s.db, conn)
}

func (s *AgentService) DeleteCompanyCJDropshippingConnection(ctx context.Context, companyID string) error {
	return data.DeleteCompanyCJDropshippingConnection(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyAmazonConnection(ctx context.Context, companyID string) (*data.CompanyAmazonConnection, error) {
	return data.GetCompanyAmazonConnection(ctx, s.db, companyID)
}

func (s *AgentService) UpsertCompanyAmazonConnection(ctx context.Context, conn *data.CompanyAmazonConnection) error {
	return data.UpsertCompanyAmazonConnection(ctx, s.db, conn)
}

func (s *AgentService) DeleteCompanyAmazonConnection(ctx context.Context, companyID string) error {
	return data.DeleteCompanyAmazonConnection(ctx, s.db, companyID)
}

func (s *AgentService) ListCompanyWebhookConfigs(ctx context.Context, companyID string) ([]data.WebhookConfig, error) {
	return data.ListCompanyWebhookConfigs(ctx, s.db, companyID)
}

func (s *AgentService) GetCompanyWebhookConfigByPath(ctx context.Context, companyID, source, eventPath string) (*data.WebhookConfig, error) {
	return data.GetCompanyWebhookConfigByPath(ctx, s.db, companyID, source, eventPath)
}

func (s *AgentService) UpsertCompanyWebhookConfig(ctx context.Context, cfg *data.WebhookConfig) error {
	return data.UpsertCompanyWebhookConfig(ctx, s.db, cfg)
}

func (s *AgentService) AddCompanyKnowledgeEntry(ctx context.Context, companyID, createdByAgentID, kind, title, content string, tags []string, metadata map[string]any) (*data.CompanyKnowledgeEntry, error) {
	return data.AddCompanyKnowledgeEntry(ctx, s.db, companyID, createdByAgentID, kind, title, content, tags, metadata)
}

func (s *AgentService) ListCompanyKnowledgeEntries(ctx context.Context, companyID, query, kind string, limit int) ([]data.CompanyKnowledgeEntry, error) {
	return data.ListCompanyKnowledgeEntries(ctx, s.db, companyID, query, kind, limit)
}

func (s *AgentService) GetCompanyKnowledgeEntry(ctx context.Context, companyID, entryID string) (*data.CompanyKnowledgeEntry, error) {
	return data.GetCompanyKnowledgeEntry(ctx, s.db, companyID, entryID)
}

func (s *AgentService) UpdateCompanyKnowledgeEntry(ctx context.Context, companyID, entryID, kind, title, content string, tags []string, metadata map[string]any) (*data.CompanyKnowledgeEntry, error) {
	return data.UpdateCompanyKnowledgeEntry(ctx, s.db, companyID, entryID, kind, title, content, tags, metadata)
}

func (s *AgentService) DeleteCompanyKnowledgeEntry(ctx context.Context, companyID, entryID string) error {
	return data.DeleteCompanyKnowledgeEntry(ctx, s.db, companyID, entryID)
}

func parseEnabledToolIDs(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("invalid enabled_tools_json: %w", err)
	}
	return ids, nil
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// EnsureMessagingToolEnabled adds "messaging" to the agent's enabled tools list
// if the agent has an explicit tools list and messaging is not already in it.
func (s *AgentService) EnsureMessagingToolEnabled(ctx context.Context, agentID string) error {
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return err
	}

	if agent.EnabledToolsJSON == "" {
		// nil means all tools enabled — nothing to do
		return nil
	}

	ids, err := parseEnabledToolIDs(agent.EnabledToolsJSON)
	if err != nil {
		return fmt.Errorf("parse enabled tools for agent %q: %w", agentID, err)
	}
	if containsID(ids, "messaging") {
		return nil
	}

	// Add "messaging" to the list
	ids = append(ids, "messaging")
	agent.SetEnabledTools(ids)
	return s.UpdateAgent(ctx, agent)
}

// EnsureMessagingToolDisabled removes "messaging" from the agent's enabled tools list
// if the agent has no remaining peer groups.
func (s *AgentService) EnsureMessagingToolDisabled(ctx context.Context, agentID string) error {
	groups, err := s.GetPeerGroupsForAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if len(groups) > 0 {
		// Still in other groups — keep messaging enabled
		return nil
	}
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if agent.EnabledToolsJSON == "" {
		return nil
	}

	ids, err := parseEnabledToolIDs(agent.EnabledToolsJSON)
	if err != nil {
		return fmt.Errorf("parse enabled tools for agent %q: %w", agentID, err)
	}
	if !containsID(ids, "messaging") {
		return nil
	}

	// Remove "messaging" from the list
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "messaging" {
			filtered = append(filtered, id)
		}
	}
	agent.SetEnabledTools(filtered)
	return s.UpdateAgent(ctx, agent)
}

// Email Approval methods

// GetPendingEmails returns all pending emails for an agent.
func (s *AgentService) GetPendingEmails(ctx context.Context, agentID string) ([]*data.PendingEmail, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetPendingEmails(ctx)
}

// ApprovePendingEmail sends a pending email and marks it as approved.
func (s *AgentService) ApprovePendingEmail(ctx context.Context, agentID, emailID string) (*data.PendingEmail, error) {
	agentSvc := data.NewAgentService(s.db, agentID)

	pe, err := agentSvc.GetPendingEmailByID(ctx, emailID)
	if err != nil {
		return nil, fmt.Errorf("email not found: %w", err)
	}
	if pe.Status != "pending" {
		return nil, fmt.Errorf("email is already %s", pe.Status)
	}

	// Create EmailTools from agent config to actually send the email
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	if agent.AgentMailAPIKey == "" || agent.AgentMailInboxID == "" {
		return nil, fmt.Errorf("agent has no email configuration")
	}

	emailTools := tools.NewEmailTools(agent.AgentMailAPIKey, agent.AgentMailInboxID)

	var input tools.SendEmailInput
	if err := json.Unmarshal([]byte(pe.RequestData), &input); err != nil {
		return nil, fmt.Errorf("failed to deserialize request: %w", err)
	}

	result, err := emailTools.SendEmailTool(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to send email: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("email API error: %s", result.Error)
	}

	if err := agentSvc.UpdatePendingEmailStatus(ctx, emailID, "approved"); err != nil {
		return nil, fmt.Errorf("email sent but failed to update status: %w", err)
	}

	return pe, nil
}

// RejectPendingEmail marks a pending email as rejected.
func (s *AgentService) RejectPendingEmail(ctx context.Context, agentID, emailID string) (*data.PendingEmail, error) {
	agentSvc := data.NewAgentService(s.db, agentID)

	pe, err := agentSvc.GetPendingEmailByID(ctx, emailID)
	if err != nil {
		return nil, fmt.Errorf("email not found: %w", err)
	}
	if pe.Status != "pending" {
		return nil, fmt.Errorf("email is already %s", pe.Status)
	}

	if err := agentSvc.UpdatePendingEmailStatus(ctx, emailID, "rejected"); err != nil {
		return nil, err
	}
	return pe, nil
}

// GetEmailWhitelist returns the email whitelist for an agent.
func (s *AgentService) GetEmailWhitelist(ctx context.Context, agentID string) ([]string, error) {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.GetEmailWhitelist(ctx)
}

// AddEmailWhitelistEntry adds an email to an agent's whitelist.
func (s *AgentService) AddEmailWhitelistEntry(ctx context.Context, agentID, email string) error {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.AddEmailWhitelistEntry(ctx, email)
}

// RemoveEmailWhitelistEntry removes an email from an agent's whitelist.
func (s *AgentService) RemoveEmailWhitelistEntry(ctx context.Context, agentID, email string) error {
	agentSvc := data.NewAgentService(s.db, agentID)
	return agentSvc.RemoveEmailWhitelistEntry(ctx, email)
}
