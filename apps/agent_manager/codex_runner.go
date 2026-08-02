package main

import (
	"bufio"
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
	"github.com/original-david-knight/go_wild/codexllm"
	"github.com/original-david-knight/go_wild/my"
)

const (
	codexSandboxHome      = "/sandbox/home"
	codexSandboxMCPBroker = "/sandbox/bin/mcp-broker-server"
	codexSandboxPath      = "/sandbox/bin:/usr/local/bin:/usr/bin:/bin"
	codexTmpfsSizeBytes   = 268435456 // 256MB
)

// codexSemaphore returns the shared semaphore that limits concurrent
// `codex exec` processes for pipeline steps.
func codexSemaphore() *gowild_my.Semaphore {
	return gowild_my.EnvSemaphore("CODEX_MAX_CONCURRENT", pipelineRunnerDefaultMaxConcurrent)
}

// executeCodexStep runs a pipeline step using Codex with MCP tools.
func (pe *PipelineEngine) executeCodexStep(ctx context.Context, svc *data.AgentService, run *data.PipelineRun, step PipelineStep, stepIdx int, result map[string]any, explicitParams map[string]any, existingStepRun ...*data.PipelineStepRun) bool {
	methodDef := pe.loadPipelineMethodDefinition(ctx, svc, step.NextMethod)
	profile, profileErr := pe.resolveCodexProfile(step, methodDef)
	return pe.executePipelineStepShared(ctx, svc, run, step, stepIdx, result, explicitParams, codexRunnerSpec(pe, profile, profileErr), existingStepRun)
}

// codexRunnerSpec returns the runner spec wiring Codex into
// executePipelineStepShared. profileErr, when non-nil, is surfaced through
// invoke so a missing CODEX_{FAST,SMART}_PROFILE fails the step-run cleanly
// instead of crashing the pipeline goroutine.
func codexRunnerSpec(pe *PipelineEngine, profile string, profileErr error) pipelineRunnerSpec {
	return pipelineRunnerSpec{
		label:         "codex",
		jobIDPrefix:   "codex-",
		modelProvider: data.LLMProviderOpenAI,
		missionIntro:  "This is a Codex pipeline step.",
		// Codex defers the "running" transition on fan-out branches until
		// the codex semaphore is acquired, so queued items don't appear as
		// stale-running to the orphan detector.
		deferFanOutActivation: true,
		invoke: func(ctx context.Context, _ *PipelineEngine, agentID, prompt, systemPrompt string, activate func()) (string, error) {
			if profileErr != nil {
				return "", profileErr
			}
			return pe.invokeCodexRunner(ctx, agentID, prompt, systemPrompt, profile, activate)
		},
		parse:          func(s string) parsedRunnerResult { return toParsedCodexResult(codexllm.ParseResult(s)) },
		extractFinal:   codexllm.ExtractFinalResponse,
		formatEventLog: codexllm.FormatEventLog,
		buildFailure:   codexllm.BuildFailureArtifacts,
		buildCorrectionPrompt: func(reason, prior string, md *data.A2AMethod) string {
			return buildPipelineCorrectionPrompt(reason, prior, md, pipelineRunnerCorrectionMaxChars)
		},
	}
}

func toParsedCodexResult(p codexllm.ParsedResult) parsedRunnerResult {
	return parsedRunnerResult{
		Status:        p.Status,
		Payload:       p.Payload,
		FailureReason: p.FailureReason,
		FormatError:   p.FormatError,
	}
}

