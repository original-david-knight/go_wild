package main

import (
	"context"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestHashMCPConfig_Deterministic(t *testing.T) {
	cfg := mcpResolvedConfig{
		command: "cmd",
		args:    []string{"a", "b"},
		env:     map[string]string{"X": "1", "Y": "2"},
	}
	h1 := hashMCPConfig(cfg)
	h2 := hashMCPConfig(cfg)
	if h1 != h2 {
		t.Fatalf("expected deterministic hash, got %q and %q", h1, h2)
	}
}

func TestHashMCPConfig_DifferentEnvOrder(t *testing.T) {
	// Build two configs with same env vars added in different order.
	// hashMCPConfig sorts keys, so these should produce the same hash.
	env1 := map[string]string{"A": "1", "B": "2", "C": "3"}
	env2 := map[string]string{"C": "3", "A": "1", "B": "2"}

	cfg1 := mcpResolvedConfig{command: "cmd", args: []string{"x"}, env: env1}
	cfg2 := mcpResolvedConfig{command: "cmd", args: []string{"x"}, env: env2}

	h1 := hashMCPConfig(cfg1)
	h2 := hashMCPConfig(cfg2)
	if h1 != h2 {
		t.Fatalf("expected same hash for same env vars in different order, got %q and %q", h1, h2)
	}
}

func TestHashMCPConfig_DifferentConfig(t *testing.T) {
	cfg1 := mcpResolvedConfig{command: "cmd1", args: []string{"a"}, env: map[string]string{"X": "1"}}
	cfg2 := mcpResolvedConfig{command: "cmd2", args: []string{"a"}, env: map[string]string{"X": "1"}}

	h1 := hashMCPConfig(cfg1)
	h2 := hashMCPConfig(cfg2)
	if h1 == h2 {
		t.Fatalf("expected different hash for different commands, both got %q", h1)
	}
}

func TestHashMCPConfig_DifferentArgs(t *testing.T) {
	cfg1 := mcpResolvedConfig{command: "cmd", args: []string{"a", "b"}}
	cfg2 := mcpResolvedConfig{command: "cmd", args: []string{"a", "c"}}

	h1 := hashMCPConfig(cfg1)
	h2 := hashMCPConfig(cfg2)
	if h1 == h2 {
		t.Fatalf("expected different hash for different args, both got %q", h1)
	}
}

func TestHashMCPConfig_EmptyEnv(t *testing.T) {
	cfg := mcpResolvedConfig{command: "cmd", args: []string{"a"}}
	h := hashMCPConfig(cfg)
	if h == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestResolveConfig_AgentOverridesServerDefaults(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	now := time.Now()
	server := &data.MCPServer{
		ID:         "srv-1",
		Name:       "Test Server",
		Command:    "/usr/bin/mcp-test",
		Args:       []string{"--default-flag"},
		WorkingDir: "/default/dir",
		DefaultEnv: map[string]string{"DEFAULT_KEY": "default_val", "SHARED": "from_server"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Table(data.MCPServer{}).Insert(ctx, server); err != nil {
		t.Fatalf("insert MCP server failed: %v", err)
	}

	agentCfg := &data.AgentMCPServer{
		AgentID:    "agent-1",
		ServerID:   "srv-1",
		Enabled:    true,
		Args:       []string{"--agent-flag"},
		WorkingDir: "/agent/dir",
		Env:        map[string]string{"AGENT_KEY": "agent_val", "SHARED": "from_agent"},
	}
	if err := data.UpsertAgentMCPServer(ctx, db, agentCfg); err != nil {
		t.Fatalf("upsert agent MCP config failed: %v", err)
	}

	m := NewMCPHostManager(db)
	resolved, enabled, err := m.resolveConfig(ctx, "agent-1", "srv-1")
	if err != nil {
		t.Fatalf("resolveConfig failed: %v", err)
	}
	if !enabled {
		t.Fatal("expected enabled=true")
	}

	// Command always comes from server
	if resolved.command != "/usr/bin/mcp-test" {
		t.Errorf("expected command '/usr/bin/mcp-test', got %q", resolved.command)
	}

	// Args should be overridden by agent config
	if len(resolved.args) != 1 || resolved.args[0] != "--agent-flag" {
		t.Errorf("expected args ['--agent-flag'], got %v", resolved.args)
	}

	// WorkingDir should be overridden by agent config
	if resolved.workingDir != "/agent/dir" {
		t.Errorf("expected workingDir '/agent/dir', got %q", resolved.workingDir)
	}

	// Env: server defaults merged with agent overrides
	if resolved.env["DEFAULT_KEY"] != "default_val" {
		t.Errorf("expected DEFAULT_KEY='default_val', got %q", resolved.env["DEFAULT_KEY"])
	}
	if resolved.env["AGENT_KEY"] != "agent_val" {
		t.Errorf("expected AGENT_KEY='agent_val', got %q", resolved.env["AGENT_KEY"])
	}
	// Agent should override shared key
	if resolved.env["SHARED"] != "from_agent" {
		t.Errorf("expected SHARED='from_agent', got %q", resolved.env["SHARED"])
	}
	// Auto-injected env vars
	if resolved.env["GOWILD_AGENT_ID"] != "agent-1" {
		t.Errorf("expected GOWILD_AGENT_ID='agent-1', got %q", resolved.env["GOWILD_AGENT_ID"])
	}
	if resolved.env["MCP_AGENT_ID"] != "agent-1" {
		t.Errorf("expected MCP_AGENT_ID='agent-1', got %q", resolved.env["MCP_AGENT_ID"])
	}
}

func TestResolveConfig_DisabledServer(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	now := time.Now()
	server := &data.MCPServer{
		ID:        "srv-disabled",
		Name:      "Disabled Server",
		Command:   "/usr/bin/mcp-test",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Table(data.MCPServer{}).Insert(ctx, server); err != nil {
		t.Fatalf("insert MCP server failed: %v", err)
	}

	agentCfg := &data.AgentMCPServer{
		AgentID:  "agent-1",
		ServerID: "srv-disabled",
		Enabled:  false,
	}
	if err := data.UpsertAgentMCPServer(ctx, db, agentCfg); err != nil {
		t.Fatalf("upsert agent MCP config failed: %v", err)
	}

	m := NewMCPHostManager(db)
	_, enabled, err := m.resolveConfig(ctx, "agent-1", "srv-disabled")
	if err != nil {
		t.Fatalf("resolveConfig failed: %v", err)
	}
	if enabled {
		t.Fatal("expected enabled=false for disabled server")
	}
}

func TestResolveConfig_NoAgentConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	now := time.Now()
	server := &data.MCPServer{
		ID:        "srv-noagent",
		Name:      "No Agent Config",
		Command:   "/usr/bin/mcp-test",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Table(data.MCPServer{}).Insert(ctx, server); err != nil {
		t.Fatalf("insert MCP server failed: %v", err)
	}

	m := NewMCPHostManager(db)
	_, enabled, err := m.resolveConfig(ctx, "agent-1", "srv-noagent")
	if err != nil {
		t.Fatalf("resolveConfig failed: %v", err)
	}
	if enabled {
		t.Fatal("expected enabled=false when no agent config exists")
	}
}

func TestResolveConfig_ServerDefaultsUsedWhenNoAgentOverride(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	now := time.Now()
	server := &data.MCPServer{
		ID:         "srv-defaults",
		Name:       "Defaults Server",
		Command:    "/usr/bin/mcp-default",
		Args:       []string{"--server-arg"},
		WorkingDir: "/server/dir",
		DefaultEnv: map[string]string{"SERVER_KEY": "server_val"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Table(data.MCPServer{}).Insert(ctx, server); err != nil {
		t.Fatalf("insert MCP server failed: %v", err)
	}

	agentCfg := &data.AgentMCPServer{
		AgentID:  "agent-1",
		ServerID: "srv-defaults",
		Enabled:  true,
		// No overrides
	}
	if err := data.UpsertAgentMCPServer(ctx, db, agentCfg); err != nil {
		t.Fatalf("upsert agent MCP config failed: %v", err)
	}

	m := NewMCPHostManager(db)
	resolved, enabled, err := m.resolveConfig(ctx, "agent-1", "srv-defaults")
	if err != nil {
		t.Fatalf("resolveConfig failed: %v", err)
	}
	if !enabled {
		t.Fatal("expected enabled=true")
	}

	// Should use server defaults
	if len(resolved.args) != 1 || resolved.args[0] != "--server-arg" {
		t.Errorf("expected server default args, got %v", resolved.args)
	}
	if resolved.workingDir != "/server/dir" {
		t.Errorf("expected server default workingDir, got %q", resolved.workingDir)
	}
	if resolved.env["SERVER_KEY"] != "server_val" {
		t.Errorf("expected SERVER_KEY='server_val', got %q", resolved.env["SERVER_KEY"])
	}
}

func TestMCPHostManager_NilDB(t *testing.T) {
	m := NewMCPHostManager(nil)
	_, err := m.getClient(context.Background(), "agent-1", "srv-1")
	if err == nil {
		t.Fatal("expected error for nil DB")
	}
}
