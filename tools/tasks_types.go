package tools

// AddTaskInput defines the input for adding a task.
type AddTaskInput struct {
	Description  string `json:"description" description:"A clear description of the task to accomplish" required:"true"`
	Position     string `json:"position" description:"Where to add the task: 'beginning' or 'end'. Default is 'end'." required:"false" enum:"beginning,end"`
	ParentTaskID string `json:"parent_task_id" description:"Optional parent task ID to create this as a subtask" required:"false"`
}

// MarkTaskDoneInput defines the input for marking a task done.
type MarkTaskDoneInput struct {
	TaskID string `json:"task_id" description:"The ID of the task to mark as done" required:"true"`
}

// MarkTaskDeprecatedInput defines the input for marking a task deprecated.
type MarkTaskDeprecatedInput struct {
	TaskID string `json:"task_id" description:"The ID of the task to mark as deprecated" required:"true"`
	Reason string `json:"reason" description:"Why this task is no longer needed" required:"false"`
}

// ListTasksInput defines the input for listing tasks.
type ListTasksInput struct {
	IncludeCompleted bool `json:"include_completed" description:"If true, also include done tasks. Default false (only pending). Deprecated tasks are never shown." required:"false"`
}

// MoveTaskInput defines the input for moving a task.
type MoveTaskInput struct {
	TaskID   string `json:"task_id" description:"The ID of the task to move" required:"true"`
	Position string `json:"position" description:"Where to move the task: 'beginning' or 'end'" required:"true" enum:"beginning,end"`
}

// BlockTaskInput defines the input for blocking a task.
type BlockTaskInput struct {
	TaskID string `json:"task_id" description:"The ID of the task to block" required:"true"`
	Reason string `json:"reason" description:"Why this task is blocked (e.g., waiting on something)" required:"false"`
}

// UnblockTaskInput defines the input for unblocking a task.
type UnblockTaskInput struct {
	TaskID string `json:"task_id" description:"The ID of the task to unblock" required:"true"`
}

// SleepTaskInput defines the input for sleeping a task.
type SleepTaskInput struct {
	TaskID  string `json:"task_id" description:"The ID of the task to sleep" required:"true"`
	Minutes int    `json:"minutes" description:"Number of minutes to sleep the task. Use 0 to wake immediately." required:"true"`
}

// PlanTaskInput defines the input for decomposing a task into subtasks.
type PlanTaskInput struct {
	TaskID string   `json:"task_id" description:"The ID of the task to decompose into subtasks" required:"true"`
	Steps  []string `json:"steps" description:"Ordered list of step descriptions. Each becomes a subtask." required:"true"`
}

// EvaluateTaskInput defines the input for evaluating a completed task.
type EvaluateTaskInput struct {
	TaskID  string `json:"task_id" description:"The ID of the task to evaluate" required:"true"`
	Outcome string `json:"outcome" description:"What was learned or accomplished. Persists across sessions." required:"true"`
}
