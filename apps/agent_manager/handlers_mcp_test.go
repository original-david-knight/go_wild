package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPServers_ListEmpty(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	rec := httptest.NewRecorder()

	h.handleMCPServers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	var servers []json.RawMessage
	if err := json.Unmarshal(body["servers"], &servers); err != nil {
		t.Fatalf("failed to parse servers: %v", err)
	}
	if len(servers) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(servers))
	}
}

func TestMCPServers_CreateAndList(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)

	// Create
	createBody := `{"id":"test-server","name":"Test","command":"/usr/bin/test","args":["--flag"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	h.handleMCPServers(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	rec = httptest.NewRecorder()
	h.handleMCPServers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	var servers []json.RawMessage
	if err := json.Unmarshal(body["servers"], &servers); err != nil {
		t.Fatalf("failed to parse servers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
}

func TestMCPServers_Create_MissingFields(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)

	tests := []struct {
		name string
		body string
		msg  string
	}{
		{"missing id", `{"name":"Test","command":"/usr/bin/test"}`, "id is required"},
		{"missing name", `{"id":"s1","command":"/usr/bin/test"}`, "name is required"},
		{"missing command", `{"id":"s1","name":"Test"}`, "command is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.handleMCPServers(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.msg) {
				t.Fatalf("expected error %q, got %s", tc.msg, rec.Body.String())
			}
		})
	}
}

func TestMCPServer_UpdateAndDelete(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)

	// Create server
	createBody := `{"id":"update-me","name":"Original","command":"/usr/bin/test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	h.handleMCPServers(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Update name
	updateBody := `{"name":"Updated"}`
	req = httptest.NewRequest(http.MethodPut, "/api/mcp-servers/update-me", strings.NewReader(updateBody))
	rec = httptest.NewRecorder()
	h.handleMCPServer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify update
	var updated map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse update response: %v", err)
	}
	if updated["name"] != "Updated" {
		t.Fatalf("expected name 'Updated', got %v", updated["name"])
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/mcp-servers/update-me", nil)
	rec = httptest.NewRecorder()
	h.handleMCPServer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted (list should be empty)
	req = httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	rec = httptest.NewRecorder()
	h.handleMCPServers(rec, req)
	var body map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &body)
	var servers []json.RawMessage
	json.Unmarshal(body["servers"], &servers)
	if len(servers) != 0 {
		t.Fatalf("expected 0 servers after delete, got %d", len(servers))
	}
}

func TestAgentMCPServers_UpsertAndList(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	// Create agent and MCP server
	svc.CreateAgent(ctx, "agent-1")
	createBody := `{"id":"mcp-1","name":"Test MCP","command":"/usr/bin/mcp"}`
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	h.handleMCPServers(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create server: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Upsert agent MCP config
	configBody := `{"enabled":true,"args":["--flag"],"env":{"KEY":"val"}}`
	req = httptest.NewRequest(http.MethodPut, "/api/agents/agent-1/mcp-servers/mcp-1", strings.NewReader(configBody))
	rec = httptest.NewRecorder()
	h.upsertAgentMCPServer(rec, req, "agent-1", "mcp-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert config: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// List agent MCP servers
	req = httptest.NewRequest(http.MethodGet, "/api/agents/agent-1/mcp-servers", nil)
	rec = httptest.NewRecorder()
	h.listAgentMCPServers(rec, req, "agent-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	var configs []map[string]any
	if err := json.Unmarshal(body["configs"], &configs); err != nil {
		t.Fatalf("failed to parse configs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0]["server_id"] != "mcp-1" {
		t.Fatalf("expected server_id 'mcp-1', got %v", configs[0]["server_id"])
	}
	if configs[0]["server_name"] != "Test MCP" {
		t.Fatalf("expected server_name 'Test MCP', got %v", configs[0]["server_name"])
	}
}

func TestAgentMCPServers_Delete(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()

	svc.CreateAgent(ctx, "agent-1")
	h := NewHandlers(svc, nil, nil, nil, nil)

	// Create server
	createBody := `{"id":"mcp-del","name":"Delete Me","command":"/usr/bin/mcp"}`
	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	h.handleMCPServers(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", rec.Code)
	}

	// Upsert config
	configBody := `{"enabled":true}`
	req = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(configBody))
	rec = httptest.NewRecorder()
	h.upsertAgentMCPServer(rec, req, "agent-1", "mcp-del")
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert: expected 200, got %d", rec.Code)
	}

	// Delete agent config
	req = httptest.NewRequest(http.MethodDelete, "/", nil)
	rec = httptest.NewRecorder()
	h.deleteAgentMCPServer(rec, req, "agent-1", "mcp-del")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete config: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted (list should be empty)
	req = httptest.NewRequest(http.MethodGet, "/api/agents/agent-1/mcp-servers", nil)
	rec = httptest.NewRecorder()
	h.listAgentMCPServers(rec, req, "agent-1")

	var body map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &body)
	var configs []json.RawMessage
	json.Unmarshal(body["configs"], &configs)
	if len(configs) != 0 {
		t.Fatalf("expected 0 configs after delete, got %d", len(configs))
	}
}

func TestMCPServers_MethodNotAllowed(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)

	req := httptest.NewRequest(http.MethodPatch, "/api/mcp-servers", nil)
	rec := httptest.NewRecorder()
	h.handleMCPServers(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestMCPServer_MissingID(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)

	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/", nil)
	rec := httptest.NewRecorder()
	h.handleMCPServer(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpsertAgentMCPServer_ServerNotFound(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)

	configBody := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(configBody))
	rec := httptest.NewRecorder()
	h.upsertAgentMCPServer(rec, req, "agent-1", "nonexistent")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
