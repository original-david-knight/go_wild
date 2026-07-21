package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitRootEnvFromUsesRepoRootOnly(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	nested := filepath.Join(repo, "apps", "screen_agent")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootEnv := filepath.Join(repo, ".env")
	if err := os.WriteFile(rootEnv, []byte("GEMINI_API_KEY=root-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".env"), []byte("GEMINI_API_KEY=nested-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := gitRootEnvFrom(nested)
	if got != rootEnv {
		t.Fatalf("gitRootEnvFrom = %q, want %q", got, rootEnv)
	}
}

func TestLoadUserConfigEnvLoadsGeminiAPIKey(t *testing.T) {
	home := t.TempDir()
	// os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".config", "screen-agent")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envPath, []byte("GEMINI_API_KEY=from-config-dir\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	old, hadOld := os.LookupEnv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if hadOld {
			os.Setenv("GEMINI_API_KEY", old)
		} else {
			os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	if loaded := LoadUserConfigEnv(); loaded != envPath {
		t.Fatalf("LoadUserConfigEnv = %q, want %q", loaded, envPath)
	}
	if got := os.Getenv("GEMINI_API_KEY"); got != "from-config-dir" {
		t.Fatalf("GEMINI_API_KEY = %q, want from-config-dir", got)
	}
}

func TestLoadUserConfigEnvDoesNotOverrideExistingValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GEMINI_API_KEY", "already-set")

	configDir := filepath.Join(home, ".config", "screen-agent")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("GEMINI_API_KEY=from-config-dir\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if loaded := LoadUserConfigEnv(); loaded == "" {
		t.Fatalf("LoadUserConfigEnv should report the loaded path")
	}
	if got := os.Getenv("GEMINI_API_KEY"); got != "already-set" {
		t.Fatalf("GEMINI_API_KEY = %q, want already-set to win", got)
	}
}

func TestLoadGitRootEnvLoadsGeminiAPIKey(t *testing.T) {
	repo := t.TempDir()
	nested := filepath.Join(repo, "apps", "screen_agent")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(repo, ".env")
	if err := os.WriteFile(envPath, []byte("GEMINI_API_KEY=from-root\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	old, hadOld := os.LookupEnv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if hadOld {
			os.Setenv("GEMINI_API_KEY", old)
		} else {
			os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	t.Chdir(nested)
	if loaded := LoadGitRootEnv(); loaded != envPath {
		t.Fatalf("LoadGitRootEnv = %q, want %q", loaded, envPath)
	}
	if got := os.Getenv("GEMINI_API_KEY"); got != "from-root" {
		t.Fatalf("GEMINI_API_KEY = %q, want from-root", got)
	}
}
