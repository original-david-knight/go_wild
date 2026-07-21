package main

import (
	"context"
	"fmt"

	"github.com/original-david-knight/go_wild/agent_data"
)

type recurringToolHandlerFunc func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error)

type addRecurringTaskInput struct {
	Description     string `json:"description"`
	IntervalMinutes int    `json:"interval_minutes"`
}

type deleteRecurringTaskInput struct {
	ID string `json:"id"`
}

var recurringToolHandlers = map[string]recurringToolHandlerFunc{
	"check_recurring_tasks": func(ctx context.Context, svc *data.AgentService, _ []byte) (any, error) {
		created, err := svc.CheckAndCreateRecurringTasks(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"created": created}, nil
	},
	"get_recurring_tasks": func(ctx context.Context, svc *data.AgentService, _ []byte) (any, error) {
		tasks, err := svc.GetRecurringTasks(ctx)
		if err != nil {
			return nil, err
		}
		taskList := make([]map[string]any, len(tasks))
		for i, t := range tasks {
			taskList[i] = map[string]any{
				"id":               t.ID,
				"description":      t.Description,
				"interval_minutes": t.IntervalMinutes,
				"last_created_at":  t.LastCreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			}
		}
		return map[string]any{"tasks": taskList}, nil
	},
	"add_recurring_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[addRecurringTaskInput](inputJSON, func(input addRecurringTaskInput) (any, error) {
			if input.Description == "" {
				return nil, fmt.Errorf("description is required")
			}
			if input.IntervalMinutes <= 0 {
				return nil, fmt.Errorf("interval_minutes must be positive")
			}
			task, err := svc.AddRecurringTask(ctx, input.Description, input.IntervalMinutes)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"id":               task.ID,
				"description":      task.Description,
				"interval_minutes": task.IntervalMinutes,
			}, nil
		})
	},
	"delete_recurring_task": func(ctx context.Context, svc *data.AgentService, inputJSON []byte) (any, error) {
		return callWithInput[deleteRecurringTaskInput](inputJSON, func(input deleteRecurringTaskInput) (any, error) {
			if input.ID == "" {
				return nil, fmt.Errorf("id is required")
			}
			// Get task description before deleting.
			task, err := svc.GetRecurringTask(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			description := task.Description
			if err := svc.DeleteRecurringTask(ctx, input.ID); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": true, "description": description}, nil
		})
	},
}

func isRecurringTool(toolName string) bool {
	_, ok := recurringToolHandlers[toolName]
	return ok
}

func (h *BrokerToolsHandler) callRecurringTools(ctx context.Context, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isRecurringTool(toolName) {
		return false, nil, nil
	}

	handler, ok := recurringToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(ctx, svc, inputJSON)
	return true, result, err
}
