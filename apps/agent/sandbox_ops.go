package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// StopSandbox stops an agent's container.
func StopSandbox(ctx context.Context, agentID string) error {
	name := containerName(agentID)

	status := ContainerStatus(ctx, agentID)
	if status != "running" {
		fmt.Println(color.YellowString("Container %s is not running", name))
		return nil
	}

	fmt.Println(color.CyanString("Stopping container %s...", name))

	cmd := exec.CommandContext(ctx, "docker", "stop", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w - %s", err, stderr.String())
	}

	fmt.Println(color.GreenString("Container %s stopped", name))
	return nil
}

// RemoveSandbox removes an agent's container (but keeps the volume).
func RemoveSandbox(ctx context.Context, agentID string) error {
	name := containerName(agentID)

	// Stop if running
	if status := ContainerStatus(ctx, agentID); status == "running" {
		if err := StopSandbox(ctx, agentID); err != nil {
			return err
		}
	}

	// Remove container
	cmd := exec.CommandContext(ctx, "docker", "rm", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Ignore "no such container" errors
		if !strings.Contains(stderr.String(), "No such container") {
			return fmt.Errorf("failed to remove container: %w - %s", err, stderr.String())
		}
	}

	fmt.Println(color.GreenString("Container %s removed", name))
	return nil
}

// PurgeSandbox removes both the container and volume for an agent.
func PurgeSandbox(ctx context.Context, agentID string) error {
	// Remove container first
	if err := RemoveSandbox(ctx, agentID); err != nil {
		return err
	}

	// Remove volume
	volName := volumeName(agentID)
	cmd := exec.CommandContext(ctx, "docker", "volume", "rm", volName)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Ignore "no such volume" errors
		if !strings.Contains(stderr.String(), "No such volume") {
			return fmt.Errorf("failed to remove volume: %w - %s", err, stderr.String())
		}
	}

	fmt.Println(color.GreenString("Volume %s removed", volName))
	return nil
}

// ListSandboxes lists all agent sandbox containers.
func ListSandboxes(ctx context.Context) ([]SandboxInfo, error) {
	// List containers with our prefix
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "name="+containerPrefix,
		"--format", "{{.Names}}\t{{.Status}}\t{{.CreatedAt}}")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var sandboxes []SandboxInfo
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) >= 2 {
			agentID := strings.TrimPrefix(parts[0], containerPrefix)
			sandboxes = append(sandboxes, SandboxInfo{
				AgentID:   agentID,
				Container: parts[0],
				Status:    parts[1],
			})
		}
	}

	return sandboxes, nil
}

// ExecInSandbox executes a command in a running agent sandbox.
func ExecInSandbox(ctx context.Context, agentID string, command []string, interactive bool) error {
	name := containerName(agentID)

	status := ContainerStatus(ctx, agentID)
	if status != "running" {
		return fmt.Errorf("container %s is not running (status: %s)", name, status)
	}

	args := []string{"exec"}
	if interactive {
		args = append(args, "-it")
	}
	args = append(args, name)
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if interactive {
		cmd.Stdin = os.Stdin
	}

	return cmd.Run()
}

// LogsSandbox shows logs from an agent's container.
func LogsSandbox(ctx context.Context, agentID string, follow bool, tail int) error {
	name := containerName(agentID)

	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	args = append(args, name)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// handleSandboxList lists all agent sandbox containers.
func handleSandboxList(ctx context.Context) error {
	sandboxes, err := ListSandboxes(ctx)
	if err != nil {
		return err
	}

	if len(sandboxes) == 0 {
		fmt.Println("No sandbox containers found.")
		return nil
	}

	fmt.Println(color.CyanString("Sandbox Containers:"))
	for _, sb := range sandboxes {
		statusColor := color.YellowString
		if strings.Contains(strings.ToLower(sb.Status), "up") {
			statusColor = color.GreenString
		}
		fmt.Printf("  %s - %s\n", color.WhiteString(sb.AgentID), statusColor(sb.Status))
	}
	return nil
}

func buildSandboxLLMEnv(ctx context.Context, runtime *agentRuntime) (llmSessionConfig, map[string]string, error) {
	cfg, err := resolveModelConfig(ctx, runtime)
	if err != nil {
		return llmSessionConfig{}, nil, err
	}

	envVars := make(map[string]string)
	if cfg.Provider == "" {
		return llmSessionConfig{}, nil, fmt.Errorf("llm provider is not configured")
	}

	switch cfg.Provider {
	case loop.ProviderOpenAI:
		if cfg.OpenAIAuthMode == "" {
			return llmSessionConfig{}, nil, fmt.Errorf("openai auth mode is not configured")
		}
		if cfg.OpenAIAuthMode == loop.OpenAIAuthModeAPIKey {
			if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
				envVars["OPENAI_API_KEY"] = apiKey
			} else {
				return llmSessionConfig{}, nil, fmt.Errorf("OPENAI_API_KEY not set")
			}
		}
		for _, key := range []string{
			"OPENAI_BASE_URL",
			"OPENAI_ORG_ID",
			"OPENAI_PROJECT_ID",
			openAIFastModelEnv,
			openAIFastModelAltEnv,
			openAISmartModelEnv,
			openAISmartModelAltEnv,
		} {
			if value := os.Getenv(key); value != "" {
				envVars[key] = value
			}
		}
		if cfg.OpenAIAuthMode == loop.OpenAIAuthModeCodexOAuth {
			if authFile := os.Getenv("OPENAI_CODEX_AUTH_FILE"); authFile != "" {
				envVars["OPENAI_CODEX_AUTH_FILE"] = authFile
			} else if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
				envVars["OPENAI_CODEX_AUTH_FILE"] = filepath.Join(homeDir, ".codex", "auth.json")
			}
		}
	case loop.ProviderAnthropic:
		if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
			envVars["ANTHROPIC_API_KEY"] = apiKey
		} else {
			return llmSessionConfig{}, nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
			envVars["ANTHROPIC_BASE_URL"] = baseURL
		}
		for _, key := range []string{claudeFastModelEnv, claudeSmartModelEnv} {
			if value := os.Getenv(key); value != "" {
				envVars[key] = value
			}
		}
	case loop.ProviderGemini:
		// Pass API keys only when running standalone (no broker socket available).
		if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
			envVars["GEMINI_API_KEY"] = apiKey
		} else {
			return llmSessionConfig{}, nil, fmt.Errorf("GEMINI_API_KEY not set")
		}
		if smartModel := strings.TrimSpace(os.Getenv("SMART_MODEL")); smartModel != "" {
			envVars["SMART_MODEL"] = smartModel
		}
	default:
		return llmSessionConfig{}, nil, fmt.Errorf("unsupported llm provider %q", cfg.Provider)
	}

	return cfg, envVars, nil
}

