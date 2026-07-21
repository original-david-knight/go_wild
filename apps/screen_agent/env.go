package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
)

// LoadUserConfigEnv loads ~/.config/screen-agent/.env, the credential file
// written by installed deployments that run outside a git checkout. Variables
// already present in the environment (or loaded from the git-root .env) are
// not overridden.
func LoadUserConfigEnv() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".config", "screen-agent", ".env")
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	_ = godotenv.Load(path)
	return path
}

func LoadGitRootEnv() string {
	if wd, err := os.Getwd(); err == nil {
		if path := gitRootEnvFrom(wd); path != "" {
			_ = godotenv.Load(path)
			return path
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		if path := gitRootEnvFrom(filepath.Dir(file)); path != "" {
			_ = godotenv.Load(path)
			return path
		}
	}
	return ""
}

func gitRootEnvFrom(start string) string {
	root := findGitRoot(start)
	if root == "" {
		return ""
	}
	path := filepath.Join(root, ".env")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func findGitRoot(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
