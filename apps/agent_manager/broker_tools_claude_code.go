package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
	"github.com/original-david-knight/go_wild/claudellm"
	"github.com/original-david-knight/go_wild/tools"
)

const (
	claudeCodeContainerRoot      = "/data"
	claudeCodeDefaultTimeoutSecs = 600
	claudeCodeMaxTimeoutSecs     = 3600
	claudeCodeSandboxHome        = "/home/sandbox"
	claudeCodeSandboxPath        = "/sandbox/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	claudeCodeTmpfsSizeBytes     = 268435456 // 256MiB
	claudeCodeSandboxMCPConfig   = "/sandbox/config/mcp.json"
	claudeCodeSandboxMCPBroker   = "/sandbox/bin/mcp-broker-server"
)

var claudeCodeRunnerDisallowedTools = []string{
	"Read",
	"Edit",
	"MultiEdit",
	"Write",
	"LS",
	"Glob",
	"Grep",
	"Bash",
	"WebFetch",
}

type claudeCodeToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error)

var claudeCodeToolHandlers = map[string]claudeCodeToolHandlerFunc{
	"claude_code": func(h *BrokerToolsHandler, ctx context.Context, agentID string, inputJSON []byte) (any, error) {
		return callWithInput[tools.ClaudeCodeInput](inputJSON, func(input tools.ClaudeCodeInput) (any, error) {
			prompt := strings.TrimSpace(input.Prompt)
			if prompt == "" {
				return nil, fmt.Errorf("prompt is required")
			}

			timeoutSecs := normalizeClaudeCodeTimeout(input.Timeout)

			containerDir, err := mapClaudeTargetDirectoryToContainerPath(input.TargetDirectory)
			if err != nil {
				return nil, err
			}

			hostDir, err := h.mapClaudeContainerPathToHost(ctx, agentID, containerDir)
			if err != nil {
				return nil, err
			}

			info, err := os.Stat(hostDir)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf("target directory not found: %s", containerDir)
				}
				return nil, fmt.Errorf("failed to access target directory: %w", err)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("target path is not a directory: %s", containerDir)
			}

			claudeExecutable, err := resolveClaudeExecutable()
			if err != nil {
				return nil, err
			}

			execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
			defer cancel()

			cmd, cleanup, err := buildClaudeSandboxCommand(execCtx, hostDir, input.Prompt, claudeExecutable)
			if err != nil {
				return nil, err
			}
			defer cleanup()

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()
			if execCtx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("claude code timed out after %d seconds", timeoutSecs)
			}

			exitCode := 0
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					errText := strings.TrimSpace(stderr.String())
					if errText == "" {
						errText = runErr.Error()
					}
					return nil, fmt.Errorf("failed to execute claude: %s", errText)
				}
			}

			result := map[string]any{
				"stdout":              strings.TrimRight(stdout.String(), "\n"),
				"stderr":              strings.TrimRight(stderr.String(), "\n"),
				"exit_code":           exitCode,
				"success":             exitCode == 0,
				"working_directory":   hostDir,
				"container_directory": containerDir,
			}
			if exitCode != 0 {
				result["error"] = fmt.Sprintf("claude exited with code %d", exitCode)
			}

			return result, nil
		})
	},
}

func isClaudeCodeTool(toolName string) bool {
	_, ok := claudeCodeToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callClaudeCodeTools(ctx context.Context, agentID, toolName string, inputJSON []byte) (bool, any, error) {
	if !isClaudeCodeTool(toolName) {
		return false, nil, nil
	}

	handler, ok := claudeCodeToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, agentID, inputJSON)
	return true, result, err
}

func normalizeClaudeCodeTimeout(timeoutSecs int) int {
	if timeoutSecs <= 0 {
		return claudeCodeDefaultTimeoutSecs
	}
	if timeoutSecs > claudeCodeMaxTimeoutSecs {
		return claudeCodeMaxTimeoutSecs
	}
	return timeoutSecs
}

