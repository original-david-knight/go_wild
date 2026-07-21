package main

import (
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestBuildAgentCmd(t *testing.T) {
	// Minimal agent
	agent := &data.Agent{ID: "jake"}
	cmd := buildAgentCmd(agent)
	if len(cmd) < 2 || cmd[0] != "-agent" || cmd[1] != "jake" {
		t.Errorf("buildAgentCmd minimal: got %v, want [-agent jake ...]", cmd)
	}

	// Full agent config
	agent = &data.Agent{
		ID:               "alice",
		ModelProvider:    data.LLMProviderOpenAI,
		OpenAIAuthMode:   data.OpenAIAuthModeCodexOAuth,
		Model:            "gemini-3-flash-preview",
		SmartModel:       "gemini-3-flash-preview-exp",
		SmartDefault:     true,
		MaxTurns:         20,
		Heartbeat:        "30m",
		WorkTasksTimeout: "5m",
		ExtraFlags:       "-no-sandbox -debug",
	}
	cmd = buildAgentCmd(agent)

	assertContainsFlag := func(flag, value string) {
		for i, c := range cmd {
			if c == flag && i+1 < len(cmd) && cmd[i+1] == value {
				return
			}
		}
		t.Errorf("buildAgentCmd: expected flag %s %s in %v", flag, value, cmd)
	}

	assertContainsFlag("-agent", "alice")
	assertContainsFlag("-provider", data.LLMProviderOpenAI)
	assertContainsFlag("-openai-auth", data.OpenAIAuthModeCodexOAuth)
	assertContainsFlag("-model", "gemini-3-flash-preview")
	assertContainsFlag("-smart-model", "gemini-3-flash-preview-exp")
	assertContainsFlag("-max-turns", "20")
	assertContainsFlag("-heartbeat", "30m")
	assertContainsFlag("-worktasks-timeout", "5m")

	// Check smart flag is present
	found := false
	for _, c := range cmd {
		if c == "-smart" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("buildAgentCmd: expected -smart flag in %v", cmd)
	}

	// Check extra flags are split and appended
	foundNoSandbox := false
	foundDebug := false
	for _, c := range cmd {
		if c == "-no-sandbox" {
			foundNoSandbox = true
		}
		if c == "-debug" {
			foundDebug = true
		}
	}
	if !foundNoSandbox {
		t.Errorf("buildAgentCmd: expected -no-sandbox in %v", cmd)
	}
	if !foundDebug {
		t.Errorf("buildAgentCmd: expected -debug in %v", cmd)
	}
}

func TestBuildAgentCmd_NoOptionalFlags(t *testing.T) {
	agent := &data.Agent{
		ID: "minimal",
	}
	cmd := buildAgentCmd(agent)

	// Should only have -agent minimal
	if len(cmd) != 2 {
		t.Errorf("buildAgentCmd minimal: expected 2 args, got %d: %v", len(cmd), cmd)
	}

	// SmartDefault false -> no -smart flag
	for _, c := range cmd {
		if c == "-smart" {
			t.Error("buildAgentCmd: -smart should not be present when SmartDefault is false")
		}
		if c == "-model" || c == "-smart-model" || c == "-max-turns" || c == "-heartbeat" || c == "-worktasks-timeout" {
			t.Errorf("buildAgentCmd: unexpected flag %q in minimal config", c)
		}
	}
}

func TestBuildContainerEnvBrokerEnabled(t *testing.T) {
	t.Setenv("BROKER_SOCKET_PATH", "/tmp/test-broker.sock")
	secret := []byte("01234567890123456789012345678901")

	env := buildContainerEnv(map[string]string{
		"CUSTOM":                       "value",
		"GEMINI_API_KEY":               "should-filter",
		"OPENAI_API_KEY":               "should-filter",
		"OPENAI_BASE_URL":              "https://example.invalid/v1",
		"OPENAI_ORG_ID":                "should-filter",
		"OPENAI_PROJECT_ID":            "should-filter",
		"ANTHROPIC_API_KEY":            "should-filter",
		"ANTHROPIC_AUTH_TOKEN":         "should-filter",
		"ANTHROPIC_BASE_URL":           "https://example.invalid",
		"GOWILD_DATABASE_URL":          "should-filter",
		"BROKER_URL":                   "http://example",
		"BROKER_TOKEN":                 "should-filter",
		"BROKER_SOCKET_PATH":           "/tmp/override.sock",
		"GOWILD_AGENT_ETH_PRIVATE_KEY": "should-filter",
		"GOWILD_BROKER_ONLY":           "0",
	}, "agent-1", secret, "0xauth-private-key")
	envMap := envSliceToMap(env)

	if got := envMap["CUSTOM"]; got != "value" {
		t.Fatalf("expected CUSTOM=value, got %q", got)
	}
	if _, ok := envMap["GEMINI_API_KEY"]; ok {
		t.Fatalf("expected GEMINI_API_KEY to be filtered")
	}
	if _, ok := envMap["OPENAI_API_KEY"]; ok {
		t.Fatalf("expected OPENAI_API_KEY to be filtered")
	}
	if _, ok := envMap["OPENAI_BASE_URL"]; ok {
		t.Fatalf("expected OPENAI_BASE_URL to be filtered")
	}
	if _, ok := envMap["OPENAI_ORG_ID"]; ok {
		t.Fatalf("expected OPENAI_ORG_ID to be filtered")
	}
	if _, ok := envMap["OPENAI_PROJECT_ID"]; ok {
		t.Fatalf("expected OPENAI_PROJECT_ID to be filtered")
	}
	if _, ok := envMap["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("expected ANTHROPIC_API_KEY to be filtered")
	}
	if _, ok := envMap["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Fatalf("expected ANTHROPIC_AUTH_TOKEN to be filtered")
	}
	if _, ok := envMap["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("expected ANTHROPIC_BASE_URL to be filtered")
	}
	if _, ok := envMap["GOWILD_DATABASE_URL"]; ok {
		t.Fatalf("expected GOWILD_DATABASE_URL to be filtered")
	}
	if _, ok := envMap["BROKER_URL"]; ok {
		t.Fatalf("expected BROKER_URL to be filtered")
	}
	if got := envMap["BROKER_SOCKET_PATH"]; got != "/tmp/test-broker.sock" {
		t.Fatalf("expected injected BROKER_SOCKET_PATH, got %q", got)
	}
	if _, ok := envMap["BROKER_TOKEN"]; ok {
		t.Fatalf("expected BROKER_TOKEN to be filtered")
	}
	if got := envMap["GOWILD_AGENT_ETH_PRIVATE_KEY"]; got != "0xauth-private-key" {
		t.Fatalf("expected injected GOWILD_AGENT_ETH_PRIVATE_KEY, got %q", got)
	}
	if got := envMap["GOWILD_BROKER_ONLY"]; got != "1" {
		t.Fatalf("expected GOWILD_BROKER_ONLY=1, got %q", got)
	}
	if got := envMap["GOWILD_AGENT_ID"]; got != "agent-1" {
		t.Fatalf("expected GOWILD_AGENT_ID=agent-1, got %q", got)
	}
}

func TestBuildContainerEnvBrokerDisabled(t *testing.T) {
	env := buildContainerEnv(map[string]string{
		"CUSTOM":              "value",
		"GEMINI_API_KEY":      "keep",
		"GOWILD_DATABASE_URL": "keep",
		"BROKER_URL":          "http://example",
		"BROKER_TOKEN":        "keep-token",
		"BROKER_SOCKET_PATH":  "/tmp/keep.sock",
	}, "agent-2", nil)
	envMap := envSliceToMap(env)

	if got := envMap["GEMINI_API_KEY"]; got != "keep" {
		t.Fatalf("expected GEMINI_API_KEY preserved, got %q", got)
	}
	if got := envMap["GOWILD_DATABASE_URL"]; got != "keep" {
		t.Fatalf("expected GOWILD_DATABASE_URL preserved, got %q", got)
	}
	if got := envMap["BROKER_URL"]; got != "http://example" {
		t.Fatalf("expected BROKER_URL preserved, got %q", got)
	}
	if got := envMap["BROKER_TOKEN"]; got != "keep-token" {
		t.Fatalf("expected BROKER_TOKEN preserved, got %q", got)
	}
	if got := envMap["BROKER_SOCKET_PATH"]; got != "/tmp/keep.sock" {
		t.Fatalf("expected BROKER_SOCKET_PATH preserved, got %q", got)
	}
	if got := envMap["GOWILD_AGENT_ID"]; got != "agent-2" {
		t.Fatalf("expected GOWILD_AGENT_ID=agent-2, got %q", got)
	}
}

func envSliceToMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}
