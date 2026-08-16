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

// Objective is a node in the objective tree. It carries identity, tree
// position, content, lifecycle and measurable progress — and nothing about how
// some agent would pursue it. The mission-planner fields (schedule, tool
// allowlist, autonomy level, cooldown, last result) left the node with the
// planner they served; their Postgres columns are orphaned and inert, because
// the data/ migrations are additive-only.
type Objective struct {
	ID          string          `json:"id"`
	CompanyID   string          `json:"company_id"`
	ParentID    string          `json:"parent_id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      ObjectiveStatus `json:"status"`
	Priority    int             `json:"priority"`
	Depth       int             `json:"depth"`
	Deadline    time.Time       `json:"deadline"`
	// Target/Current/Unit carry measurable progress: "198 → 180 lb" is a
	// Target of 180, a Current of 197.4 and a Unit of "lb". Objectives with
	// no measurable target leave them zero and report progress another way.
	Target  float64 `json:"target"`
	Current float64 `json:"current"`
	Unit    string  `json:"unit"`
	// Revision counts the writes applied to this node. The store neither sets
	// nor bumps it: when to count a write is concurrency policy, and policy
	// belongs to the layer that owns the write path. This column only makes
	// the count durable.
	Revision    int            `json:"revision"`
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
	Action      MutationAction  `json:"action"`
	ObjectiveID string          `json:"objective_id,omitempty"` // for update/remove/move
	ParentID    string          `json:"parent_id,omitempty"`    // for add/move
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Status      ObjectiveStatus `json:"status,omitempty"`
	Priority    int             `json:"priority,omitempty"`
}
