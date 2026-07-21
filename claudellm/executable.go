package claudellm

import (
	"fmt"
	"os/exec"
	"strings"
)

// FindExecutable locates the Claude CLI binary ("claude" or "claude-code") in PATH.
func FindExecutable() (string, error) {
	for _, name := range []string{"claude", "claude-code"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("claude executable not found in PATH (tried: %s)", strings.Join([]string{"claude", "claude-code"}, ", "))
}

// FindBwrapExecutable locates the bubblewrap (bwrap) binary in PATH.
func FindBwrapExecutable() (string, error) {
	path, err := exec.LookPath("bwrap")
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf("bwrap executable not found; claude sandboxing requires bubblewrap")
}
