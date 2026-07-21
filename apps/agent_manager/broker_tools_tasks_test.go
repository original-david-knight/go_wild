package main

import (
	"context"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCallTaskToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "task-agent")

	handled, result, err := h.callTaskTools(context.Background(), svc, "not_a_task_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestCallTaskToolsAddTaskAndGetPending(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "task-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "task-agent")

	addHandled, addResult, addErr := h.callTaskTools(context.Background(), svc, "add_task", []byte(`{"description":"write test plan"}`))
	if addErr != nil {
		t.Fatalf("unexpected add_task error: %v", addErr)
	}
	if !addHandled {
		t.Fatalf("expected add_task to be handled")
	}
	addMap, ok := addResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected add_task result type: %T", addResult)
	}
	if addMap["description"] != "write test plan" {
		t.Fatalf("unexpected add_task description: %#v", addMap["description"])
	}

	pendingHandled, pendingResult, pendingErr := h.callTaskTools(context.Background(), svc, "get_pending_tasks", []byte(`{}`))
	if pendingErr != nil {
		t.Fatalf("unexpected get_pending_tasks error: %v", pendingErr)
	}
	if !pendingHandled {
		t.Fatalf("expected get_pending_tasks to be handled")
	}
	pendingMap, ok := pendingResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected pending result type: %T", pendingResult)
	}
	tasksAny, ok := pendingMap["tasks"]
	if !ok {
		t.Fatalf("expected tasks in pending response")
	}
	switch tasks := tasksAny.(type) {
	case []map[string]any:
		if len(tasks) == 0 {
			t.Fatalf("expected at least one pending task")
		}
	case []any:
		if len(tasks) == 0 {
			t.Fatalf("expected at least one pending task")
		}
	default:
		t.Fatalf("unexpected tasks type: %T", tasksAny)
	}
}

func TestCallTaskToolsGetTaskContextRequiresTaskID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "task-agent")

	handled, result, err := h.callTaskTools(context.Background(), svc, "get_task_context", []byte(`{}`))
	if !handled {
		t.Fatalf("expected get_task_context to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected task_id validation error")
	}
	if !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsTaskToolRecognition(t *testing.T) {
	if !isTaskTool("add_task") {
		t.Fatalf("expected add_task to be recognized")
	}
	if isTaskTool("task_not_real") {
		t.Fatalf("expected unknown task tool to be rejected")
	}
}
