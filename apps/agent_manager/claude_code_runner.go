package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/claudellm"
	"github.com/original-david-knight/go_wild/my"
)

const (
	claudeCodeRunnerDefaultBudgetUSD    = 5.0
	claudeCodeRunnerCorrectionBudgetUSD = 0.50
)

// claudeSemaphore returns the shared semaphore that limits concurrent
// `claude -p` processes for pipeline steps.
// Capacity is read from CLAUDE_CODE_MAX_CONCURRENT (default 3).
func claudeSemaphore() *gowild_my.Semaphore {
	return gowild_my.EnvSemaphore("CLAUDE_CODE_MAX_CONCURRENT", pipelineRunnerDefaultMaxConcurrent)
}

// executeClaudeCodeStep runs a pipeline step using Claude Code with MCP tools.
// An optional pre-created step run can be passed for fan-out steps where step
// runs are created upfront with "queued" status.
func (pe *PipelineEngine) executeClaudeCodeStep(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any, existingStepRun ...*data.PipelineStepRun) bool {
	methodDef := pe.loadPipelineMethodDefinition(ctx, svc, step.NextMethod)
	model, modelErr := pe.resolveClaudeModel(step, methodDef)
	return pe.executePipelineStepShared(ctx, svc, run, step, stepIdx, result, explicitParams, claudeCodeRunnerSpec(pe, model, modelErr), existingStepRun)
}

// claudeCodeRunnerSpec returns the runner spec wiring Claude Code into
// executePipelineStepShared. modelErr, when non-nil, is surfaced through
// invoke so a missing CLAUDE_{FAST,SMART}_MODEL fails the step-run
// cleanly instead of crashing the pipeline goroutine.
func claudeCodeRunnerSpec(pe *PipelineEngine, model string, modelErr error) pipelineRunnerSpec {
	return pipelineRunnerSpec{
		label:         "claude-code",
		jobIDPrefix:   "claude-code-",
		modelProvider: data.LLMProviderAnthropic,
		missionIntro:  "This is a Claude Code pipeline step.",
		invoke: func(ctx context.Context, _ *PipelineEngine, agentID, prompt, systemPrompt string, activate func()) (string, error) {
			if modelErr != nil {
				return "", modelErr
			}
			if activate != nil {
				activate()
			}
			budget := claudeCodeRunnerDefaultBudgetUSD
			// Format-correction retries use the lower correction budget. We detect
			// this by the DisableAllTools flag which executePipelineStepShared sets
			// on the correction context.
			if v, _ := ctx.Value(pipelineDisableAllToolsKey).(bool); v {
				budget = claudeCodeRunnerCorrectionBudgetUSD
			}
			return pe.invokeClaudeCodeRunner(ctx, agentID, prompt, systemPrompt, model, budget)
		},
		parse:          func(s string) parsedRunnerResult { return toParsedRunnerResult(claudellm.ParseResult(s)) },
		extractFinal:   claudellm.ExtractFinalResponse,
		formatEventLog: claudellm.FormatEventLog,
		buildFailure:   claudellm.BuildFailureArtifacts,
		buildCorrectionPrompt: func(reason, prior string, md *data.A2AMethod) string {
			return buildPipelineCorrectionPrompt(reason, prior, md, pipelineRunnerCorrectionMaxChars)
		},
	}
}

func toParsedRunnerResult(p claudellm.ParsedResult) parsedRunnerResult {
	return parsedRunnerResult{
		Status:        p.Status,
		Payload:       p.Payload,
		FailureReason: p.FailureReason,
		FormatError:   p.FormatError,
	}
}

// ensureAgentModelProvider sets the agent's model_provider if it differs.
func (pe *PipelineEngine) ensureAgentModelProvider(ctx context.Context, agentID, provider string) {
	if agentID == "" || pe.db == nil {
		return
	}
	agentSvc := data.NewAgentService(pe.db, agentID)
	agent, err := agentSvc.GetAgent(ctx)
	if err != nil {
		return
	}
	if strings.TrimSpace(agent.ModelProvider) == provider {
		return
	}
	agent.ModelProvider = provider
	_ = agentSvc.UpdateAgent(ctx, agent)
}

