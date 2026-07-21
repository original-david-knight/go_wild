package main

import (
	"sort"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
)

func TestBuildAgentResponse(t *testing.T) {
	now := time.Now()
	agent := &data.Agent{
		ID:               "jake",
		Name:             "Jake",
		Description:      "Test agent",
		ModelProvider:    data.LLMProviderOpenAI,
		OpenAIAuthMode:   data.OpenAIAuthModeCodexOAuth,
		Model:            "gemini-3-flash-preview",
		SmartModel:       "gemini-3-flash-preview",
		SmartDefault:     true,
		MaxTurns:         10,
		Heartbeat:        "15m",
		WorkTasksTimeout: "4m",
		AutoStart:        true,
		TelegramBotToken: "bot123:ABC",
		AgentMailInboxID: "inbox-1",
		MemoryLimit:      "512m",
		CPULimit:         "2.0",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	resp := buildAgentResponse(agent, "running")

	if resp.ID != "jake" {
		t.Errorf("expected ID jake, got %s", resp.ID)
	}
	if resp.ContainerStatus != "running" {
		t.Errorf("expected running, got %s", resp.ContainerStatus)
	}
	if !resp.HasTelegram {
		t.Error("expected HasTelegram true")
	}
	if !resp.HasEmail {
		t.Error("expected HasEmail true")
	}
	if !resp.SmartDefault {
		t.Error("expected SmartDefault true")
	}
	if resp.ModelProvider != data.LLMProviderOpenAI {
		t.Errorf("expected openai provider, got %q", resp.ModelProvider)
	}
	if resp.OpenAIAuthMode != data.OpenAIAuthModeCodexOAuth {
		t.Errorf("expected codex_oauth auth mode, got %q", resp.OpenAIAuthMode)
	}
	if resp.MemoryLimit != "512m" {
		t.Errorf("expected 512m, got %s", resp.MemoryLimit)
	}
	if resp.WorkTasksTimeout != "4m" {
		t.Errorf("expected work_tasks_timeout 4m, got %s", resp.WorkTasksTimeout)
	}
}

func TestBuildAgentResponse_NoTokens(t *testing.T) {
	agent := &data.Agent{
		ID:   "test",
		Name: "Test",
	}

	resp := buildAgentResponse(agent, "stopped")

	if resp.HasTelegram {
		t.Error("expected HasTelegram false")
	}
	if resp.HasEmail {
		t.Error("expected HasEmail false")
	}
	if resp.ContainerStatus != "stopped" {
		t.Errorf("expected stopped, got %s", resp.ContainerStatus)
	}
	if resp.Mode != "interactive" {
		t.Errorf("expected interactive mode, got %q", resp.Mode)
	}
	if resp.WorkerContextMode != "stateless" {
		t.Errorf("expected stateless worker context, got %q", resp.WorkerContextMode)
	}
}

func TestEnabledToolsList(t *testing.T) {
	// No tools configured (nil)
	agent := &data.Agent{}
	got := enabledToolsList(agent)
	if got != nil {
		t.Errorf("expected nil for unconfigured tools, got %v", got)
	}

	// With configured tools
	agent.SetEnabledTools([]string{"skills", "shell", "file"})
	got = enabledToolsList(agent)
	if len(got) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(got))
	}

	sort.Strings(got)
	expected := []string{"file", "shell", "skills"}
	for i, e := range expected {
		if got[i] != e {
			t.Errorf("expected %q at index %d, got %q", e, i, got[i])
		}
	}
}

func TestBuildInfoKnown(t *testing.T) {
	tests := []struct {
		name     string
		info     dockermgr.BuildInfo
		expected bool
	}{
		{"empty", dockermgr.BuildInfo{}, false},
		{"unknown", dockermgr.BuildInfo{ID: "unknown"}, false},
		{"valid sha", dockermgr.BuildInfo{ID: "abc123"}, true},
		{"dirty", dockermgr.BuildInfo{ID: "abc123-dirty-def456"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.Known(); got != tc.expected {
				t.Errorf("Known() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestComputeDesiredBuildInfo(t *testing.T) {
	info := dockermgr.ComputeDesiredBuildInfo()
	// We're in a git repo, so this should succeed
	if !info.Known() {
		t.Skip("not in a git repo or git unavailable")
	}
	if info.SHA == "" {
		t.Error("expected non-empty SHA")
	}
	if info.Source != "git" {
		t.Errorf("expected source git, got %q", info.Source)
	}
}
