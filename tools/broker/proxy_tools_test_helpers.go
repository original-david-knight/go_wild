package broker

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// startSocketServer spins up a Unix socket server that bridges incoming socket
// requests to the given http.Handler (e.g. an httptest-style handler).
// Returns the socket path and a cleanup function.
func startSocketServer(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	socketPath := shortTestUnixSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix failed: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleTestConn(conn, handler)
		}
	}()

	return socketPath, func() { ln.Close() }
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

// handleTestConn reads a socket request, converts it to an HTTP request,
// passes it through the handler, and writes the socket response.
func handleTestConn(conn net.Conn, handler http.Handler) {
	defer conn.Close()

	var req socketRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(socketResponse{StatusCode: 400, Error: err.Error()})
		return
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}

	var bodyReader *jsonBodyReader
	if len(req.Body) > 0 {
		bodyReader = &jsonBodyReader{data: req.Body}
	}

	httpReq := httptest.NewRequest(method, req.Path, bodyReader)
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httpReq)

	body := rec.Body.Bytes()
	if len(body) == 0 {
		body = []byte(`{}`)
	}

	_ = json.NewEncoder(conn).Encode(socketResponse{
		StatusCode: rec.Code,
		Body:       json.RawMessage(body),
	})
}

type jsonBodyReader struct {
	data []byte
	pos  int
}

func (r *jsonBodyReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// newTestClient creates a broker Client backed by a Unix socket test server
// using the given HTTP handler. The socket is automatically cleaned up when t ends.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	socketPath, cleanup := startSocketServer(t, handler)
	t.Cleanup(cleanup)
	return NewTestClient(socketPath, "test-token")
}
