package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

const (
	openAIFastModelEnv     = "OPEN_AI_FAST_MODEL"
	openAISmartModelEnv    = "OPEN_AI_SMART_MODEL"
	openAIFastModelAltEnv  = "OPENAI_FAST_MODEL"
	openAISmartModelAltEnv = "OPENAI_SMART_MODEL"
	claudeFastModelEnv     = "CLAUDE_FAST_MODEL"
	claudeSmartModelEnv    = "CLAUDE_SMART_MODEL"
)

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeProviderStrict(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "":
		return "", nil
	case loop.ProviderGemini:
		return loop.ProviderGemini, nil
	case loop.ProviderOpenAI:
		return loop.ProviderOpenAI, nil
	case loop.ProviderAnthropic:
		return loop.ProviderAnthropic, nil
	default:
		return "", fmt.Errorf("unsupported llm provider %q", provider)
	}
}

func normalizeOpenAIAuthModeStrict(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return "", nil
	case loop.OpenAIAuthModeAPIKey:
		return loop.OpenAIAuthModeAPIKey, nil
	case loop.OpenAIAuthModeCodexOAuth:
		return loop.OpenAIAuthModeCodexOAuth, nil
	default:
		return "", fmt.Errorf("unsupported openai auth mode %q", mode)
	}
}

func resolveProviderConfig(provider string, modelHints ...string) (string, error) {
	normalized, err := normalizeProviderStrict(provider)
	if err != nil {
		return "", err
	}
	if normalized != "" {
		return normalized, nil
	}
	for _, hint := range modelHints {
		if inferred := loop.InferLLMProviderFromModel(hint); inferred != "" {
			return inferred, nil
		}
	}
	return loop.ProviderGemini, nil
}

func resolveRequiredModel(provider, rawModel, field string) (string, error) {
	rawModel = strings.TrimSpace(rawModel)
	if rawModel == "" {
		return "", fmt.Errorf("%s is not configured", field)
	}
	model := loop.NormalizeModelForProvider(provider, rawModel)
	if model == "" {
		return "", fmt.Errorf("%s %q does not match provider %s", field, rawModel, provider)
	}
	return model, nil
}

func resolveOptionalModel(provider, rawModel, field string) (string, error) {
	rawModel = strings.TrimSpace(rawModel)
	if rawModel == "" {
		return "", nil
	}
	model := loop.NormalizeModelForProvider(provider, rawModel)
	if model == "" {
		return "", fmt.Errorf("%s %q does not match provider %s", field, rawModel, provider)
	}
	return model, nil
}

func resolvePersistedModelConfig(ctx context.Context, runtime *agentRuntime) (llmSessionConfig, error) {
	if runtime == nil || runtime.service == nil {
		return llmSessionConfig{}, nil
	}
	agent, err := runtime.service.GetAgent(ctx)
	if err != nil {
		return llmSessionConfig{}, fmt.Errorf("failed to load persisted agent llm config: %w", err)
	}
	if agent == nil {
		return llmSessionConfig{}, nil
	}
	return llmSessionConfig{
		Provider:       strings.TrimSpace(agent.ModelProvider),
		OpenAIAuthMode: strings.TrimSpace(agent.OpenAIAuthMode),
		BaseModel:      strings.TrimSpace(agent.Model),
		SmartModel:     strings.TrimSpace(agent.SmartModel),
	}, nil
}

func envBaseModel(provider string) string {
	switch provider {
	case loop.ProviderOpenAI:
		return firstEnv(openAIFastModelEnv, openAIFastModelAltEnv)
	case loop.ProviderAnthropic:
		return firstEnv(claudeFastModelEnv)
	default:
		return ""
	}
}

func envSmartModel(provider string) string {
	switch provider {
	case loop.ProviderGemini:
		return strings.TrimSpace(os.Getenv("SMART_MODEL"))
	case loop.ProviderOpenAI:
		return firstEnv(openAISmartModelEnv, openAISmartModelAltEnv)
	case loop.ProviderAnthropic:
		return firstEnv(claudeSmartModelEnv)
	default:
		return ""
	}
}

