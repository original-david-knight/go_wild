package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gorilla/websocket"
	agentdata "github.com/original-david-knight/go_wild/agent_data"
)

func TestTryParseAgentJSON_ValidMessage(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	tests := []struct {
		name      string
		input     string
		agentType string
	}{
		{"prompt", `{"type":"prompt"}`, "prompt"},
		{"system", `{"type":"system","content":"hello"}`, "system"},
		{"response", `{"type":"response","content":"hi"}`, "response"},
		{"tool_call", `{"type":"tool_call","name":"read_file","detail":"path=/data/x"}`, "tool_call"},
		{"tool_result", `{"type":"tool_result","name":"read_file","status":"completed"}`, "tool_result"},
		{"error", `{"type":"error","content":"failed"}`, "error"},
		{"thinking", `{"type":"thinking"}`, "thinking"},
		{"response_end", `{"type":"response_end","tokens":100}`, "response_end"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := rs.tryParseAgentJSON([]byte(tc.input))
			if !ok {
				t.Fatal("expected successful parse")
			}
			if msg.Type != "agent" {
				t.Errorf("expected type 'agent', got %q", msg.Type)
			}
			if msg.AgentType != tc.agentType {
				t.Errorf("expected agent_type %q, got %q", tc.agentType, msg.AgentType)
			}
		})
	}
}

func TestTryParseAgentJSON_Invalid(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"plain text", "hello world"},
		{"non-json-object", "[1,2,3]"},
		{"no type field", `{"content":"hello"}`},
		{"invalid json", `{invalid`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := rs.tryParseAgentJSON([]byte(tc.input))
			if ok {
				t.Error("expected parse failure")
			}
		})
	}
}

func TestTryParseAgentJSON_ResponseAccumulation(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	// Response chunks should accumulate
	rs.tryParseAgentJSON([]byte(`{"type":"response","content":"Hello "}`))
	if rs.responseBuf != "Hello " {
		t.Errorf("expected 'Hello ', got %q", rs.responseBuf)
	}

	rs.tryParseAgentJSON([]byte(`{"type":"response","content":"world!"}`))
	if rs.responseBuf != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", rs.responseBuf)
	}

	// response_end should clear buffer (service is nil so no DB save)
	rs.tryParseAgentJSON([]byte(`{"type":"response_end","tokens":50}`))
	if rs.responseBuf != "" {
		t.Errorf("expected empty buffer after response_end, got %q", rs.responseBuf)
	}
}

func TestTryParseAgentJSON_ToolCallFields(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	msg, ok := rs.tryParseAgentJSON([]byte(`{"type":"tool_call","name":"read_file","detail":"path=/data/test.txt"}`))
	if !ok {
		t.Fatal("expected successful parse")
	}
	if msg.Name != "read_file" {
		t.Errorf("expected name 'read_file', got %q", msg.Name)
	}
	if msg.Detail != "path=/data/test.txt" {
		t.Errorf("expected detail 'path=/data/test.txt', got %q", msg.Detail)
	}
}

func TestTryParseAgentJSON_TokensField(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	msg, ok := rs.tryParseAgentJSON([]byte(`{"type":"response_end","tokens":1234}`))
	if !ok {
		t.Fatal("expected successful parse")
	}
	if msg.Tokens != 1234 {
		t.Errorf("expected tokens 1234, got %d", msg.Tokens)
	}
}

