package main

import (
	"context"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func restoreLLMProviderFlags(t *testing.T) {
	t.Helper()
	origProvider := *providerFlag
	origAuth := *openAIAuthFlag
	origModel := *modelFlag
	origSmartModel := *smartModelFlag
	t.Cleanup(func() {
		*providerFlag = origProvider
		*openAIAuthFlag = origAuth
		*modelFlag = origModel
		*smartModelFlag = origSmartModel
		globalLLMConfig = llmSessionConfig{}
	})
}

func clearModelEnvHints(t *testing.T) {
	t.Helper()
	t.Setenv(openAIFastModelEnv, "")
	t.Setenv(openAIFastModelAltEnv, "")
	t.Setenv(openAISmartModelEnv, "")
	t.Setenv(openAISmartModelAltEnv, "")
	t.Setenv(claudeFastModelEnv, "")
	t.Setenv(claudeSmartModelEnv, "")
	t.Setenv("SMART_MODEL", "")
}

func newDirectRuntimeForModelConfigTest(t *testing.T, configure func(*data.Agent)) *agentRuntime {
	t.Helper()

	ctx := context.Background()
	db, err := openDBURL("sqlite://:memory:")
	if err != nil {
		t.Fatalf("openDBURL failed: %v", err)
	}

	service := data.NewAgentService(db, "alice")
	agent, err := service.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	if configure != nil {
		configure(agent)
	}
	if err := service.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	return &agentRuntime{
		agentID: "alice",
		service: service,
		db:      db,
	}
}

func TestResolveModelConfig_UsesPersistedDirectConfig(t *testing.T) {
	restoreLLMProviderFlags(t)
	*providerFlag = ""
	*openAIAuthFlag = ""
	*modelFlag = ""
	*smartModelFlag = ""

	runtime := newDirectRuntimeForModelConfigTest(t, func(agent *data.Agent) {
		agent.ModelProvider = data.LLMProviderOpenAI
		agent.OpenAIAuthMode = data.OpenAIAuthModeCodexOAuth
		agent.Model = "gpt-5.4-mini"
		agent.SmartModel = "gpt-5.4"
	})
	defer runtime.close()

	cfg, err := resolveModelConfig(context.Background(), runtime)
	if err != nil {
		t.Fatalf("resolveModelConfig returned error: %v", err)
	}
	if cfg.Provider != data.LLMProviderOpenAI {
		t.Fatalf("provider = %q, want %q", cfg.Provider, data.LLMProviderOpenAI)
	}
	if cfg.OpenAIAuthMode != data.OpenAIAuthModeCodexOAuth {
		t.Fatalf("openai auth mode = %q, want %q", cfg.OpenAIAuthMode, data.OpenAIAuthModeCodexOAuth)
	}
	if cfg.BaseModel != "gpt-5.4-mini" {
		t.Fatalf("base model = %q, want gpt-5.4-mini", cfg.BaseModel)
	}
	if cfg.SmartModel != "gpt-5.4" {
		t.Fatalf("smart model = %q, want gpt-5.4", cfg.SmartModel)
	}
}

func TestResolveModelConfig_FlagsOverridePersistedDirectConfig(t *testing.T) {
	restoreLLMProviderFlags(t)
	*providerFlag = "anthropic"
	*openAIAuthFlag = ""
	*modelFlag = "claude-opus-4-7"
	*smartModelFlag = "claude-opus-4-7"

	runtime := newDirectRuntimeForModelConfigTest(t, func(agent *data.Agent) {
		agent.ModelProvider = data.LLMProviderOpenAI
		agent.OpenAIAuthMode = data.OpenAIAuthModeAPIKey
		agent.Model = "gpt-5.4-mini"
		agent.SmartModel = "gpt-5.4"
	})
	defer runtime.close()

	cfg, err := resolveModelConfig(context.Background(), runtime)
	if err != nil {
		t.Fatalf("resolveModelConfig returned error: %v", err)
	}
	if cfg.Provider != data.LLMProviderAnthropic {
		t.Fatalf("provider = %q, want %q", cfg.Provider, data.LLMProviderAnthropic)
	}
	if cfg.OpenAIAuthMode != "" {
		t.Fatalf("openai auth mode = %q, want empty", cfg.OpenAIAuthMode)
	}
	if cfg.BaseModel != "claude-opus-4-7" {
		t.Fatalf("base model = %q, want claude-opus-4-7", cfg.BaseModel)
	}
	if cfg.SmartModel != "claude-opus-4-7" {
		t.Fatalf("smart model = %q, want claude-opus-4-7", cfg.SmartModel)
	}
}

func TestResolveModelConfig_UsesOpenAIEnvWhenExplicitlyConfigured(t *testing.T) {
	restoreLLMProviderFlags(t)
	*providerFlag = "openai"
	*openAIAuthFlag = "api_key"
	*modelFlag = ""
	*smartModelFlag = ""

	t.Setenv(openAIFastModelEnv, "gpt-5.4-mini")
	t.Setenv(openAISmartModelEnv, "gpt-5.4")

	cfg, err := resolveModelConfig(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveModelConfig returned error: %v", err)
	}
	if cfg.BaseModel != "gpt-5.4-mini" {
		t.Fatalf("base model = %q, want gpt-5.4-mini", cfg.BaseModel)
	}
	if cfg.SmartModel != "gpt-5.4" {
		t.Fatalf("smart model = %q, want gpt-5.4", cfg.SmartModel)
	}
}

func TestResolveModelConfig_DefaultsBlankPersistedConfigToGemini(t *testing.T) {
	restoreLLMProviderFlags(t)
	clearModelEnvHints(t)
	*providerFlag = ""
	*openAIAuthFlag = ""
	*modelFlag = ""
	*smartModelFlag = ""

	runtime := newDirectRuntimeForModelConfigTest(t, nil)
	defer runtime.close()

	cfg, err := resolveModelConfig(context.Background(), runtime)
	if err != nil {
		t.Fatalf("resolveModelConfig returned error: %v", err)
	}
	if cfg.Provider != data.LLMProviderGemini {
		t.Fatalf("provider = %q, want %q", cfg.Provider, data.LLMProviderGemini)
	}
	if cfg.BaseModel != loop.DefaultModel {
		t.Fatalf("base model = %q, want %q", cfg.BaseModel, loop.DefaultModel)
	}
	if cfg.SmartModel != loop.DefaultSmartModelForProvider(cfg.Provider, cfg.BaseModel) {
		t.Fatalf("smart model = %q, want %q", cfg.SmartModel, loop.DefaultSmartModelForProvider(cfg.Provider, cfg.BaseModel))
	}
	if cfg.OpenAIAuthMode != "" {
		t.Fatalf("openai auth mode = %q, want empty", cfg.OpenAIAuthMode)
	}
	initialModel, err := cfg.initialModel(true)
	if err != nil {
		t.Fatalf("initialModel(true) returned error: %v", err)
	}
	if initialModel != cfg.SmartModel {
		t.Fatalf("smart initial model = %q, want %q", initialModel, cfg.SmartModel)
	}
}

func TestResolveModelConfig_DefaultsOpenAIAuthMode(t *testing.T) {
	restoreLLMProviderFlags(t)
	*providerFlag = "openai"
	*openAIAuthFlag = ""
	*modelFlag = "gpt-5.4-mini"
	*smartModelFlag = ""

	cfg, err := resolveModelConfig(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveModelConfig returned error: %v", err)
	}
	if cfg.OpenAIAuthMode != data.OpenAIAuthModeAPIKey {
		t.Fatalf("openai auth mode = %q, want %q", cfg.OpenAIAuthMode, data.OpenAIAuthModeAPIKey)
	}
}

func TestBuildSandboxLLMEnv_DefaultsToGemini(t *testing.T) {
	restoreLLMProviderFlags(t)
	clearModelEnvHints(t)
	t.Setenv("GEMINI_API_KEY", "test-gemini-key")

	cfg, envVars, err := buildSandboxLLMEnv(context.Background(), nil)
	if err != nil {
		t.Fatalf("buildSandboxLLMEnv returned error: %v", err)
	}
	if cfg.Provider != data.LLMProviderGemini {
		t.Fatalf("provider = %q, want %q", cfg.Provider, data.LLMProviderGemini)
	}
	if cfg.OpenAIAuthMode != "" {
		t.Fatalf("openai auth mode = %q, want empty", cfg.OpenAIAuthMode)
	}
	if envVars["GEMINI_API_KEY"] != "test-gemini-key" {
		t.Fatalf("GEMINI_API_KEY = %q, want test-gemini-key", envVars["GEMINI_API_KEY"])
	}
}

func TestBuildSandboxLLMEnv_InfersOpenAIProviderAndForwardsModelEnv(t *testing.T) {
	restoreLLMProviderFlags(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv(openAIFastModelEnv, "gpt-5.4-mini")
	t.Setenv(openAISmartModelEnv, "gpt-5.4")

	cfg, envVars, err := buildSandboxLLMEnv(context.Background(), nil)
	if err != nil {
		t.Fatalf("buildSandboxLLMEnv returned error: %v", err)
	}
	if cfg.Provider != data.LLMProviderOpenAI {
		t.Fatalf("provider = %q, want %q", cfg.Provider, data.LLMProviderOpenAI)
	}
	if cfg.OpenAIAuthMode != data.OpenAIAuthModeAPIKey {
		t.Fatalf("openai auth mode = %q, want %q", cfg.OpenAIAuthMode, data.OpenAIAuthModeAPIKey)
	}
	if envVars["OPENAI_API_KEY"] != "test-openai-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want test-openai-key", envVars["OPENAI_API_KEY"])
	}
	if envVars["OPENAI_BASE_URL"] != "https://example.test/v1" {
		t.Fatalf("OPENAI_BASE_URL = %q, want https://example.test/v1", envVars["OPENAI_BASE_URL"])
	}
	if envVars[openAIFastModelEnv] != "gpt-5.4-mini" {
		t.Fatalf("%s = %q, want gpt-5.4-mini", openAIFastModelEnv, envVars[openAIFastModelEnv])
	}
	if envVars[openAISmartModelEnv] != "gpt-5.4" {
		t.Fatalf("%s = %q, want gpt-5.4", openAISmartModelEnv, envVars[openAISmartModelEnv])
	}
}