// invokeCodexRunner runs codex for a pipeline step, subject to the codex
// semaphore. onAcquire (may be nil) is called after the semaphore is held and
// before real work begins; executePipelineStepShared uses it to defer the
// step-run "running" transition for fan-out branches until they're actually
// about to execute. Linear (non-fan-out) codex steps activate synchronously
// inside executePipelineStepShared and pass nil for onAcquire on this code
// path; the correction-retry invoke also passes nil.
//
// Semaphore-vs-status ordering within this function:
//
//   - onAcquire and the caller's post-invoke updates both run in the same
//     goroutine; invoke blocks until it returns, so the two mutation sites
//     are sequential, not concurrent.
//   - If ctx is cancelled before sema.Acquire succeeds, onAcquire is NOT
//     called — the pre-created fan-out step-run stays in its queued status
//     so the orphan detector (which scans status="running" rows) does not
//     match it.
//   - When onAcquire does run, it orders markClaudeJobActive BEFORE the DB
//     status write so the orphan detector's inhibitor set contains the
//     jobID at the first instant any other goroutine can observe the
//     "running" row via failOrphanedClaudeCodeStepRun's isClaudeJobActive
//     check, closing that specific window.
//   - The test seam (pe.codexRunner != nil) bypasses the semaphore but still
//     calls onAcquire before executing, preserving the same ordering contract
//     for tests that assert on step-run transitions.
//
// Out of scope for this function: activateStepRun also writes the backing
// localA2AJob row AFTER UpdateStepRun, so a poll of processCompletions that
// lands between those two writes observes status="running" but gets
// ErrLocalA2AJobNotFound from queue.GetJob and hard-fails the run with
// "pipeline step job not found" — the orphan-detector guards above do not
// cover that window. Window is microseconds vs. second-scale poll interval,
// and the fix (reorder the writes or make them atomic) belongs in
// executePipelineStepShared, not here; tracked as a pre-existing race.
func (pe *PipelineEngine) invokeCodexRunner(ctx context.Context, agentID, prompt, systemPrompt, profile string, onAcquire func()) (string, error) {
	if pe != nil && pe.codexRunner != nil {
		if onAcquire != nil {
			onAcquire()
		}
		return pe.codexRunner(ctx, agentID, prompt, systemPrompt, profile)
	}

	sema := codexSemaphore()
	if err := sema.Acquire(ctx); err != nil {
		return "", err
	}
	defer sema.Release()

	if onAcquire != nil {
		onAcquire()
	}

	return pe.runCodex(ctx, agentID, prompt, systemPrompt, profile)
}

// runCodex executes `codex exec` with MCP tools pointing at the broker.
func (pe *PipelineEngine) runCodex(ctx context.Context, agentID, prompt, systemPrompt, profile string) (string, error) {
	codexExec, err := resolveCodexExecutable()
	if err != nil {
		return "", err
	}

	disabledTools, _ := ctx.Value(pipelineDisabledToolsKey).([]string)
	disableAllTools, _ := ctx.Value(pipelineDisableAllToolsKey).(bool)
	mcpConfigPath, mcpServerBin, cleanup, err := pe.writeCodexMCPConfig(agentID, BrokerExecutionMethod(ctx), disabledTools, disableAllTools)
	if err != nil {
		return "", fmt.Errorf("failed to write MCP config: %w", err)
	}
	defer cleanup()

	timeoutSec := pipelineRunnerDefaultTimeoutSec
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd, cmdCleanup, err := buildCodexPipelineSandboxCommand(execCtx, prompt, systemPrompt, profile, mcpConfigPath, mcpServerBin, codexExec, disableAllTools)
	if err != nil {
		return "", err
	}
	defer cmdCleanup()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	log.Printf("Pipeline engine: running codex (agent=%s, profile=%s)", agentID, profile)
	log.Printf("Pipeline engine: codex prompt: %s", prompt[:min(len(prompt), 200)])

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start codex: %w", err)
	}

	// Drain stderr in background.
	var stderr bytes.Buffer
	go func() {
		s := bufio.NewScanner(stderrPipe)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			line := s.Text()
			stderr.WriteString(line)
			stderr.WriteByte('\n')
			log.Printf("Pipeline engine: codex stderr: %s", line)
		}
	}()

	// Stream stdout, logging events as they arrive.
	var stdout bytes.Buffer
	started := time.Now()
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')
		codexLogPipelineEvent(agentID, line, time.Since(started))
	}

	runErr := cmd.Wait()
	elapsed := time.Since(started).Round(time.Millisecond)
	log.Printf("Pipeline engine: codex finished in %s (stdout=%d bytes, stderr=%d bytes)", elapsed, stdout.Len(), stderr.Len())
	if execCtx.Err() == context.DeadlineExceeded {
		return stdout.String(), &codexllm.ExecutionError{
			Message: fmt.Sprintf("codex timed out after %d seconds", timeoutSec),
			Stdout:  stdout.String(),
			Stderr:  stderr.String(),
		}
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			if exitErr.ExitCode() != 0 {
				errText := strings.TrimSpace(stderr.String())
				if errText == "" {
					errText = fmt.Sprintf("codex exited with code %d", exitErr.ExitCode())
				}
				return stdout.String(), &codexllm.ExecutionError{
					Message:  fmt.Sprintf("codex failed (exit %d): %s", exitErr.ExitCode(), errText),
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
			return stdout.String(), &codexllm.ExecutionError{
				Message: fmt.Sprintf("failed to execute codex: %s", errText),
				Stdout:  stdout.String(),
				Stderr:  stderr.String(),
			}
		}
	}

	return stdout.String(), nil
}

