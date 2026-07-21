package data

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// UpsertAgentSiteUnscoped inserts or replaces a site record, checking ownership.
func UpsertAgentSiteUnscoped(ctx context.Context, db gowild_data.Database, site *AgentSite) error {
	dao := db.Table(AgentSite{})

	// Check if slug already exists
	var existing AgentSite
	err := dao.Get(ctx, site.ID, &existing)
	if err == nil {
		// Exists — verify ownership
		if existing.AgentID != site.AgentID {
			return fmt.Errorf("slug %q is owned by another agent", site.ID)
		}
		// Update existing
		site.CreatedAt = existing.CreatedAt
		site.UpdatedAt = time.Now()
		return dao.Update(ctx, site)
	}

	// New site
	site.CreatedAt = time.Now()
	site.UpdatedAt = site.CreatedAt
	if site.Status == "" {
		site.Status = "active"
	}
	return dao.Insert(ctx, site)
}

// GetAgentSiteUnscoped retrieves a site by slug.
func GetAgentSiteUnscoped(ctx context.Context, db gowild_data.Database, slug string) (*AgentSite, error) {
	dao := db.Table(AgentSite{})
	var site AgentSite
	if err := dao.Get(ctx, slug, &site); err != nil {
		return nil, err
	}
	return &site, nil
}

// ListAgentSitesUnscoped lists all sites for an agent.
func ListAgentSitesUnscoped(ctx context.Context, db gowild_data.Database, agentID string) ([]*AgentSite, error) {
	results, err := db.Table(AgentSite{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": agentID},
		OrderBy: "updated_at DESC",
	})
	if err != nil {
		return nil, err
	}
	sites := make([]*AgentSite, len(results))
	for i, r := range results {
		sites[i] = r.(*AgentSite)
	}
	return sites, nil
}