func mapClaudeTargetDirectoryToContainerPath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return claudeCodeContainerRoot, nil
	}

	cleanTarget := filepath.Clean(target)
	if cleanTarget == "." {
		return claudeCodeContainerRoot, nil
	}
	if filepath.IsAbs(cleanTarget) {
		if cleanTarget == claudeCodeContainerRoot || strings.HasPrefix(cleanTarget, claudeCodeContainerRoot+"/") {
			return cleanTarget, nil
		}
		cleanTarget = strings.TrimPrefix(cleanTarget, "/")
	}

	mapped := filepath.Clean(filepath.Join(claudeCodeContainerRoot, cleanTarget))
	if mapped != claudeCodeContainerRoot && !strings.HasPrefix(mapped, claudeCodeContainerRoot+"/") {
		return "", fmt.Errorf("target_directory escapes %s: %q", claudeCodeContainerRoot, target)
	}
	return mapped, nil
}

func (h *BrokerToolsHandler) mapClaudeContainerPathToHost(ctx context.Context, agentID, containerPath string) (string, error) {
	mountpoint, err := h.resolveClaudeVolumeMountpoint(ctx, agentID)
	if err != nil {
		return "", err
	}

	containerPath = filepath.Clean(strings.TrimSpace(containerPath))
	if containerPath == "" {
		containerPath = claudeCodeContainerRoot
	}
	if containerPath != claudeCodeContainerRoot && !strings.HasPrefix(containerPath, claudeCodeContainerRoot+"/") {
		return "", fmt.Errorf("container path must be under %s: %s", claudeCodeContainerRoot, containerPath)
	}

	relativePath, err := filepath.Rel(claudeCodeContainerRoot, containerPath)
	if err != nil {
		return "", fmt.Errorf("failed to map target_directory: %w", err)
	}

	hostPath := mountpoint
	if relativePath != "." {
		hostPath = filepath.Clean(filepath.Join(mountpoint, relativePath))
	}
	if hostPath != mountpoint && !strings.HasPrefix(hostPath, mountpoint+string(filepath.Separator)) {
		return "", fmt.Errorf("mapped host path escapes volume mountpoint")
	}
	return hostPath, nil
}

func (h *BrokerToolsHandler) resolveClaudeVolumeMountpoint(ctx context.Context, agentID string) (string, error) {
	if h.resolveClaudeVolumeMountpointFn != nil {
		return h.resolveClaudeVolumeMountpointFn(ctx, agentID)
	}
	return resolveAgentVolumeMountpoint(ctx, agentID)
}

func resolveAgentVolumeMountpoint(ctx context.Context, agentID string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("failed to initialize docker client: %w", err)
	}
	defer cli.Close()

	volName := dockermgr.VolumeName(agentID)
	vol, err := cli.VolumeInspect(ctx, volName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect agent volume %q: %w", volName, err)
	}
	mountpoint := strings.TrimSpace(vol.Mountpoint)
	if mountpoint == "" {
		return "", fmt.Errorf("agent volume %q has empty mountpoint", volName)
	}
	return mountpoint, nil
}

func resolveClaudeExecutable() (string, error) {
	return claudellm.FindExecutable()
}

func resolveBwrapExecutable() (string, error) {
	return claudellm.FindBwrapExecutable()
}

