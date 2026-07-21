package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestNewSessionLogger(t *testing.T) {
	// Use a temp directory for logs
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	if logger.path == "" {
		t.Error("expected non-empty path")
	}
	if !strings.Contains(logger.path, "test-agent") {
		t.Errorf("expected path to contain agent ID, got: %s", logger.path)
	}
	if !strings.HasSuffix(logger.path, ".jsonl") {
		t.Errorf("expected .jsonl extension, got: %s", logger.path)
	}

	// Check that logs directory was created
	info, err := os.Stat(filepath.Join(tmpDir, "logs"))
	if err != nil {
		t.Fatalf("logs dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("logs should be a directory")
	}
}

func TestSessionLogger_LogEvent_TextDelta(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.TextDeltaEvent{Text: "Hello world"})
	logger.Close()

	// Read the log file
	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry.Type != "text_delta" {
		t.Errorf("expected type 'text_delta', got %q", entry.Type)
	}
	if entry.Text != "Hello world" {
		t.Errorf("expected text 'Hello world', got %q", entry.Text)
	}
	if entry.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestSessionLogger_LogEvent_ToolCall(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.ToolCallEvent{
		ID:    "call_123",
		Name:  "read_file",
		Input: map[string]any{"path": "/data/test.go"},
	})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Type != "tool_call" {
		t.Errorf("expected type 'tool_call', got %q", entry.Type)
	}
	if entry.ToolID != "call_123" {
		t.Errorf("expected tool_id 'call_123', got %q", entry.ToolID)
	}
	if entry.ToolName != "read_file" {
		t.Errorf("expected tool_name 'read_file', got %q", entry.ToolName)
	}
}

func TestSessionLogger_LogEvent_ToolResult(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Success result
	logger.LogEvent(loop.ToolResultEvent{
		ID:   "call_123",
		Name: "read_file",
		Result: &loop.ToolResult{
			Success: true,
			Content: "file contents",
		},
	})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Type != "tool_result" {
		t.Errorf("expected type 'tool_result', got %q", entry.Type)
	}
	if entry.Success == nil || !*entry.Success {
		t.Error("expected success=true")
	}
}

func TestSessionLogger_LogEvent_ToolResult_Error(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.ToolResultEvent{
		ID:   "call_456",
		Name: "run_shell",
		Result: &loop.ToolResult{
			Success: false,
			Error:   "command not found",
		},
	})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Success == nil || *entry.Success {
		t.Error("expected success=false")
	}
	if entry.Error != "command not found" {
		t.Errorf("expected error 'command not found', got %q", entry.Error)
	}
}

func TestSessionLogger_LogEvent_Done(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.DoneEvent{
		FinalText: "Here's the answer",
		TurnCount: 3,
		Usage: loop.ModelUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
		StopReason: "STOP",
	})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Type != "done" {
		t.Errorf("expected type 'done', got %q", entry.Type)
	}
	if entry.TurnCount != 3 {
		t.Errorf("expected turn_count 3, got %d", entry.TurnCount)
	}
	if entry.FinalText != "Here's the answer" {
		t.Errorf("expected final text, got %q", entry.FinalText)
	}
	if entry.Usage == nil {
		t.Fatal("expected usage")
	}
	if entry.Usage.TotalTokens != 150 {
		t.Errorf("expected total_tokens 150, got %d", entry.Usage.TotalTokens)
	}
	if entry.StopReason != "STOP" {
		t.Errorf("expected stop_reason 'STOP', got %q", entry.StopReason)
	}
}

func TestSessionLogger_LogEvent_Error(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.ErrorEvent{Err: fmt.Errorf("something went wrong")})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Type != "error" {
		t.Errorf("expected type 'error', got %q", entry.Type)
	}
	if entry.Error != "something went wrong" {
		t.Errorf("expected error message, got %q", entry.Error)
	}
}

func TestSessionLogger_LogEvent_Thinking(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.ThinkingEvent{Turn: 2})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Type != "thinking" {
		t.Errorf("expected type 'thinking', got %q", entry.Type)
	}
	if entry.Turn != 2 {
		t.Errorf("expected turn 2, got %d", entry.Turn)
	}
}

func TestSessionLogger_LogEvent_ContextLimit(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	logger, err := NewSessionLogger("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logger.LogEvent(loop.ContextLimitEvent{
		PromptTokens: 50000,
		MaxTokens:    65000,
	})
	logger.Close()

	data, err := os.ReadFile(logger.path)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	var entry LogEntry
	json.Unmarshal(data, &entry)
	if entry.Type != "context_limit" {
		t.Errorf("expected type 'context_limit', got %q", entry.Type)
	}
	if entry.PromptTokens != 50000 {
		t.Errorf("expected prompt_tokens 50000, got %d", entry.PromptTokens)
	}
	if entry.MaxTokens != 65000 {
		t.Errorf("expected max_tokens 65000, got %d", entry.MaxTokens)
	}
}

func TestCleanOldLogs(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Create logs directory and files
	logsDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logsDir, 0755)

	// Create an "old" log file
	oldFile := filepath.Join(logsDir, "old_session.jsonl")
	os.WriteFile(oldFile, []byte(`{"type":"test"}`), 0644)
	// Set modification time to 2 hours ago
	oldTime := time.Now().Add(-2 * time.Hour)
	os.Chtimes(oldFile, oldTime, oldTime)

	// Create a "new" log file
	newFile := filepath.Join(logsDir, "new_session.jsonl")
	os.WriteFile(newFile, []byte(`{"type":"test"}`), 0644)

	// Create a non-jsonl file (should be ignored)
	otherFile := filepath.Join(logsDir, "notes.txt")
	os.WriteFile(otherFile, []byte("notes"), 0644)
	os.Chtimes(otherFile, oldTime, oldTime)

	cleanOldLogs()

	// Old jsonl should be deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("expected old log file to be deleted")
	}

	// New jsonl should still exist
	if _, err := os.Stat(newFile); err != nil {
		t.Error("expected new log file to still exist")
	}

	// Non-jsonl should still exist
	if _, err := os.Stat(otherFile); err != nil {
		t.Error("expected non-jsonl file to still exist")
	}
}

func TestCleanOldLogs_NoLogsDir(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Should not panic when logs dir doesn't exist
	cleanOldLogs()
}
