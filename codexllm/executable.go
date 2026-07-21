package codexllm

import (
	"fmt"
	"os/exec"
)

// FindExecutable locates the Codex CLI binary in PATH.
func FindExecutable() (string, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("codex executable not found in PATH")
	}
	return path, nil
}
