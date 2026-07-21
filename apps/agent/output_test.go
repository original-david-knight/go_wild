package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// captureOutput captures stdout during fn execution and returns it.
func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func parseOutputMessage(t *testing.T, raw string) OutputMessage {
	t.Helper()
	var msg OutputMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("failed to parse output message: %v\nraw: %s", err, raw)
	}
	return msg
}

func TestEmitterPrompt(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Prompt() })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgPrompt {
		t.Errorf("expected type %q, got %q", MsgPrompt, msg.Type)
	}
}

func TestEmitterSystem(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.System("hello %s", "world") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgSystem {
		t.Errorf("expected type %q, got %q", MsgSystem, msg.Type)
	}
	if msg.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", msg.Content)
	}
}

func TestEmitterSystemWarning(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.SystemWarning("bad %s", "thing") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgSystem {
		t.Errorf("expected type %q, got %q", MsgSystem, msg.Type)
	}
	if !containsHelper(msg.Content, "bad thing") {
		t.Errorf("expected content to contain 'bad thing', got %q", msg.Content)
	}
}

func TestEmitterSystemSuccess(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.SystemSuccess("done %d", 1) })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgSystem {
		t.Errorf("expected type %q, got %q", MsgSystem, msg.Type)
	}
	if !containsHelper(msg.Content, "done 1") {
		t.Errorf("expected content to contain 'done 1', got %q", msg.Content)
	}
}

func TestEmitterThinking(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Thinking() })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgThinking {
		t.Errorf("expected type %q, got %q", MsgThinking, msg.Type)
	}
}

func TestEmitterResponse(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Response("chunk text") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgResponse {
		t.Errorf("expected type %q, got %q", MsgResponse, msg.Type)
	}
	if msg.Content != "chunk text" {
		t.Errorf("expected content 'chunk text', got %q", msg.Content)
	}
}

func TestEmitterResponseEnd(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.ResponseEnd(1234) })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgResponseEnd {
		t.Errorf("expected type %q, got %q", MsgResponseEnd, msg.Type)
	}
	if msg.Tokens != 1234 {
		t.Errorf("expected tokens 1234, got %d", msg.Tokens)
	}
}

func TestEmitterToolCall(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.ToolCall("read_file", "path=/data/test.go") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgToolCall {
		t.Errorf("expected type %q, got %q", MsgToolCall, msg.Type)
	}
	if msg.Name != "read_file" {
		t.Errorf("expected name 'read_file', got %q", msg.Name)
	}
	if msg.Detail != "path=/data/test.go" {
		t.Errorf("expected detail 'path=/data/test.go', got %q", msg.Detail)
	}
}

func TestEmitterToolResult_Success(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.ToolResult("read_file", true, "") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgToolResult {
		t.Errorf("expected type %q, got %q", MsgToolResult, msg.Type)
	}
	if msg.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", msg.Status)
	}
}

func TestEmitterToolResult_Failure(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.ToolResult("run_shell", false, "exit code 1") })
	msg := parseOutputMessage(t, out)
	if msg.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", msg.Status)
	}
	if msg.Detail != "exit code 1" {
		t.Errorf("expected detail 'exit code 1', got %q", msg.Detail)
	}
}

func TestEmitterError(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Error("error: %v", "timeout") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgError {
		t.Errorf("expected type %q, got %q", MsgError, msg.Type)
	}
	if msg.Content != "error: timeout" {
		t.Errorf("expected content 'error: timeout', got %q", msg.Content)
	}
}

func TestEmitterSmartMode(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.SmartMode(true, "gemini-3-pro-preview") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgSmartMode {
		t.Errorf("expected type %q, got %q", MsgSmartMode, msg.Type)
	}
	if msg.Content != "on" {
		t.Errorf("expected content 'on', got %q", msg.Content)
	}
	if msg.Detail != "gemini-3-pro-preview" {
		t.Errorf("expected detail 'gemini-3-pro-preview', got %q", msg.Detail)
	}

	out2 := captureOutput(func() { e.SmartMode(false, "gemini-3-flash-preview") })
	msg2 := parseOutputMessage(t, out2)
	if msg2.Content != "off" {
		t.Errorf("expected content 'off', got %q", msg2.Content)
	}
}

func TestEmitterCompaction(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Compaction(5, 3, 10000, 5000) })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgCompaction {
		t.Errorf("expected type %q, got %q", MsgCompaction, msg.Type)
	}
	if msg.Content != "masked=5 kept=3 tokens=10000->5000" {
		t.Errorf("unexpected content: %q", msg.Content)
	}
}

func TestEmitterTaskPrompt(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.TaskPrompt("Check emails") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgSystem {
		t.Errorf("expected type %q, got %q", MsgSystem, msg.Type)
	}
	if !containsHelper(msg.Content, "Check emails") {
		t.Errorf("expected content to contain 'Check emails', got %q", msg.Content)
	}
}

func TestEmitterContextInfo(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.ContextInfo(5000, 32000) })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgSystem {
		t.Errorf("expected type %q, got %q", MsgSystem, msg.Type)
	}
	if msg.Content != "context: 5000/32000 tokens" {
		t.Errorf("unexpected content: %q", msg.Content)
	}
}

func TestEmitterRichContent(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.RichContent("data123", "image/png", "screenshot") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgContent {
		t.Errorf("expected type %q, got %q", MsgContent, msg.Type)
	}
	if msg.Content != "data123" {
		t.Errorf("expected content 'data123', got %q", msg.Content)
	}
	if msg.ContentType != "image/png" {
		t.Errorf("expected content_type 'image/png', got %q", msg.ContentType)
	}
	if msg.Detail != "screenshot" {
		t.Errorf("expected detail 'screenshot', got %q", msg.Detail)
	}
}

func TestEmitterImage(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Image([]byte{0x89, 0x50, 0x4E, 0x47}, "image/png", "test") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgContent {
		t.Errorf("expected type %q, got %q", MsgContent, msg.Type)
	}
	if msg.ContentType != "image/png" {
		t.Errorf("expected content_type 'image/png', got %q", msg.ContentType)
	}
	// Content should be base64 encoded
	if msg.Content == "" {
		t.Error("expected non-empty base64 content")
	}
}

func TestEmitterSVG(t *testing.T) {
	e := &Emitter{}
	svgData := "<svg><rect width='100' height='100'/></svg>"
	out := captureOutput(func() { e.SVG(svgData, "diagram") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgContent {
		t.Errorf("expected type %q, got %q", MsgContent, msg.Type)
	}
	if msg.ContentType != "image/svg+xml" {
		t.Errorf("expected content_type 'image/svg+xml', got %q", msg.ContentType)
	}
	if msg.Content != svgData {
		t.Error("SVG content should be passed as-is")
	}
}

func TestEmitterAudio(t *testing.T) {
	e := &Emitter{}
	out := captureOutput(func() { e.Audio([]byte{0x01, 0x02, 0x03}, "audio/mpeg", "sample") })
	msg := parseOutputMessage(t, out)
	if msg.Type != MsgContent {
		t.Errorf("expected type %q, got %q", MsgContent, msg.Type)
	}
	if msg.ContentType != "audio/mpeg" {
		t.Errorf("expected content_type 'audio/mpeg', got %q", msg.ContentType)
	}
	if msg.Content == "" {
		t.Error("expected non-empty base64 content")
	}
}
