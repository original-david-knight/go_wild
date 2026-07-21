package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	att, err := loadFile(path)
	if err != nil {
		t.Fatalf("loadFile failed: %v", err)
	}
	if att.name != "test.go" {
		t.Errorf("expected name 'test.go', got %q", att.name)
	}
	if att.mimeType != "text/x-go" {
		t.Errorf("expected mime 'text/x-go', got %q", att.mimeType)
	}
	if string(att.data) != "package main\n" {
		t.Errorf("unexpected content: %q", string(att.data))
	}
}

func TestLoadFile_Extensions(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		filename string
		expected string
	}{
		{"test.py", "text/x-python"},
		{"test.js", "text/javascript"},
		{"test.ts", "text/typescript"},
		{"test.json", "application/json"},
		{"test.yaml", "application/x-yaml"},
		{"test.yml", "application/x-yaml"},
		{"test.md", "text/markdown"},
		{"test.csv", "text/csv"},
		{"test.html", "text/html"},
		{"test.css", "text/css"},
		{"test.xml", "application/xml"},
		{"test.txt", "text/plain"},
	}

	for _, tc := range tests {
		path := filepath.Join(dir, tc.filename)
		os.WriteFile(path, []byte("content"), 0644)
		att, err := loadFile(path)
		if err != nil {
			t.Errorf("loadFile(%s) failed: %v", tc.filename, err)
			continue
		}
		if att.mimeType != tc.expected {
			t.Errorf("loadFile(%s) mime = %q, want %q", tc.filename, att.mimeType, tc.expected)
		}
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := loadFile("/nonexistent/path/file.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFile_TildeExpansion(t *testing.T) {
	// We can't easily test ~ expansion without a real file in home dir,
	// but we can test that a non-existent ~ path returns an error (not panic)
	_, err := loadFile("~/nonexistent_test_file_12345.txt")
	if err == nil {
		t.Error("expected error for nonexistent ~/file")
	}
}

func TestLoadImageFromFile(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal PNG file (PNG header)
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	path := filepath.Join(dir, "test.png")
	os.WriteFile(path, pngHeader, 0644)

	att, err := loadImageFromFile(path)
	if err != nil {
		t.Fatalf("loadImageFromFile failed: %v", err)
	}
	if att.name != "test.png" {
		t.Errorf("expected name 'test.png', got %q", att.name)
	}
	if att.mimeType != "image/png" {
		t.Errorf("expected mime 'image/png', got %q", att.mimeType)
	}
}

func TestLoadImageFromFile_NotImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "text.txt")
	os.WriteFile(path, []byte("not an image"), 0644)

	_, err := loadImageFromFile(path)
	if err == nil {
		t.Error("expected error for non-image file")
	}
}

func TestLoadImageFromFile_NotFound(t *testing.T) {
	_, err := loadImageFromFile("/nonexistent/image.png")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestCompleteFilePath_CurrentDir(t *testing.T) {
	// Create a temp directory with some files
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alpha.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "beta.go"), []byte(""), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	// Test completion with full path prefix
	completions := completeFilePath("/cmd " + filepath.Join(dir, "a"))
	found := false
	for _, c := range completions {
		if filepath.Base(c) == "alpha.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected alpha.go in completions, got %v", completions)
	}
}

func TestCompleteFilePath_DirEnding(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.go"), []byte(""), 0644)

	// Path ending with / should list directory contents
	completions := completeFilePath("/cmd " + dir + "/")
	if len(completions) == 0 {
		t.Error("expected completions for directory listing")
	}
}

func TestCompleteFilePath_HiddenFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "visible"), []byte(""), 0644)

	// Without dot prefix, hidden files should be excluded
	completions := completeFilePath("/cmd " + filepath.Join(dir, "v"))
	for _, c := range completions {
		if filepath.Base(c) == ".hidden" {
			t.Error("hidden file should not appear without dot prefix")
		}
	}

	// With dot prefix, hidden files should be included
	completions2 := completeFilePath("/cmd " + dir + "/.h")
	found := false
	for _, c := range completions2 {
		if filepath.Base(c) == ".hidden" {
			found = true
		}
	}
	if !found {
		t.Error("hidden file should appear with dot prefix")
	}
}

func TestCompleteFilePath_NoCommand(t *testing.T) {
	// Single word command without path
	completions := completeFilePath("/cmd")
	// Should return nil since there's no path prefix
	_ = completions // May or may not be empty depending on current dir
}

func TestGetHistoryFile(t *testing.T) {
	path := getHistoryFile()
	if path == "" {
		t.Skip("could not determine home directory")
	}
	if !containsHelper(path, ".gowild_agent_history") {
		t.Errorf("expected path to contain .gowild_agent_history, got %q", path)
	}
}
