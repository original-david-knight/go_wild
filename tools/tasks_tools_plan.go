package tools

import (
	"context"
	"fmt"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// PlanTaskTool decomposes a task into ordered subtasks.
func (t *TaskTools) PlanTaskTool(ctx context.Context, input PlanTaskInput) (*loop.ToolResult, error) {
	// Verify parent task exists
	parent, err := t.service.GetTask(ctx, input.TaskID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get parent task: %v", err)), nil
	}

	if len(input.Steps) == 0 {
		return loop.NewErrorResult("steps cannot be empty"), nil
	}

	// Create subtasks
	var subtasks []map[string]any
	for _, step := range input.Steps {
		task, err := t.service.AddTaskWithParent(ctx, step, "end", input.TaskID)
		if err != nil {
			return loop.NewErrorResult(fmt.Sprintf("failed to create subtask: %v", err)), nil
		}
		subtasks = append(subtasks, map[string]any{
			"task_id":     task.ID,
			"description": task.Description,
		})
	}

	return loop.NewSuccessResult(map[string]any{
		"parent_task_id":     parent.ID,
		"parent_description": parent.Description,
		"subtasks":           subtasks,
		"count":              len(subtasks),
		"message":            fmt.Sprintf("Created %d subtasks for: %s", len(subtasks), parent.Description),
	}), nil
}

// EvaluateTaskTool records an outcome and marks a task done.
func (t *TaskTools) EvaluateTaskTool(ctx context.Context, input EvaluateTaskInput) (*loop.ToolResult, error) {
	task, err := t.service.GetTask(ctx, input.TaskID)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to get task: %v", err)), nil
	}

	if err := t.service.SetTaskOutcome(ctx, input.TaskID, input.Outcome); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to set outcome: %v", err)), nil
	}

	result := map[string]any{
		"task_id": input.TaskID,
		"status":  "done",
		"outcome": input.Outcome,
		"message": "Task evaluated and marked done",
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
