package tools

import (
	"context"
	"fmt"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// ListTasksTool lists tasks.
func (t *TaskTools) ListTasksTool(ctx context.Context, input ListTasksInput) (*loop.ToolResult, error) {
	tasks, err := t.service.GetPendingTasks(ctx)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to load tasks: %v", err)), nil
	}

	now := time.Now()
	var result []map[string]any
	for _, task := range tasks {
		taskInfo := map[string]any{
			"id":          task.ID,
			"description": task.Description,
			"status":      task.Status,
			"created_at":  task.CreatedAt.Format(time.RFC3339),
		}
		if task.Blocked {
			taskInfo["blocked"] = true
		}
		if task.SleepUntil != nil && task.SleepUntil.After(now) {
			taskInfo["sleep_until"] = task.SleepUntil.Format(time.RFC3339)
		}
		if task.ParentTaskID != "" {
			taskInfo["parent_task_id"] = task.ParentTaskID
		}
		if task.Outcome != "" {
			taskInfo["outcome"] = task.Outcome
		}
		result = append(result, taskInfo)
	}

	// Include completed tasks if requested
	if input.IncludeCompleted {
		finished, err := t.service.GetFinishedTasks(ctx)
		if err == nil {
			for _, task := range finished {
				if task.Status == "done" { // Only done, not deprecated
					taskInfo := map[string]any{
						"id":          task.ID,
						"description": task.Description,
						"status":      task.Status,
						"created_at":  task.CreatedAt.Format(time.RFC3339),
					}
					if task.ParentTaskID != "" {
						taskInfo["parent_task_id"] = task.ParentTaskID
					}
					if task.Outcome != "" {
						taskInfo["outcome"] = task.Outcome
					}
					result = append(result, taskInfo)
				}
			}
		}
	}

	return loop.NewSuccessResult(map[string]any{
		"tasks": result,
		"count": len(result),
	}), nil
}