func TestRelaySessionCloseRequeuesClaimedJobs(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	queue := newLocalA2AQueue(db)

	jobResult, _, err := queue.Submit(ctx, "pipeline:run-1", "", "", localA2ARequest{
		Method: "test_method",
		Params: map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	jobID, _ := jobResult["job_id"].(string)
	if jobID == "" {
		t.Fatal("expected job_id")
	}

	if _, err := queue.ClaimJob(ctx, "agent-restart", jobID, 300); err != nil {
		t.Fatalf("ClaimJob failed: %v", err)
	}

	rs := &RelaySession{
		agentID: "agent-restart",
		clients: make(map[*websocket.Conn]bool),
		input:   make(chan []byte, 1),
		done:    make(chan struct{}),
		service: service,
	}
	rs.close()

	job, err := queue.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if status, _ := job["status"].(string); status != localA2AStatusQueued {
		t.Fatalf("expected queued after relay close, got %q", status)
	}
	if toAgent, _ := job["to_public_key"].(string); toAgent != "" {
		t.Fatalf("expected queued pool job to be unassigned, got %q", toAgent)
	}
}

func TestWSMessage_JSON_Roundtrip(t *testing.T) {
	msg := WSMessage{
		Type:      "agent",
		AgentType: "response",
		Content:   "hello",
		Tokens:    42,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got WSMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != msg.Type || got.AgentType != msg.AgentType || got.Content != msg.Content || got.Tokens != msg.Tokens {
		t.Error("round-trip mismatch")
	}
}

// --- RuntimeStatus caching tests ---

func TestTryParseAgentJSON_RuntimeStatusCaching(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	// Initially nil
	if rs.GetRuntimeStatus() != nil {
		t.Fatal("expected nil runtime status initially")
	}

	// prompt → state=idle
	rs.tryParseAgentJSON([]byte(`{"type":"prompt"}`))
	status := rs.GetRuntimeStatus()
	if status == nil {
		t.Fatal("expected non-nil status after prompt")
	}
	if status.State != "idle" {
		t.Errorf("expected state 'idle', got %q", status.State)
	}

	// thinking → state=thinking
	rs.tryParseAgentJSON([]byte(`{"type":"thinking"}`))
	if s := rs.GetRuntimeStatus(); s.State != "thinking" {
		t.Errorf("expected state 'thinking', got %q", s.State)
	}

	// response → state=responding
	rs.tryParseAgentJSON([]byte(`{"type":"response","content":"hi"}`))
	if s := rs.GetRuntimeStatus(); s.State != "responding" {
		t.Errorf("expected state 'responding', got %q", s.State)
	}

	// tool_call → state=tool_running
	rs.tryParseAgentJSON([]byte(`{"type":"tool_call","name":"run_shell"}`))
	if s := rs.GetRuntimeStatus(); s.State != "tool_running" {
		t.Errorf("expected state 'tool_running', got %q", s.State)
	}
}

func TestTryParseAgentJSON_SmartModeUpdatesStatus(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	rs.tryParseAgentJSON([]byte(`{"type":"smart_mode","content":"on","detail":"gemini-3-flash-preview"}`))
	status := rs.GetRuntimeStatus()
	if status == nil {
		t.Fatal("expected non-nil status after smart_mode")
	}
	if !status.SmartMode {
		t.Error("expected SmartMode true")
	}
	if status.Model != "gemini-3-flash-preview" {
		t.Errorf("expected model 'gemini-3-flash-preview', got %q", status.Model)
	}

	// Turn off
	rs.tryParseAgentJSON([]byte(`{"type":"smart_mode","content":"off","detail":"gemini-3-flash-preview"}`))
	status = rs.GetRuntimeStatus()
	if status.SmartMode {
		t.Error("expected SmartMode false")
	}
	if status.Model != "gemini-3-flash-preview" {
		t.Errorf("expected model 'gemini-3-flash-preview', got %q", status.Model)
	}
}

func TestTryParseAgentJSON_FullRuntimeStatusReplaces(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	// Set some initial state via incremental updates
	rs.tryParseAgentJSON([]byte(`{"type":"thinking"}`))
	rs.tryParseAgentJSON([]byte(`{"type":"smart_mode","content":"on","detail":"pro"}`))

	// Full runtime_status snapshot should replace everything
	rs.tryParseAgentJSON([]byte(`{"type":"runtime_status","state":"idle","smart_mode":false,"model":"flash"}`))
	status := rs.GetRuntimeStatus()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.State != "idle" {
		t.Errorf("expected state 'idle', got %q", status.State)
	}
	if status.SmartMode {
		t.Error("expected SmartMode false after full replace")
	}
	if status.Model != "flash" {
		t.Errorf("expected model 'flash', got %q", status.Model)
	}
}

func TestGetRuntimeStatus_ReturnsCopy(t *testing.T) {
	rs := &RelaySession{agentID: "test"}

	rs.tryParseAgentJSON([]byte(`{"type":"prompt"}`))
	s1 := rs.GetRuntimeStatus()
	s1.State = "mutated"

	s2 := rs.GetRuntimeStatus()
	if s2.State == "mutated" {
		t.Error("GetRuntimeStatus should return a copy, not a reference")
	}
	if s2.State != "idle" {
		t.Errorf("expected 'idle', got %q", s2.State)
	}
}

func TestApplyAgentMessageHistoryUnknownTypeNoop(t *testing.T) {
	rs := &RelaySession{agentID: "test", responseBuf: "existing"}
	applyAgentMessageHistory(rs, AgentMessage{Type: "not-real", Content: "ignored"})
	if rs.responseBuf != "existing" {
		t.Fatalf("expected unknown type to leave response buffer unchanged, got %q", rs.responseBuf)
	}
}

func TestApplyRuntimeStatusUpdateUnknownTypeNoop(t *testing.T) {
	rs := &RelaySession{
		agentID:       "test",
		runtimeStatus: &agentdata.RuntimeStatus{Type: "runtime_status", State: "idle"},
	}

	applyRuntimeStatusUpdate(rs, AgentMessage{Type: "not-real"}, nil)
	if rs.runtimeStatus.State != "idle" {
		t.Fatalf("expected unknown type to leave runtime state unchanged, got %q", rs.runtimeStatus.State)
	}
}

func TestSessionHub_GetSession(t *testing.T) {
	hub := NewSessionHub(nil, nil)

	// No session exists
	if s := hub.GetSession("nonexistent"); s != nil {
		t.Error("expected nil for nonexistent session")
	}

	// Add a session manually
	session := &RelaySession{
		agentID: "test",
		done:    make(chan struct{}),
		clients: make(map[*websocket.Conn]bool),
	}
	hub.mu.Lock()
	hub.sessions["test"] = session
	hub.mu.Unlock()

	// Should find it
	if s := hub.GetSession("test"); s == nil {
		t.Error("expected non-nil session")
	}

	// Close the session's done channel to simulate closed session
	close(session.done)
	if s := hub.GetSession("test"); s != nil {
		t.Error("expected nil for closed session")
	}
}

func TestAgentMessage_JSON(t *testing.T) {
	input := `{"type":"tool_call","name":"run_python","detail":"code=print(42)","status":"running"}`
	var msg AgentMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg.Type != "tool_call" {
		t.Errorf("expected type 'tool_call', got %q", msg.Type)
	}
	if msg.Name != "run_python" {
		t.Errorf("expected name 'run_python', got %q", msg.Name)
	}
}
