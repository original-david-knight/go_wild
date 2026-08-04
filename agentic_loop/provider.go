package gowild_agentic_loop

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	LLMProviderGemini    = "gemini"
	LLMProviderOpenAI    = "openai"
	LLMProviderAnthropic = "anthropic"

	OpenAIAuthModeAPIKey     = "api_key"
	OpenAIAuthModeCodexOAuth = "codex_oauth"

	DefaultGeminiSmartModel   = "gemini-3.1-pro-preview"
	DefaultOpenAIModel        = "gpt-5.4"
	DefaultAnthropicModel     = "claude-opus-4-7"
	defaultProviderHTTPTimout = 10 * time.Minute
)

const (
	ProviderGemini    = LLMProviderGemini
	ProviderOpenAI    = LLMProviderOpenAI
	ProviderAnthropic = LLMProviderAnthropic
)

// ProviderClientConfig configures a non-broker LLM client instance.
type ProviderClientConfig struct {
	Provider       string
	Model          string
	APIKey         string
	OpenAIAuthMode string
	BaseURL        string
	HTTPClient     *http.Client
}

// httpStatusError wraps provider HTTP failures so callers can inspect status codes.
type httpStatusError struct {
	Provider string
	Code     int
	Message  string
	Body     string
}

func (e *httpStatusError) Error() string {
	if e == nil {
		return "llm request failed"
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		return fmt.Sprintf("%s request failed with status %d", e.Provider, e.Code)
	}
	return fmt.Sprintf("%s request failed with status %d: %s", e.Provider, e.Code, msg)
}

// NormalizeLLMProvider normalizes persisted provider strings.
func NormalizeLLMProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", LLMProviderGemini:
		return LLMProviderGemini
	case LLMProviderOpenAI:
		return LLMProviderOpenAI
	case LLMProviderAnthropic:
		return LLMProviderAnthropic
	default:
		return LLMProviderGemini
	}
}

// InferLLMProviderFromModel infers a provider from a model name.
func InferLLMProviderFromModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case model == "":
		return ""
	case strings.HasPrefix(model, "gemini"):
		return LLMProviderGemini
	case strings.HasPrefix(model, "gpt"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return LLMProviderOpenAI
	case strings.HasPrefix(model, "claude"):
		return LLMProviderAnthropic
	default:
		return ""
	}
}

// modelMatchesProvider returns true when a model name is either provider-neutral
// or clearly belongs to the selected provider.
func modelMatchesProvider(provider, model string) bool {
	provider = NormalizeLLMProvider(provider)
	inferred := InferLLMProviderFromModel(model)
	return inferred == "" || inferred == provider
}

// NormalizeModelForProvider drops clearly mismatched model names while preserving
// unknown/custom names for compatibility with proxy endpoints.
func NormalizeModelForProvider(provider, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if !modelMatchesProvider(provider, model) {
		return ""
	}
	return model
}

// resolveLLMProvider resolves a provider field, falling back to model-name inference.
func resolveLLMProvider(provider string, modelHints ...string) string {
	if strings.TrimSpace(provider) != "" {
		return NormalizeLLMProvider(provider)
	}
	for _, hint := range modelHints {
		if inferred := InferLLMProviderFromModel(hint); inferred != "" {
			return inferred
		}
	}
	return LLMProviderGemini
}

// NormalizeOpenAIAuthMode normalizes persisted OpenAI auth-mode strings.
func NormalizeOpenAIAuthMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", OpenAIAuthModeAPIKey:
		return OpenAIAuthModeAPIKey
	case OpenAIAuthModeCodexOAuth:
		return OpenAIAuthModeCodexOAuth
	default:
		return OpenAIAuthModeAPIKey
	}
}

// DefaultModelForProvider returns the default model alias for a provider.
func DefaultModelForProvider(provider string) string {
	switch NormalizeLLMProvider(provider) {
	case LLMProviderOpenAI:
		return DefaultOpenAIModel
	case LLMProviderAnthropic:
		return DefaultAnthropicModel
	default:
		return DefaultModel
	}
}

// DefaultSmartModelForProvider returns the default smart-mode model for a provider.
func DefaultSmartModelForProvider(provider string, baseModel string) string {
	provider = NormalizeLLMProvider(provider)
	baseModel = strings.TrimSpace(baseModel)
	switch provider {
	case LLMProviderOpenAI, LLMProviderAnthropic:
		if baseModel != "" {
			return baseModel
		}
		return DefaultModelForProvider(provider)
	default:
		return DefaultGeminiSmartModel
	}
}