func buildCodexPipelineSandboxCommand(execCtx context.Context, prompt, systemPrompt, profile, mcpConfigPath, mcpServerBin, codexExecutable string, disableAllTools bool) (*exec.Cmd, func(), error) {
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

	scratchDir, err := os.MkdirTemp("", "gowild-codex-runner-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create scratch workdir for codex runner: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(scratchDir)
	}

	// Resolve host ~/.codex directory for config.toml and auth.json.
	hostCodexDir := filepath.Join(os.Getenv("HOME"), ".codex")

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

	args := baseBwrapSandboxArgs(codexTmpfsSizeBytes)
	args = append(args,
		"--ro-bind", mcpServerHost, codexSandboxMCPBroker,
		"--bind", scratchDir, "/work",
		"--chdir", "/work",
		// Codex needs config.toml (merged: profiles + MCP broker) and auth.json (OAuth credentials).
		"--dir", codexSandboxHome+"/.codex",
		"--ro-bind", mcpConfigHost, codexSandboxHome+"/.codex/config.toml",
		"--ro-bind-try", filepath.Join(hostCodexDir, "auth.json"), codexSandboxHome+"/.codex/auth.json",
	)
	args = append(args, baseBwrapEnvArgs(codexSandboxHome, codexSandboxPath)...)
	args = append(args, codexSandboxEnvArgs()...)

	// Codex has no --system-prompt flag; fold systemPrompt into the user
	// prompt with JSON-encoded isolation (see codexllm.WrapSystemPrompt).
	fullPrompt := codexllm.WrapSystemPrompt(systemPrompt, prompt)

	// Codex is a Node.js app — it needs its full node_modules tree.
	// Since /usr is already bind-mounted, run codex from its installed location.
	//
	// Why --dangerously-bypass-approvals-and-sandbox is safe here:
	//
	// Threat model. The flag lets codex (and any process it spawns, including
	// tool calls driven by the model) execute arbitrary commands and write
	// arbitrary files without approval prompts or codex's own seccomp/landlock
	// restrictions. We accept that threat because codex is already running
	// inside a bwrap jail constructed in baseBwrapSandboxArgs above, and that
	// jail — not codex — is the authoritative isolation boundary for this
	// code path. The jail is scoped to filesystem / process / capability
	// containment; it is deliberately NOT a network or broker-authorization
	// boundary (see "Intentional non-isolation" below for what that means).
	//
	// Concrete properties the bwrap jail gives us, all of which remain in force
	// regardless of what codex does internally:
	//   - Filesystem writes: the only writable host-visible path is /work
	//     (scratchDir on the host, scoped per-invocation and removed by
	//     cleanup). /tmp is a sized tmpfs discarded on sandbox exit, and
	//     --dir entries under /sandbox live on the ephemeral sandbox rootfs
	//     — writable but invisible to the host. /usr, /bin, /lib, /lib64,
	//     and the selective /etc allowlist are ro-bind, so even a malicious
	//     tool call cannot overwrite host binaries, config, or
	//     ca-certificates. No --dev-bind, and no --bind other than the
	//     per-invocation /work mapping.
	//   - No host /etc secrets: /etc/shadow, /etc/sudoers, /etc/ssh, etc. are
	//     not bound in (see selectiveEtcBinds), so they are simply absent
	//     inside the sandbox — unreadable at any UID.
	//   - Process isolation: --unshare-pid gives codex its own PID namespace
	//     (it cannot signal, ptrace, or see host processes); --unshare-uts
	//     and --unshare-ipc block host-metadata leaks and SysV IPC access;
	//     --unshare-cgroup-try virtualizes the cgroup hierarchy view so the
	//     process cannot read or navigate the host's cgroup tree (it does
	//     NOT by itself enforce resource limits — the parent cgroup's
	//     limits do that).
	//   - User namespace escape prevention: --disable-userns +
	//     --assert-userns-disabled mean a child cannot enter its own userns
	//     to gain capabilities; the sandbox runs as the host user's real UID.
	//   - Env-var scrubbing: --clearenv drops the host env; only HOME, PATH,
	//     LANG, LC_ALL, and the codex-specific vars from codexSandboxEnvArgs
	//     (OPENAI_API_KEY, proxy vars) are exposed through the environment.
	//     Note that this is narrower than "no secrets in the sandbox":
	//     BROKER_SECRET and AGENT_ID are deliberately delivered via the
	//     mounted ~/.codex/config.toml so the codex-driven MCP broker child
	//     can authenticate — see writeCodexMCPConfig. They reach the
	//     sandbox as a ro-bound file, not as env vars.
	// Codex's own sandbox would merely duplicate a strict subset of these
	// properties inside the same jail, so disabling it does not weaken the
	// isolation the deployment actually depends on.
	//
	// Intentional non-isolation. bwrap does NOT --unshare-net here: network
	// access is required (codex reaches the OpenAI API, the MCP broker child
	// reaches the manager's broker service). Two corollaries a reader MUST
	// keep in mind:
	//   1. This sandbox is NOT a network-exfiltration boundary. A rogue tool
	//      invocation can make arbitrary outbound HTTP(S) calls from inside
	//      the jail.
	//   2. When MCP is enabled, this sandbox is NOT a broker-authorization
	//      boundary either. writeCodexMCPConfig deliberately mounts a
	//      config.toml containing BROKER_SECRET so the codex-driven MCP
	//      broker child can authenticate to the manager. From the broker's
	//      perspective, traffic originating in this sandbox is a legitimate,
	//      fully-authorized client. Broker HMAC gates unauthenticated
	//      outsiders, not sandboxed code we chose to hand the secret to.
	//      Note also that the default BROKER_URL is plain http://localhost
	//      (see writeCodexMCPConfig); TLS is not inherent to the broker
	//      path and does not feature in this argument.
	// The risk that codex + a malicious model exfiltrates data or abuses the
	// broker API is part of the accepted design. Mitigations for that risk
	// live outside the sandbox: prompt isolation (codexllm.WrapSystemPrompt),
	// per-tool authorization checks on the broker side, and ops-level rate
	// limits. The sandbox's job is to keep the damage confined to the
	// ephemeral jail filesystem and the sandbox's own process tree.
	//
	// Why unconditional (not gated on MCP). Keeping codex's sandbox active
	// would block MCP broker child processes from launching when tools are
	// enabled, which was the original motivation to add the bypass flag.
	// But we pass it even when tools are disabled, because the redundancy
	// argument above stands regardless: codex's sandbox buys nothing the
	// outer jail doesn't already guarantee, and gating the flag on
	// disableAllTools would create two subtly different runtime behaviors
	// (and a future footgun if someone enables MCP without re-reading this
	// comment).
	//
	// Host-side (non-bwrap) callers. Code paths that invoke codex directly
	// on the host with the user's real privileges (not inside bwrap) MUST
	// NOT use this flag — their only sandbox is codex's own. Use
	// gowild_codexllm.Client there; it defaults SandboxMode to "read-only"
	// and never passes --dangerously-bypass-approvals-and-sandbox.
	codexArgs := []string{
		codexExecutable,
		"exec",
		"--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		"--ephemeral",
	}
	if profile != "" {
		codexArgs = append(codexArgs, "-p", profile)
	}
	// MCP broker is configured via the sandbox config.toml, not via -c flags.
	codexArgs = append(codexArgs, fullPrompt)
	args = append(args, "--")
	args = append(args, codexArgs...)

	cmd := exec.CommandContext(execCtx, bwrapExecutable, args...)
	cmd.Dir = scratchDir
	return cmd, cleanup, nil
}

