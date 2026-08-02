// Package objectives_planner is the autonomous mission planner that used to
// live inside `objectives`: the strategic planner, the execution engine, the
// post-execution evaluator, the scheduler and the planner's own memory store.
//
// It was split out so `objectives` could become what the life dashboard needs —
// goals with measurable progress and a REST/WS surface — without dragging in
// genai, agent_node and go-ethereum. The behaviour is unchanged; only the
// module boundary moved.
package objectives_planner

import (
	objectives "github.com/original-david-knight/go_wild/objectives"
)

// The planner half was written against these names unqualified. Aliasing them
// here keeps that code byte-identical across the split, so the move is a
// boundary change rather than a rewrite.
type (
	Objective        = objectives.Objective
	ObjectiveStatus  = objectives.ObjectiveStatus
	ObjectiveStore   = objectives.ObjectiveStore
	AutonomyLevel    = objectives.AutonomyLevel
	ScheduleType     = objectives.ScheduleType
	ActivityEvent    = objectives.ActivityEvent
	ActivityStore    = objectives.ActivityStore
	Escalation       = objectives.Escalation
	EventSeverity    = objectives.EventSeverity
	StatusRollup     = objectives.StatusRollup
	TreeMutation     = objectives.TreeMutation
	EscalationStatus = objectives.EscalationStatus
	MutationAction   = objectives.MutationAction
)

const (
	StatusPending   = objectives.StatusPending
	StatusActive    = objectives.StatusActive
	StatusBlocked   = objectives.StatusBlocked
	StatusCompleted = objectives.StatusCompleted
	StatusFailed    = objectives.StatusFailed
	StatusPaused    = objectives.StatusPaused

	AutonomyFull           = objectives.AutonomyFull
	AutonomyApprovePlan    = objectives.AutonomyApprovePlan
	AutonomyApproveActions = objectives.AutonomyApproveActions

	ScheduleOneShot    = objectives.ScheduleOneShot
	ScheduleCron       = objectives.ScheduleCron
	ScheduleEvent      = objectives.ScheduleEvent
	ScheduleContinuous = objectives.ScheduleContinuous

	SeverityInfo     = objectives.SeverityInfo
	SeverityWarning  = objectives.SeverityWarning
	SeverityError    = objectives.SeverityError
	SeverityCritical = objectives.SeverityCritical

	MutationAdd    = objectives.MutationAdd
	MutationRemove = objectives.MutationRemove
	MutationUpdate = objectives.MutationUpdate
	MutationMove   = objectives.MutationMove

	EscalationPending  = objectives.EscalationPending
	EscalationResolved = objectives.EscalationResolved
)

var (
	NewObjectiveStore = objectives.NewObjectiveStore
	NewActivityStore  = objectives.NewActivityStore
)
