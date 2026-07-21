package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleKnowledgeGraphUnknownAction(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/kg/not-real", nil)
	rec := httptest.NewRecorder()

	h.handleKnowledgeGraph(rec, req, agentID)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleKnowledgeGraphNodeRequiresID(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/kg/node", nil)
	rec := httptest.NewRecorder()

	h.handleKnowledgeGraph(rec, req, agentID)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestParseKnowledgeGraphRoute(t *testing.T) {
	action, nodeID := parseKnowledgeGraphRoute("/api/agents/agent-1/kg/node/node-9", "agent-1")
	if action != "node" {
		t.Fatalf("expected action=node, got %q", action)
	}
	if nodeID != "node-9" {
		t.Fatalf("expected nodeID=node-9, got %q", nodeID)
	}

	action, nodeID = parseKnowledgeGraphRoute("/api/agents/agent-1/kg", "agent-1")
	if action != "" {
		t.Fatalf("expected empty action for root kg route, got %q", action)
	}
	if nodeID != "" {
		t.Fatalf("expected empty nodeID for root kg route, got %q", nodeID)
	}
}

func TestIsKnowledgeGraphActionRecognition(t *testing.T) {
	if !isKnowledgeGraphAction("nodes") {
		t.Fatalf("expected nodes action to be recognized")
	}
	if !isKnowledgeGraphAction("node") {
		t.Fatalf("expected node action to be recognized")
	}
	if isKnowledgeGraphAction("kg-not-real") {
		t.Fatalf("expected unknown action to be rejected")
	}
}
