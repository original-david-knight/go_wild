package main

import (
	"os"
	"runtime"
	"testing"
)

// socketSafeDir returns a directory usable as XDG_RUNTIME_DIR in tests that
// bind AF_UNIX sockets. On Windows, t.TempDir() paths embed the test name and
// exceed the 108-byte sockaddr_un limit, so a short directory under the
// system temp dir is used instead.
func socketSafeDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp(os.TempDir(), "sa")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
