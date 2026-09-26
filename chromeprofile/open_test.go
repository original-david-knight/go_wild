package chromeprofile

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLocalStatePathIsUnderHome(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got, want := LocalStatePath(), "/home/someone/.config/google-chrome/Local State"; got != want {
		t.Fatalf("LocalStatePath = %q, want %q", got, want)
	}
	t.Setenv("HOME", "")
	if got := LocalStatePath(); got != "" {
		t.Fatalf("LocalStatePath with no home = %q, want empty", got)
	}
}

// TestOpenPassesTheProfileAndURL swaps the browser for a script that writes
// its arguments into a FIFO; reading the FIFO blocks until the script has run.
func TestOpenPassesTheProfileAndURL(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "args")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "fake-chrome")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + fifo + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	old := browserPath
	browserPath = script
	t.Cleanup(func() { browserPath = old })

	if err := Open("Profile 1", "https://mail.example/inbox?x=1"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "--profile-directory=Profile 1\nhttps://mail.example/inbox?x=1\n"
	if string(got) != want {
		t.Fatalf("browser got args %q, want %q", got, want)
	}
}

func TestOpenReportsAMissingBrowser(t *testing.T) {
	old := browserPath
	browserPath = filepath.Join(t.TempDir(), "no-chrome")
	t.Cleanup(func() { browserPath = old })

	err := Open("Default", "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("Open with no browser = %v, want a not-found error", err)
	}
}
