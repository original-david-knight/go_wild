package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	agentdata "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
)

func TestHandleHealth(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	h.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", result["status"])
	}
}

func TestHandleAgents_MethodNotAllowed(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/api/agents", nil)
	rec := httptest.NewRecorder()
	h.handleAgents(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleIndex_NotFound(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rec := httptest.NewRecorder()
	h.handleIndex(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleIndex_Root(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.handleIndex(rec, req)

	// Should return 200 with the static HTML
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestHandleIndex_PolymarketRoute(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest("GET", "/polymarket/company-123", nil)
	rec := httptest.NewRecorder()
	h.handleIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %q", rec.Header().Get("Content-Type"))
	}
}

// --- RuntimeStatus handler tests ---

func TestGetRuntimeStatus_ContainerNotRunning(t *testing.T) {
	dm := dockerManagerOrSkip(t)
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	hub := NewSessionHub(dm, svc)
	h := NewHandlers(svc, dm, hub, nil, nil)

	req := httptest.NewRequest("GET", "/api/agents/nonexistent/runtime-status", nil)
	rec := httptest.NewRecorder()
	h.getRuntimeStatus(rec, req, "nonexistent")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["state"] != "stopped" {
		t.Errorf("expected state 'stopped', got %v", result["state"])
	}
}

func TestGetRuntimeStatus_MethodNotAllowed(t *testing.T) {
	dm := dockerManagerOrSkip(t)
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	hub := NewSessionHub(dm, svc)
	h := NewHandlers(svc, dm, hub, nil, nil)

	req := httptest.NewRequest("POST", "/api/agents/test/runtime-status", nil)
	rec := httptest.NewRecorder()
	h.getRuntimeStatus(rec, req, "test")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestGetRuntimeStatus_NoSession(t *testing.T) {
	dm := dockerManagerOrSkip(t)
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	hub := NewSessionHub(dm, svc)
	h := NewHandlers(svc, dm, hub, nil, nil)

	// hub has no session for this agent, docker says not running → "stopped"
	req := httptest.NewRequest("GET", "/api/agents/test/runtime-status", nil)
	rec := httptest.NewRecorder()
	h.getRuntimeStatus(rec, req, "test")

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["state"] != "stopped" {
		t.Errorf("expected state 'stopped' (container not running), got %v", result["state"])
	}
}

func TestGetRuntimeStatus_WithCachedStatus(t *testing.T) {
	dm := dockerManagerOrSkip(t)
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	hub := NewSessionHub(dm, svc)
	h := NewHandlers(svc, dm, hub, nil, nil)

	// Manually inject a session with cached runtime status
	session := &RelaySession{
		agentID: "test",
		done:    make(chan struct{}),
		clients: make(map[*websocket.Conn]bool),
		input:   make(chan []byte, 1),
		runtimeStatus: &agentdata.RuntimeStatus{
			Type:      "runtime_status",
			State:     "idle",
			SmartMode: true,
			Model:     "gemini-3-flash-preview",
		},
	}
	hub.mu.Lock()
	hub.sessions["test"] = session
	hub.mu.Unlock()

	// Docker says not running → returns "stopped" (the handler checks docker first)
	req := httptest.NewRequest("GET", "/api/agents/test/runtime-status", nil)
	rec := httptest.NewRecorder()
	h.getRuntimeStatus(rec, req, "test")

	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	// Since docker says container isn't running, we get "stopped"
	if result["state"] != "stopped" {
		t.Errorf("expected 'stopped' (docker not running), got %v", result["state"])
	}
}

func TestNormalizeDuration(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"0", "0"},
		{"60", "60m"},
		{"15", "15m"},
		{"30m", "30m"},
		{"1h", "1h"},
		{"2h30m", "2h30m"},
		{"45m", "45m"},
		{"abc", "abc"}, // invalid — returned as-is
	}
	for _, tt := range tests {
		got := normalizeDuration(tt.input)
		if got != tt.want {
			t.Errorf("normalizeDuration(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// dockerManagerOrSkip creates a DockerManager, skipping the test if Docker is unavailable.
func dockerManagerOrSkip(t *testing.T) *dockermgr.DockerManager {
	t.Helper()
	dm, err := dockermgr.NewDockerManager()
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	return dm
}
