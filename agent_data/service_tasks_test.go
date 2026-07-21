package data

import (
	"context"
	"testing"
	"time"
)

func TestTaskOperations(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Add tasks
	task1, err := svc.AddTask(ctx, "Task 1", "end")
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}
	task2, _ := svc.AddTask(ctx, "Task 2", "end")
	task3, _ := svc.AddTask(ctx, "Task 3", "beginning")

	// Get pending tasks
	pending, err := svc.GetPendingTasks(ctx)
	if err != nil {
		t.Fatalf("GetPendingTasks failed: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending tasks, got %d", len(pending))
	}
	// Task 3 should be first (position 0)
	if pending[0].Description != "Task 3" {
		t.Errorf("expected Task 3 first, got %q", pending[0].Description)
	}

	// Get task by ID
	got, err := svc.GetTask(ctx, task1.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if got.Description != "Task 1" {
		t.Errorf("expected 'Task 1', got %q", got.Description)
	}

	// Update task status
	if err := svc.UpdateTaskStatus(ctx, task1.ID, "done"); err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	// Verify finished tasks
	finished, err := svc.GetFinishedTasks(ctx)
	if err != nil {
		t.Fatalf("GetFinishedTasks failed: %v", err)
	}
	if len(finished) != 1 {
		t.Errorf("expected 1 finished task, got %d", len(finished))
	}

	// Set blocked
	if err := svc.SetTaskBlocked(ctx, task2.ID, true); err != nil {
		t.Fatalf("SetTaskBlocked failed: %v", err)
	}

	// Get workable (excludes blocked and done)
	workable, err := svc.GetWorkableTasks(ctx)
	if err != nil {
		t.Fatalf("GetWorkableTasks failed: %v", err)
	}
	if len(workable) != 1 {
		t.Errorf("expected 1 workable task, got %d", len(workable))
	}
	if workable[0].Description != "Task 3" {
		t.Errorf("expected Task 3 workable, got %q", workable[0].Description)
	}

	// Sleep task
	future := time.Now().Add(1 * time.Hour)
	if err := svc.SleepTask(ctx, task3.ID, &future); err != nil {
		t.Fatalf("SleepTask failed: %v", err)
	}

	workable, _ = svc.GetWorkableTasks(ctx)
	if len(workable) != 0 {
		t.Errorf("expected 0 workable tasks (all blocked/sleeping/done), got %d", len(workable))
	}

	// Wake task
	if err := svc.SleepTask(ctx, task3.ID, nil); err != nil {
		t.Fatalf("SleepTask (wake) failed: %v", err)
	}

	workable, _ = svc.GetWorkableTasks(ctx)
	if len(workable) != 1 {
		t.Errorf("expected 1 workable after wake, got %d", len(workable))
	}
}

func TestTaskMoveAndOutcome(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	svc.AddTask(ctx, "Task A", "end")
	task2, _ := svc.AddTask(ctx, "Task B", "end")
	svc.AddTask(ctx, "Task C", "end")

	// Move task B to beginning
	if err := svc.MoveTask(ctx, task2.ID, "beginning"); err != nil {
		t.Fatalf("MoveTask beginning failed: %v", err)
	}

	pending, _ := svc.GetPendingTasks(ctx)
	if pending[0].Description != "Task B" {
		t.Errorf("expected Task B first after move, got %q", pending[0].Description)
	}

	// Move task B to end
	if err := svc.MoveTask(ctx, task2.ID, "end"); err != nil {
		t.Fatalf("MoveTask end failed: %v", err)
	}

	pending, _ = svc.GetPendingTasks(ctx)
	if pending[len(pending)-1].Description != "Task B" {
		t.Errorf("expected Task B last after move, got %q", pending[len(pending)-1].Description)
	}

	// Set outcome
	if err := svc.SetTaskOutcome(ctx, task2.ID, "completed successfully"); err != nil {
		t.Fatalf("SetTaskOutcome failed: %v", err)
	}

	got, _ := svc.GetTask(ctx, task2.ID)
	if got.Status != "done" {
		t.Errorf("expected done status, got %q", got.Status)
	}
	if got.Outcome != "completed successfully" {
		t.Errorf("expected outcome, got %q", got.Outcome)
	}
}