func (pe *PipelineEngine) invokeClaudeCodeRunner(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
	if pe != nil && pe.claudeCodeRunner != nil {
		return pe.claudeCodeRunner(ctx, agentID, prompt, systemPrompt, model, budgetUSD)
	}

	// Limit concurrent claude -p processes.
	sema := claudeSemaphore()
	if err := sema.Acquire(ctx); err != nil {
		return "", err
	}
	defer sema.Release()

	return pe.runClaudeCode(ctx, agentID, prompt, systemPrompt, model, budgetUSD)
}

// runClaudeCode executes `claude -p` with MCP tools pointing at the broker.
func (pe *PipelineEngine) runClaudeCode(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
	claudeExec, err := resolveClaudeExecutable()
	if err != nil {
		return "", err
	}

	// Build MCP config
	disabledTools, _ := ctx.Value(pipelineDisabledToolsKey).([]string)
	disableAllTools, _ := ctx.Value(pipelineDisableAllToolsKey).(bool)
	mcpConfigPath, mcpServerBin, cleanup, err := pe.writeMCPConfig(agentID, BrokerExecutionMethod(ctx), disabledTools, disableAllTools)
	if err != nil {
		return "", fmt.Errorf("failed to write MCP config: %w", err)
	}
	defer cleanup()

	// Write research output style to strip the default SE system prompt.
	outputStylePath, outputStyleCleanup, err := claudellm.WriteResearchOutputStyle()
	if err != nil {
		return "", fmt.Errorf("failed to write output style: %w", err)
	}
	defer outputStyleCleanup()

	timeoutSec := pipelineRunnerDefaultTimeoutSec
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var builtinTools []string
	if disableAllTools {
		builtinTools = []string{}
	}
	cmd, cleanup, err := buildClaudePipelineSandboxCommand(execCtx, prompt, systemPrompt, model, mcpConfigPath, mcpServerBin, claudeExec, budgetUSD, outputStylePath, builtinTools)
	if err != nil {
		return "", err
	}
	defer cleanup()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Pipeline engine: running claude-code (agent=%s, model=%s, budget=%.2f)", agentID, model, budgetUSD)
	log.Printf("Pipeline engine: claude-code prompt: %s", prompt[:min(len(prompt), 200)])

	runErr := cmd.Run()
	log.Printf("Pipeline engine: claude-code finished (stdout=%d bytes, stderr=%d bytes)", stdout.Len(), stderr.Len())
	if stderrStr := strings.TrimSpace(stderr.String()); stderrStr != "" {
		log.Printf("Pipeline engine: claude-code stderr: %s", stderrStr[:min(len(stderrStr), 500)])
	}
	if runErr != nil {
		if stdoutStr := strings.TrimSpace(stdout.String()); stdoutStr != "" {
			log.Printf("Pipeline engine: claude-code stdout: %s", stdoutStr[:min(len(stdoutStr), 500)])
		}
	}
	if execCtx.Err() == context.DeadlineExceeded {
		return stdout.String(), &claudellm.ExecutionError{
			Message: fmt.Sprintf("claude-code timed out after %d seconds", timeoutSec),
			Stdout:  stdout.String(),
			Stderr:  stderr.String(),
		}
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			if exitErr.ExitCode() != 0 {
				errText := strings.TrimSpace(stderr.String())
				if errText == "" {
					errText = fmt.Sprintf("claude exited with code %d", exitErr.ExitCode())
				}
				return stdout.String(), &claudellm.ExecutionError{
					Message:  fmt.Sprintf("claude-code failed (exit %d): %s", exitErr.ExitCode(), errText),
					ExitCode: exitErr.ExitCode(),
					Stdout:   stdout.String(),
					Stderr:   stderr.String(),
				}
			}
		} else {
			errText := strings.TrimSpace(stderr.String())
			if errText == "" {
				errText = runErr.Error()
			}
			return stdout.String(), &claudellm.ExecutionError{
				Message: fmt.Sprintf("failed to execute claude: %s", errText),
				Stdout:  stdout.String(),
				Stderr:  stderr.String(),
			}
		}
	}

	return stdout.String(), nil
}

