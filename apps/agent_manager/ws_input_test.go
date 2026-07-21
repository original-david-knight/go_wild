package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	agentdata "github.com/original-david-knight/go_wild/agent_data"
)

func newTestRelaySession() *RelaySession {
	return &RelaySession{
		agentID: "test",
		input:   make(chan []byte, 1),
		done:    make(chan struct{}),
		clients: make(map[*websocket.Conn]bool),
	}
}

func TestHandleUIMessage_PromptWritesInput(t *testing.T) {
	rs := newTestRelaySession()

	err := rs.handleUIMessage(nil, []byte(`{"type":"prompt","text":"hello"}`))
	if err != nil {
		t.Fatalf("handleUIMessage prompt failed: %v", err)
	}

	select {
	case got := <-rs.input:
		if string(got) != "hello\r\n" {
			t.Fatalf("expected prompt to write %q, got %q", "hello\r\n", string(got))
		}
	default:
		t.Fatal("expected prompt to write to input channel")
	}
}

func TestHandleUIMessage_CommandWritesJSON(t *testing.T) {
	rs := newTestRelaySession()

	err := rs.handleUIMessage(nil, []byte(`{"type":"command","command":"/help","args":{"topic":"agents"},"raw":"/help"}`))
	if err != nil {
		t.Fatalf("handleUIMessage command failed: %v", err)
	}

	select {
	case got := <-rs.input:
		payload := string(got)
		if !strings.HasSuffix(payload, "\n") {
			t.Fatalf("expected command payload to end with newline, got %q", payload)
		}
		payload = strings.TrimSuffix(payload, "\n")
		var cm agentdata.CommandMessage
		if err := json.Unmarshal([]byte(payload), &cm); err != nil {
			t.Fatalf("unmarshal command payload: %v", err)
		}
		if cm.Command != "/help" {
			t.Fatalf("expected command /help, got %q", cm.Command)
		}
		if cm.Args["topic"] != "agents" {
			t.Fatalf("expected arg topic=agents, got %v", cm.Args["topic"])
		}
	default:
		t.Fatal("expected command to write to input channel")
	}
}

func TestHandleUIMessage_InputLegacyDecodes(t *testing.T) {
	rs := newTestRelaySession()

	msg := WSMessage{
		Type: "input",
		Data: base64.StdEncoding.EncodeToString([]byte("echo\n")),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal legacy input: %v", err)
	}

	if err := rs.handleUIMessage(nil, data); err != nil {
		t.Fatalf("handleUIMessage legacy input failed: %v", err)
	}

	select {
	case got := <-rs.input:
		if string(got) != "echo\n" {
			t.Fatalf("expected decoded input %q, got %q", "echo\n", string(got))
		}
	default:
		t.Fatal("expected legacy input to write to input channel")
	}
}

func TestHandleUIMessage_ResizeNoInput(t *testing.T) {
	rs := newTestRelaySession()

	if err := rs.handleUIMessage(nil, []byte(`{"type":"resize","cols":80,"rows":24}`)); err != nil {
		t.Fatalf("handleUIMessage resize failed: %v", err)
	}

	select {
	case got := <-rs.input:
		t.Fatalf("expected no input on resize, got %q", string(got))
	default:
	}
}

func TestHandleUIMessage_UnknownTypeFallsBackLegacy(t *testing.T) {
	rs := newTestRelaySession()

	if err := rs.handleUIMessage(nil, []byte(`{"type":"not-real","data":"ignored"}`)); err != nil {
		t.Fatalf("expected unknown type to fall back without error, got %v", err)
	}

	select {
	case got := <-rs.input:
		t.Fatalf("expected no input for unknown type fallback, got %q", string(got))
	default:
	}
}

func TestHandleLegacyInput_InvalidBase64(t *testing.T) {
	rs := newTestRelaySession()

	msg := WSMessage{Type: "input", Data: "not-base64"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal legacy input: %v", err)
	}

	if err := rs.handleLegacyInput(data); err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestHandleLegacyInput_IgnoresNonInput(t *testing.T) {
	rs := newTestRelaySession()

	msg := WSMessage{Type: "output", Data: ""}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal legacy input: %v", err)
	}

	if err := rs.handleLegacyInput(data); err != nil {
		t.Fatalf("expected no error for non-input legacy message: %v", err)
	}

	select {
	case got := <-rs.input:
		t.Fatalf("expected no input for non-input message, got %q", string(got))
	default:
	}
}

func TestWriteToAgent_ClosedSession(t *testing.T) {
	rs := newTestRelaySession()
	rs.input = make(chan []byte)
	close(rs.done)

	if err := rs.writeToAgent("hi"); err == nil || !strings.Contains(err.Error(), "session closed") {
		t.Fatalf("expected session closed error, got %v", err)
	}
}

func TestWriteToAgent_InputFull(t *testing.T) {
	rs := newTestRelaySession()
	rs.input <- []byte("x")

	if err := rs.writeToAgent("y"); err == nil || !strings.Contains(err.Error(), "input channel full") {
		t.Fatalf("expected input channel full error, got %v", err)
	}
}

func TestWSInputRecognitionHelpers(t *testing.T) {
	if !isUIMessageType("prompt") || !isUIMessageType("command") || !isUIMessageType("control") || !isUIMessageType("input") || !isUIMessageType("resize") {
		t.Fatalf("expected prompt/command/control/input/resize to be recognized UI message types")
	}
	if isUIMessageType("not-real") {
		t.Fatalf("expected unknown UI message type to be rejected")
	}

	if !isControlAction("ping") {
		t.Fatalf("expected ping control action to be recognized")
	}
	if isControlAction("restart") {
		t.Fatalf("expected unsupported control action to be rejected")
	}
}
