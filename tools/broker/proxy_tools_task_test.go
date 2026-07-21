package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

// --- TaskTools ---

func TestTaskTools_AddTask_Success(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"task_id": "123"})
	}))

	tt := NewTaskTools(c)
	result, err := tt.AddTaskTool(context.Background(), tools.AddTaskInput{
		Description: "Test task",
		Position:    "beginning",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/tools/add_task" {
		t.Errorf("expected path /broker/v1/tools/add_task, got %s", gotPath)
	}
	if gotBody["description"] != "Test task" {
		t.Errorf("expected description 'Test task', got %v", gotBody["description"])
	}
}

func TestTaskTools_AddTask_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "db failure"})
	}))

	tt := NewTaskTools(c)
	result, err := tt.AddTaskTool(context.Background(), tools.AddTaskInput{Description: "x"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure result")
	}
}

func TestTaskTools_MarkTaskDone(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.MarkTaskDoneTool(context.Background(), tools.MarkTaskDoneInput{TaskID: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_MarkTaskDeprecated(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.MarkTaskDeprecatedTool(context.Background(), tools.MarkTaskDeprecatedInput{
		TaskID: "t1", Reason: "no longer needed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_ListTasks(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tasks": []any{"task1", "task2"}})
	}))

	tt := NewTaskTools(c)
	result, err := tt.ListTasksTool(context.Background(), tools.ListTasksInput{IncludeCompleted: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_MoveTask(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.MoveTaskTool(context.Background(), tools.MoveTaskInput{TaskID: "t1", Position: "beginning"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_BlockTask(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.BlockTaskTool(context.Background(), tools.BlockTaskInput{TaskID: "t1", Reason: "waiting"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_UnblockTask(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.UnblockTaskTool(context.Background(), tools.UnblockTaskInput{TaskID: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_SleepTask(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.SleepTaskTool(context.Background(), tools.SleepTaskInput{TaskID: "t1", Minutes: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_PlanTask(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.PlanTaskTool(context.Background(), tools.PlanTaskInput{
		TaskID: "t1", Steps: []string{"step1", "step2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_EvaluateTask(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tt := NewTaskTools(c)
	result, err := tt.EvaluateTaskTool(context.Background(), tools.EvaluateTaskInput{
		TaskID: "t1", Outcome: "completed successfully",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestTaskTools_DescribeTool(t *testing.T) {
	tt := NewTaskTools(nil)
	if tt.DescribeTool("add_task") == "" {
		t.Error("expected non-empty description for add_task")
	}
	if tt.DescribeTool("mark_task_done") == "" {
		t.Error("expected non-empty description for mark_task_done")
	}
	if tt.DescribeTool("nonexistent") != "" {
		t.Error("expected empty description for unknown tool")
	}
}
