package main

import (
	"context"
	"fmt"

	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"github.com/original-david-knight/go_wild/tools"
)

type taskToolHandlerFunc func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error)

type taskContextInput struct {
	TaskID string `json:"task_id"`
}

var taskToolHandlers = map[string]taskToolHandlerFunc{
	"add_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.AddTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).AddTaskTool)
	},
	"mark_task_done": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.MarkTaskDoneInput](ctx, svc, inputJSON, (*tools.TaskTools).MarkTaskDoneTool)
	},
	"mark_task_deprecated": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.MarkTaskDeprecatedInput](ctx, svc, inputJSON, (*tools.TaskTools).MarkTaskDeprecatedTool)
	},
	"list_tasks": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.ListTasksInput](ctx, svc, inputJSON, (*tools.TaskTools).ListTasksTool)
	},
	"move_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.MoveTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).MoveTaskTool)
	},
	"block_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.BlockTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).BlockTaskTool)
	},
	"unblock_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.UnblockTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).UnblockTaskTool)
	},
	"sleep_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.SleepTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).SleepTaskTool)
	},
	"plan_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.PlanTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).PlanTaskTool)
	},
	"evaluate_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callTaskToolWithInput[tools.EvaluateTaskInput](ctx, svc, inputJSON, (*tools.TaskTools).EvaluateTaskTool)
	},
	"get_pending_tasks":  getPendingTasksResult,
	"get_workable_tasks": getWorkableTasksResult,
	"get_finished_tasks": getFinishedTasksResult,
	"get_task_context":   getTaskContextResult,
}

func isTaskTool(toolName string) bool {
	_, ok := taskToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callTaskTools(ctx context.Context, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isTaskTool(toolName) {
		return false, nil, nil
	}

	handler, ok := taskToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(ctx, svc, inputJSON)
	return true, result, err
}

func callTaskToolWithInput[T any](
	ctx context.Context,
	svc *data.AgentService,
	inputJSON []byte,
	call func(*tools.TaskTools, context.Context, T) (*loop.ToolResult, error),
) (any, error) {
	t := tools.NewTaskTools(svc)
	return callWithInput[T](inputJSON, func(input T) (any, error) {
		r, err := call(t, ctx, input)
		return toolResultContent(r, err)
	})
}

func getPendingTasksResult(ctx context.Context, svc *data.AgentService, _ []byte) (any, error) {
	tasks, err := svc.GetPendingTasks(ctx)
	if err != nil {
		return nil, err
	}
	taskList := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		taskList[i] = map[string]any{
			"id":          t.ID,
			"description": t.Description,
			"blocked":     t.Blocked,
		}
		if t.SleepUntil != nil {
			taskList[i]["sleep_until"] = t.SleepUntil.Format("2006-01-02T15:04:05Z07:00")
		}
		if t.ParentTaskID != "" {
			taskList[i]["parent_task_id"] = t.ParentTaskID
		}
		if t.Outcome != "" {
			taskList[i]["outcome"] = t.Outcome
		}
	}
	return map[string]any{"tasks": taskList}, nil
}

func getWorkableTasksResult(ctx context.Context, svc *data.AgentService, _ []byte) (any, error) {
	tasks, err := svc.GetWorkableTasks(ctx)
	if err != nil {
		return nil, err
	}
	taskList := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		taskList[i] = map[string]any{
			"id":          t.ID,
			"description": t.Description,
		}
		if t.ParentTaskID != "" {
			taskList[i]["parent_task_id"] = t.ParentTaskID
		}
	}
	return map[string]any{"tasks": taskList}, nil
}

func getFinishedTasksResult(ctx context.Context, svc *data.AgentService, _ []byte) (any, error) {
	tasks, err := svc.GetFinishedTasks(ctx)
	if err != nil {
		return nil, err
	}
	taskList := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		taskList[i] = map[string]any{
			"id":          t.ID,
			"description": t.Description,
			"status":      t.Status,
			"updated_at":  t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if t.Outcome != "" {
			taskList[i]["outcome"] = t.Outcome
		}
		if t.ParentTaskID != "" {
			taskList[i]["parent_task_id"] = t.ParentTaskID
		}
	}
	return map[string]any{"tasks": taskList}, nil
}

func getTaskContextResult(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
	return callWithInput[taskContextInput](inputJSON, func(input taskContextInput) (any, error) {
		if input.TaskID == "" {
			return nil, fmt.Errorf("task_id is required")
		}
		task, err := svc.GetTask(ctx, input.TaskID)
		if err != nil {
			return nil, err
		}
		result := map[string]any{
			"task_id":     task.ID,
			"description": task.Description,
		}
		if task.ParentTaskID != "" {
			parent, err := svc.GetTask(ctx, task.ParentTaskID)
			if err == nil {
				result["parent_description"] = parent.Description
				result["parent_task_id"] = parent.ID
			}
			// Get all siblings to compute step number and total.
			siblings, err := svc.GetSubtasks(ctx, task.ParentTaskID)
			if err == nil {
				result["total_steps"] = len(siblings)
				var completedOutcomes []map[string]any
				for i, s := range siblings {
					if s.ID == task.ID {
						result["step_number"] = i + 1
					}
					if (s.Status == "done" || s.Status == "deprecated") && s.Outcome != "" {
						completedOutcomes = append(completedOutcomes, map[string]any{
							"description": s.Description,
							"outcome":     s.Outcome,
						})
					}
				}
				if len(completedOutcomes) > 0 {
					result["completed_outcomes"] = completedOutcomes
				}
			}
		}
		return result, nil
	})
}