func TestBuildSandboxLLMEnv_UsesPersistedDirectConfig(t *testing.T) {
	restoreLLMProviderFlags(t)
	clearModelEnvHints(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	runtime := newDirectRuntimeForModelConfigTest(t, func(agent *data.Agent) {
		agent.ModelProvider = data.LLMProviderOpenAI
		agent.OpenAIAuthMode = data.OpenAIAuthModeAPIKey
		agent.Model = "gpt-5.4-mini"
		agent.SmartModel = "gpt-5.4"
	})
	defer runtime.close()

	cfg, envVars, err := buildSandboxLLMEnv(context.Background(), runtime)
	if err != nil {
		t.Fatalf("buildSandboxLLMEnv returned error: %v", err)
	}
	if cfg.Provider != data.LLMProviderOpenAI {
		t.Fatalf("provider = %q, want %q", cfg.Provider, data.LLMProviderOpenAI)
	}
	if cfg.BaseModel != "gpt-5.4-mini" {
		t.Fatalf("base model = %q, want gpt-5.4-mini", cfg.BaseModel)
	}
	if cfg.SmartModel != "gpt-5.4" {
		t.Fatalf("smart model = %q, want gpt-5.4", cfg.SmartModel)
	}
	if envVars["OPENAI_API_KEY"] != "test-openai-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want test-openai-key", envVars["OPENAI_API_KEY"])
	}
}

func TestResolveModelConfig_RejectsProviderModelMismatch(t *testing.T) {
	restoreLLMProviderFlags(t)
	*providerFlag = "openai"
	*openAIAuthFlag = "api_key"
	*modelFlag = "gemini-3-flash-preview"
	*smartModelFlag = ""

	if _, err := resolveModelConfig(context.Background(), nil); err == nil {
		t.Fatalf("expected resolveModelConfig to reject mismatched provider/model")
	}
}
