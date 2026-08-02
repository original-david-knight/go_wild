package objectives

import (
	"time"
)

// ObjectiveStatus represents the lifecycle state of an objective.
type ObjectiveStatus string

const (
	StatusPending   ObjectiveStatus = "pending"
	StatusActive    ObjectiveStatus = "active"
	StatusBlocked   ObjectiveStatus = "blocked"
	StatusCompleted ObjectiveStatus = "completed"
	StatusFailed    ObjectiveStatus = "failed"
	StatusPaused    ObjectiveStatus = "paused"
)

// AutonomyLevel controls how much human oversight an objective requires.
type AutonomyLevel string

const (
	AutonomyFull           AutonomyLevel = "full"
	AutonomyApprovePlan    AutonomyLevel = "approve_plan"
	AutonomyApproveActions AutonomyLevel = "approve_actions"
)

// ScheduleType describes how an objective is triggered.
type ScheduleType string

const (
	ScheduleOneShot    ScheduleType = "one_shot"
	ScheduleCron       ScheduleType = "cron"
	ScheduleEvent      ScheduleType = "event"
	ScheduleContinuous ScheduleType = "continuous"
)

// Objective is a node in the objective tree. It represents a goal at any
// level of abstraction, from a high-level mission down to a concrete task.
type Objective struct {
	ID            string          `json:"id"`
	CompanyID     string          `json:"company_id"`
	ParentID      string          `json:"parent_id"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Status        ObjectiveStatus `json:"status"`
	Priority      int             `json:"priority"`
	Depth         int             `json:"depth"`
	ScheduleType  ScheduleType    `json:"schedule_type"`
	ScheduleCron  string          `json:"schedule_cron"`
	ScheduleEvent string          `json:"schedule_event"`
	ToolAllowlist []string        `json:"tool_allowlist"`
	AutonomyLevel AutonomyLevel   `json:"autonomy_level"`
	Deadline      time.Time       `json:"deadline"`
	CooldownUntil time.Time       `json:"cooldown_until"`
	LastResult    string          `json:"last_result"`
	// Target/Current/Unit carry measurable progress: "198 → 180 lb" is a
	// Target of 180, a Current of 197.4 and a Unit of "lb". Objectives with
	// no measurable target leave them zero and report progress another way.
	Target      float64        `json:"target"`
	Current     float64        `json:"current"`
	Unit        string         `json:"unit"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CompletedAt time.Time      `json:"completed_at"`
}

// EventSeverity classifies the importance of an activity event.
type EventSeverity string

const (
	SeverityInfo     EventSeverity = "info"
	SeverityWarning  EventSeverity = "warning"
	SeverityError    EventSeverity = "error"
	SeverityCritical EventSeverity = "critical"
)

// ActivityEvent is an append-only log entry recording a system action.
type ActivityEvent struct {
	ID          string         `json:"id"`
	ObjectiveID string         `json:"objective_id"`
	EventType   string         `json:"event_type"`
	Severity    EventSeverity  `json:"severity"`
	Summary     string         `json:"summary"`
	Details     map[string]any `json:"details"`
	CreatedAt   time.Time      `json:"created_at"`
}

// EscalationStatus tracks the state of a human escalation request.
type EscalationStatus string

const (
	EscalationPending  EscalationStatus = "pending"
	EscalationResolved EscalationStatus = "resolved"
)

// Escalation represents a request for human help.
type Escalation struct {
	ID          string           `json:"id"`
	ObjectiveID string           `json:"objective_id"`
	Question    string           `json:"question"`
	Context     string           `json:"context"`
	Severity    EventSeverity    `json:"severity"`
	Status      EscalationStatus `json:"status"`
	Resolution  string           `json:"resolution"`
	CreatedAt   time.Time        `json:"created_at"`
	ResolvedAt  time.Time        `json:"resolved_at"`
}

// MutationAction describes what kind of tree modification to apply.
type MutationAction string

const (
	MutationAdd    MutationAction = "add"
	MutationRemove MutationAction = "remove"
	MutationUpdate MutationAction = "update"
	MutationMove   MutationAction = "move"
)

// TreeMutation describes a single modification to the objective tree.
type TreeMutation struct {
	Action        MutationAction  `json:"action"`
	ObjectiveID   string          `json:"objective_id,omitempty"` // for update/remove/move
	ParentID      string          `json:"parent_id,omitempty"`    // for add/move
	Title         string          `json:"title,omitempty"`
	Description   string          `json:"description,omitempty"`
	Status        ObjectiveStatus `json:"status,omitempty"`
	Priority      int             `json:"priority,omitempty"`
	ScheduleType  ScheduleType    `json:"schedule_type,omitempty"`
	ScheduleCron  string          `json:"schedule_cron,omitempty"`
	ToolAllowlist []string        `json:"tool_allowlist,omitempty"`
}
