// Package gowild_my provides shared utilities for GoWild applications.
package gowild_my

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/joho/godotenv"
)

var (
	loadEnvOnce sync.Once
	loadedPath  string
)

// LoadEnv loads environment variables from .env files.
// It searches for .env files starting from the current directory
// and walking up parent directories until it finds one or reaches the git root.
// This function is safe to call multiple times - it only loads once.
// Returns the path to the loaded .env file, or empty string if none found.
func LoadEnv() string {
	loadEnvOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		envPath := findEnvFileFrom(dir)
		if envPath != "" {
			_ = godotenv.Load(envPath)
			loadedPath = envPath
		}
	})
	return loadedPath
}

func findEnvFileFrom(dir string) string {
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}

		if isGitRootDir(dir) {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}
	return ""
}

func isGitRootDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// GetEnvOrDefault returns the value of an environment variable,
// or the default value if not set.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