// writeCodexMCPConfig writes a temporary codex config.toml that merges the
// host's config (profiles, model settings) with the MCP broker server definition.
// Returns the path to the merged config.toml, the MCP broker server binary path,
// and a cleanup function.
func (pe *PipelineEngine) writeCodexMCPConfig(agentID string, executionMethod string, disabledTools []string, disableAllTools bool) (string, string, func(), error) {
	mcpServerBin, err := resolveMCPBrokerServerBinary()
	if err != nil {
		return "", "", nil, err
	}

	// Read the host config.toml as a base.
	hostConfigPath := filepath.Join(os.Getenv("HOME"), ".codex", "config.toml")
	hostConfig, _ := os.ReadFile(hostConfigPath)
	sanitizedHost := sanitizeHostCodexConfigForMerge(hostConfig)

	var mcpSection string
	if !disableAllTools {
		brokerSecret := os.Getenv("BROKER_SECRET")
		if brokerSecret == "" {
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

		brokerURL := os.Getenv("BROKER_URL")
		if brokerURL == "" {
			brokerURL = "http://localhost:8888"
		}

		var envLines strings.Builder
		envPairs := map[string]string{
			"AGENT_ID":      agentID,
			"BROKER_SECRET": brokerSecret,
			"BROKER_URL":    brokerURL,
		}
		if executionMethod != "" {
			envPairs["EXECUTION_METHOD"] = executionMethod
		}
		if len(disabledTools) > 0 {
			envPairs["DISABLED_TOOLS"] = strings.Join(disabledTools, ",")
		}
		for k, v := range envPairs {
			fmt.Fprintf(&envLines, "%s = %q\n", k, v)
		}

		mcpSection = fmt.Sprintf("[mcp_servers.broker]\ncommand = %q\ntool_timeout_sec = 600\n\n[mcp_servers.broker.env]\n%s",
			codexSandboxMCPBroker, envLines.String())
	}

	// sanitizedHost is "" or ends with exactly one "\n", so concatenation
	// always produces a well-formed section boundary even when the host
	// config had no trailing newline or was missing entirely.
	mergedConfig := sanitizedHost + mcpSection

	f, err := os.CreateTemp("", "codex-config-*.toml")
	if err != nil {
		return "", "", nil, err
	}
	path := f.Name()
	if _, err := f.WriteString(mergedConfig); err != nil {
		f.Close()
		os.Remove(path)
		return "", "", nil, err
	}
	f.Close()
	return path, mcpServerBin, func() { os.Remove(path) }, nil
}

// sanitizeHostCodexConfigForMerge returns a host codex config.toml prepared for
// concatenation with a freshly-generated [mcp_servers.broker] section:
//
//   - Any existing [mcp_servers.broker] or [mcp_servers.broker.<sub>] sections
//     are stripped. Without this, appending our section would produce a TOML
//     file with duplicate tables and codex would fail to parse it. Variants
//     like `[mcp_servers . broker]` and `[mcp_servers."broker"]` — which
//     TOML treats as the same table — are normalized before matching.
//   - The result is either empty or ends with exactly one "\n", so the caller
//     can append an arbitrary section without worrying about whether the host
//     file had a trailing newline.
//
// Limitations (the line-based approach does not implement a full TOML parser):
//   - Multi-line strings are not tracked. If a stale broker section contains
//     a triple-quoted string whose contents include a line that looks like a
//     TOML header (e.g. `[profiles.fast]`), the sanitizer treats that inner
//     line as a real header, stops skipping, and leaves the remainder of the
//     stale block in the output.
//   - Only header-form broker tables are stripped. If the host expresses the
//     broker via a top-level dotted key (`mcp_servers.broker.command = ...`)
//     or an inline table under `[mcp_servers]` (`broker = { ... }`), the
//     merged config will still conflict with our appended section. Codex's
//     own docs use the header form, and no tooling we ship emits the other
//     shapes, so this is tracked as a known limitation rather than handled.
func sanitizeHostCodexConfigForMerge(hostConfig []byte) string {
	if len(hostConfig) == 0 {
		return ""
	}
	lines := strings.Split(string(hostConfig), "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		name, isHeader := extractTOMLSectionName(line)
		if isHeader {
			if name == "mcp_servers.broker" || strings.HasPrefix(name, "mcp_servers.broker.") {
				skipping = true
				continue
			}
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}
	joined := strings.Join(out, "\n")
	joined = strings.TrimRight(joined, "\n")
	if joined == "" {
		return ""
	}
	return joined + "\n"
}

// extractTOMLSectionName returns the normalized dotted name inside a TOML
// section header line (e.g. "mcp_servers.broker" for "[mcp_servers.broker]")
// and whether the line is a section header at all. Recognises both `[name]`
// and `[[name]]` (array-of-tables) forms; anything following the closing
// bracket (e.g. a trailing `# comment`) is ignored.
//
// Normalization: TOML treats `[a.b]`, `[a . b]`, `[a."b"]`, and `["a".b]` as
// references to the same table, so the returned name strips whitespace and
// surrounding ASCII quotes around each dot-delimited segment. Without this
// the sanitizer would miss the whitespace/quoted variants and produce a
// duplicate [mcp_servers.broker] table in the merged config.
func extractTOMLSectionName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	var raw string
	if strings.HasPrefix(trimmed, "[[") {
		end := strings.Index(trimmed, "]]")
		if end < 2 {
			return "", false
		}
		raw = trimmed[2:end]
	} else {
		end := strings.Index(trimmed, "]")
		if end < 1 {
			return "", false
		}
		raw = trimmed[1:end]
	}
	return normalizeTOMLKey(raw), true
}

// normalizeTOMLKey canonicalizes a dotted TOML key by trimming whitespace
// around each segment and stripping surrounding ASCII quotes (" or ').
// Simple dot-split: does not preserve a literal "." inside a quoted segment,
// but that's a truly pathological form for a key under `mcp_servers.*` and
// none of the MCP tooling we ship emits it.
func normalizeTOMLKey(raw string) string {
	segments := strings.Split(raw, ".")
	for i, s := range segments {
		s = strings.TrimSpace(s)
		if len(s) >= 2 {
			first, last := s[0], s[len(s)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				s = s[1 : len(s)-1]
			}
		}
		segments[i] = s
	}
	return strings.Join(segments, ".")
}

func resolveCodexExecutable() (string, error) {
	return codexllm.FindExecutable()
}

// resolveCodexProfile returns the codex config profile for a pipeline step.
// Methods with model_tier="fast" use CODEX_FAST_PROFILE; all others use CODEX_SMART_PROFILE.
// Returns an error if the selected env var is unset — callers must surface
// this through the step-failure path rather than panicking.
func (pe *PipelineEngine) resolveCodexProfile(step PipelineStep, methodDef *data.A2AMethod) (string, error) {
	return resolveTieredEnvConfig(methodDef, "CODEX_FAST_PROFILE", "CODEX_SMART_PROFILE", "resolveCodexProfile")
}

// codexSandboxEnvArgs returns bwrap --setenv args for env vars that codex needs.
func codexSandboxEnvArgs() []string {
	keys := []string{
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
	}
	var args []string
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			args = append(args, "--setenv", key, val)
		}
	}
	return args
}

