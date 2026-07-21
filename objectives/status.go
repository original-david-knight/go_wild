package objectives

import (
	"context"
)

// StatusRollup aggregates status information for an objective and its children.
type StatusRollup struct {
	Objective      *Objective     `json:"objective"`
	ChildCount     int            `json:"child_count"`
	CompletedCount int            `json:"completed_count"`
	FailedCount    int            `json:"failed_count"`
	ActiveCount    int            `json:"active_count"`
	LastActivity   *ActivityEvent `json:"last_activity,omitempty"`
}

// GetObjectiveStatus returns a status rollup for a single objective.
func GetObjectiveStatus(ctx context.Context, store *ObjectiveStore, activity *ActivityStore, objectiveID string) (*StatusRollup, error) {
	obj, err := store.Get(ctx, objectiveID)
	if err != nil {
		return nil, err
	}

	children, err := store.GetChildren(ctx, obj.ID)
	if err != nil {
		return nil, err
	}

	rollup := &StatusRollup{
		Objective:  obj,
		ChildCount: len(children),
	}

	for _, child := range children {
		switch child.Status {
		case StatusCompleted:
			rollup.CompletedCount++
		case StatusFailed:
			rollup.FailedCount++
		case StatusActive:
			rollup.ActiveCount++
		}
	}

	events, err := activity.GetEvents(ctx, objectiveID, 1)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		rollup.LastActivity = events[0]
	}

	return rollup, nil
}

// GetTreeStatus returns status rollups for every node in a subtree.
func GetTreeStatus(ctx context.Context, store *ObjectiveStore, activity *ActivityStore, rootID string) ([]*StatusRollup, error) {
	tree, err := store.GetTree(ctx, rootID)
	if err != nil {
		return nil, err
	}

	var rollups []*StatusRollup
	for _, obj := range tree {
		rollup, err := GetObjectiveStatus(ctx, store, activity, obj.ID)
		if err != nil {
			return nil, err
		}
		rollups = append(rollups, rollup)
	}

	return rollups, nil
}
