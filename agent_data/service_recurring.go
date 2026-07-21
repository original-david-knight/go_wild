package data

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Recurring Task operations

// AddRecurringTask creates a new recurring task.
func (s *AgentService) AddRecurringTask(ctx context.Context, description string, intervalMinutes int) (*RecurringTask, error) {
	dao := s.db.Table(RecurringTask{})
	now := time.Now()

	recurring := &RecurringTask{
		ID:              newID(),
		AgentID:         s.agentID,
		Description:     description,
		IntervalMinutes: intervalMinutes,
		LastCreatedAt:   now, // Start counting from now
		CreatedAt:       now,
	}

	if err := dao.Insert(ctx, recurring); err != nil {
		return nil, err
	}
	return recurring, nil
}

// GetRecurringTasks retrieves all recurring tasks for the agent.
func (s *AgentService) GetRecurringTasks(ctx context.Context) ([]*RecurringTask, error) {
	dao := s.db.Table(RecurringTask{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": s.agentID},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}

	recurring := make([]*RecurringTask, len(results))
	for i, r := range results {
		recurring[i] = r.(*RecurringTask)
	}
	return recurring, nil
}

// GetRecurringTask retrieves a recurring task by ID.
func (s *AgentService) GetRecurringTask(ctx context.Context, id string) (*RecurringTask, error) {
	dao := s.db.Table(RecurringTask{})
	var recurring RecurringTask
	if err := dao.Get(ctx, id, &recurring); err != nil {
		return nil, err
	}
	if recurring.AgentID != s.agentID {
		return nil, fmt.Errorf("recurring task not found")
	}
	return &recurring, nil
}

// DeleteRecurringTask deletes a recurring task.
func (s *AgentService) DeleteRecurringTask(ctx context.Context, id string) error {
	recurring, err := s.GetRecurringTask(ctx, id)
	if err != nil {
		return err
	}
	return s.db.Table(RecurringTask{}).Delete(ctx, recurring.ID)
}

// CheckAndCreateRecurringTasks checks if any recurring tasks are due and creates new tasks from them.
// Skips creating a task if the previous recurrence is still pending (not done or deprecated).
// Returns the number of tasks created.
func (s *AgentService) CheckAndCreateRecurringTasks(ctx context.Context) (int, error) {
	recurring, err := s.GetRecurringTasks(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	created := 0

	for _, rt := range recurring {
		// Check if enough time has passed since last creation
		nextDue := rt.LastCreatedAt.Add(time.Duration(rt.IntervalMinutes) * time.Minute)
		if now.Before(nextDue) {
			continue // Not yet due
		}

		// Check if there's a pending task from this recurring task
		hasPending, err := s.hasPendingTaskFromRecurring(ctx, rt.ID)
		if err != nil {
			return created, err
		}
		if hasPending {
			continue // Previous recurrence not completed, skip
		}

		// Create new task from recurring template
		task, err := s.createTaskFromRecurring(ctx, rt)
		if err != nil {
			return created, err
		}
		if task != nil {
			created++
		}

		// Update last created time
		rt.LastCreatedAt = now
		if err := s.db.Table(RecurringTask{}).Update(ctx, rt); err != nil {
			return created, err
		}
	}

	return created, nil
}

// hasPendingTaskFromRecurring checks if there's a pending task from a recurring task.
func (s *AgentService) hasPendingTaskFromRecurring(ctx context.Context, recurringTaskID string) (bool, error) {
	dao := s.db.Table(Task{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"agent_id":          s.agentID,
			"recurring_task_id": recurringTaskID,
			"status":            "pending",
		},
		Limit: 1,
	})
	if err != nil {
		return false, err
	}
	return len(results) > 0, nil
}

// createTaskFromRecurring creates a new task from a recurring task template.
func (s *AgentService) createTaskFromRecurring(ctx context.Context, rt *RecurringTask) (*Task, error) {
	dao := s.db.Table(Task{})
	now := time.Now()

	// Get max position for ordering
	maxPos := 0
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID},
		OrderBy:   "position",
		OrderDesc: true,
		Limit:     1,
	})
	if err == nil && len(results) > 0 {
		maxPos = results[0].(*Task).Position + 1
	}

	task := &Task{
		ID:              newID(),
		AgentID:         s.agentID,
		Description:     rt.Description,
		Status:          "pending",
		Blocked:         false,
		Position:        maxPos,
		RecurringTaskID: rt.ID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := dao.Insert(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// deleteAllRecurringTasks deletes all recurring tasks for the agent.
func (s *AgentService) deleteAllRecurringTasks(ctx context.Context) error {
	dao := s.db.Table(RecurringTask{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		rt := r.(*RecurringTask)
		if err := dao.Delete(ctx, rt.ID); err != nil {
			return err
		}
	}
	return nil
}