func buildClaudePipelineSandboxCommand(execCtx context.Context, prompt, systemPrompt, model, mcpConfigPath, mcpServerBin, claudeExecutable string, budgetUSD float64, outputStylePath string, builtinTools []string) (*exec.Cmd, func(), error) {
	bwrapExecutable, err := resolveBwrapExecutable()
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(mcpConfigPath) == "" {
		return nil, nil, fmt.Errorf("mcp config path is required")
	}
	if strings.TrimSpace(mcpServerBin) == "" {
		return nil, nil, fmt.Errorf("mcp broker binary path is required")
	}

	resolvedClaudeExecutable := claudeExecutable
	if realPath, realErr := filepath.EvalSymlinks(claudeExecutable); realErr == nil && strings.TrimSpace(realPath) != "" {
		resolvedClaudeExecutable = realPath
	}

	sandboxHomeHost, homeCleanup, err := prepareClaudeOAuthSandboxHome()
	if err != nil {
		return nil, nil, err
	}

	scratchDir, err := os.MkdirTemp("", "gowild-claude-runner-*")
	if err != nil {
		homeCleanup()
		return nil, nil, fmt.Errorf("failed to create scratch workdir for claude runner: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(scratchDir)
		homeCleanup()
	}

	mcpConfigHost, err := filepath.Abs(mcpConfigPath)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to resolve MCP config path: %w", err)
	}
	mcpServerHost, err := filepath.Abs(mcpServerBin)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to resolve MCP broker binary path: %w", err)
	}
	outputStyleHost := ""
	if strings.TrimSpace(outputStylePath) != "" {
		abs, err := filepath.Abs(outputStylePath)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to resolve output style path: %w", err)
		}
		outputStyleHost = abs
	}

	args := baseBwrapSandboxArgs(claudeCodeTmpfsSizeBytes)
	args = append(args,
		"--ro-bind", resolvedClaudeExecutable, "/sandbox/bin/claude",
		"--ro-bind", mcpServerHost, claudeCodeSandboxMCPBroker,
		"--bind", scratchDir, "/work",
		"--chdir", "/work",
		"--bind", sandboxHomeHost, claudeCodeSandboxHome,
		"--ro-bind", mcpConfigHost, claudeCodeSandboxMCPConfig,
	)
	if outputStyleHost != "" {
		args = append(args, "--ro-bind", outputStyleHost, claudellm.SandboxOutputStylePath)
	}
	args = append(args, baseBwrapEnvArgs(claudeCodeSandboxHome, claudeCodeSandboxPath)...)
	args = append(args, claudeSandboxNonAuthEnvArgs()...)

	claudeArgs := []string{
		"claude",
		"--dangerously-skip-permissions",
		"-p", prompt,
		"--verbose",
		"--output-format", "stream-json",
		"--strict-mcp-config",
		"--mcp-config", claudeCodeSandboxMCPConfig,
		"--disallowedTools", strings.Join(claudeCodeRunnerDisallowedTools, ","),
	}
	if model != "" {
		claudeArgs = append(claudeArgs, "--model", model)
	}
	if budgetUSD > 0 {
		claudeArgs = append(claudeArgs, "--max-budget-usd", fmt.Sprintf("%.2f", budgetUSD))
	}
	if systemPrompt != "" {
		claudeArgs = append(claudeArgs, "--system-prompt", systemPrompt)
	}
	if outputStyleHost != "" {
		claudeArgs = append(claudeArgs, "--settings", fmt.Sprintf(`{"outputStyle":"%s"}`, claudellm.SandboxOutputStylePath))
	}
	if builtinTools != nil {
		claudeArgs = append(claudeArgs, "--tools", strings.Join(builtinTools, ","))
	}
	args = append(args, "--")
	args = append(args, claudeArgs...)

	cmd := exec.CommandContext(execCtx, bwrapExecutable, args...)
	cmd.Dir = scratchDir
	return cmd, cleanup, nil
}

