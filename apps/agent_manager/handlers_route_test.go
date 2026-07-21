package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupHandlersWithAgent(t *testing.T) (*Handlers, string) {
	t.Helper()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	agent, err := svc.CreateAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	return NewHandlers(svc, nil, nil, nil, nil), agent.ID
}

func TestHandleAgentMissingID(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAgentsMethodGuard(t *testing.T) {
	h, _ := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/agents", nil)
	rec := httptest.NewRecorder()

	h.handleAgents(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentRootMethodGuard(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID, nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAgentUpdatePreservesEnabledToolsWhenOmitted(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	ctx := context.Background()

	agent, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills"})
	if err := h.service.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/agents/"+agentID, strings.NewReader(`{"model":"gemini-3-flash-preview"}`))
	rec := httptest.NewRecorder()
	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	enabled := updated.EnabledTools()
	if enabled == nil || !enabled["skills"] {
		t.Fatalf("expected enabled tools to preserve skills when omitted, got %#v (json=%q)", enabled, updated.EnabledToolsJSON)
	}
}

func TestHandleAgentUpdateCanSetExplicitEmptyEnabledTools(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	ctx := context.Background()

	agent, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills"})
	if err := h.service.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/agents/"+agentID, strings.NewReader(`{"enabled_tools":[]}`))
	rec := httptest.NewRecorder()
	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if updated.EnabledToolsJSON != "[]" {
		t.Fatalf("expected explicit empty enabled_tools_json, got %q", updated.EnabledToolsJSON)
	}
	enabled := updated.EnabledTools()
	if enabled == nil {
		t.Fatalf("expected explicit empty enabled tool map, got nil")
	}
	if len(enabled) != 0 {
		t.Fatalf("expected no enabled tools, got %#v", enabled)
	}
}

func TestHandleAgentMemoryRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/memory", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["content"]; !ok {
		t.Fatalf("expected content field")
	}
}

func TestHandleAgentTasksRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["tasks"]; !ok {
		t.Fatalf("expected tasks field")
	}
}

func TestHandleAgentChatHistoryRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/chat-history?limit=5", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["messages"]; !ok {
		t.Fatalf("expected messages field")
	}
}

func TestHandleAgentKnowledgeGraphRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/kg/nodes", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["nodes"]; !ok {
		t.Fatalf("expected nodes field")
	}
}

func TestHandleAgentPendingEmailsRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/pending-emails", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["emails"]; !ok {
		t.Fatalf("expected emails field")
	}
}

func TestHandleAgentEmailWhitelistRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/email-whitelist", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["emails"]; !ok {
		t.Fatalf("expected emails field")
	}
}

func TestHandleAgentRecurringTasksRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/recurring-tasks", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["tasks"]; !ok {
		t.Fatalf("expected tasks field")
	}
}

func TestHandleAgentCompanyMethodToolsRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/company-method-tools", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["tools"]; !ok {
		t.Fatalf("expected tools field")
	}
}

func TestHandleAgentUnknownAction(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/unknown-action", nil)
	rec := httptest.NewRecorder()

	h.handleAgent(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestParseAgentRouteNestedSegments(t *testing.T) {
	route, ok := parseAgentRoute("/api/agents/agent-1/mcp-servers/server-a/test")
	if !ok {
		t.Fatalf("expected route parsing to succeed")
	}
	if route.agentID != "agent-1" {
		t.Fatalf("expected agentID agent-1, got %q", route.agentID)
	}
	if route.action != "mcp-servers" {
		t.Fatalf("expected action mcp-servers, got %q", route.action)
	}
	if route.serverID != "server-a" {
		t.Fatalf("expected serverID server-a, got %q", route.serverID)
	}
	if route.serverAction != "test" {
		t.Fatalf("expected serverAction test, got %q", route.serverAction)
	}

	route, ok = parseAgentRoute("/api/agents/agent-1/recurring-tasks/task-42")
	if !ok {
		t.Fatalf("expected recurring task route parsing to succeed")
	}
	if route.action != "recurring-tasks" {
		t.Fatalf("expected action recurring-tasks, got %q", route.action)
	}
	if route.taskID != "task-42" {
		t.Fatalf("expected taskID task-42, got %q", route.taskID)
	}

	route, ok = parseAgentRoute("/api/agents/agent-1/capabilities/cap-7")
	if !ok {
		t.Fatalf("expected capability route parsing to succeed")
	}
	if route.action != "capabilities" {
		t.Fatalf("expected action capabilities, got %q", route.action)
	}
	if route.capID != "cap-7" {
		t.Fatalf("expected capID cap-7, got %q", route.capID)
	}

	if _, ok := parseAgentRoute("/api/agents/"); ok {
		t.Fatalf("expected missing ID parse to fail")
	}
}

func TestHandleAgentMCPServerMethodGuardsViaRoute(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)

	getReq := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/mcp-servers/server-1", nil)
	getRec := httptest.NewRecorder()
	h.handleAgent(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected GET /mcp-servers/{id} status %d, got %d", http.StatusMethodNotAllowed, getRec.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/mcp-servers", nil)
	postRec := httptest.NewRecorder()
	h.handleAgent(postRec, postReq)
	if postRec.Code != http.StatusBadRequest {
		t.Fatalf("expected POST /mcp-servers status %d, got %d", http.StatusBadRequest, postRec.Code)
	}
}

func TestMCPMethodRecognitionHelpers(t *testing.T) {
	if !isMCPCollectionMethod(http.MethodGet) || !isMCPCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized MCP collection methods")
	}
	if isMCPCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected MCP collection method")
	}

	if !isMCPServerMethod(http.MethodPut) || !isMCPServerMethod(http.MethodDelete) {
		t.Fatalf("expected PUT/DELETE to be recognized MCP server methods")
	}
	if isMCPServerMethod(http.MethodGet) {
		t.Fatalf("expected GET to be rejected MCP server method")
	}

	if !isAgentMCPMethod(http.MethodGet) || !isAgentMCPMethod(http.MethodPost) || !isAgentMCPMethod(http.MethodPut) || !isAgentMCPMethod(http.MethodDelete) {
		t.Fatalf("expected GET/POST/PUT/DELETE to be recognized agent MCP methods")
	}
	if isAgentMCPMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected agent MCP method")
	}
}

func TestAgentRouteRecognitionHelpers(t *testing.T) {
	if !isAgentCollectionMethod(http.MethodGet) || !isAgentCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized agent collection methods")
	}
	if isAgentCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected agent collection method")
	}

	if !isAgentRootMethod(http.MethodGet) || !isAgentRootMethod(http.MethodPut) {
		t.Fatalf("expected GET/PUT to be recognized root agent methods")
	}
	if isAgentRootMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be rejected root agent method")
	}

	if !isAgentAction("memory") || !isAgentAction("mcp-servers") {
		t.Fatalf("expected memory/mcp-servers to be recognized agent actions")
	}
	if isAgentAction("not-real") {
		t.Fatalf("expected unknown action to be rejected")
	}
}
