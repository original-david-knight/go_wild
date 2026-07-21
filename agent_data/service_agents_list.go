package data

import (
	"context"

	"github.com/original-david-knight/go_wild/data"
)

// AgentInfo contains summary information about an agent.
type AgentInfo struct {
	ID      string
	Name    string
	HasSoul bool
}

// ListAgents returns all agents in the database.
func ListAgents(ctx context.Context, db gowild_data.Database) ([]AgentInfo, error) {
	dao := db.Table(Agent{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}

	agents := make([]AgentInfo, 0, len(results))
	for _, r := range results {
		agent := r.(*Agent)
		// Check if agent has a soul
		soulDao := db.Table(Soul{})
		soulResults, _ := soulDao.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"agent_id": agent.ID},
			Limit: 1,
		})
		agents = append(agents, AgentInfo{
			ID:      agent.ID,
			Name:    agent.Name,
			HasSoul: len(soulResults) > 0,
		})
	}
	return agents, nil
}
