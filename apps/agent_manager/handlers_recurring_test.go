package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRecurringTasksCollectionMethodGuard(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodPut, "/api/agents/"+agentID+"/recurring-tasks", nil)
	rec := httptest.NewRecorder()

	h.handleRecurringTasks(rec, req, agentID, "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "method not allowed") {
		t.Fatalf("expected method not allowed error, got %s", rec.Body.String())
	}
}

func TestHandleRecurringTasksTaskMethodGuard(t *testing.T) {
	h, agentID := setupHandlersWithAgent(t)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+agentID+"/recurring-tasks/task-1", nil)
	rec := httptest.NewRecorder()

	h.handleRecurringTasks(rec, req, agentID, "task-1")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "method not allowed") {
		t.Fatalf("expected method not allowed error, got %s", rec.Body.String())
	}
}

func TestIsRecurringMethodRecognition(t *testing.T) {
	if !isRecurringCollectionMethod(http.MethodGet) {
		t.Fatalf("expected GET to be recognized as recurring collection method")
	}
	if isRecurringCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected for recurring collection method")
	}
	if !isRecurringTaskMethod(http.MethodPut) {
		t.Fatalf("expected PUT to be recognized as recurring task method")
	}
	if isRecurringTaskMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected for recurring task method")
	}
}
