package data

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// GetPeerAgents returns agents that share any peer group with this agent.
func (s *AgentService) GetPeerAgents(ctx context.Context) ([]AgentInfo, error) {
	memberDAO := s.db.Table(PeerGroupMember{})

	// Find groups this agent belongs to
	myMemberships, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return nil, err
	}
	if len(myMemberships) == 0 {
		return nil, nil
	}

	// Collect unique group IDs
	groupIDs := make(map[string]bool)
	for _, m := range myMemberships {
		groupIDs[m.(*PeerGroupMember).GroupID] = true
	}

	// Find other agents in those groups
	peerIDs := make(map[string]bool)
	for gid := range groupIDs {
		members, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"group_id": gid},
		})
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			aid := m.(*PeerGroupMember).AgentID
			if aid != s.agentID {
				peerIDs[aid] = true
			}
		}
	}

	if len(peerIDs) == 0 {
		return nil, nil
	}

	// Look up agent names
	agentDAO := s.db.Table(Agent{})
	var peers []AgentInfo
	for pid := range peerIDs {
		var agent Agent
		if err := agentDAO.Get(ctx, pid, &agent); err == nil {
			peers = append(peers, AgentInfo{ID: agent.ID, Name: agent.Name})
		}
	}
	return peers, nil
}

// SendAgentMessage sends a message from this agent to a peer agent.
func (s *AgentService) SendAgentMessage(ctx context.Context, toAgentID, content string) (*AgentMessage, error) {
	// Verify recipient is a peer
	peers, err := s.GetPeerAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check peers: %w", err)
	}
	isPeer := false
	for _, p := range peers {
		if p.ID == toAgentID {
			isPeer = true
			break
		}
	}
	if !isPeer {
		return nil, fmt.Errorf("agent %s is not a peer", toAgentID)
	}

	msg := &AgentMessage{
		ID:          newID(),
		FromAgentID: s.agentID,
		ToAgentID:   toAgentID,
		Content:     content,
		CreatedAt:   time.Now(),
	}
	if err := s.db.Table(AgentMessage{}).Insert(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// GetAgentMessages retrieves messages between this agent and a peer (both directions).
func (s *AgentService) GetAgentMessages(ctx context.Context, peerAgentID string, limit int, unreadOnly bool) ([]*AgentMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	dao := s.db.Table(AgentMessage{})

	// Get messages in both directions
	var allMessages []*AgentMessage

	if unreadOnly {
		// Only unread messages TO this agent FROM peer
		results, err := dao.Query(ctx, gowild_data.QueryOpts{
			Where:   map[string]any{"from_agent_id": peerAgentID, "to_agent_id": s.agentID},
			OrderBy: "created_at",
			Limit:   limit,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range results {
			msg := r.(*AgentMessage)
			if msg.ReadAt == nil {
				allMessages = append(allMessages, msg)
			}
		}
	} else {
		// Messages from peer to this agent
		inbound, err := dao.Query(ctx, gowild_data.QueryOpts{
			Where:     map[string]any{"from_agent_id": peerAgentID, "to_agent_id": s.agentID},
			OrderBy:   "created_at",
			OrderDesc: true,
			Limit:     limit,
		})
		if err != nil {
			return nil, err
		}
		// Messages from this agent to peer
		outbound, err := dao.Query(ctx, gowild_data.QueryOpts{
			Where:     map[string]any{"from_agent_id": s.agentID, "to_agent_id": peerAgentID},
			OrderBy:   "created_at",
			OrderDesc: true,
			Limit:     limit,
		})
		if err != nil {
			return nil, err
		}

		// Merge and sort by created_at
		for _, r := range inbound {
			allMessages = append(allMessages, r.(*AgentMessage))
		}
		for _, r := range outbound {
			allMessages = append(allMessages, r.(*AgentMessage))
		}

		// Sort descending by created_at, then take limit, then reverse
		sortMessagesByTime(allMessages)
		if len(allMessages) > limit {
			allMessages = allMessages[:limit]
		}
		// Reverse to chronological order
		for i, j := 0, len(allMessages)-1; i < j; i, j = i+1, j-1 {
			allMessages[i], allMessages[j] = allMessages[j], allMessages[i]
		}
	}

	return allMessages, nil
}

// sortMessagesByTime sorts messages descending by CreatedAt (newest first).
func sortMessagesByTime(msgs []*AgentMessage) {
	for i := 1; i < len(msgs); i++ {
		for j := i; j > 0 && msgs[j].CreatedAt.After(msgs[j-1].CreatedAt); j-- {
			msgs[j], msgs[j-1] = msgs[j-1], msgs[j]
		}
	}
}

// GetUnreadMessageCounts returns the count of unread messages per peer.
func (s *AgentService) GetUnreadMessageCounts(ctx context.Context) (map[string]int, error) {
	dao := s.db.Table(AgentMessage{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"to_agent_id": s.agentID},
	})
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, r := range results {
		msg := r.(*AgentMessage)
		if msg.ReadAt == nil {
			counts[msg.FromAgentID]++
		}
	}
	return counts, nil
}

// MarkMessageRead sets read_at on a message. Verifies this agent is the recipient.
func (s *AgentService) MarkMessageRead(ctx context.Context, messageID string) error {
	dao := s.db.Table(AgentMessage{})
	var msg AgentMessage
	if err := dao.Get(ctx, messageID, &msg); err != nil {
		return fmt.Errorf("message not found: %w", err)
	}
	if msg.ToAgentID != s.agentID {
		return fmt.Errorf("not the recipient of this message")
	}
	if msg.ReadAt != nil {
		return nil // already read
	}
	now := time.Now()
	msg.ReadAt = &now
	return dao.Update(ctx, &msg)
}

// MarkAllMessagesRead marks all messages from a specific peer as read.
func (s *AgentService) MarkAllMessagesRead(ctx context.Context, fromAgentID string) error {
	dao := s.db.Table(AgentMessage{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"from_agent_id": fromAgentID, "to_agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	now := time.Now()
	for _, r := range results {
		msg := r.(*AgentMessage)
		if msg.ReadAt == nil {
			msg.ReadAt = &now
			if err := dao.Update(ctx, msg); err != nil {
				return err
			}
		}
	}
	return nil
}