func buildClaudeSandboxCommand(execCtx context.Context, hostDir, prompt, claudeExecutable string) (*exec.Cmd, func(), error) {
	bwrapExecutable, err := resolveBwrapExecutable()
	if err != nil {
		return nil, nil, err
	}

	resolvedClaudeExecutable := claudeExecutable
	if realPath, realErr := filepath.EvalSymlinks(claudeExecutable); realErr == nil && strings.TrimSpace(realPath) != "" {
		resolvedClaudeExecutable = realPath
	}

	sandboxHomeHost, cleanup, err := prepareClaudeSandboxHome()
	if err != nil {
		return nil, nil, err
	}

	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-user",
		"--uid", strconv.Itoa(os.Getuid()),
		"--gid", strconv.Itoa(os.Getgid()),
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
		"--unshare-cgroup-try",
		"--disable-userns",
		"--assert-userns-disabled",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
		"--ro-bind", "/etc", "/etc",
		"--ro-bind-try", "/run/systemd/resolve", "/run/systemd/resolve",
		"--proc", "/proc",
		"--dev", "/dev",
		"--size", strconv.Itoa(claudeCodeTmpfsSizeBytes),
		"--tmpfs", "/tmp",
		"--dir", "/sandbox",
		"--dir", "/sandbox/bin",
		"--ro-bind", resolvedClaudeExecutable, "/sandbox/bin/claude",
		"--bind", hostDir, "/work",
		"--chdir", "/work",
		"--bind", sandboxHomeHost, claudeCodeSandboxHome,
		"--clearenv",
		"--setenv", "HOME", claudeCodeSandboxHome,
		"--setenv", "PATH", claudeCodeSandboxPath,
		"--setenv", "LANG", "C.UTF-8",
		"--setenv", "LC_ALL", "C.UTF-8",
	}
	args = append(args, claudeSandboxEnvArgs()...)
	args = append(args,
		"--",
		"claude",
		"--dangerously-skip-permissions",
		"-p", prompt,
		"--disallowedTools", strings.Join(claudeCodeRunnerDisallowedTools, ","),
	)

	cmd := exec.CommandContext(execCtx, bwrapExecutable, args...)
	cmd.Dir = hostDir
	return cmd, cleanup, nil
}

func prepareClaudeSandboxHome() (string, func(), error) {
	tempHome, cleanup, err := initClaudeSandboxHome()
	if err != nil {
		return "", nil, err
	}

	userHome, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(userHome) != "" {
		if err := copyFileIfExists(
			filepath.Join(userHome, ".claude", ".credentials.json"),
			filepath.Join(tempHome, ".claude", ".credentials.json"),
		); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("failed to prepare claude credentials for sandbox: %w", err)
		}
		if err := copyFileIfExists(
			filepath.Join(userHome, ".claude.json"),
			filepath.Join(tempHome, ".claude.json"),
		); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("failed to prepare claude config for sandbox: %w", err)
		}
	}

	return tempHome, cleanup, nil
}

func prepareClaudeOAuthSandboxHome() (string, func(), error) {
	tempHome, cleanup, err := initClaudeSandboxHome()
	if err != nil {
		return "", nil, err
	}

	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		cleanup()
		if err == nil {
			err = fmt.Errorf("home directory is not available")
		}
		return "", nil, fmt.Errorf("failed to locate personal Claude OAuth credentials: %w", err)
	}

	credentialsSrc := filepath.Join(userHome, ".claude", ".credentials.json")
	if err := copyFile(credentialsSrc, filepath.Join(tempHome, ".claude", ".credentials.json")); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to prepare personal Claude OAuth credentials: %w", err)
	}
	if err := copyFileIfExists(
		filepath.Join(userHome, ".claude.json"),
		filepath.Join(tempHome, ".claude.json"),
	); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to prepare claude config for sandbox: %w", err)
	}

	return tempHome, cleanup, nil
}

func initClaudeSandboxHome() (string, func(), error) {
	tempHome, err := os.MkdirTemp("", "gowild-claude-home-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary claude home: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempHome) }

	if err := os.MkdirAll(filepath.Join(tempHome, ".claude"), 0700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to initialize temporary claude home: %w", err)
	}

	return tempHome, cleanup, nil
}

func claudeSandboxEnvArgs() []string {
	return claudeSandboxEnvArgsForKeys([]string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_BASE_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
		"all_proxy",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
		"NODE_EXTRA_CA_CERTS",
	})
}

func claudeSandboxNonAuthEnvArgs() []string {
	return claudeSandboxEnvArgsForKeys([]string{
		"ANTHROPIC_BASE_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
		"all_proxy",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
		"NODE_EXTRA_CA_CERTS",
	})
}

func claudeSandboxEnvArgsForKeys(keys []string) []string {
	args := make([]string, 0, len(keys)*3)
	for _, key := range keys {
		val := strings.TrimSpace(os.Getenv(key))
		if val == "" {
			continue
		}
		args = append(args, "--setenv", key, val)
	}
	return args
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("source is a directory: %s", src)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0600
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func copyFileIfExists(src, dst string) error {
	_, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return copyFile(src, dst)
}
