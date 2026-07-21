package data

import (
	"context"
	"testing"
)

func TestAgentEnvVars(t *testing.T) {
	a := &Agent{}

	// Empty returns nil
	if got := a.EnvVars(); got != nil {
		t.Errorf("expected nil for empty EnvVarsJSON, got %v", got)
	}

	// Set and get
	a.SetEnvVars(map[string]string{"KEY": "value", "FOO": "bar"})
	got := a.EnvVars()
	if got["KEY"] != "value" || got["FOO"] != "bar" {
		t.Errorf("unexpected env vars: %v", got)
	}

	// Set empty clears
	a.SetEnvVars(nil)
	if a.EnvVarsJSON != "" {
		t.Errorf("expected empty EnvVarsJSON after setting nil, got %q", a.EnvVarsJSON)
	}
}

func TestAgentEnabledTools(t *testing.T) {
	a := &Agent{}

	// Empty returns nil
	if got := a.EnabledTools(); got != nil {
		t.Errorf("expected nil for empty EnabledToolsJSON, got %v", got)
	}

	// Set and get
	a.SetEnabledTools([]string{"skills", "shell", "file"})
	got := a.EnabledTools()
	if !got["skills"] || !got["shell"] || !got["file"] {
		t.Errorf("unexpected enabled tools: %v", got)
	}
	if got["wallet"] {
		t.Error("wallet should not be enabled")
	}

	// Empty slice is explicit "no tools", not nil/"all tools".
	a.SetEnabledTools([]string{})
	if a.EnabledToolsJSON != "[]" {
		t.Errorf("expected EnabledToolsJSON to preserve empty slice as [], got %q", a.EnabledToolsJSON)
	}
	got = a.EnabledTools()
	if got == nil {
		t.Fatal("expected non-nil map for explicit empty tool list")
	}
	if len(got) != 0 {
		t.Errorf("expected explicit empty tool map, got %v", got)
	}

	// Set empty clears
	a.SetEnabledTools(nil)
	if a.EnabledToolsJSON != "" {
		t.Errorf("expected empty EnabledToolsJSON after setting nil, got %q", a.EnabledToolsJSON)
	}
}

func TestEnsureAgent(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// First call creates the agent
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	if agent.ID != "test-agent" {
		t.Errorf("expected ID test-agent, got %s", agent.ID)
	}
	if agent.Name != "Test-agent" {
		t.Errorf("expected Name Test-agent, got %s", agent.Name)
	}
	if agent.WalletSeedPhrase == "" {
		t.Error("expected non-empty seed phrase")
	}

	seedPhrase := agent.WalletSeedPhrase

	// Second call returns existing agent with same seed
	agent2, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("second EnsureAgent failed: %v", err)
	}
	if agent2.WalletSeedPhrase != seedPhrase {
		t.Error("expected same seed phrase on second call")
	}
}

func TestGetUpdateAgent(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	svc.EnsureAgent(ctx)

	agent, err := svc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent.ID != "test-agent" {
		t.Errorf("expected ID test-agent, got %s", agent.ID)
	}

	agent.Description = "Updated description"
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	agent, _ = svc.GetAgent(ctx)
	if agent.Description != "Updated description" {
		t.Errorf("expected updated description, got %q", agent.Description)
	}
}

func TestGetSetWalletSeedPhrase(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()
	svc.EnsureAgent(ctx)

	phrase, err := svc.GetWalletSeedPhrase(ctx)
	if err != nil {
		t.Fatalf("GetWalletSeedPhrase failed: %v", err)
	}
	if phrase == "" {
		t.Error("expected non-empty seed phrase")
	}

	if err := svc.SetWalletSeedPhrase(ctx, "custom seed phrase here"); err != nil {
		t.Fatalf("SetWalletSeedPhrase failed: %v", err)
	}

	phrase, _ = svc.GetWalletSeedPhrase(ctx)
	if phrase != "custom seed phrase here" {
		t.Errorf("unexpected seed phrase: %q", phrase)
	}
}

func TestGetSetTelegramBotToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()
	svc.EnsureAgent(ctx)

	token, _ := svc.GetTelegramBotToken(ctx)
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}

	svc.SetTelegramBotToken(ctx, "bot12345:ABCDEF")
	token, _ = svc.GetTelegramBotToken(ctx)
	if token != "bot12345:ABCDEF" {
		t.Errorf("unexpected token: %q", token)
	}
}

func TestGetSetAgentMailConfig(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()
	svc.EnsureAgent(ctx)

	// API key
	key, _ := svc.GetAgentMailAPIKey(ctx)
	if key != "" {
		t.Errorf("expected empty API key, got %q", key)
	}

	svc.SetAgentMailAPIKey(ctx, "am_key_123")
	key, _ = svc.GetAgentMailAPIKey(ctx)
	if key != "am_key_123" {
		t.Errorf("unexpected API key: %q", key)
	}

	// Inbox ID
	inbox, _ := svc.GetAgentMailInboxID(ctx)
	if inbox != "" {
		t.Errorf("expected empty inbox ID, got %q", inbox)
	}

	svc.SetAgentMailInboxID(ctx, "inbox_456")
	inbox, _ = svc.GetAgentMailInboxID(ctx)
	if inbox != "inbox_456" {
		t.Errorf("unexpected inbox ID: %q", inbox)
	}
}

func TestAgentIsolation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	svc1 := NewAgentService(db, "agent-1")
	svc1.EnsureAgent(ctx)
	svc1.SaveMemory(ctx, "agent-1 memory")
	svc1.AddTask(ctx, "agent-1 task", "end")

	svc2 := NewAgentService(db, "agent-2")
	svc2.EnsureAgent(ctx)
	svc2.SaveMemory(ctx, "agent-2 memory")

	// Agent 1's memory
	mem, _ := svc1.GetMemory(ctx)
	if mem.Content != "agent-1 memory" {
		t.Errorf("expected agent-1 memory, got %q", mem.Content)
	}

	// Agent 2's memory
	mem, _ = svc2.GetMemory(ctx)
	if mem.Content != "agent-2 memory" {
		t.Errorf("expected agent-2 memory, got %q", mem.Content)
	}

	// Agent 2 should not see agent-1's tasks
	tasks, _ := svc2.GetPendingTasks(ctx)
	if len(tasks) != 0 {
		t.Errorf("agent-2 should have 0 tasks, got %d", len(tasks))
	}

	// Agent 1 trying to get agent-2's task by ID should fail
	tasks1, _ := svc1.GetPendingTasks(ctx)
	if len(tasks1) != 1 {
		t.Fatalf("expected 1 task for agent-1")
	}

	_, err := svc2.GetTask(ctx, tasks1[0].ID)
	if err == nil {
		t.Error("agent-2 should not be able to access agent-1's task")
	}
}