// codexLogPipelineEvent logs codex JSONL events as they stream in,
// giving visibility into long-running pipeline steps.
func codexLogPipelineEvent(agentID, line string, elapsed time.Duration) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	var event struct {
		Type string `json:"type"`
		Item *struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Message string `json:"message"`
		} `json:"item,omitempty"`
		Message string `json:"message,omitempty"`
		Usage   *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}
	elapsedStr := elapsed.Round(time.Second).String()
	switch event.Type {
	case "turn.started":
		log.Printf("Pipeline engine: codex [%s] turn started (%s)", agentID, elapsedStr)
	case "item.completed":
		if event.Item != nil {
			switch event.Item.Type {
			case "agent_message":
				textLen := len(event.Item.Text)
				preview := event.Item.Text
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				log.Printf("Pipeline engine: codex [%s] response received (%d chars, %s): %s", agentID, textLen, elapsedStr, preview)
			case "tool_call":
				log.Printf("Pipeline engine: codex [%s] tool call (%s)", agentID, elapsedStr)
			case "tool_output":
				log.Printf("Pipeline engine: codex [%s] tool output (%s)", agentID, elapsedStr)
			case "error":
				log.Printf("Pipeline engine: codex [%s] item error: %s (%s)", agentID, event.Item.Message, elapsedStr)
			default:
				log.Printf("Pipeline engine: codex [%s] item %s (%s)", agentID, event.Item.Type, elapsedStr)
			}
		}
	case "turn.completed":
		if event.Usage != nil {
			log.Printf("Pipeline engine: codex [%s] turn completed (input=%d, output=%d, %s)", agentID, event.Usage.InputTokens, event.Usage.OutputTokens, elapsedStr)
		} else {
			log.Printf("Pipeline engine: codex [%s] turn completed (%s)", agentID, elapsedStr)
		}
	case "error":
		log.Printf("Pipeline engine: codex [%s] error: %s (%s)", agentID, event.Message, elapsedStr)
	case "turn.failed":
		log.Printf("Pipeline engine: codex [%s] turn FAILED: %s (%s)", agentID, event.Message, elapsedStr)
	}
}
