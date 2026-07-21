package main

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// loadEnvFiles loads .env files from the current working directory up to the git
// root, nearest-first. godotenv never overrides a variable that is already set, so
// a closer .env (the app's own, holding e.g. the wallet seed) takes precedence
// over a parent (the repo-root .env, holding shared settings like the Polymarket
// VPN proxy). This lets the app pick up the shared proxy from the repo root while
// keeping app-specific overrides local. It returns the paths it loaded, nearest
// first.
func loadEnvFiles() []string {
	dir, err := os.Getwd()
	if err != nil {
		return nil
	}
	var loaded []string
	for {
		p := filepath.Join(dir, ".env")
		if _, statErr := os.Stat(p); statErr == nil {
			if godotenv.Load(p) == nil {
				loaded = append(loaded, p)
			}
		}
		if _, gitErr := os.Stat(filepath.Join(dir, ".git")); gitErr == nil {
			break // reached the repo root
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return loaded
}
