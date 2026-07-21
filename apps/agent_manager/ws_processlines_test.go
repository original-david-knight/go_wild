package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestProcessLines_PlainText(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("hello\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf after processing, got %q", rs.lineBuf)
	}
}

func TestProcessLines_AgentJSON(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("{\"type\":\"response\",\"content\":\"hi\"}\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf after processing, got %q", rs.lineBuf)
	}
	// Verify the agent message was parsed (response content accumulated)
	if rs.responseBuf != "hi" {
		t.Fatalf("expected responseBuf 'hi', got %q", rs.responseBuf)
	}
}

func TestProcessLines_IncompleteLine(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("partial data")
	rs.processLines()
	if string(rs.lineBuf) != "partial data" {
		t.Fatalf("expected incomplete data to remain in buffer, got %q", rs.lineBuf)
	}
}

func TestProcessLines_ANSIEmbeddedJSON(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("\x1b[0myou>{\"type\":\"prompt\",\"content\":\">\"}\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf after processing, got %q", rs.lineBuf)
	}
}

func TestProcessLines_MultipleLines(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("line1\nline2\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf after processing two lines, got %q", rs.lineBuf)
	}
}

func TestProcessLines_CRLFHandling(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("hello\r\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf after CRLF line, got %q", rs.lineBuf)
	}
}

func TestProcessLines_PlainTextIsBroadcastAsOutput(t *testing.T) {
	// With no clients, broadcastMessage just iterates an empty map.
	// We verify the line is consumed (lineBuf is cleared).
	rs := newTestRelaySession()
	rs.lineBuf = []byte("plain text line\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf, got %q", rs.lineBuf)
	}
}

func TestProcessLines_MixedJSONAndPlain(t *testing.T) {
	rs := newTestRelaySession()
	rs.lineBuf = []byte("plain text\n{\"type\":\"response\",\"content\":\"ok\"}\nmore text\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf, got %q", rs.lineBuf)
	}
	if rs.responseBuf != "ok" {
		t.Fatalf("expected responseBuf 'ok', got %q", rs.responseBuf)
	}
}

func TestTryParseAgentJSON_ValidMessage_ProcessLines(t *testing.T) {
	rs := newTestRelaySession()
	data := []byte(`{"type":"response","content":"hello"}`)
	msg, ok := rs.tryParseAgentJSON(data)
	if !ok {
		t.Fatal("expected successful parse")
	}
	if msg.AgentType != "response" {
		t.Errorf("expected agent_type 'response', got %q", msg.AgentType)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", msg.Content)
	}
}

func TestTryParseAgentJSON_InvalidJSON_ProcessLines(t *testing.T) {
	rs := newTestRelaySession()
	_, ok := rs.tryParseAgentJSON([]byte("not json"))
	if ok {
		t.Error("expected parse failure for plain text")
	}
}

func TestTryParseAgentJSON_EmptyType_ProcessLines(t *testing.T) {
	rs := newTestRelaySession()
	_, ok := rs.tryParseAgentJSON([]byte(`{"content":"hi"}`))
	if ok {
		t.Error("expected parse failure for empty type")
	}
}

func TestProcessLines_ANSIPrefix_RawOutputBase64(t *testing.T) {
	// Verify that the prefix before embedded JSON would be sent as base64 "output".
	// Since there are no clients, we just verify lineBuf is consumed.
	rs := newTestRelaySession()
	prefix := "\x1b[0myou>"
	jsonPart := `{"type":"prompt","content":">"}`
	rs.lineBuf = []byte(prefix + jsonPart + "\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf, got %q", rs.lineBuf)
	}
}

func TestProcessLines_LeadingWhitespaceJSON(t *testing.T) {
	// JSON with leading whitespace should still be parsed
	rs := newTestRelaySession()
	rs.lineBuf = []byte("  {\"type\":\"response\",\"content\":\"trimmed\"}\n")
	rs.processLines()
	if len(rs.lineBuf) != 0 {
		t.Fatalf("expected empty lineBuf, got %q", rs.lineBuf)
	}
	if rs.responseBuf != "trimmed" {
		t.Fatalf("expected responseBuf 'trimmed', got %q", rs.responseBuf)
	}
}

func TestBroadcastMessage_EmptyClients(t *testing.T) {
	rs := newTestRelaySession()
	// Should not panic with no clients
	msg := WSMessage{Type: "output", Data: base64.StdEncoding.EncodeToString([]byte("test\n"))}
	rs.broadcastMessage(msg)
}

func TestBroadcastStatus_EmptyClients(t *testing.T) {
	rs := newTestRelaySession()
	// Should not panic with no clients
	rs.broadcastStatus("running", "test")
}

func TestWSMessage_OutputType_JSON(t *testing.T) {
	msg := WSMessage{
		Type: "output",
		Data: base64.StdEncoding.EncodeToString([]byte("hello\n")),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got WSMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Type != "output" {
		t.Errorf("expected type 'output', got %q", got.Type)
	}
	decoded, _ := base64.StdEncoding.DecodeString(got.Data)
	if string(decoded) != "hello\n" {
		t.Errorf("expected decoded data 'hello\\n', got %q", string(decoded))
	}
}
