package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// RunPythonInput defines the input for the Python execution tool.
type RunPythonInput struct {
	Code    string `json:"code" description:"Python code to execute" required:"true"`
	Timeout int    `json:"timeout" description:"Timeout in seconds (default 30, max 120)" required:"false"`
}

// PythonTools provides Python code execution tools.
type PythonTools struct {
	useDocker  bool   // Use Docker for sandboxed execution
	pythonPath string // Path to python binary (when not using Docker)
}

const pythonImage = "python:3.12-slim"

// NewPythonTools creates a new PythonTools instance.
// It tries Docker first for sandboxed execution, then falls back to direct Python.
func NewPythonTools() (*PythonTools, error) {
	// Check if docker is available and working
	cmd := exec.Command("docker", "version")
	if err := cmd.Run(); err == nil {
		return &PythonTools{
			useDocker: true,
		}, nil
	}

	// Docker not available, try direct Python
	pythonPath, err := findPython()
	if err != nil {
		return nil, fmt.Errorf("neither docker nor python available: %w", err)
	}

	return &PythonTools{
		useDocker:  false,
		pythonPath: pythonPath,
	}, nil
}

// findPython looks for a Python interpreter.
func findPython() (string, error) {
	// Try python3 first, then python
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err == nil {
			// Verify it works
			cmd := exec.Command(path, "--version")
			if err := cmd.Run(); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("python not found in PATH")
}

// Close is a no-op for CLI-based implementation.
func (p *PythonTools) Close() error {
	return nil
}

// UsesDocker returns true if Python runs in Docker containers.
func (p *PythonTools) UsesDocker() bool {
	return p.useDocker
}

// ensureImage pulls the Python image if not already present.
func (p *PythonTools) ensureImage(ctx context.Context) error {
	if !p.useDocker {
		return nil // No image needed for direct execution
	}

	// Check if image exists
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", pythonImage)
	if err := cmd.Run(); err == nil {
		return nil // Image exists
	}

	// Pull the image
	cmd = exec.CommandContext(ctx, "docker", "pull", pythonImage)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image: %v - %s", err, stderr.String())
	}

	return nil
}

// RunPythonTool executes Python code.
// Uses Docker for sandboxed execution when available, otherwise runs Python directly.
func (p *PythonTools) RunPythonTool(ctx context.Context, input RunPythonInput) (*loop.ToolResult, error) {
	if input.Code == "" {
		return loop.NewErrorResult("code is required"), nil
	}

	// Set timeout (default 30s, max 120s)
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 120 {
		timeout = 120
	}

	if p.useDocker {
		return p.runWithDocker(ctx, input.Code, timeout)
	}
	return p.runDirect(ctx, input.Code, timeout)
}

// runWithDocker executes Python code in a Docker container.
func (p *PythonTools) runWithDocker(ctx context.Context, code string, timeout int) (*loop.ToolResult, error) {
	// Ensure image is available
	if err := p.ensureImage(ctx); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to prepare Python environment: %v", err)), nil
	}

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Build docker run command with security constraints
	args := []string{
		"run",
		"--rm",                                // Remove container after run
		"--user=65534:65534",                  // Run as nobody (non-root)
		"--memory=256m",                       // Memory limit
		"--memory-swap=256m",                  // No swap
		"--cpus=1",                            // CPU limit
		"--pids-limit=100",                    // Process limit
		"--read-only",                         // Read-only filesystem
		"--tmpfs=/tmp:rw,size=128m,uid=65534", // Writable /tmp for Python
		"--tmpfs=/home:rw,size=64m,uid=65534", // Writable /home for pip cache
		"--security-opt=no-new-privileges",    // Prevent privilege escalation
		"--cap-drop=ALL",                      // Drop all capabilities
		pythonImage,
		"python", "-c", code,
	}

	cmd := exec.CommandContext(execCtx, "docker", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Check for timeout
	if execCtx.Err() == context.DeadlineExceeded {
		return loop.NewErrorResult(fmt.Sprintf("execution timed out after %d seconds", timeout)), nil
	}

	return p.buildResult(stdout, stderr, err)
}

// runDirect executes Python code directly (used when running inside a container).
func (p *PythonTools) runDirect(ctx context.Context, code string, timeout int) (*loop.ToolResult, error) {
	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, p.pythonPath, "-c", code)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Check for timeout
	if execCtx.Err() == context.DeadlineExceeded {
		return loop.NewErrorResult(fmt.Sprintf("execution timed out after %d seconds", timeout)), nil
	}

	return p.buildResult(stdout, stderr, err)
}

// buildResult constructs a ToolResult from command output.
func (p *PythonTools) buildResult(stdout, stderr bytes.Buffer, err error) (*loop.ToolResult, error) {
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Command itself failed
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
		result["error"] = fmt.Sprintf("Python exited with code %d", exitCode)
	}

	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider for tool descriptions.
func (p *PythonTools) DescribeTool(name string) string {
	mode := "Docker container"
	if !p.useDocker {
		mode = "direct execution"
	}
	descriptions := map[string]string{
		"run_python": fmt.Sprintf("Execute Python code (%s). STATELESS: Each execution runs in a fresh environment - all code, variables, and files are lost after execution. Only stdout/stderr output is returned. Has network access for pip installs and web requests. 30s default timeout (max 120s). Use print() to return results. You can pip install packages inline: subprocess.run(['pip', 'install', 'requests']). For reusable code with dependencies, use save_skill instead.", mode),
	}
	return descriptions[name]
}
