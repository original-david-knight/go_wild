package data

import (
	"context"
	"fmt"

	"github.com/original-david-knight/go_wild/data"
	kg "github.com/original-david-knight/go_wild/knowledge_graph"
)

// DeleteAgent deletes the agent and all associated data.
func (s *AgentService) DeleteAgent(ctx context.Context) error {
	// Delete in order: knowledge graph, recurring tasks, tasks, pending emails, skills, archive, memory, history snapshot, soul, then agent
	if err := s.deleteKnowledgeGraph(ctx); err != nil {
		return fmt.Errorf("failed to delete knowledge graph: %w", err)
	}
	if err := s.deleteAllRecurringTasks(ctx); err != nil {
		return fmt.Errorf("failed to delete recurring tasks: %w", err)
	}
	if err := s.deleteAllTasks(ctx); err != nil {
		return fmt.Errorf("failed to delete tasks: %w", err)
	}
	if err := s.deleteAllPendingEmails(ctx); err != nil {
		return fmt.Errorf("failed to delete pending emails: %w", err)
	}
	if err := s.deleteChatHistory(ctx); err != nil {
		return fmt.Errorf("failed to delete chat history: %w", err)
	}
	if err := s.deleteAllSkills(ctx); err != nil {
		return fmt.Errorf("failed to delete skills: %w", err)
	}
	if err := s.deleteAllArchive(ctx); err != nil {
		return fmt.Errorf("failed to delete archive: %w", err)
	}
	if err := s.deleteMemory(ctx); err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}
	if err := s.DeleteHistorySnapshot(ctx); err != nil {
		return fmt.Errorf("failed to delete history snapshot: %w", err)
	}
	if err := s.deleteSoul(ctx); err != nil {
		return fmt.Errorf("failed to delete soul: %w", err)
	}
	// Delete the agent record
	return s.db.Table(Agent{}).Delete(ctx, s.agentID)
}

func (s *AgentService) deleteKnowledgeGraph(ctx context.Context) error {
	// Delete edges first (they reference nodes)
	edgeDao := s.db.Table(kg.Edge{})
	edges, err := edgeDao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"user_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range edges {
		edge := r.(*kg.Edge)
		if err := edgeDao.Delete(ctx, edge.ID); err != nil {
			return err
		}
	}

	// Delete nodes
	nodeDao := s.db.Table(kg.Node{})
	nodes, err := nodeDao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"user_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range nodes {
		node := r.(*kg.Node)
		if err := nodeDao.Delete(ctx, node.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentService) deleteAllSkills(ctx context.Context) error {
	dao := s.db.Table(Skill{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		skill := r.(*Skill)
		if err := dao.Delete(ctx, skill.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentService) deleteAllArchive(ctx context.Context) error {
	dao := s.db.Table(ArchiveEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		entry := r.(*ArchiveEntry)
		if err := dao.Delete(ctx, entry.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentService) deleteMemory(ctx context.Context) error {
	dao := s.db.Table(MemoryEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		entry := r.(*MemoryEntry)
		if err := dao.Delete(ctx, entry.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentService) deleteSoul(ctx context.Context) error {
	dao := s.db.Table(Soul{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		soul := r.(*Soul)
		if err := dao.Delete(ctx, soul.ID); err != nil {
			return err
		}
	}
	return nil
}
