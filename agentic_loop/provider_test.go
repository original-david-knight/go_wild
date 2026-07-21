package gowild_agentic_loop

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeLLMProvider(t *testing.T) {
	cases := map[string]string{
		"":                       LLMProviderGemini,
		"gemini":                 LLMProviderGemini,
		"  Gemini  ":             LLMProviderGemini,
		"openai":                 LLMProviderOpenAI,
		"OpenAI":                 LLMProviderOpenAI,
		"anthropic":              LLMProviderAnthropic,
		"something-unrecognized": LLMProviderGemini,
	}
	for in, want := range cases {
		if got := NormalizeLLMProvider(in); got != want {
			t.Errorf("NormalizeLLMProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInferLLMProviderFromModel(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"gemini-3-flash":  LLMProviderGemini,
		"Gemini-3-Pro":    LLMProviderGemini,
		"gpt-5.4":         LLMProviderOpenAI,
		"o1-preview":      LLMProviderOpenAI,
		"o3-mini":         LLMProviderOpenAI,
		"o4":              LLMProviderOpenAI,
		"claude-opus-4-7": LLMProviderAnthropic,
		"llama-3":         "",
	}
	for in, want := range cases {
		if got := InferLLMProviderFromModel(in); got != want {
			t.Errorf("InferLLMProviderFromModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModelMatchesProvider(t *testing.T) {
	type tc struct {
		provider, model string
		want            bool
	}
	for _, c := range []tc{
		{LLMProviderGemini, "gemini-3-flash", true},
		{LLMProviderGemini, "", true},            // empty model is neutral
		{LLMProviderGemini, "custom-name", true}, // unknown model is neutral
		{LLMProviderGemini, "gpt-5", false},
		{LLMProviderOpenAI, "gpt-5", true},
		{LLMProviderOpenAI, "claude-opus", false},
		{LLMProviderAnthropic, "claude-opus", true},
	} {
		if got := modelMatchesProvider(c.provider, c.model); got != c.want {
			t.Errorf("modelMatchesProvider(%q,%q) = %v, want %v", c.provider, c.model, got, c.want)
		}
	}
}

func TestNormalizeModelForProvider(t *testing.T) {
	if got := NormalizeModelForProvider(LLMProviderGemini, "gpt-5"); got != "" {
		t.Errorf("mismatched model should be dropped, got %q", got)
	}
	if got := NormalizeModelForProvider(LLMProviderOpenAI, "gpt-5"); got != "gpt-5" {
		t.Errorf("matching model should pass through, got %q", got)
	}
	if got := NormalizeModelForProvider(LLMProviderOpenAI, "custom-proxy-model"); got != "custom-proxy-model" {
		t.Errorf("unknown model should pass through for proxy compatibility, got %q", got)
	}
	if got := NormalizeModelForProvider(LLMProviderOpenAI, "   "); got != "" {
		t.Errorf("whitespace-only model should become empty, got %q", got)
	}
}

func TestResolveLLMProvider(t *testing.T) {
	// Explicit provider wins even with a mismatching model hint.
	if got := resolveLLMProvider("openai", "claude-opus"); got != LLMProviderOpenAI {
		t.Errorf("explicit provider should win, got %q", got)
	}
	// Model hint is used when provider is blank.
	if got := resolveLLMProvider("", "claude-opus-4-7"); got != LLMProviderAnthropic {
		t.Errorf("model hint should resolve to anthropic, got %q", got)
	}
	// Falls back to Gemini when neither provider nor hints resolve.
	if got := resolveLLMProvider("", "llama-3", "mystery"); got != LLMProviderGemini {
		t.Errorf("unresolvable inputs should fall back to gemini, got %q", got)
	}
	// First resolvable hint wins.
	if got := resolveLLMProvider("", "", "gpt-5.4"); got != LLMProviderOpenAI {
		t.Errorf("first resolvable hint should win, got %q", got)
	}
}

func TestNormalizeOpenAIAuthMode(t *testing.T) {
	cases := map[string]string{
		"":            OpenAIAuthModeAPIKey,
		"api_key":     OpenAIAuthModeAPIKey,
		"API_KEY":     OpenAIAuthModeAPIKey,
		"codex_oauth": OpenAIAuthModeCodexOAuth,
		"garbage":     OpenAIAuthModeAPIKey,
	}
	for in, want := range cases {
		if got := NormalizeOpenAIAuthMode(in); got != want {
			t.Errorf("NormalizeOpenAIAuthMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	if got := DefaultModelForProvider(LLMProviderOpenAI); got != DefaultOpenAIModel {
		t.Errorf("openai default = %q, want %q", got, DefaultOpenAIModel)
	}
	if got := DefaultModelForProvider(LLMProviderAnthropic); got != DefaultAnthropicModel {
		t.Errorf("anthropic default = %q, want %q", got, DefaultAnthropicModel)
	}
	if got := DefaultModelForProvider(LLMProviderGemini); got != DefaultModel {
		t.Errorf("gemini default = %q, want %q", got, DefaultModel)
	}
}

func TestDefaultSmartModelForProvider(t *testing.T) {
	// Non-gemini providers reuse the caller's base model when one is supplied.
	if got := DefaultSmartModelForProvider(LLMProviderOpenAI, "gpt-5.7"); got != "gpt-5.7" {
		t.Errorf("openai smart = %q, want base passthrough", got)
	}
	// When base is empty, falls back to the provider default.
	if got := DefaultSmartModelForProvider(LLMProviderAnthropic, ""); got != DefaultAnthropicModel {
		t.Errorf("anthropic smart fallback = %q, want %q", got, DefaultAnthropicModel)
	}
	// Gemini always uses the dedicated smart model regardless of base.
	if got := DefaultSmartModelForProvider(LLMProviderGemini, "gemini-3-flash"); got != DefaultGeminiSmartModel {
		t.Errorf("gemini smart = %q, want %q", got, DefaultGeminiSmartModel)
	}
}

func TestNewProviderClient_RoutesByProvider(t *testing.T) {
	ctx := context.Background()

	// Gemini: NewGeminiClient doesn't require an API key up front, so this
	// succeeds even without credentials.
	c, err := NewProviderClient(ctx, ProviderClientConfig{Provider: LLMProviderGemini, APIKey: "x"})
	if err != nil {
		t.Fatalf("gemini route: %v", err)
	}
	if _, ok := c.(*GeminiClient); !ok {
		t.Fatalf("gemini route returned %T, want *GeminiClient", c)
	}

	// OpenAI routing: constructor requires an API key. Passing one inline
	// proves the dispatcher reached newOpenAIClient.
	c, err = NewProviderClient(ctx, ProviderClientConfig{
		Provider: LLMProviderOpenAI,
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("openai route: %v", err)
	}
	if _, ok := c.(*openAIClient); !ok {
		t.Fatalf("openai route returned %T, want *openAIClient", c)
	}

	// Anthropic routing: same pattern.
	c, err = NewProviderClient(ctx, ProviderClientConfig{
		Provider: LLMProviderAnthropic,
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("anthropic route: %v", err)
	}
	if _, ok := c.(*anthropicClient); !ok {
		t.Fatalf("anthropic route returned %T, want *anthropicClient", c)
	}
}

func TestNewProviderClient_InfersProviderFromModel(t *testing.T) {
	// With no Provider field set, the model should route the call.
	c, err := NewProviderClient(context.Background(), ProviderClientConfig{
		Model:  "claude-opus-4-7",
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("inferred anthropic route: %v", err)
	}
	if _, ok := c.(*anthropicClient); !ok {
		t.Fatalf("inferred anthropic route returned %T, want *anthropicClient", c)
	}
}

func TestHTTPStatusError(t *testing.T) {
	e := &httpStatusError{Provider: "openai", Code: 429, Message: "rate limited"}
	msg := e.Error()
	if !strings.Contains(msg, "openai") || !strings.Contains(msg, "429") || !strings.Contains(msg, "rate limited") {
		t.Errorf("Error() = %q, missing provider/code/message", msg)
	}

	// Falls back to Body when Message is blank.
	e = &httpStatusError{Provider: "anthropic", Code: 500, Body: "upstream crash"}
	if msg := e.Error(); !strings.Contains(msg, "upstream crash") {
		t.Errorf("Error() should surface Body when Message is empty, got %q", msg)
	}

	// Falls back to a bare status line when both are empty.
	e = &httpStatusError{Provider: "gemini", Code: 404}
	if msg := e.Error(); !strings.Contains(msg, "404") {
		t.Errorf("bare Error() should contain code, got %q", msg)
	}
}
