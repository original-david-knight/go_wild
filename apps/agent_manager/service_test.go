package main

import (
	"context"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	kg "github.com/original-david-knight/go_wild/knowledge_graph"
)

func setupManagerTestDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("failed to initialize schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAgentServiceCRUD(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agent
	agent, err := svc.CreateAgent(ctx, "test-1")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if agent.ID != "test-1" {
		t.Errorf("expected ID test-1, got %s", agent.ID)
	}

	// Duplicate create should fail
	_, err = svc.CreateAgent(ctx, "test-1")
	if err == nil {
		t.Error("expected error creating duplicate agent")
	}

	// Get agent
	got, err := svc.GetAgent(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if got.Name != "Test-1" {
		t.Errorf("expected Name Test-1, got %s", got.Name)
	}

	// Get non-existent
	_, err = svc.GetAgent(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent agent")
	}

	// Update
	got.Description = "Updated"
	if err := svc.UpdateAgent(ctx, got); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	got, _ = svc.GetAgent(ctx, "test-1")
	if got.Description != "Updated" {
		t.Errorf("expected Updated, got %q", got.Description)
	}

	// List agents
	svc.CreateAgent(ctx, "test-2")
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
}

func TestListAutoStartAgents(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agents with different auto_start
	a1, _ := svc.CreateAgent(ctx, "agent-1")
	a1.AutoStart = true
	svc.UpdateAgent(ctx, a1)

	svc.CreateAgent(ctx, "agent-2") // auto_start = false by default

	a3, _ := svc.CreateAgent(ctx, "agent-3")
	a3.AutoStart = true
	svc.UpdateAgent(ctx, a3)

	autoStart, err := svc.ListAutoStartAgents(ctx)
	if err != nil {
		t.Fatalf("ListAutoStartAgents failed: %v", err)
	}
	if len(autoStart) != 2 {
		t.Errorf("expected 2 auto-start agents, got %d", len(autoStart))
	}
}

func TestAgentServiceRecurringTasks(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	// Add recurring task
	rt, err := svc.AddRecurringTask(ctx, "jake", "Check email", 60)
	if err != nil {
		t.Fatalf("AddRecurringTask failed: %v", err)
	}
	if rt.Description != "Check email" {
		t.Errorf("unexpected description: %q", rt.Description)
	}

	// List
	tasks, err := svc.GetRecurringTasks(ctx, "jake")
	if err != nil {
		t.Fatalf("GetRecurringTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 recurring task, got %d", len(tasks))
	}

	// Update
	updated, err := svc.UpdateRecurringTask(ctx, "jake", rt.ID, "Check email v2", 30)
	if err != nil {
		t.Fatalf("UpdateRecurringTask failed: %v", err)
	}
	if updated.Description != "Check email v2" {
		t.Errorf("expected updated description, got %q", updated.Description)
	}
	if updated.IntervalMinutes != 30 {
		t.Errorf("expected 30 minutes, got %d", updated.IntervalMinutes)
	}

	// Delete
	if err := svc.DeleteRecurringTask(ctx, "jake", rt.ID); err != nil {
		t.Fatalf("DeleteRecurringTask failed: %v", err)
	}

	tasks, _ = svc.GetRecurringTasks(ctx, "jake")
	if len(tasks) != 0 {
		t.Errorf("expected 0 recurring tasks after delete, got %d", len(tasks))
	}
}

func TestAgentServiceDataAccess(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agent and populate data
	svc.CreateAgent(ctx, "jake")
	agentSvc := data.NewAgentService(db, "jake")
	agentSvc.SaveMemory(ctx, "test memory")
	agentSvc.SaveSoul(ctx, "test soul")
	agentSvc.AddTask(ctx, "test task", "end")
	agentSvc.AddArchiveEntry(ctx, "summary", "tags", "content")

	// Memory
	mem, err := svc.GetMemory(ctx, "jake")
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}
	if mem.Content != "test memory" {
		t.Errorf("unexpected memory: %q", mem.Content)
	}

	// Soul
	soul, err := svc.GetSoul(ctx, "jake")
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}
	if soul.Content != "test soul" {
		t.Errorf("unexpected soul: %q", soul.Content)
	}

	// Tasks
	tasks, err := svc.GetPendingTasks(ctx, "jake")
	if err != nil {
		t.Fatalf("GetPendingTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	// Archive
	entries, err := svc.GetArchiveEntries(ctx, "jake")
	if err != nil {
		t.Fatalf("GetArchiveEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 archive entry, got %d", len(entries))
	}
}

func TestAgentServiceChatHistory(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")
	svc.SaveChatMessage(ctx, "jake", "user", "hello")
	svc.SaveChatMessage(ctx, "jake", "assistant", "hi")

	msgs, err := svc.GetChatHistory(ctx, "jake", 10)
	if err != nil {
		t.Fatalf("GetChatHistory failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestAgentServiceKnowledgeGraph(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	// No nodes initially
	nodes, err := svc.ListKGNodes(ctx, "jake", "")
	if err != nil {
		t.Fatalf("ListKGNodes failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}

	// No edges
	edges, err := svc.ListKGEdges(ctx, "jake")
	if err != nil {
		t.Fatalf("ListKGEdges failed: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}

	// Search empty
	results, err := svc.SearchKGNodes(ctx, "jake", "test")
	if err != nil {
		t.Fatalf("SearchKGNodes failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestAgentServicePeerGroups(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "alice")
	svc.CreateAgent(ctx, "bob")

	// No groups initially
	groups, err := svc.ListPeerGroups(ctx)
	if err != nil {
		t.Fatalf("ListPeerGroups failed: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}

	// Create group
	group, err := svc.CreatePeerGroup(ctx, "test-group")
	if err != nil {
		t.Fatalf("CreatePeerGroup failed: %v", err)
	}
	if group.Name != "test-group" {
		t.Errorf("expected name 'test-group', got %q", group.Name)
	}

	// List after create
	groups, err = svc.ListPeerGroups(ctx)
	if err != nil {
		t.Fatalf("ListPeerGroups failed: %v", err)
	}
	if len(groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(groups))
	}

	// Add agents to group
	if err := svc.AddAgentToGroup(ctx, group.ID, "alice"); err != nil {
		t.Fatalf("AddAgentToGroup(alice) failed: %v", err)
	}
	if err := svc.AddAgentToGroup(ctx, group.ID, "bob"); err != nil {
		t.Fatalf("AddAgentToGroup(bob) failed: %v", err)
	}

	// Get group members
	members, err := svc.GetGroupMembers(ctx, group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Get groups for agent
	agentGroups, err := svc.GetPeerGroupsForAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("GetPeerGroupsForAgent failed: %v", err)
	}
	if len(agentGroups) != 1 {
		t.Errorf("expected 1 group for alice, got %d", len(agentGroups))
	}

	// Remove agent from group
	if err := svc.RemoveAgentFromGroup(ctx, group.ID, "bob"); err != nil {
		t.Fatalf("RemoveAgentFromGroup failed: %v", err)
	}
	members, _ = svc.GetGroupMembers(ctx, group.ID)
	if len(members) != 1 {
		t.Errorf("expected 1 member after removal, got %d", len(members))
	}

	// Delete group
	if err := svc.DeletePeerGroup(ctx, group.ID); err != nil {
		t.Fatalf("DeletePeerGroup failed: %v", err)
	}
	groups, _ = svc.ListPeerGroups(ctx)
	if len(groups) != 0 {
		t.Errorf("expected 0 groups after delete, got %d", len(groups))
	}
}

func TestEnsureMessagingToolEnabled(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agent with explicit tool list (not nil)
	agent, _ := svc.CreateAgent(ctx, "alice")
	agent.SetEnabledTools([]string{"shell", "python", "file"})
	svc.UpdateAgent(ctx, agent)

	// Enable messaging
	if err := svc.EnsureMessagingToolEnabled(ctx, "alice"); err != nil {
		t.Fatalf("EnsureMessagingToolEnabled failed: %v", err)
	}

	// Verify messaging is now in the list
	agent, _ = svc.GetAgent(ctx, "alice")
	enabled := agent.EnabledTools()
	if !enabled["messaging"] {
		t.Error("expected messaging to be enabled")
	}

	// Calling again should be idempotent
	if err := svc.EnsureMessagingToolEnabled(ctx, "alice"); err != nil {
		t.Fatalf("EnsureMessagingToolEnabled (idempotent) failed: %v", err)
	}
	agent, _ = svc.GetAgent(ctx, "alice")
	// Count messaging entries
	count := 0
	for id := range agent.EnabledTools() {
		if id == "messaging" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 messaging entry, got %d", count)
	}
}

func TestEnsureMessagingToolEnabled_NilTools(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agent with nil tools (all enabled)
	svc.CreateAgent(ctx, "alice")

	// Should be no-op since nil means all enabled
	if err := svc.EnsureMessagingToolEnabled(ctx, "alice"); err != nil {
		t.Fatalf("EnsureMessagingToolEnabled (nil tools) failed: %v", err)
	}

	agent, _ := svc.GetAgent(ctx, "alice")
	if agent.EnabledTools() != nil {
		t.Error("expected tools to remain nil")
	}
}

func TestEnsureMessagingToolDisabled(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "alice")

	// Set up agent with messaging enabled
	agent, _ := svc.GetAgent(ctx, "alice")
	agent.SetEnabledTools([]string{"shell", "python", "messaging"})
	svc.UpdateAgent(ctx, agent)

	// No peer groups -> should disable messaging
	if err := svc.EnsureMessagingToolDisabled(ctx, "alice"); err != nil {
		t.Fatalf("EnsureMessagingToolDisabled failed: %v", err)
	}

	agent, _ = svc.GetAgent(ctx, "alice")
	enabled := agent.EnabledTools()
	if enabled["messaging"] {
		t.Error("expected messaging to be disabled when no peer groups")
	}
	if !enabled["shell"] || !enabled["python"] {
		t.Error("expected other tools to be preserved")
	}
}

func TestEnsureMessagingToolDisabled_OnlyMessagingLeavesExplicitEmptyList(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "alice")

	agent, _ := svc.GetAgent(ctx, "alice")
	agent.SetEnabledTools([]string{"messaging"})
	svc.UpdateAgent(ctx, agent)

	if err := svc.EnsureMessagingToolDisabled(ctx, "alice"); err != nil {
		t.Fatalf("EnsureMessagingToolDisabled failed: %v", err)
	}

	agent, _ = svc.GetAgent(ctx, "alice")
	if agent.EnabledToolsJSON != "[]" {
		t.Fatalf("expected explicit empty enabled_tools_json, got %q", agent.EnabledToolsJSON)
	}
	enabled := agent.EnabledTools()
	if enabled == nil {
		t.Fatal("expected explicit empty tool list, got nil/all-tools")
	}
	if len(enabled) != 0 {
		t.Fatalf("expected no enabled tools, got %v", enabled)
	}
}

func TestEnsureMessagingToolDisabled_KeepsWhenInGroup(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "alice")

	// Set up agent with messaging enabled and in a group
	agent, _ := svc.GetAgent(ctx, "alice")
	agent.SetEnabledTools([]string{"shell", "messaging"})
	svc.UpdateAgent(ctx, agent)

	group, _ := svc.CreatePeerGroup(ctx, "team")
	svc.AddAgentToGroup(ctx, group.ID, "alice")

	// Should keep messaging since alice is still in a group
	if err := svc.EnsureMessagingToolDisabled(ctx, "alice"); err != nil {
		t.Fatalf("EnsureMessagingToolDisabled failed: %v", err)
	}

	agent, _ = svc.GetAgent(ctx, "alice")
	if !agent.EnabledTools()["messaging"] {
		t.Error("expected messaging to remain enabled while in a group")
	}
}

func TestEnsureMessagingToolEnabled_InvalidEnabledToolsJSON(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	agent, err := svc.CreateAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	agent.EnabledToolsJSON = `{"not":"a-list"}`
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	if err := svc.EnsureMessagingToolEnabled(ctx, "alice"); err == nil {
		t.Fatal("expected error for invalid enabled_tools_json")
	}
}

func TestEnsureMessagingToolDisabled_InvalidEnabledToolsJSON(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	agent, err := svc.CreateAgent(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	agent.EnabledToolsJSON = `{"not":"a-list"}`
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	if err := svc.EnsureMessagingToolDisabled(ctx, "alice"); err == nil {
		t.Fatal("expected error for invalid enabled_tools_json")
	}
}

func TestGetKGNodeWithNeighbors(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "jake")

	// Create nodes and edges via KG service
	kgSvc := kg.NewService(db, "jake")
	node1, err := kgSvc.CreateNode(ctx, "Alice", "person", "", map[string]any{"age": 30})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}
	node2, err := kgSvc.CreateNode(ctx, "Bob", "person", "", map[string]any{"age": 25})
	if err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}
	_, err = kgSvc.CreateEdge(ctx, node1.ID, node2.ID, "knows", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge failed: %v", err)
	}

	// Get node with neighbors
	node, neighbors, err := svc.GetKGNodeWithNeighbors(ctx, "jake", node1.ID)
	if err != nil {
		t.Fatalf("GetKGNodeWithNeighbors failed: %v", err)
	}
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	if node.Name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", node.Name)
	}
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
	}
	if neighbors[0].Node.Name != "Bob" {
		t.Errorf("expected neighbor 'Bob', got %q", neighbors[0].Node.Name)
	}
	if neighbors[0].Direction != "outgoing" {
		t.Errorf("expected direction 'outgoing', got %q", neighbors[0].Direction)
	}
}
