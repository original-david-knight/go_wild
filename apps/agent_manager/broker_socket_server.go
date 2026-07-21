package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultBrokerSocketPath = "/tmp/gowild-broker/broker.sock"

func brokerSocketPath() string {
	if raw := strings.TrimSpace(os.Getenv("BROKER_SOCKET_PATH")); raw != "" {
		return raw
	}
	return defaultBrokerSocketPath
}

type brokerSocketRequest struct {
	Method          string          `json:"method"`
	Path            string          `json:"path"`
	Token           string          `json:"token,omitempty"`
	AgentID         string          `json:"agent_id,omitempty"`
	ExecutionMethod string          `json:"execution_method,omitempty"`
	Body            json.RawMessage `json:"body,omitempty"`
}

type brokerSocketResponse struct {
	StatusCode int             `json:"status_code"`
	Body       json.RawMessage `json:"body,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type BrokerSocketServer struct {
	path    string
	handler http.Handler
	auth    *BrokerAuthHandler
	ln      net.Listener
}

func NewBrokerSocketServer(path string, broker *BrokerHandlers) *BrokerSocketServer {
	if strings.TrimSpace(path) == "" {
		path = brokerSocketPath()
	}
	var auth *BrokerAuthHandler
	if broker != nil {
		auth = broker.auth
	}
	return &BrokerSocketServer{
		path:    path,
		handler: buildBrokerSocketMux(broker),
		auth:    auth,
	}
}

func (s *BrokerSocketServer) ListenAndServe() error {
	if s == nil {
		return fmt.Errorf("broker socket server is nil")
	}
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("broker socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("failed to create broker socket directory: %w", err)
	}
	_ = os.Remove(s.path)

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("failed to listen on broker socket %q: %w", s.path, err)
	}
	s.ln = ln
	_ = os.Chmod(s.path, 0o666)

	fmt.Printf("Broker socket listening on %s\n", s.path)
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener closed during shutdown.
			if ne, ok := err.(*net.OpError); ok && ne != nil && strings.Contains(strings.ToLower(ne.Error()), "use of closed network connection") {
				return nil
			}
			return fmt.Errorf("broker socket accept failed: %w", err)
		}
		go s.handleConn(conn)
	}
}

func (s *BrokerSocketServer) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	if s.ln != nil {
		closeErr = s.ln.Close()
	}
	if strings.TrimSpace(s.path) != "" {
		_ = os.Remove(s.path)
	}
	return closeErr
}

func (s *BrokerSocketServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// Set a generous server-side deadline so we don't hang forever if the
	// handler blocks.  This must exceed the longest expected handler duration
	// (deep research defaults to 15 minutes).
	_ = conn.SetDeadline(time.Now().Add(16 * time.Minute))

	enc := json.NewEncoder(conn)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("broker socket panic: %v", r)
			if err := enc.Encode(brokerSocketResponse{StatusCode: http.StatusInternalServerError, Error: fmt.Sprintf("internal error: %v", r)}); err != nil {
				log.Printf("broker socket: failed to send panic response: %v", err)
			}
		}
	}()
	dec := json.NewDecoder(conn)

	var req brokerSocketRequest
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(brokerSocketResponse{StatusCode: http.StatusBadRequest, Error: "invalid broker socket request: " + err.Error()})
		return
	}

	method := strings.TrimSpace(strings.ToUpper(req.Method))
	if method == "" {
		method = http.MethodPost
	}
	path := strings.TrimSpace(req.Path)
	if path == "" || !strings.HasPrefix(path, "/broker/") {
		_ = enc.Encode(brokerSocketResponse{StatusCode: http.StatusBadRequest, Error: "invalid broker path"})
		return
	}

	bodyReader := io.Reader(bytes.NewReader(req.Body))
	if len(req.Body) == 0 {
		bodyReader = nil
	}
	httpReq := httptest.NewRequest(method, path, bodyReader)
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	requestedAgentID := strings.TrimSpace(req.AgentID)
	started := time.Now()
	rec := httptest.NewRecorder()

	// Give the handler a context with a timeout matching the connection deadline
	// so long-running operations (e.g. deep research) get cancelled cleanly
	// instead of the socket being pulled out from under them.
	const connTimeout = 16 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), connTimeout)
	defer cancel()

	if strings.HasPrefix(path, "/broker/v1/auth/") {
		httpReq = httpReq.WithContext(ctx)
		s.serveSocketHTTP(enc, method, path, "", started, rec, httpReq)
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		_ = enc.Encode(brokerSocketResponse{StatusCode: http.StatusUnauthorized, Body: json.RawMessage(`{"error":"missing agent session token in broker socket request"}`)})
		return
	}
	session, err := s.auth.ValidateSessionToken(ctx, token)
	if err != nil {
		body, _ := json.Marshal(map[string]string{"error": "invalid agent session token: " + err.Error()})
		_ = enc.Encode(brokerSocketResponse{StatusCode: http.StatusUnauthorized, Body: json.RawMessage(body)})
		return
	}
	if requestedAgentID != "" && requestedAgentID != session.AgentID {
		_ = enc.Encode(brokerSocketResponse{StatusCode: http.StatusUnauthorized, Body: json.RawMessage(`{"error":"agent_id does not match session token identity"}`)})
		return
	}

	agentID := session.AgentID
	ctx = context.WithValue(ctx, brokerAgentIDKey, agentID)
	ctx = context.WithValue(ctx, brokerAgentAddressKey, session.Address)
	if executionMethod := strings.TrimSpace(req.ExecutionMethod); executionMethod != "" {
		ctx = context.WithValue(ctx, brokerExecutionMethodKey, executionMethod)
	}
	httpReq = httpReq.WithContext(ctx)

	s.serveSocketHTTP(enc, method, path, agentID, started, rec, httpReq)
}

func (s *BrokerSocketServer) serveSocketHTTP(enc *json.Encoder, method, path, agentID string, started time.Time, rec *httptest.ResponseRecorder, httpReq *http.Request) {
	s.handler.ServeHTTP(rec, httpReq)
	elapsed := time.Since(started)

	body := rec.Body.Bytes()
	// The recorder body may contain non-JSON content (e.g. plain text 404 from
	// the default mux handler).  Verify it is valid JSON before embedding it in
	// the socket response; otherwise wrap it in an error object so the client
	// always receives a well-formed JSON frame.
	if len(body) == 0 {
		body = []byte(`{}`)
	} else if !json.Valid(body) {
		body, _ = json.Marshal(map[string]string{"error": strings.TrimSpace(string(body))})
	}

	resp := brokerSocketResponse{
		StatusCode: rec.Code,
		Body:       json.RawMessage(body),
	}
	if err := enc.Encode(resp); err != nil {
		log.Printf("broker socket: failed to encode response for %s %s (agent=%s, status=%d, body=%d bytes, handler=%s): %v",
			method, path, agentID, rec.Code, len(body), elapsed.Round(time.Millisecond), err)
	} else if elapsed > 30*time.Second {
		// Log slow requests to help diagnose timeout issues.
		log.Printf("broker socket: slow request %s %s (agent=%s, status=%d, body=%d bytes, handler=%s)",
			method, path, agentID, rec.Code, len(body), elapsed.Round(time.Millisecond))
	}
}
