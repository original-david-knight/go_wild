package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Task operations

// AddTask adds a new task for the agent.
// If position is "beginning", it's inserted at position 0; otherwise at the end.
func (s *AgentService) AddTask(ctx context.Context, description, position string) (*Task, error) {
	return s.AddTaskWithParent(ctx, description, position, "")
}

// AddTaskWithParent adds a new task with an optional parent task ID.
func (s *AgentService) AddTaskWithParent(ctx context.Context, description, position, parentTaskID string) (*Task, error) {
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
		ID:           newID(),
		AgentID:      s.agentID,
		Description:  description,
		Status:       "pending",
		Blocked:      false,
		ParentTaskID: parentTaskID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if position == "beginning" {
		// Shift all existing tasks by incrementing their position
		allTasks, err := s.GetPendingTasks(ctx)
		if err == nil {
			for _, t := range allTasks {
				t.Position++
				_ = dao.Update(ctx, t)
			}
		}
		task.Position = 0
	} else {
		task.Position = maxPos
	}

	if err := dao.Insert(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

// GetTask retrieves a task by ID.
func (s *AgentService) GetTask(ctx context.Context, taskID string) (*Task, error) {
	dao := s.db.Table(Task{})
	var task Task
	if err := dao.Get(ctx, taskID, &task); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task not found")
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	if task.AgentID != s.agentID {
		return nil, fmt.Errorf("task not found")
	}
	return &task, nil
}

// GetPendingTasks retrieves all pending tasks for the agent, ordered by position.
func (s *AgentService) GetPendingTasks(ctx context.Context) ([]*Task, error) {
	dao := s.db.Table(Task{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": s.agentID, "status": "pending"},
		OrderBy: "position",
	})
	if err != nil {
		return nil, err
	}

	tasks := make([]*Task, len(results))
	for i, r := range results {
		tasks[i] = r.(*Task)
	}
	return tasks, nil
}

// GetWorkableTasks retrieves pending tasks that are not blocked and not sleeping.
// Parents that have pending subtasks are skipped so the agent works on subtasks instead.
func (s *AgentService) GetWorkableTasks(ctx context.Context) ([]*Task, error) {
	tasks, err := s.GetPendingTasks(ctx)
	if err != nil {
		return nil, err
	}

	// Build set of parent IDs that have pending subtasks
	parentsWithPendingSubtasks := make(map[string]bool)
	for _, t := range tasks {
		if t.ParentTaskID != "" {
			parentsWithPendingSubtasks[t.ParentTaskID] = true
		}
	}

	now := time.Now()
	var workable []*Task
	for _, t := range tasks {
		if t.Blocked {
			continue
		}
		if t.SleepUntil != nil && t.SleepUntil.After(now) {
			continue
		}
		// Skip parents that have pending subtasks
		if parentsWithPendingSubtasks[t.ID] {
			continue
		}
		workable = append(workable, t)
	}
	return workable, nil
}

// GetFinishedTasks retrieves done and deprecated tasks.
func (s *AgentService) GetFinishedTasks(ctx context.Context) ([]*Task, error) {
	dao := s.db.Table(Task{})

	// Query done tasks
	doneResults, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID, "status": "done"},
		OrderBy:   "updated_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, err
	}

	// Query deprecated tasks
	deprecatedResults, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"agent_id": s.agentID, "status": "deprecated"},
		OrderBy:   "updated_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, err
	}

	// Combine results
	tasks := make([]*Task, 0, len(doneResults)+len(deprecatedResults))
	for _, r := range doneResults {
		tasks = append(tasks, r.(*Task))
	}
	for _, r := range deprecatedResults {
		tasks = append(tasks, r.(*Task))
	}
	return tasks, nil
}

// UpdateTaskStatus updates a task's status.
func (s *AgentService) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.Status = status
	task.UpdatedAt = time.Now()
	return s.db.Table(Task{}).Update(ctx, task)
}

// SetTaskBlocked sets the blocked state of a task.
func (s *AgentService) SetTaskBlocked(ctx context.Context, taskID string, blocked bool) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.Blocked = blocked
	task.UpdatedAt = time.Now()
	return s.db.Table(Task{}).Update(ctx, task)
}

// SleepTask sets a task to sleep until the specified time.
// Pass nil to wake the task immediately.
func (s *AgentService) SleepTask(ctx context.Context, taskID string, until *time.Time) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.SleepUntil = until
	task.UpdatedAt = time.Now()
	return s.db.Table(Task{}).Update(ctx, task)
}

// MoveTask moves a task to the beginning or end of the list.
func (s *AgentService) MoveTask(ctx context.Context, taskID, position string) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	dao := s.db.Table(Task{})
	allTasks, err := s.GetPendingTasks(ctx)
	if err != nil {
		return err
	}

	if position == "beginning" {
		// Move to position 0, shift others
		for _, t := range allTasks {
			if t.ID != taskID && t.Position <= task.Position {
				t.Position++
				_ = dao.Update(ctx, t)
			}
		}
		task.Position = 0
	} else {
		// Find max position and set to max+1
		maxPos := 0
		for _, t := range allTasks {
			if t.Position > maxPos {
				maxPos = t.Position
			}
		}
		task.Position = maxPos + 1
	}

	task.UpdatedAt = time.Now()
	return dao.Update(ctx, task)
}

// GetSubtasks retrieves subtasks for a given parent task, ordered by position.
func (s *AgentService) GetSubtasks(ctx context.Context, parentTaskID string) ([]*Task, error) {
	dao := s.db.Table(Task{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": s.agentID, "parent_task_id": parentTaskID},
		OrderBy: "position",
	})
	if err != nil {
		return nil, err
	}

	tasks := make([]*Task, len(results))
	for i, r := range results {
		tasks[i] = r.(*Task)
	}
	return tasks, nil
}

// AutoCompleteParent checks if all subtasks of a parent are done/deprecated and auto-completes the parent.
// Returns true if the parent was auto-completed.
func (s *AgentService) AutoCompleteParent(ctx context.Context, parentTaskID string) (bool, error) {
	if parentTaskID == "" {
		return false, nil
	}

	subtasks, err := s.GetSubtasks(ctx, parentTaskID)
	if err != nil {
		return false, err
	}
	if len(subtasks) == 0 {
		return false, nil
	}

	for _, st := range subtasks {
		if st.Status == "pending" {
			return false, nil
		}
	}

	// All subtasks are done/deprecated — mark parent done
	if err := s.UpdateTaskStatus(ctx, parentTaskID, "done"); err != nil {
		return false, err
	}
	return true, nil
}

// SetTaskOutcome sets the outcome of a task and marks it done.
func (s *AgentService) SetTaskOutcome(ctx context.Context, taskID, outcome string) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.Outcome = outcome
	task.Status = "done"
	task.UpdatedAt = time.Now()
	return s.db.Table(Task{}).Update(ctx, task)
}

// DeleteAllTasks deletes all tasks for the agent (used in agent deletion).
func (s *AgentService) deleteAllTasks(ctx context.Context) error {
	dao := s.db.Table(Task{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		task := r.(*Task)
		if err := dao.Delete(ctx, task.ID); err != nil {
			return err
		}
	}
	return nil
}
