package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestInitializeAgentRuntimeFallsBackToDirectSQLite(t *testing.T) {
	t.Setenv("BROKER_SOCKET_PATH", filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv("GOWILD_DATABASE_URL", "sqlite://:memory:")

	runtime := initializeAgentRuntime(context.Background(), "alice")
	defer runtime.close()

	if runtime.usingBroker() {
		t.Fatalf("expected direct fallback when broker socket is unavailable")
	}
	if !runtime.usingDirectService() {
		t.Fatalf("expected direct service fallback, brokerErr=%v directErr=%v", runtime.brokerErr, runtime.directErr)
	}
	if runtime.brokerErr == nil {
		t.Fatalf("expected broker error to be recorded")
	}
	if runtime.directErr != nil {
		t.Fatalf("unexpected direct mode error: %v", runtime.directErr)
	}
	if err := runtime.startupError(); err != nil {
		t.Fatalf("expected standalone direct fallback to remain allowed, got %v", err)
	}

	agent, err := runtime.service.GetAgent(context.Background())
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if agent == nil || agent.ID != "alice" {
		t.Fatalf("expected direct service to initialize agent alice, got %#v", agent)
	}
}

func TestInitializeAgentRuntimeRequiresBrokerForManagedLaunch(t *testing.T) {
	t.Setenv("BROKER_SOCKET_PATH", filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv("GOWILD_BROKER_ONLY", "1")
	t.Setenv("GOWILD_DATABASE_URL", "sqlite://:memory:")

	runtime := initializeAgentRuntime(context.Background(), "alice")
	defer runtime.close()

	if runtime.usingBroker() {
		t.Fatalf("expected managed launch with missing broker to fail broker initialization")
	}
	if runtime.usingDirectService() {
		t.Fatalf("expected managed launch to avoid direct fallback, brokerErr=%v directErr=%v", runtime.brokerErr, runtime.directErr)
	}
	if !runtime.requiresBroker() {
		t.Fatalf("expected managed launch to require broker")
	}
	if runtime.brokerErr == nil {
		t.Fatalf("expected broker error to be recorded")
	}
	if runtime.directErr != nil {
		t.Fatalf("expected direct mode to be skipped entirely, got %v", runtime.directErr)
	}
	if err := runtime.startupError(); err == nil {
		t.Fatalf("expected startupError for managed launch without broker")
	}
}

func TestInitializeAgentRuntimeAllowsStatelessStandaloneWhenDirectInitFails(t *testing.T) {
	t.Setenv("BROKER_SOCKET_PATH", filepath.Join(t.TempDir(), "missing.sock"))
	t.Setenv("GOWILD_DATABASE_URL", "sqlite:///proc/gowild-agent-unwritable/runtime.db")

	runtime := initializeAgentRuntime(context.Background(), "alice")
	defer runtime.close()

	if runtime.usingBroker() {
		t.Fatalf("expected broker initialization to fail")
	}
	if runtime.usingDirectService() {
		t.Fatalf("expected direct runtime initialization to fail")
	}
	if runtime.directErr == nil {
		t.Fatalf("expected direct runtime error to be recorded")
	}
	if err := runtime.startupError(); err != nil {
		t.Fatalf("expected standalone stateless fallback to remain allowed, got %v", err)
	}
}

func TestLoadSystemPromptUsesDirectServiceState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	db, err := openDBURL("sqlite://:memory:")
	if err != nil {
		t.Fatalf("openDBURL failed: %v", err)
	}
	defer db.Close()

	service := data.NewAgentService(db, "alice")
	agent, err := service.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.Name = "Alice"
	agent.SystemPrompt = "Configured section for {{AgentName}}."
	agent.SetEnabledTools([]string{"skills", "tasks"})
	if err := service.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}
	if err := service.SaveSoul(ctx, "Persistent soul"); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}
	if _, err := service.AddTask(ctx, "Review direct mode", ""); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	runtime := &agentRuntime{
		agentID: "alice",
		service: service,
		db:      db,
	}

	prompt := loadSystemPrompt(ctx, "alice", runtime)

	if !strings.Contains(prompt, "Configured section for Alice.") {
		t.Fatalf("expected direct configured section in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Persistent soul") {
		t.Fatalf("expected soul content in prompt")
	}
	if !strings.Contains(prompt, "Review direct mode") {
		t.Fatalf("expected pending task in prompt")
	}
	if strings.Contains(prompt, "{{AgentName}}") {
		t.Fatalf("expected agent name placeholders to be resolved")
	}
	if strings.Contains(prompt, "{{AGENT_CONFIGURED_SECTION}}") {
		t.Fatalf("expected configured section placeholder to be resolved")
	}
}

func TestGetEnabledToolsDirectMode(t *testing.T) {
	ctx := context.Background()
	db, err := openDBURL("sqlite://:memory:")
	if err != nil {
		t.Fatalf("openDBURL failed: %v", err)
	}
	defer db.Close()

	service := data.NewAgentService(db, "alice")
	agent, err := service.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills", "tasks"})
	if err := service.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	enabled := getEnabledTools(ctx, &agentRuntime{
		agentID: "alice",
		service: service,
		db:      db,
	})

	if !enabled["skills"] || !enabled["tasks"] {
		t.Fatalf("expected direct enabled tools to include configured entries, got %#v", enabled)
	}
	if enabled["wallet"] {
		t.Fatalf("expected direct enabled tools to exclude unconfigured entries, got %#v", enabled)
	}
}
