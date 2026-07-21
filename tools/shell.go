package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// RunShellInput defines the input for the shell command tool.
type RunShellInput struct {
	Command string `json:"command" description:"Shell command to execute" required:"true"`
	Timeout int    `json:"timeout" description:"Timeout in seconds (default 30, max 300)" required:"false"`
}

// ShellTools provides shell command execution tools.
// Only available when running inside a container (sandboxed environment).
type ShellTools struct{}

// NewShellTools creates a new ShellTools instance.
// Returns nil if not running in a container.
func NewShellTools() *ShellTools {
	if !IsInContainer() {
		return nil
	}
	return &ShellTools{}
}

// IsInContainer returns true if the current process is running inside a Docker container.
func IsInContainer() bool {
	// Method 1: Check for /.dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Method 2: Check cgroup for docker/container indicators
	data, err := os.ReadFile("/proc/1/cgroup")
	if err == nil {
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "containerd") ||
			strings.Contains(content, "kubepods") {
			return true
		}
	}

	// Method 3: Check for container environment variable (set in Dockerfile)
	if os.Getenv("GOWILD_IN_CONTAINER") == "1" {
		return true
	}

	return false
}

// RunShellTool executes a shell command.
// Only available when running inside a container for security.
func (s *ShellTools) RunShellTool(ctx context.Context, input RunShellInput) (*loop.ToolResult, error) {
	if input.Command == "" {
		return loop.NewErrorResult("command is required"), nil
	}

	// Set timeout (default 30s, max 300s for longer operations)
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 300 {
		timeout = 300
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Execute command through bash
	cmd := exec.CommandContext(execCtx, "bash", "-c", input.Command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set working directory to /data (the persistent volume)
	cmd.Dir = "/data"

	err := cmd.Run()

	// Check for timeout
	if execCtx.Err() == context.DeadlineExceeded {
		return loop.NewErrorResult(fmt.Sprintf("command timed out after %d seconds", timeout)), nil
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return loop.NewErrorResult(fmt.Sprintf("execution error: %v - %s", err, stderr.String())), nil
		}
	}

	result := map[string]any{
		"stdout":    strings.TrimRight(stdout.String(), "\n"),
		"stderr":    strings.TrimRight(stderr.String(), "\n"),
		"exit_code": exitCode,
		"success":   exitCode == 0,
	}

	if exitCode != 0 {
		result["error"] = fmt.Sprintf("command exited with code %d", exitCode)
	}

	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (s *ShellTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"run_shell": "Execute a shell command in the sandboxed container. Available tools include: curl, wget, git, grep, sed, awk, jq, and more. Working directory is /data (persistent volume). Use this for file operations, network requests, and system tasks. 30s default timeout (max 300s). Returns stdout, stderr, and exit code.",
	}
	return descriptions[name]
}
