package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// TaskTools provides task management tools.
type TaskTools struct {
	service *data.AgentService
}

// NewTaskTools creates a new TaskTools instance.
// Returns nil if no AgentService is available.
func NewTaskTools(service *data.AgentService) *TaskTools {
	if service == nil {
		return nil
	}
	return &TaskTools{
		service: service,
	}
}

// AddTaskTool adds a new task.
func (t *TaskTools) AddTaskTool(ctx context.Context, input AddTaskInput) (*loop.ToolResult, error) {
	task, err := t.service.AddTaskWithParent(ctx, input.Description, input.Position, input.ParentTaskID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to add task: %v", err)), nil
	}

	result := map[string]any{
		"task_id":     task.ID,
		"description": task.Description,
		"status":      task.Status,
		"message":     "Task added successfully",
	}
	if task.ParentTaskID != "" {
		result["parent_task_id"] = task.ParentTaskID
	}
	return loop.NewSuccessResult(result), nil
}

// MarkTaskDoneTool marks a task as done.
func (t *TaskTools) MarkTaskDoneTool(ctx context.Context, input MarkTaskDoneInput) (*loop.ToolResult, error) {
	// Get task to check for parent
	task, err := t.service.GetTask(ctx, input.TaskID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get task: %v", err)), nil
	}

	if err := t.service.UpdateTaskStatus(ctx, input.TaskID, "done"); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to mark task done: %v", err)), nil
	}

	result := map[string]any{
		"task_id": input.TaskID,
		"status":  "done",
		"message": "Task marked as done",
	}

	// Auto-complete parent if all siblings are done
	if task.ParentTaskID != "" {
		autoCompleted, err := t.service.AutoCompleteParent(ctx, task.ParentTaskID)
		if err == nil && autoCompleted {
			result["parent_auto_completed"] = true
			result["parent_task_id"] = task.ParentTaskID
		}
	}

	return loop.NewSuccessResult(result), nil
}

// MarkTaskDeprecatedTool marks a task as deprecated.
func (t *TaskTools) MarkTaskDeprecatedTool(ctx context.Context, input MarkTaskDeprecatedInput) (*loop.ToolResult, error) {
	if err := t.service.UpdateTaskStatus(ctx, input.TaskID, "deprecated"); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to mark task deprecated: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"task_id": input.TaskID,
		"status":  "deprecated",
		"reason":  input.Reason,
		"message": "Task marked as deprecated",
	}), nil
}

// MoveTaskTool moves a task to the beginning or end of the list.
func (t *TaskTools) MoveTaskTool(ctx context.Context, input MoveTaskInput) (*loop.ToolResult, error) {
	if err := t.service.MoveTask(ctx, input.TaskID, input.Position); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to move task: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"task_id":  input.TaskID,
		"position": input.Position,
		"message":  fmt.Sprintf("Task moved to %s", input.Position),
	}), nil
}

// BlockTaskTool marks a task as blocked.
func (t *TaskTools) BlockTaskTool(ctx context.Context, input BlockTaskInput) (*loop.ToolResult, error) {
	if err := t.service.SetTaskBlocked(ctx, input.TaskID, true); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to block task: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"task_id": input.TaskID,
		"blocked": true,
		"reason":  input.Reason,
		"message": "Task marked as blocked",
	}), nil
}

// UnblockTaskTool marks a task as unblocked.
func (t *TaskTools) UnblockTaskTool(ctx context.Context, input UnblockTaskInput) (*loop.ToolResult, error) {
	if err := t.service.SetTaskBlocked(ctx, input.TaskID, false); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to unblock task: %v", err)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"task_id": input.TaskID,
		"blocked": false,
		"message": "Task unblocked",
	}), nil
}

// SleepTaskTool puts a task to sleep for a specified duration.
func (t *TaskTools) SleepTaskTool(ctx context.Context, input SleepTaskInput) (*loop.ToolResult, error) {
	var until *time.Time
	if input.Minutes > 0 {
		wakeTime := time.Now().Add(time.Duration(input.Minutes) * time.Minute)
		until = &wakeTime
	}

	if err := t.service.SleepTask(ctx, input.TaskID, until); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to sleep task: %v", err)), nil
	}

	if until == nil {
		return loop.NewSuccessResult(map[string]any{
			"task_id": input.TaskID,
			"message": "Task awakened",
		}), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"task_id":     input.TaskID,
		"sleep_until": until.Format(time.RFC3339),
		"message":     fmt.Sprintf("Task sleeping until %s", until.Format("15:04")),
	}), nil
}
