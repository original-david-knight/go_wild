package gowild_my

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindEnvFileFrom(t *testing.T) {
	// Create a nested directory structure with .env at the top
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	if err := os.WriteFile(envPath, []byte("TEST=1"), 0644); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	// Create nested directories
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}

	// Search from nested directory should find .env in parent
	found := findEnvFileFrom(nestedDir)
	if found != envPath {
		t.Errorf("expected to find %s, got %s", envPath, found)
	}
}

func TestFindEnvFileFrom_NotFound(t *testing.T) {
	// Create a directory without .env
	tmpDir := t.TempDir()

	found := findEnvFileFrom(tmpDir)
	if found != "" {
		t.Errorf("expected empty string, got %s", found)
	}
}

func TestFindEnvFileFrom_StopsAtGitRoot(t *testing.T) {
	parent := t.TempDir()
	parentEnvPath := filepath.Join(parent, ".env")
	if err := os.WriteFile(parentEnvPath, []byte("OUTSIDE=1"), 0644); err != nil {
		t.Fatalf("failed to write parent .env: %v", err)
	}

	repoDir := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "a", "b"), 0755); err != nil {
		t.Fatalf("failed to create repo dirs: %v", err)
	}
	// Simulate git root marker.
	if err := os.WriteFile(filepath.Join(repoDir, ".git"), []byte("gitdir"), 0644); err != nil {
		t.Fatalf("failed to write .git marker: %v", err)
	}

	found := findEnvFileFrom(filepath.Join(repoDir, "a", "b"))
	if found != "" {
		t.Errorf("expected empty result due to git-root boundary, got %s", found)
	}
}

func TestFindEnvFileFrom_CurrentDir(t *testing.T) {
	// Create .env in the search directory itself
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")

	if err := os.WriteFile(envPath, []byte("TEST=1"), 0644); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	found := findEnvFileFrom(tmpDir)
	if found != envPath {
		t.Errorf("expected to find %s, got %s", envPath, found)
	}
}

func TestLoadEnv(t *testing.T) {
	// LoadEnv uses sync.Once on a package global, so this test must run once
	// in the process. Chdir to a sandboxed directory with a .git marker so
	// the search stops inside our tmpdir instead of walking up into the real
	// repo's .env file.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".git"), []byte("gitdir"), 0644); err != nil {
		t.Fatalf("failed to write .git marker: %v", err)
	}
	envPath := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envPath, []byte("LOAD_ENV_TEST_VAR=loaded"), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	os.Unsetenv("LOAD_ENV_TEST_VAR")
	defer os.Unsetenv("LOAD_ENV_TEST_VAR")

	t.Chdir(tmpDir)

	loaded := LoadEnv()
	if loaded != envPath {
		t.Errorf("expected loaded path %q, got %q", envPath, loaded)
	}
	if val := os.Getenv("LOAD_ENV_TEST_VAR"); val != "loaded" {
		t.Errorf("expected env var loaded='loaded', got %q", val)
	}

	// Subsequent calls must return the cached path (sync.Once doesn't re-run,
	// but the cached result is still reported).
	if again := LoadEnv(); again != envPath {
		t.Errorf("second LoadEnv returned %q, want cached %q", again, envPath)
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	os.Setenv("TEST_EXISTS", "exists")
	defer os.Unsetenv("TEST_EXISTS")

	if val := GetEnvOrDefault("TEST_EXISTS", "default"); val != "exists" {
		t.Errorf("expected 'exists', got '%s'", val)
	}

	if val := GetEnvOrDefault("TEST_NOT_EXISTS", "default"); val != "default" {
		t.Errorf("expected 'default', got '%s'", val)
	}
}
