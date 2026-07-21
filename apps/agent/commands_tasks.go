package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func handleTasksCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printTasks()
	return cmdContinue
}

func handleAddTaskCommand(cm data.CommandMessage, _ commandContext) commandResult {
	description := cmdArg(cm, "description")
	if description == "" {
		output.Error("Usage: /addtask <description>")
		return cmdContinue
	}
	addTask(description)
	return cmdContinue
}

func handleFinishedCommand(_ data.CommandMessage, _ commandContext) commandResult {
	printFinishedTasks()
	return cmdContinue
}

func handleWorkTasksCommand(_ data.CommandMessage, _ commandContext) commandResult {
	status, err := checkWorkTasks(context.Background())
	if err != nil {
		output.Error("Error getting tasks: %v", err)
		return cmdContinue
	}
	if status.recurringErr != nil {
		output.SystemWarning("Recurring task check failed: %v", status.recurringErr)
	}
	if status.created > 0 {
		output.System("Created %d task(s) from recurring templates", status.created)
	}
	if status.pendingCount == 0 {
		output.System("No pending tasks to work on.")
		return cmdContinue
	}
	if status.blockedMessage != "" {
		output.System("%s", status.blockedMessage)
		return cmdContinue
	}

	preparedWorkTasksPrompt = status.prompt
	workTasksMode = true
	workTasksStartTime = time.Now()
	output.System("📋 Work Tasks Mode enabled - agent will work through pending tasks")
	output.System("   Use /tasks to see pending, or /stoptasks to exit this mode")
	// Return the initial task prompt
	return cmdWorkTasks
}

func handleStopTasksCommand(_ data.CommandMessage, _ commandContext) commandResult {
	workTasksMode = false
	output.System("📋 Work Tasks Mode disabled")
	return cmdContinue
}

