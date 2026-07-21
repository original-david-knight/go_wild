package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func getShortTermMemory(ctx context.Context) (string, error) {
	if globalBrokerClient != nil {
		result, err := globalBrokerClient.CallTool(ctx, "get_memory", map[string]any{})
		if err != nil {
			return "", err
		}
		content, _ := result["content"].(string)
		return content, nil
	}
	if globalAgentService == nil {
		return "", fmt.Errorf("memory unavailable without broker or database")
	}
	entry, err := globalAgentService.GetMemory(ctx)
	if err != nil || entry == nil {
		return "", err
	}
	return entry.Content, nil
}

func getPendingTasksData(ctx context.Context) ([]any, error) {
	if globalBrokerClient != nil {
		result, err := globalBrokerClient.CallTool(ctx, "get_pending_tasks", map[string]any{})
		if err != nil {
			return nil, err
		}
		taskList, _ := result["tasks"].([]any)
		return taskList, nil
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("tasks unavailable without broker or database")
	}
	tasks, err := globalAgentService.GetPendingTasks(ctx)
	if err != nil {
		return nil, err
	}
	return tasksToAny(tasks), nil
}

func getWorkableTasksData(ctx context.Context) ([]any, error) {
	if globalBrokerClient != nil {
		result, err := globalBrokerClient.CallTool(ctx, "get_workable_tasks", map[string]any{})
		if err != nil {
			return nil, err
		}
		taskList, _ := result["tasks"].([]any)
		return taskList, nil
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("tasks unavailable without broker or database")
	}
	tasks, err := globalAgentService.GetWorkableTasks(ctx)
	if err != nil {
		return nil, err
	}
	return tasksToAny(tasks), nil
}

func addTaskData(ctx context.Context, description string) (map[string]any, error) {
	if globalBrokerClient != nil {
		return globalBrokerClient.CallTool(ctx, "add_task", map[string]any{
			"description": description,
		})
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("tasks unavailable without broker or database")
	}
	task, err := globalAgentService.AddTask(ctx, description, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":          task.ID,
		"description": task.Description,
	}, nil
}

func getTaskContextData(ctx context.Context, taskID string) (map[string]any, error) {
	if globalBrokerClient != nil {
		return globalBrokerClient.CallTool(ctx, "get_task_context", map[string]any{
			"task_id": taskID,
		})
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("tasks unavailable without broker or database")
	}

	task, err := globalAgentService.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.ParentTaskID == "" {
		return map[string]any{}, nil
	}

	parent, err := globalAgentService.GetTask(ctx, task.ParentTaskID)
	if err != nil {
		return nil, err
	}
	subtasks, err := globalAgentService.GetSubtasks(ctx, task.ParentTaskID)
	if err != nil {
		return nil, err
	}
	sort.Slice(subtasks, func(i, j int) bool {
		return subtasks[i].Position < subtasks[j].Position
	})

	stepNum := 0
	completedOutcomes := make([]any, 0)
	for i, subtask := range subtasks {
		if subtask.ID == taskID {
			stepNum = i + 1
		}
		if subtask.Status == "done" || subtask.Status == "deprecated" {
			if subtask.Description != "" || subtask.Outcome != "" {
				completedOutcomes = append(completedOutcomes, map[string]any{
					"description": subtask.Description,
					"outcome":     subtask.Outcome,
				})
			}
		}
	}

	return map[string]any{
		"parent_description": parent.Description,
		"step_number":        float64(stepNum),
		"total_steps":        float64(len(subtasks)),
		"completed_outcomes": completedOutcomes,
	}, nil
}

func checkRecurringTasksData(ctx context.Context) (map[string]any, error) {
	if globalBrokerClient != nil {
		return globalBrokerClient.CallTool(ctx, "check_recurring_tasks", map[string]any{})
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("recurring tasks unavailable without broker or database")
	}
	created, err := globalAgentService.CheckAndCreateRecurringTasks(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"created": float64(created)}, nil
}

func getFinishedTasksData(ctx context.Context) ([]any, error) {
	if globalBrokerClient != nil {
		result, err := globalBrokerClient.CallTool(ctx, "get_finished_tasks", map[string]any{})
		if err != nil {
			return nil, err
		}
		taskList, _ := result["tasks"].([]any)
		return taskList, nil
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("tasks unavailable without broker or database")
	}
	tasks, err := globalAgentService.GetFinishedTasks(ctx)
	if err != nil {
		return nil, err
	}
	return tasksToAny(tasks), nil
}

func getRecurringTasksData(ctx context.Context) ([]any, error) {
	if globalBrokerClient != nil {
		result, err := globalBrokerClient.CallTool(ctx, "get_recurring_tasks", map[string]any{})
		if err != nil {
			return nil, err
		}
		taskList, _ := result["tasks"].([]any)
		return taskList, nil
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("recurring tasks unavailable without broker or database")
	}
	recurring, err := globalAgentService.GetRecurringTasks(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, len(recurring))
	for _, task := range recurring {
		items = append(items, map[string]any{
			"id":               task.ID,
			"description":      task.Description,
			"interval_minutes": float64(task.IntervalMinutes),
			"last_created_at":  task.LastCreatedAt.Format(time.RFC3339),
		})
	}
	return items, nil
}

func addRecurringTaskData(ctx context.Context, description string, intervalMinutes int) (map[string]any, error) {
	if globalBrokerClient != nil {
		return globalBrokerClient.CallTool(ctx, "add_recurring_task", map[string]any{
			"description":      description,
			"interval_minutes": intervalMinutes,
		})
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("recurring tasks unavailable without broker or database")
	}
	task, err := globalAgentService.AddRecurringTask(ctx, description, intervalMinutes)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":               task.ID,
		"description":      task.Description,
		"interval_minutes": float64(task.IntervalMinutes),
		"last_created_at":  task.LastCreatedAt.Format(time.RFC3339),
	}, nil
}

func deleteRecurringTaskData(ctx context.Context, taskID string) (map[string]any, error) {
	if globalBrokerClient != nil {
		return globalBrokerClient.CallTool(ctx, "delete_recurring_task", map[string]any{
			"id": taskID,
		})
	}
	if globalAgentService == nil {
		return nil, fmt.Errorf("recurring tasks unavailable without broker or database")
	}
	task, err := globalAgentService.GetRecurringTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := globalAgentService.DeleteRecurringTask(ctx, taskID); err != nil {
		return nil, err
	}
	return map[string]any{
		"id":          task.ID,
		"description": task.Description,
	}, nil
}

func tasksToAny(tasks []*data.Task) []any {
	items := make([]any, 0, len(tasks))
	for _, task := range tasks {
		entry := map[string]any{
			"id":             task.ID,
			"description":    task.Description,
			"status":         task.Status,
			"blocked":        task.Blocked,
			"parent_task_id": task.ParentTaskID,
			"outcome":        task.Outcome,
			"updated_at":     task.UpdatedAt.Format(time.RFC3339),
		}
		if task.SleepUntil != nil {
			entry["sleep_until"] = task.SleepUntil.Format(time.RFC3339)
		}
		items = append(items, entry)
	}
	return items
}
