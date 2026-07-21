package data

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Skill operations

// GetSkill retrieves a skill by name.
func (s *AgentService) GetSkill(ctx context.Context, name string) (*Skill, error) {
	dao := s.db.Table(Skill{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID, "name": name},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*Skill), nil
}

// GetAllSkills retrieves all skills for the agent.
func (s *AgentService) GetAllSkills(ctx context.Context) ([]*Skill, error) {
	dao := s.db.Table(Skill{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": s.agentID},
		OrderBy: "name",
	})
	if err != nil {
		return nil, err
	}

	skills := make([]*Skill, len(results))
	for i, r := range results {
		skills[i] = r.(*Skill)
	}
	return skills, nil
}

// SaveSkill saves or updates a skill.
func (s *AgentService) SaveSkill(ctx context.Context, skill *Skill) (bool, error) {
	dao := s.db.Table(Skill{})
	now := time.Now()

	existing, err := s.GetSkill(ctx, skill.Name)
	if err != nil {
		return false, err
	}

	if existing != nil {
		// Update
		existing.Description = skill.Description
		existing.InputSchema = skill.InputSchema
		existing.Code = skill.Code
		existing.Dependencies = skill.Dependencies
		existing.UpdatedAt = now
		return true, dao.Update(ctx, existing)
	}

	// Create
	skill.ID = newID()
	skill.AgentID = s.agentID
	skill.CreatedAt = now
	skill.UpdatedAt = now
	return false, dao.Insert(ctx, skill)
}

// DeleteSkill deletes a skill by name.
func (s *AgentService) DeleteSkill(ctx context.Context, name string) error {
	skill, err := s.GetSkill(ctx, name)
	if err != nil {
		return err
	}
	if skill == nil {
		return fmt.Errorf("skill not found: %s", name)
	}

	dao := s.db.Table(Skill{})
	return dao.Delete(ctx, skill.ID)
}
