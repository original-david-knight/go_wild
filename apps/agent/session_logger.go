package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// SessionLogger writes JSONL session logs for debugging.
type SessionLogger struct {
	file    *os.File
	encoder *json.Encoder
	path    string
}

// LogEntry is a flat JSONL entry with omitempty fields per event type.
type LogEntry struct {
	Timestamp string `json:"ts"`
	Type      string `json:"type"`

	// tool_call
	ToolID   string         `json:"tool_id,omitempty"`
	ToolName string         `json:"tool_name,omitempty"`
	Input    map[string]any `json:"input,omitempty"`

	// tool_result
	Success *bool  `json:"success,omitempty"`
	Content any    `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`

	// text_delta
	Text string `json:"text,omitempty"`

	// thinking
	Turn int `json:"turn,omitempty"`

	// done
	Usage      *LogUsage `json:"usage,omitempty"`
	FinalText  string    `json:"final_text,omitempty"`
	TurnCount  int       `json:"turn_count,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`

	// context_limit
	PromptTokens int `json:"prompt_tokens,omitempty"`
	MaxTokens    int `json:"max_tokens,omitempty"`
}

// LogUsage mirrors ModelUsage for JSON serialization.
type LogUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// NewSessionLogger creates a session log file under logs/.
func NewSessionLogger(agentID string) (*SessionLogger, error) {
	dir := "logs"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.jsonl", agentID, time.Now().Format("20060102_150405"))
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	return &SessionLogger{
		file:    f,
		encoder: enc,
		path:    path,
	}, nil
}

// LogEvent writes an event to the JSONL log. Errors are silently discarded.
func (l *SessionLogger) LogEvent(event loop.Event) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	switch e := event.(type) {
	case loop.TextDeltaEvent:
		entry.Type = "text_delta"
		entry.Text = e.Text

	case loop.ToolCallEvent:
		entry.Type = "tool_call"
		entry.ToolID = e.ID
		entry.ToolName = e.Name
		entry.Input = e.Input

	case loop.ToolResultEvent:
		entry.Type = "tool_result"
		entry.ToolID = e.ID
		entry.ToolName = e.Name
		entry.Success = &e.Result.Success
		if e.Result.Success {
			entry.Content = e.Result.Content
		} else {
			entry.Error = e.Result.Error
		}
		// Replace image data with placeholder
		if e.Result.HasImage() {
			entry.Content = fmt.Sprintf("[image: %s, %d bytes]", e.Result.Image.MIMEType, len(e.Result.Image.Data))
		}

	case loop.ThinkingEvent:
		entry.Type = "thinking"
		entry.Turn = e.Turn

	case loop.DoneEvent:
		entry.Type = "done"
		entry.Usage = &LogUsage{
			PromptTokens:     e.Usage.PromptTokens,
			CompletionTokens: e.Usage.CompletionTokens,
			TotalTokens:      e.Usage.TotalTokens,
		}
		entry.FinalText = e.FinalText
		entry.TurnCount = e.TurnCount
		entry.StopReason = e.StopReason

	case loop.ContextLimitEvent:
		entry.Type = "context_limit"
		entry.PromptTokens = e.PromptTokens
		entry.MaxTokens = e.MaxTokens

	case loop.ErrorEvent:
		entry.Type = "error"
		entry.Error = e.Err.Error()

	default:
		return
	}

	_ = l.encoder.Encode(entry)
}

// Close closes the log file.
func (l *SessionLogger) Close() {
	l.file.Close()
}

// cleanOldLogs deletes .jsonl files in logs/ older than 1 hour.
func cleanOldLogs() {
	dir := "logs"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-1 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
