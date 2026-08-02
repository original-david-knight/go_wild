package objectives_planner

import (
	"fmt"
	"os"

	"github.com/original-david-knight/go_wild/my"
)

// Config holds all configuration for the objectives system.
type Config struct {
	DatabaseURL    string
	GeminiAPIKey   string
	Model          string // execution model (FAST_MODEL)
	SmartModel     string // planning model (SMART_MODEL)
	MaxConcurrency int
	ListenAddr     string
}

// NewConfig creates a Config from explicit values (for embedding in another process).
func NewConfig(geminiAPIKey, model, smartModel string) Config {
	maxConc := 4
	return Config{
		GeminiAPIKey:   geminiAPIKey,
		Model:          model,
		SmartModel:     smartModel,
		MaxConcurrency: maxConc,
	}
}

// LoadConfig reads configuration from environment variables.
// It panics if any required variable is missing.
func LoadConfig() Config {
	gowild_my.LoadEnv()

	cfg := Config{
		DatabaseURL:    requireEnv("OBJECTIVES_DATABASE_URL", "GOWILD_DATABASE_URL", "DATABASE_URL"),
		GeminiAPIKey:   requireEnv("GEMINI_API_KEY"),
		Model:          requireEnv("OBJECTIVES_MODEL", "FAST_MODEL"),
		SmartModel:     requireEnv("OBJECTIVES_SMART_MODEL", "SMART_MODEL"),
		MaxConcurrency: 4,
		ListenAddr:     gowild_my.GetEnvOrDefault("OBJECTIVES_LISTEN", ":9090"),
	}

	return cfg
}

// requireEnv returns the value of the first non-empty env var from names.
// Panics if none are set.
func requireEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	panic(fmt.Sprintf("required env var not set: %v", names))
}