func configuredLLMProvider() string {
	if strings.TrimSpace(globalLLMConfig.Provider) != "" {
		return globalLLMConfig.Provider
	}
	provider, err := resolveProviderConfig(
		*providerFlag,
		*modelFlag,
		*smartModelFlag,
		firstEnv(openAIFastModelEnv, openAIFastModelAltEnv),
		firstEnv(openAISmartModelEnv, openAISmartModelAltEnv),
		firstEnv(claudeFastModelEnv),
		firstEnv(claudeSmartModelEnv),
		strings.TrimSpace(os.Getenv("SMART_MODEL")),
	)
	if err != nil {
		return ""
	}
	return provider
}

func configuredOpenAIAuthMode() string {
	if strings.TrimSpace(globalLLMConfig.OpenAIAuthMode) != "" {
		return globalLLMConfig.OpenAIAuthMode
	}
	mode, err := normalizeOpenAIAuthModeStrict(*openAIAuthFlag)
	if err != nil {
		return ""
	}
	if mode == "" && configuredLLMProvider() == loop.ProviderOpenAI {
		return loop.OpenAIAuthModeAPIKey
	}
	return mode
}

func resolveModelConfig(ctx context.Context, runtime *agentRuntime) (llmSessionConfig, error) {
	persisted, err := resolvePersistedModelConfig(ctx, runtime)
	if err != nil {
		return llmSessionConfig{}, err
	}

	provider, err := resolveProviderConfig(
		firstNonEmpty(*providerFlag, persisted.Provider),
		*modelFlag,
		persisted.BaseModel,
		firstEnv(openAIFastModelEnv, openAIFastModelAltEnv),
		*smartModelFlag,
		persisted.SmartModel,
		firstEnv(openAISmartModelEnv, openAISmartModelAltEnv),
		firstEnv(claudeFastModelEnv),
		firstEnv(claudeSmartModelEnv),
		strings.TrimSpace(os.Getenv("SMART_MODEL")),
	)
	if err != nil {
		return llmSessionConfig{}, err
	}

	cfg := llmSessionConfig{Provider: provider}
	cfg.BaseModel, err = resolveRequiredModel(
		provider,
		firstNonEmpty(*modelFlag, persisted.BaseModel, envBaseModel(provider), loop.DefaultModelForProvider(provider)),
		"base model",
	)
	if err != nil {
		return llmSessionConfig{}, err
	}

	if provider == loop.ProviderOpenAI {
		cfg.OpenAIAuthMode, err = normalizeOpenAIAuthModeStrict(firstNonEmpty(*openAIAuthFlag, persisted.OpenAIAuthMode))
		if err != nil {
			return llmSessionConfig{}, err
		}
		if cfg.OpenAIAuthMode == "" {
			cfg.OpenAIAuthMode = loop.OpenAIAuthModeAPIKey
		}
	}

	cfg.SmartModel, err = resolveOptionalModel(
		provider,
		firstNonEmpty(
			*smartModelFlag,
			persisted.SmartModel,
			envSmartModel(provider),
			loop.DefaultSmartModelForProvider(provider, cfg.BaseModel),
		),
		"smart model",
	)
	if err != nil {
		return llmSessionConfig{}, err
	}

	return cfg, nil
}

func newConfiguredLLMClient(ctx context.Context, model string) (loop.LLMClient, error) {
	if strings.TrimSpace(globalLLMConfig.Provider) == "" {
		return nil, fmt.Errorf("llm provider is not configured")
	}
	return loop.NewProviderClient(ctx, loop.ProviderClientConfig{
		Provider:       globalLLMConfig.Provider,
		Model:          model,
		OpenAIAuthMode: globalLLMConfig.OpenAIAuthMode,
	})
}