// printTasks displays pending tasks, grouped by parent.
func printTasks() {
	taskList, err := getPendingTasksData(context.Background())
	if err != nil {
		output.Error("Error loading tasks: %v", err)
		return
	}
	if len(taskList) == 0 {
		output.System("No pending tasks.")
		return
	}

	// Count blocked tasks
	blockedCount := 0
	for _, t := range taskList {
		task, _ := t.(map[string]any)
		if blocked, _ := task["blocked"].(bool); blocked {
			blockedCount++
		}
	}

	var header string
	if blockedCount > 0 {
		header = fmt.Sprintf("Pending Tasks (%d, %d blocked):", len(taskList), blockedCount)
	} else {
		header = fmt.Sprintf("Pending Tasks (%d):", len(taskList))
	}
	output.System("%s", header)

	// Group tasks: top-level and subtasks by parent
	type taskInfo struct {
		id, desc, parentID string
		blocked            bool
	}
	var topLevel []taskInfo
	subtasksByParent := make(map[string][]taskInfo)
	parentIDs := make(map[string]bool) // track which IDs are parents

	for _, t := range taskList {
		task, _ := t.(map[string]any)
		info := taskInfo{
			id:       strVal(task, "id"),
			desc:     strVal(task, "description"),
			parentID: strVal(task, "parent_task_id"),
		}
		info.blocked, _ = task["blocked"].(bool)

		if info.parentID != "" {
			subtasksByParent[info.parentID] = append(subtasksByParent[info.parentID], info)
			parentIDs[info.parentID] = true
		} else {
			topLevel = append(topLevel, info)
		}
	}

	for _, t := range topLevel {
		if t.blocked {
			output.System("  ⊘ %s: %s [BLOCKED]", t.id, t.desc)
		} else if parentIDs[t.id] {
			output.System("  ▸ %s: %s", t.id, t.desc)
		} else {
			output.System("  • %s: %s", t.id, t.desc)
		}
		// Show subtasks indented
		for _, st := range subtasksByParent[t.id] {
			if st.blocked {
				output.System("    ⊘ %s: %s [BLOCKED]", st.id, st.desc)
			} else {
				output.System("    ◦ %s: %s", st.id, st.desc)
			}
		}
	}

	// Show orphan subtasks whose parent is already done/not in pending list
	for parentID, subs := range subtasksByParent {
		if parentIDs[parentID] {
			// Already shown above under the parent
			found := false
			for _, t := range topLevel {
				if t.id == parentID {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		output.System("  (subtasks of %s):", parentID)
		for _, st := range subs {
			if st.blocked {
				output.System("    ⊘ %s: %s [BLOCKED]", st.id, st.desc)
			} else {
				output.System("    ◦ %s: %s", st.id, st.desc)
			}
		}
	}
}

// strVal safely extracts a string from a map.
func strVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// addTask adds a new task using the active runtime backend.
func addTask(description string) {
	ctx := context.Background()
	result, err := addTaskData(ctx, description)
	if err != nil {
		output.Error("Error adding task: %v", err)
		return
	}
	taskID, _ := result["id"].(string)
	output.SystemSuccess("Task %s added: %s", taskID, description)
}

// generateWorkTasksPrompt builds a prompt for the next workable task.
func generateWorkTasksPrompt() string {
	ctx := context.Background()
	taskList, err := getWorkableTasksData(ctx)
	if err != nil {
		return ""
	}
	if len(taskList) == 0 {
		return ""
	}
	return generateWorkTasksPromptFromList(taskList)
}

func generateWorkTasksPromptFromList(taskList []any) string {
	if len(taskList) == 0 {
		return ""
	}
	ctx := context.Background()
	first, ok := taskList[0].(map[string]any)
	if !ok {
		return ""
	}
	taskID, _ := first["id"].(string)
	taskDesc, _ := first["description"].(string)
	parentTaskID, _ := first["parent_task_id"].(string)

	var sb strings.Builder

	fmt.Fprintf(&sb, "This is a heartbeat for task %s\n\n", taskID)

	// If this is a subtask, fetch plan context
	if parentTaskID != "" {
		tcResult, err := getTaskContextData(ctx, taskID)
		if err == nil {
			parentDesc, _ := tcResult["parent_description"].(string)
			stepNum, _ := tcResult["step_number"].(float64)
			totalSteps, _ := tcResult["total_steps"].(float64)

			if parentDesc != "" {
				fmt.Fprintf(&sb, "Plan Context: You are on step %d of %d for goal: %s\n\n",
					int(stepNum), int(totalSteps), parentDesc)
			}

			if outcomes, ok := tcResult["completed_outcomes"].([]any); ok && len(outcomes) > 0 {
				sb.WriteString("Completed steps:\n")
				for _, o := range outcomes {
					if om, ok := o.(map[string]any); ok {
						desc, _ := om["description"].(string)
						outcome, _ := om["outcome"].(string)
						fmt.Fprintf(&sb, "- %s -> %s\n", desc, outcome)
					}
				}
				sb.WriteString("\n")
			}
		}
	}

	fmt.Fprintf(&sb, "%s\n\n", taskDesc)
	fmt.Fprintf(&sb, "When you complete this task, use evaluate_task to record what you learned/accomplished (preferred) or mark_task_done to mark it complete. "+
		"If the task is no longer relevant, use mark_task_deprecated to remove it. "+
		"If you identify additional work that needs to be done, use add_task to create new tasks. "+
		"For complex tasks, use plan_task to break them into steps. "+
		"If you want to come back to a task later, use sleep_task to defer it. "+
		"You have %d workable task(s).", len(taskList))

	return sb.String()
}

type workTasksStatus struct {
	prompt          string
	blockedMessage  string
	nonWorkableKind string
	pendingCount    int
	created         int
	recurringErr    error
}

var preparedWorkTasksPrompt string

func consumePreparedWorkTasksPrompt() string {
	if preparedWorkTasksPrompt == "" {
		return generateWorkTasksPrompt()
	}
	prompt := preparedWorkTasksPrompt
	preparedWorkTasksPrompt = ""
	return prompt
}

func checkWorkTasks(ctx context.Context) (workTasksStatus, error) {
	status := workTasksStatus{}

	// Ensure recurring tasks are materialized before checking workable tasks.
	if result, err := checkRecurringTasksData(ctx); err == nil {
		if c, ok := result["created"].(float64); ok && c > 0 {
			status.created = int(c)
		}
	} else {
		status.recurringErr = err
	}

	// Check pending tasks first (no tasks -> no-op)
	pendingTasks, err := getPendingTasksData(ctx)
	if err != nil {
		return status, err
	}
	status.pendingCount = len(pendingTasks)
	if status.pendingCount == 0 {
		return status, nil
	}

	// Check workable tasks
	workableTasks, err := getWorkableTasksData(ctx)
	if err != nil {
		return status, err
	}
	if len(workableTasks) == 0 {
		status.blockedMessage, status.nonWorkableKind = describeNonWorkableTasks(pendingTasks)
		return status, nil
	}

	status.prompt = generateWorkTasksPromptFromList(workableTasks)
	return status, nil
}

func describeNonWorkableTasks(pendingTasks []any) (string, string) {
	total := len(pendingTasks)
	if total == 0 {
		return "", ""
	}
	blocked := 0
	sleeping := 0
	now := time.Now()

	for _, t := range pendingTasks {
		task, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if isBlocked, _ := task["blocked"].(bool); isBlocked {
			blocked++
			continue
		}
		if sleepUntilStr, _ := task["sleep_until"].(string); sleepUntilStr != "" {
			if sleepUntil, err := time.Parse(time.RFC3339, sleepUntilStr); err == nil && sleepUntil.After(now) {
				sleeping++
			}
		}
	}

	if blocked+sleeping < total {
		return fmt.Sprintf("No workable tasks - all %d pending task(s) are blocked or sleeping.", total), "blocked_or_sleeping"
	}
	if sleeping > 0 && blocked > 0 {
		return fmt.Sprintf("No workable tasks - all %d pending task(s) are blocked or sleeping.", total), "blocked_or_sleeping"
	}
	if sleeping > 0 {
		return fmt.Sprintf("No workable tasks - all %d pending task(s) are sleeping.", total), "sleeping"
	}
	return fmt.Sprintf("No workable tasks - all %d pending task(s) are blocked.", total), "blocked"
}

func formatBlockedHeartbeat(kind, message string) string {
	if message == "" {
		return ""
	}
	prefix := "This is a heartbeat for blocked tasks"
	switch kind {
	case "sleeping":
		prefix = "This is a heartbeat for sleeping tasks"
	case "blocked_or_sleeping":
		prefix = "This is a heartbeat for blocked or sleeping tasks"
	}
	return fmt.Sprintf("%s\n\n%s", prefix, message)
}

// printFinishedTasks displays done and deprecated tasks.
func printFinishedTasks() {
	ctx := context.Background()
	taskList, err := getFinishedTasksData(ctx)
	if err != nil {
		output.Error("Error loading tasks: %v", err)
		return
	}
	if len(taskList) == 0 {
		output.System("No finished or deprecated tasks.")
		return
	}

	output.System("Finished & Deprecated Tasks (%d):", len(taskList))

	for _, t := range taskList {
		task, ok := t.(map[string]any)
		if !ok {
			continue
		}
		taskID, _ := task["id"].(string)
		description, _ := task["description"].(string)
		status, _ := task["status"].(string)
		updatedAtStr, _ := task["updated_at"].(string)
		outcome, _ := task["outcome"].(string)

		var age time.Duration
		if updatedAt, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
			age = time.Since(updatedAt)
		}
		ageStr := formatDuration(age)

		statusIcon := ""
		if status == "deprecated" {
			statusIcon = ""
		}
		output.System("  %s %s: %s", statusIcon, taskID, description)
		if outcome != "" {
			output.System("    outcome: %s", outcome)
		}
		output.System("    %s %s ago", status, ageStr)
	}
}
