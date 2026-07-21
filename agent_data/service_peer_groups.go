package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Peer group management functions (not scoped to an agent)

// GetPeerGroupsForAgent returns all peer groups that an agent belongs to.
func GetPeerGroupsForAgent(ctx context.Context, db gowild_data.Database, agentID string) ([]PeerGroup, error) {
	memberDAO := db.Table(PeerGroupMember{})
	results, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": agentID},
	})
	if err != nil {
		return nil, err
	}

	groupDAO := db.Table(PeerGroup{})
	var groups []PeerGroup
	for _, r := range results {
		m := r.(*PeerGroupMember)
		var g PeerGroup
		if err := groupDAO.Get(ctx, m.GroupID, &g); err == nil {
			groups = append(groups, g)
		}
	}
	return groups, nil
}

// ListPeerGroups returns all peer groups.
func ListPeerGroups(ctx context.Context, db gowild_data.Database) ([]PeerGroup, error) {
	dao := db.Table(PeerGroup{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	groups := make([]PeerGroup, len(results))
	for i, r := range results {
		groups[i] = *r.(*PeerGroup)
	}
	return groups, nil
}

// CreatePeerGroup creates a new peer group.
func CreatePeerGroup(ctx context.Context, db gowild_data.Database, name string) (*PeerGroup, error) {
	g := &PeerGroup{
		ID:        newID(),
		Name:      name,
		CreatedAt: time.Now(),
	}
	if err := db.Table(PeerGroup{}).Insert(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// DeletePeerGroup deletes a group and all its memberships.
func DeletePeerGroup(ctx context.Context, db gowild_data.Database, groupID string) error {
	// Delete memberships first
	memberDAO := db.Table(PeerGroupMember{})
	members, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"group_id": groupID},
	})
	if err != nil {
		return err
	}
	for _, r := range members {
		if err := memberDAO.Delete(ctx, r.(*PeerGroupMember).ID); err != nil {
			return err
		}
	}
	return db.Table(PeerGroup{}).Delete(ctx, groupID)
}

// AddAgentToGroup adds an agent to a peer group.
func AddAgentToGroup(ctx context.Context, db gowild_data.Database, groupID, agentID string) error {
	// Check for existing membership
	memberDAO := db.Table(PeerGroupMember{})
	existing, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"group_id": groupID, "agent_id": agentID},
	})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil // already a member
	}

	m := &PeerGroupMember{
		ID:        newID(),
		GroupID:   groupID,
		AgentID:   agentID,
		CreatedAt: time.Now(),
	}
	return memberDAO.Insert(ctx, m)
}

// RemoveAgentFromGroup removes an agent from a peer group.
func RemoveAgentFromGroup(ctx context.Context, db gowild_data.Database, groupID, agentID string) error {
	memberDAO := db.Table(PeerGroupMember{})
	results, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"group_id": groupID, "agent_id": agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		if err := memberDAO.Delete(ctx, r.(*PeerGroupMember).ID); err != nil {
			return err
		}
	}
	return nil
}

// GetGroupMembers returns all members of a peer group.
func GetGroupMembers(ctx context.Context, db gowild_data.Database, groupID string) ([]PeerGroupMember, error) {
	memberDAO := db.Table(PeerGroupMember{})
	results, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"group_id": groupID},
	})
	if err != nil {
		return nil, err
	}
	members := make([]PeerGroupMember, len(results))
	for i, r := range results {
		members[i] = *r.(*PeerGroupMember)
	}
	return members, nil
}
