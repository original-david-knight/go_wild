package broker

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

func TestCallToolOverSocket(t *testing.T) {
	socketPath := shortTestUnixSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix failed: %v", err)
	}
	defer ln.Close()

	t.Setenv("BROKER_SOCKET_PATH", socketPath)
	t.Setenv("GOWILD_AGENT_ID", "agent-socket")

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		var req socketRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			done <- err
			return
		}
		if req.Method != "POST" {
			done <- errUnexpected("method", req.Method, "POST")
			return
		}
		if req.Path != "/broker/v1/tools/kg_search" {
			done <- errUnexpected("path", req.Path, "/broker/v1/tools/kg_search")
			return
		}
		if req.AgentID != "agent-socket" {
			done <- errUnexpected("agent_id", req.AgentID, "agent-socket")
			return
		}
		if req.ExecutionMethod != "market_review" {
			done <- errUnexpected("execution_method", req.ExecutionMethod, "market_review")
			return
		}
		if req.Token != "token-socket" {
			done <- errUnexpected("token", req.Token, "token-socket")
			return
		}

		resp := socketResponse{
			StatusCode: 200,
			Body:       json.RawMessage(`{"ok":true}`),
		}
		if err := json.NewEncoder(conn).Encode(resp); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	c := NewTestClient(socketPath, "token-socket")
	result, err := c.CallTool(WithExecutionMethod(context.Background(), "market_review"), "kg_search", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %#v", result["ok"])
	}
	if err := <-done; err != nil {
		t.Fatalf("socket server error: %v", err)
	}
}

func TestSocketErrorStatus(t *testing.T) {
	socketPath := shortTestUnixSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix failed: %v", err)
	}
	defer ln.Close()

	t.Setenv("BROKER_SOCKET_PATH", socketPath)
	t.Setenv("GOWILD_AGENT_ID", "agent-socket")

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req socketRequest
		_ = json.NewDecoder(conn).Decode(&req)
		_ = json.NewEncoder(conn).Encode(socketResponse{
			StatusCode: 500,
			Body:       json.RawMessage(`{"error":"boom"}`),
		})
	}()

	c := NewTestClient(socketPath, "token-socket")
	_, err = c.CallTool(context.Background(), "kg_search", nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func errUnexpected(field, got, want string) error {
	return &unexpectedValueError{field: field, got: got, want: want}
}

type unexpectedValueError struct {
	field string
	got   string
	want  string
}

func (e *unexpectedValueError) Error() string {
	return "unexpected " + e.field + ": got=" + e.got + " want=" + e.want
}