func TestSubtasksAndAutoComplete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Create parent task
	parent, _ := svc.AddTask(ctx, "Parent task", "end")

	// Create subtasks
	sub1, _ := svc.AddTaskWithParent(ctx, "Subtask 1", "end", parent.ID)
	sub2, _ := svc.AddTaskWithParent(ctx, "Subtask 2", "end", parent.ID)

	// Parent should be skipped in workable (has pending subtasks)
	workable, _ := svc.GetWorkableTasks(ctx)
	for _, w := range workable {
		if w.ID == parent.ID {
			t.Error("parent with pending subtasks should not be workable")
		}
	}

	// Get subtasks
	subtasks, err := svc.GetSubtasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetSubtasks failed: %v", err)
	}
	if len(subtasks) != 2 {
		t.Errorf("expected 2 subtasks, got %d", len(subtasks))
	}

	// Complete sub1 - parent should not auto-complete yet
	svc.UpdateTaskStatus(ctx, sub1.ID, "done")
	completed, _ := svc.AutoCompleteParent(ctx, parent.ID)
	if completed {
		t.Error("parent should not auto-complete with pending subtask")
	}

	// Complete sub2 - parent should auto-complete
	svc.UpdateTaskStatus(ctx, sub2.ID, "done")
	completed, _ = svc.AutoCompleteParent(ctx, parent.ID)
	if !completed {
		t.Error("parent should auto-complete when all subtasks done")
	}

	parent2, _ := svc.GetTask(ctx, parent.ID)
	if parent2.Status != "done" {
		t.Errorf("expected parent status done, got %q", parent2.Status)
	}
}

func TestAutoCompleteParentEmpty(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Empty parent ID
	completed, err := svc.AutoCompleteParent(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completed {
		t.Error("should not complete empty parent")
	}
}

func TestRecurringTasks(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Add recurring task with 0-minute interval (always due)
	rt, err := svc.AddRecurringTask(ctx, "Check updates", 0)
	if err != nil {
		t.Fatalf("AddRecurringTask failed: %v", err)
	}

	// List recurring
	recurring, err := svc.GetRecurringTasks(ctx)
	if err != nil {
		t.Fatalf("GetRecurringTasks failed: %v", err)
	}
	if len(recurring) != 1 {
		t.Errorf("expected 1 recurring task, got %d", len(recurring))
	}

	// Get by ID
	got, err := svc.GetRecurringTask(ctx, rt.ID)
	if err != nil {
		t.Fatalf("GetRecurringTask failed: %v", err)
	}
	if got.Description != "Check updates" {
		t.Errorf("unexpected description: %q", got.Description)
	}

	// Check and create - should create one task
	// Need a small delay so the task is "due"
	time.Sleep(10 * time.Millisecond)
	created, err := svc.CheckAndCreateRecurringTasks(ctx)
	if err != nil {
		t.Fatalf("CheckAndCreateRecurringTasks failed: %v", err)
	}
	if created != 1 {
		t.Errorf("expected 1 task created, got %d", created)
	}

	// Should not create another while previous is pending
	created, _ = svc.CheckAndCreateRecurringTasks(ctx)
	if created != 0 {
		t.Errorf("expected 0 tasks created (pending exists), got %d", created)
	}

	// Mark the task done, then it should create again
	pending, _ := svc.GetPendingTasks(ctx)
	for _, p := range pending {
		if p.RecurringTaskID == rt.ID {
			svc.UpdateTaskStatus(ctx, p.ID, "done")
		}
	}

	time.Sleep(10 * time.Millisecond)
	created, _ = svc.CheckAndCreateRecurringTasks(ctx)
	if created != 1 {
		t.Errorf("expected 1 task created after done, got %d", created)
	}

	// Delete recurring task
	if err := svc.DeleteRecurringTask(ctx, rt.ID); err != nil {
		t.Fatalf("DeleteRecurringTask failed: %v", err)
	}

	recurring, _ = svc.GetRecurringTasks(ctx)
	if len(recurring) != 0 {
		t.Errorf("expected 0 recurring tasks after delete, got %d", len(recurring))
	}
}