// NewProviderClient creates a provider-specific LLM client.
func NewProviderClient(ctx context.Context, cfg ProviderClientConfig) (LLMClient, error) {
	provider := resolveLLMProvider(cfg.Provider, cfg.Model)
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModelForProvider(provider)
	}

	switch provider {
	case LLMProviderOpenAI:
		return newOpenAIClient(ctx, ProviderClientConfig{
			Provider:       provider,
			Model:          model,
			APIKey:         cfg.APIKey,
			OpenAIAuthMode: NormalizeOpenAIAuthMode(cfg.OpenAIAuthMode),
			BaseURL:        cfg.BaseURL,
			HTTPClient:     cfg.HTTPClient,
		})
	case LLMProviderAnthropic:
		return newAnthropicClient(ctx, ProviderClientConfig{
			Provider:   provider,
			Model:      model,
			APIKey:     cfg.APIKey,
			BaseURL:    cfg.BaseURL,
			HTTPClient: cfg.HTTPClient,
		})
	default:
		if cfg.HTTPClient != nil {
			return NewGeminiClientWithHTTPClient(ctx, cfg.APIKey, model, cfg.HTTPClient)
		}
		return NewGeminiClient(ctx, cfg.APIKey, model)
	}
}

func defaultHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: defaultProviderHTTPTimout}
}

func openAISchemaToMap(schema *genai.Schema) (map[string]any, error) {
	return providerSchemaToMap(schema, providerSchemaOptions{
		lowercaseTypes:  true,
		dropNullable:    true,
		dropExample:     true,
		dropPropertyOrd: true,
	})
}

func anthropicSchemaToMap(schema *genai.Schema) (map[string]any, error) {
	return providerSchemaToMap(schema, providerSchemaOptions{
		lowercaseTypes:  true,
		dropNullable:    true,
		dropExample:     true,
		dropPropertyOrd: true,
	})
}

type providerSchemaOptions struct {
	lowercaseTypes  bool
	dropNullable    bool
	dropExample     bool
	dropPropertyOrd bool
}

func providerSchemaToMap(schema *genai.Schema, opts providerSchemaOptions) (map[string]any, error) {
	if schema == nil {
		return nil, nil
	}
	return providerSchemaNode(schema, opts), nil
}

func providerSchemaNode(schema *genai.Schema, opts providerSchemaOptions) map[string]any {
	if schema == nil {
		return nil
	}

	out := make(map[string]any)
	if len(schema.AnyOf) > 0 {
		anyOf := make([]any, 0, len(schema.AnyOf))
		for _, sub := range schema.AnyOf {
			if node := providerSchemaNode(sub, opts); node != nil {
				anyOf = append(anyOf, node)
			}
		}
		if len(anyOf) > 0 {
			out["anyOf"] = anyOf
		}
	}
	if schema.Default != nil {
		out["default"] = schema.Default
	}
	if schema.Description != "" {
		out["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		out["enum"] = append([]string(nil), schema.Enum...)
	}
	if !opts.dropExample && schema.Example != nil {
		out["example"] = schema.Example
	}
	if schema.Format != "" {
		out["format"] = schema.Format
	}
	if schema.Items != nil {
		out["items"] = providerSchemaNode(schema.Items, opts)
	}
	if schema.MaxItems != nil {
		out["maxItems"] = *schema.MaxItems
	}
	if schema.MaxLength != nil {
		out["maxLength"] = *schema.MaxLength
	}
	if schema.MaxProperties != nil {
		out["maxProperties"] = *schema.MaxProperties
	}
	if schema.Maximum != nil {
		out["maximum"] = *schema.Maximum
	}
	if schema.MinItems != nil {
		out["minItems"] = *schema.MinItems
	}
	if schema.MinLength != nil {
		out["minLength"] = *schema.MinLength
	}
	if schema.MinProperties != nil {
		out["minProperties"] = *schema.MinProperties
	}
	if schema.Minimum != nil {
		out["minimum"] = *schema.Minimum
	}
	if !opts.dropNullable && schema.Nullable != nil {
		out["nullable"] = *schema.Nullable
	}
	if schema.Pattern != "" {
		out["pattern"] = schema.Pattern
	}
	if len(schema.Properties) > 0 {
		props := make(map[string]any, len(schema.Properties))
		for key, value := range schema.Properties {
			props[key] = providerSchemaNode(value, opts)
		}
		out["properties"] = props
	} else if schema.Type == genai.TypeObject {
		out["properties"] = map[string]any{}
	}
	if !opts.dropPropertyOrd && len(schema.PropertyOrdering) > 0 {
		out["propertyOrdering"] = append([]string(nil), schema.PropertyOrdering...)
	}
	if len(schema.Required) > 0 {
		out["required"] = append([]string(nil), schema.Required...)
	}
	if schema.Title != "" {
		out["title"] = schema.Title
	}
	if schema.Type != "" {
		out["type"] = providerSchemaTypeName(schema.Type, opts.lowercaseTypes)
	}
	return out
}

func providerSchemaTypeName(t genai.Type, lowercase bool) string {
	name := string(t)
	if lowercase {
		return strings.ToLower(name)
	}
	return name
}

func providerBaseURL(explicit, envKey, fallback string) string {
	if base := strings.TrimSpace(explicit); base != "" {
		return strings.TrimRight(base, "/")
	}
	if base := strings.TrimSpace(os.Getenv(envKey)); base != "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(fallback, "/")
}

func joinProviderURL(base, versionedPath string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path := strings.TrimSpace(versionedPath)
	if base == "" {
		return path
	}
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		return base + strings.TrimPrefix(path, "/v1")
	}
	return base + path
}
