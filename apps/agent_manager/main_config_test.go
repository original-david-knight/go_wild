package main

import (
	"testing"

	obj "github.com/original-david-knight/go_wild/objectives"
)

func TestResolveAgentDBURLPrecedence(t *testing.T) {
	t.Setenv("GOWILD_DATABASE_URL", "postgres://env")

	if got := resolveAgentDBURL("postgres://flag"); got != "postgres://flag" {
		t.Fatalf("expected flag value to win, got %q", got)
	}

	if got := resolveAgentDBURL(""); got != "postgres://env" {
		t.Fatalf("expected env value when flag empty, got %q", got)
	}

	t.Setenv("GOWILD_DATABASE_URL", "")
	if got := resolveAgentDBURL(""); got != defaultAgentDBURL {
		t.Fatalf("expected default DB URL %q, got %q", defaultAgentDBURL, got)
	}
}

func TestLoadObjectivesConfigFromEnvFallbacks(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gem-key")
	t.Setenv("OBJECTIVES_MODEL", "")
	t.Setenv("OBJECTIVES_SMART_MODEL", "")
	t.Setenv("FAST_MODEL", "fast-fallback")
	t.Setenv("SMART_MODEL", "smart-fallback")

	cfg := loadObjectivesConfigFromEnv()
	if cfg.GeminiAPIKey != "gem-key" {
		t.Fatalf("unexpected Gemini key: %q", cfg.GeminiAPIKey)
	}
	if cfg.Model != "fast-fallback" {
		t.Fatalf("expected fast fallback model, got %q", cfg.Model)
	}
	if cfg.SmartModel != "smart-fallback" {
		t.Fatalf("expected smart fallback model, got %q", cfg.SmartModel)
	}
}

func TestLoadObjectivesConfigFromEnvPrefersObjectivesVars(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gem-key")
	t.Setenv("OBJECTIVES_MODEL", "objectives-fast")
	t.Setenv("OBJECTIVES_SMART_MODEL", "objectives-smart")
	t.Setenv("FAST_MODEL", "fast-fallback")
	t.Setenv("SMART_MODEL", "smart-fallback")

	cfg := loadObjectivesConfigFromEnv()
	if cfg.Model != "objectives-fast" {
		t.Fatalf("expected objectives model override, got %q", cfg.Model)
	}
	if cfg.SmartModel != "objectives-smart" {
		t.Fatalf("expected objectives smart model override, got %q", cfg.SmartModel)
	}
}

func TestObjectivesModelsConfigured(t *testing.T) {
	if objectivesModelsConfigured(obj.Config{}) {
		t.Fatalf("expected empty config to be disabled")
	}

	if objectivesModelsConfigured(obj.Config{
		Model: "fast",
	}) {
		t.Fatalf("expected missing smart model to be disabled")
	}

	if !objectivesModelsConfigured(obj.Config{
		Model:      "fast",
		SmartModel: "smart",
	}) {
		t.Fatalf("expected config with both models to be enabled")
	}
}