// writeMCPConfig writes a temporary MCP config JSON for Claude Code.
func (pe *PipelineEngine) writeMCPConfig(agentID string, executionMethod string, disabledTools []string, disableAllTools bool) (string, string, func(), error) {
	// Find the mcp-broker-server binary
	mcpServerBin, err := resolveMCPBrokerServerBinary()
	if err != nil {
		return "", "", nil, err
	}

	config := map[string]any{
		"mcpServers": map[string]any{},
	}

	if !disableAllTools {
		// Get broker secret for token generation
		brokerSecret := os.Getenv("BROKER_SECRET")
		if brokerSecret == "" {
			// Try to load from the database (same pattern as loadOrGenerateBrokerSecret)
			if pe.db != nil {
				stored, _ := GetSetting(context.Background(), pe.db, "broker_secret")
				if stored != "" {
					brokerSecret = stored
				}
			}
		}
		if brokerSecret == "" {
			return "", "", nil, fmt.Errorf("BROKER_SECRET not available for MCP config")
		}

		env := map[string]string{
			"AGENT_ID":      agentID,
			"BROKER_SECRET": brokerSecret,
			"BROKER_URL":    resolveClaudeRunnerBrokerURL(),
		}
		if strings.TrimSpace(executionMethod) != "" {
			env["EXECUTION_METHOD"] = strings.TrimSpace(executionMethod)
		}
		if len(disabledTools) > 0 {
			env["DISABLED_TOOLS"] = strings.Join(disabledTools, ",")
		}
		config = map[string]any{
			"mcpServers": map[string]any{
				"gowild": map[string]any{
					"command": claudeCodeSandboxMCPBroker,
					"env":     env,
				},
			},
		}
	}

	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	f, err := os.CreateTemp("", "claude-mcp-*.json")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create MCP config file: %w", err)
	}
	configPath := f.Name()
	cleanup := func() { os.Remove(configPath) }

	if _, err := f.Write(configJSON); err != nil {
		f.Close()
		cleanup()
		return "", "", nil, fmt.Errorf("failed to write MCP config: %w", err)
	}
	f.Close()

	return configPath, mcpServerBin, cleanup, nil
}

func resolveClaudeRunnerBrokerURL() string {
	brokerURL := strings.TrimSpace(os.Getenv("BROKER_URL"))
	if brokerURL == "" {
		brokerURL = "http://localhost:8888"
	}
	return strings.TrimRight(brokerURL, "/")
}

// resolveMCPBrokerServerBinary finds the mcp-broker-server binary.
func resolveMCPBrokerServerBinary() (string, error) {
	// Check PATH first
	if path, err := exec.LookPath("mcp-broker-server"); err == nil {
		return path, nil
	}

	// Check relative to current executable
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		candidate := filepath.Join(execDir, "mcp-broker-server")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Check in common build locations
	candidates := []string{
		"./mcp-broker-server",
		"../mcp-broker-server/mcp-broker-server",
	}
	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs, nil
			}
		}
	}

	return "", fmt.Errorf("mcp-broker-server binary not found (build apps/mcp-broker-server and put it on PATH or next to agent_manager)")
}

// loadAgentSystemPrompt loads the agent's system prompt and soul for Claude Code.
func (pe *PipelineEngine) loadAgentSystemPrompt(ctx context.Context, agentID string) string {
	if agentID == "" || pe.db == nil {
		return ""
	}

	agentSvc := data.NewAgentService(pe.db, agentID)

	var parts []string

	// Load agent soul
	soul, err := agentSvc.GetSoul(ctx)
	if err == nil && soul != nil && strings.TrimSpace(soul.Content) != "" {
		parts = append(parts, "# Agent Identity\n\n"+strings.TrimSpace(soul.Content))
	}

	// Load agent's system prompt
	agent, err := agentSvc.GetAgent(ctx)
	if err == nil && agent != nil && strings.TrimSpace(agent.SystemPrompt) != "" {
		parts = append(parts, "# System Instructions\n\n"+strings.TrimSpace(agent.SystemPrompt))
	}

	// Load agent memory for context
	mem, err := agentSvc.GetMemory(ctx)
	if err == nil && mem != nil && strings.TrimSpace(mem.Content) != "" {
		parts = append(parts, "# Current Memory\n\n"+strings.TrimSpace(mem.Content))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// resolveClaudeModel determines which Claude model to use for a step.
// Methods with model_tier="fast" use CLAUDE_FAST_MODEL; all others use CLAUDE_SMART_MODEL.
// Returns an error if the selected env var is unset — callers must surface
// this through the step-failure path rather than panicking.
func (pe *PipelineEngine) resolveClaudeModel(step PipelineStep, methodDef *data.A2AMethod) (string, error) {
	return resolveTieredEnvConfig(methodDef, "CLAUDE_FAST_MODEL", "CLAUDE_SMART_MODEL", "resolveClaudeModel")
}
