package tools

// DescribeTool returns descriptions for task tools.
func (t *TaskTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"add_task": "Add a new task to your task list. Use this to track work you need to do. " +
			"Include enough detail in the description so you can fully understand the task later without additional context. " +
			"Use position='beginning' to prioritize a task, or 'end' (default) for normal priority. " +
			"Set parent_task_id to create a subtask under an existing task.",
		"mark_task_done": "Mark a task as done when you have completed it. " +
			"If all sibling subtasks are done, the parent task is auto-completed. " +
			"Use evaluate_task instead if you have findings to record.",
		"mark_task_deprecated": "Mark a task as deprecated when it's no longer needed or relevant. " +
			"Use this when circumstances change and a task becomes unnecessary.",
		"list_tasks": "List your current tasks. By default shows only pending tasks. " +
			"Set include_completed=true to also see done tasks. Deprecated tasks are never shown. " +
			"Tasks with parent_task_id are subtasks of a larger goal.",
		"move_task": "Move a task to the beginning or end of your task list. " +
			"Use this to reprioritize tasks. Moving to 'beginning' makes it your next task.",
		"block_task": "Mark a task as blocked when you cannot proceed on it. " +
			"Blocked tasks remain visible but are skipped during automatic task processing. " +
			"Use this when waiting on external dependencies or when a task needs human input.",
		"unblock_task": "Unblock a previously blocked task so it can be worked on again. " +
			"Use this when the blocker has been resolved.",
		"sleep_task": "Put a task to sleep for a specified number of minutes. " +
			"Sleeping tasks are skipped until the sleep time expires. " +
			"Use this to defer a task that should be revisited later (e.g., waiting for an external process, rate limiting, scheduled check-ins). " +
			"Set minutes=0 to wake a sleeping task immediately.",
		"plan_task": "Decompose a complex task into ordered subtasks. " +
			"Use when a task requires multiple distinct steps. " +
			"The parent task auto-completes when all subtasks are done.",
		"evaluate_task": "Record what you learned or accomplished and mark the task done. " +
			"Use instead of mark_task_done when you have findings to preserve. " +
			"Outcomes persist across sessions and are visible in heartbeat context.",
	}
	return descriptions[name]
}
