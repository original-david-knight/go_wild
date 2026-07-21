package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

// OutputMessage is a structured message sent to the frontend.
type OutputMessage struct {
	Type        string `json:"type"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Name        string `json:"name,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Status      string `json:"status,omitempty"`
	Tokens      int    `json:"tokens,omitempty"`
	Duration    string `json:"duration,omitempty"`
}

// Output message types
const (
	MsgPrompt        = "prompt"         // Ready for input
	MsgSystem        = "system"         // System/startup messages
	MsgThinking      = "thinking"       // Agent is thinking
	MsgResponse      = "response"       // Response text chunk
	MsgResponseEnd   = "response_end"   // Response complete
	MsgToolCall      = "tool_call"      // Starting a tool
	MsgToolResult    = "tool_result"    // Tool completed/failed
	MsgError         = "error"          // Error occurred
	MsgCompaction    = "compaction"     // Context compacted
	MsgContextDump   = "context_dump"   // Full context dump (history + tool outputs)
	MsgSmartMode     = "smart_mode"     // Smart mode state change
	MsgContent       = "content"        // Rich content (images, SVG, etc.)
	MsgRuntimeStatus = "runtime_status" // Full runtime status snapshot
)

// Emitter handles structured JSON output.
type Emitter struct {
	mu         sync.Mutex
	toolStarts map[string]time.Time
}

// Global emitter instance
var output = &Emitter{}

// emit sends a structured message (JSON line to stdout).
func (e *Emitter) emit(msg OutputMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}

// Prompt signals the agent is ready for input.
func (e *Emitter) Prompt() {
	e.emit(OutputMessage{Type: MsgPrompt})
}

// System outputs a system message.
func (e *Emitter) System(format string, args ...any) {
	e.emit(OutputMessage{Type: MsgSystem, Content: fmt.Sprintf(format, args...)})
}

// SystemWarning outputs a warning message.
func (e *Emitter) SystemWarning(format string, args ...any) {
	e.emit(OutputMessage{Type: MsgSystem, Content: "⚠️ " + fmt.Sprintf(format, args...)})
}

// SystemSuccess outputs a success message.
func (e *Emitter) SystemSuccess(format string, args ...any) {
	e.emit(OutputMessage{Type: MsgSystem, Content: "✅ " + fmt.Sprintf(format, args...)})
}

// Thinking signals the agent is thinking.
func (e *Emitter) Thinking() {
	e.emit(OutputMessage{Type: MsgThinking})
}

// ThinkingDone clears the thinking indicator (no-op in structured mode).
func (e *Emitter) ThinkingDone() {
	// Frontend handles state transitions
}

// Response outputs a response text chunk.
func (e *Emitter) Response(text string) {
	e.emit(OutputMessage{Type: MsgResponse, Content: text})
}

// ResponseEnd signals the response is complete.
func (e *Emitter) ResponseEnd(tokens int) {
	e.emit(OutputMessage{Type: MsgResponseEnd, Tokens: tokens})
}

// ToolCall outputs a tool call starting.
func (e *Emitter) ToolCall(name string, detail string) {
	e.mu.Lock()
	if e.toolStarts == nil {
		e.toolStarts = make(map[string]time.Time)
	}
	e.toolStarts[name] = time.Now()
	e.mu.Unlock()
	e.emit(OutputMessage{Type: MsgToolCall, Name: name, Detail: detail})
}

// ToolResult outputs a tool result.
func (e *Emitter) ToolResult(name string, success bool, errMsg string) {
	status := "completed"
	if !success {
		status = "failed"
	}
	var duration string
	e.mu.Lock()
	if start, ok := e.toolStarts[name]; ok {
		duration = time.Since(start).Truncate(time.Millisecond).String()
		delete(e.toolStarts, name)
	}
	e.mu.Unlock()
	e.emit(OutputMessage{Type: MsgToolResult, Name: name, Status: status, Detail: errMsg, Duration: duration})
}

// Error outputs an error message.
func (e *Emitter) Error(format string, args ...any) {
	e.emit(OutputMessage{Type: MsgError, Content: fmt.Sprintf(format, args...)})
}

// SmartMode emits a structured smart mode state change.
// content is "on" or "off", detail is the active model name.
func (e *Emitter) SmartMode(enabled bool, model string) {
	content := "off"
	if enabled {
		content = "on"
	}
	e.emit(OutputMessage{Type: MsgSmartMode, Content: content, Detail: model})
}

// RuntimeStatus emits a full runtime status snapshot as its own JSON line.
// The manager caches this and serves it via REST for reconnecting clients.
func (e *Emitter) RuntimeStatus(rs data.RuntimeStatus) {
	rs.Type = MsgRuntimeStatus
	e.mu.Lock()
	defer e.mu.Unlock()
	d, _ := json.Marshal(rs)
	fmt.Fprintln(os.Stdout, string(d))
}

// Compaction outputs context compaction info.
func (e *Emitter) Compaction(masked, kept, oldTokens, newTokens int) {
	e.emit(OutputMessage{
		Type:    MsgCompaction,
		Content: fmt.Sprintf("masked=%d kept=%d tokens=%d->%d", masked, kept, oldTokens, newTokens),
	})
}

// ContextDump emits a JSON-encoded context dump (history + tool outputs).
func (e *Emitter) ContextDump(payload string) {
	e.emit(OutputMessage{Type: MsgContextDump, Content: payload})
}

// TaskPrompt outputs a task being worked on.
func (e *Emitter) TaskPrompt(task string) {
	e.emit(OutputMessage{Type: MsgSystem, Content: "📋 " + task})
}

// ContextInfo outputs context token information.
func (e *Emitter) ContextInfo(current, max int) {
	e.emit(OutputMessage{Type: MsgSystem, Content: fmt.Sprintf("context: %d/%d tokens", current, max)})
}

// RichContent emits rich content with a MIME type. The data field is sent as-is
// (caller is responsible for base64-encoding binary data).
func (e *Emitter) RichContent(data, contentType, alt string) {
	e.emit(OutputMessage{Type: MsgContent, Content: data, ContentType: contentType, Detail: alt})
}

// Image emits a binary image (PNG, JPEG, etc.) as base64-encoded content.
func (e *Emitter) Image(data []byte, mimeType, alt string) {
	e.RichContent(base64.StdEncoding.EncodeToString(data), mimeType, alt)
}

// SVG emits an SVG image as raw text.
func (e *Emitter) SVG(svg, alt string) {
	e.RichContent(svg, "image/svg+xml", alt)
}

// Audio emits an audio file as base64-encoded content.
func (e *Emitter) Audio(data []byte, mimeType, alt string) {
	e.RichContent(base64.StdEncoding.EncodeToString(data), mimeType, alt)
}
