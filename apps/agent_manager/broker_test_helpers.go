package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	brokerclient "github.com/original-david-knight/go_wild/tools/broker"
)

// startTestBrokerSocket creates a Unix socket server that bridges incoming
// socket requests to the given http.Handler. Returns the socket path.
// The socket is automatically cleaned up when t ends.
func startTestBrokerSocket(t *testing.T, handler http.Handler) string {
	t.Helper()
	socketPath := shortTestUnixSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestBrokerConn(conn, handler)
		}
	}()

	return socketPath
}

func shortTestUnixSocketPath(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp("", "gw-*.sock")
	if err != nil {
		t.Fatalf("reserve unix socket path: %v", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("close unix socket placeholder: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove unix socket placeholder: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// handleTestBrokerConn reads a socket request, converts it to an HTTP request,
// passes it through the handler, and writes the socket response.
func handleTestBrokerConn(conn net.Conn, handler http.Handler) {
	defer conn.Close()

	var req struct {
		Method  string          `json:"method"`
		Path    string          `json:"path"`
		Token   string          `json:"token,omitempty"`
		AgentID string          `json:"agent_id,omitempty"`
		Body    json.RawMessage `json:"body,omitempty"`
	}
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(map[string]any{"status_code": 400, "error": err.Error()})
		return
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}

	var body *jsonRawReader
	if len(req.Body) > 0 {
		body = &jsonRawReader{data: req.Body}
	}

	httpReq := httptest.NewRequest(method, req.Path, body)
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)

	respBody := rec.Body.Bytes()
	if len(respBody) == 0 {
		respBody = []byte("{}")
	}

	json.NewEncoder(conn).Encode(map[string]any{
		"status_code": rec.Code,
		"body":        json.RawMessage(respBody),
	})
}

type jsonRawReader struct {
	data []byte
	pos  int
}

func (r *jsonRawReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// newTestBrokerClient creates a broker client backed by a Unix socket test
// server using the given HTTP handler.
func newTestBrokerClient(t *testing.T, handler http.Handler, token string) *brokerclient.Client {
	t.Helper()
	socketPath := startTestBrokerSocket(t, handler)
	return brokerclient.NewTestClient(socketPath, token)
}