func sandboxLLMFlags(cfg llmSessionConfig) []string {
	var flags []string

	if provider := strings.TrimSpace(cfg.Provider); provider != "" {
		flags = append(flags, "-provider", provider)
	}
	if strings.TrimSpace(cfg.Provider) == loop.ProviderOpenAI && strings.TrimSpace(cfg.OpenAIAuthMode) != "" {
		flags = append(flags, "-openai-auth", cfg.OpenAIAuthMode)
	}
	if baseModel := strings.TrimSpace(cfg.BaseModel); baseModel != "" {
		flags = append(flags, "-model", baseModel)
	}
	if smartModel := strings.TrimSpace(cfg.SmartModel); smartModel != "" {
		flags = append(flags, "-smart-model", smartModel)
	}

	return flags
}

// runInSandbox runs the agent in a sandboxed Docker container.
func runInSandbox(ctx context.Context, agentID string) error {
	// Ensure the sandbox image is built
	if err := EnsureSandboxImage(ctx, *sandboxRebuildFlag); err != nil {
		return fmt.Errorf("failed to ensure sandbox image: %w", err)
	}

	runtime := initializeAgentRuntime(ctx, agentID)
	defer runtime.close()
	if err := runtime.startupError(); err != nil {
		return err
	}

	// Collect environment variables to pass to container.
	// Agents use Unix socket for broker communication — no HTTP URLs or API keys needed.
	llmConfig, envVars, err := buildSandboxLLMEnv(ctx, runtime)
	if err != nil {
		return err
	}

	// Build extra flags to pass to the agent
	extraFlags := sandboxLLMFlags(llmConfig)
	if *systemFlag != "" {
		extraFlags = append(extraFlags, "-system", *systemFlag)
	}
	if *maxTurns != 10 {
		extraFlags = append(extraFlags, "-max-turns", fmt.Sprintf("%d", *maxTurns))
	}
	if *heartbeatInterval != 15*time.Minute {
		extraFlags = append(extraFlags, "-heartbeat", heartbeatInterval.String())
	}
	if *workTasksTimeout != defaultWorkTasksTimeout {
		extraFlags = append(extraFlags, "-worktasks-timeout", workTasksTimeout.String())
	}
	if *compactAt != 200000 {
		extraFlags = append(extraFlags, "-compact-at", fmt.Sprintf("%d", *compactAt))
	}
	if *responseTimeout != 5*time.Minute {
		extraFlags = append(extraFlags, "-response-timeout", responseTimeout.String())
	}
	if *smartFlag {
		extraFlags = append(extraFlags, "-smart")
	}
	if *logFlag {
		extraFlags = append(extraFlags, "-log")
	}

	// Debug: show what flags are being passed
	fmt.Println(color.HiBlackString("[DEBUG] heartbeatInterval=%v, extraFlags=%v", *heartbeatInterval, extraFlags))

	// Mount host logs/ directory into container when -log is set
	var extraMounts []string
	if *logFlag {
		logsDir, _ := filepath.Abs("logs")
		os.MkdirAll(logsDir, 0755)
		extraMounts = append(extraMounts, logsDir+":/data/logs")
	}

	// Create sandbox config
	config := SandboxConfig{
		AgentID:     agentID,
		EnvVars:     envVars,
		ExtraFlags:  extraFlags,
		ExtraMounts: extraMounts,
		Rebuild:     *sandboxRebuildFlag,
		AttachStdin: true,
		Background:  *sandboxBgFlag,
	}

	return StartSandbox(ctx, config)
}
