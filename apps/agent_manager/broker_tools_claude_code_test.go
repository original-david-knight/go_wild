package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapClaudeTargetDirectoryToContainerPath(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to data root", input: "", want: "/data"},
		{name: "dot defaults to data root", input: ".", want: "/data"},
		{name: "relative path", input: "repo", want: "/data/repo"},
		{name: "nested relative path", input: "repo/src", want: "/data/repo/src"},
		{name: "absolute data path", input: "/data/repo", want: "/data/repo"},
		{name: "absolute non-data path maps under data", input: "/home/david/work", want: "/data/home/david/work"},
		{name: "relative traversal rejected", input: "../../etc", wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mapClaudeTargetDirectoryToContainerPath(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapClaudeContainerPathToHost(t *testing.T) {
	mountpoint := t.TempDir()
	h := &BrokerToolsHandler{
		resolveClaudeVolumeMountpointFn: func(ctx context.Context, agentID string) (string, error) {
			return mountpoint, nil
		},
	}

	hostRoot, err := h.mapClaudeContainerPathToHost(context.Background(), "agent-x", "/data")
	if err != nil {
		t.Fatalf("map /data failed: %v", err)
	}
	if hostRoot != mountpoint {
		t.Fatalf("host root = %q, want %q", hostRoot, mountpoint)
	}

	hostSubdir, err := h.mapClaudeContainerPathToHost(context.Background(), "agent-x", "/data/repo/src")
	if err != nil {
		t.Fatalf("map /data/repo/src failed: %v", err)
	}
	wantSubdir := filepath.Join(mountpoint, "repo", "src")
	if hostSubdir != wantSubdir {
		t.Fatalf("host subdir = %q, want %q", hostSubdir, wantSubdir)
	}

	if _, err := h.mapClaudeContainerPathToHost(context.Background(), "agent-x", "/tmp"); err == nil {
		t.Fatalf("expected error for path outside /data")
	}
}

func TestCallClaudeCodeToolsRunsOnMappedHostDirectory(t *testing.T) {
	binDir := t.TempDir()
	bwrapPath := filepath.Join(binDir, "bwrap")
	bwrapScript := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--\" ]; then\n    shift\n    break\n  fi\n  shift\ndone\nexec \"$@\"\n"
	if err := os.WriteFile(bwrapPath, []byte(bwrapScript), 0755); err != nil {
		t.Fatalf("write fake bwrap: %v", err)
	}

	claudePath := filepath.Join(binDir, "claude")
	script := "#!/bin/sh\nprintf 'PWD=%s\\n' \"$PWD\"\nprintf 'ARGS=%s\\n' \"$*\"\n"
	if err := os.WriteFile(claudePath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	mountpoint := t.TempDir()
	targetDir := filepath.Join(mountpoint, "project")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	h := &BrokerToolsHandler{
		resolveClaudeVolumeMountpointFn: func(ctx context.Context, agentID string) (string, error) {
			return mountpoint, nil
		},
	}

	payload := []byte(`{"prompt":"print hello","target_directory":"/data/project","timeout":10}`)
	handled, resultAny, err := h.callClaudeCodeTools(context.Background(), "agent-x", "claude_code", payload)
	if !handled {
		t.Fatalf("expected claude_code to be handled")
	}
	if err != nil {
		t.Fatalf("callClaudeCodeTools returned error: %v", err)
	}

	result, ok := resultAny.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resultAny)
	}
	if success, _ := result["success"].(bool); !success {
		t.Fatalf("expected success result, got %#v", result)
	}
	if gotDir, _ := result["working_directory"].(string); gotDir != targetDir {
		t.Fatalf("working_directory = %q, want %q", gotDir, targetDir)
	}
	if gotContainerDir, _ := result["container_directory"].(string); gotContainerDir != "/data/project" {
		t.Fatalf("container_directory = %q, want /data/project", gotContainerDir)
	}

	stdout, _ := result["stdout"].(string)
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Fatalf("stdout missing mapped working directory, got: %q", stdout)
	}
	if !strings.Contains(stdout, "ARGS=--dangerously-skip-permissions -p print hello --disallowedTools "+strings.Join(claudeCodeRunnerDisallowedTools, ",")) {
		t.Fatalf("stdout missing expected args, got: %q", stdout)
	}
}

func TestCallClaudeCodeToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callClaudeCodeTools(context.Background(), "agent-x", "not_a_claude_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestCallClaudeCodeToolsRequiresPrompt(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callClaudeCodeTools(context.Background(), "agent-x", "claude_code", []byte(`{}`))
	if !handled {
		t.Fatalf("expected claude_code to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected prompt validation error")
	}
	if !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsClaudeCodeToolRecognition(t *testing.T) {
	if !isClaudeCodeTool("claude_code") {
		t.Fatalf("expected claude_code to be recognized")
	}
	if isClaudeCodeTool("claude_not_real") {
		t.Fatalf("expected unknown claude tool to be rejected")
	}
}
