package data

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Email Whitelist operations

// GetEmailWhitelist retrieves the agent's email whitelist.
func (s *AgentService) GetEmailWhitelist(ctx context.Context) ([]string, error) {
	dao := s.db.Table(EmailWhitelistEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return nil, err
	}
	emails := make([]string, 0, len(results))
	for _, r := range results {
		emails = append(emails, r.(*EmailWhitelistEntry).Email)
	}
	return emails, nil
}

// AddEmailWhitelistEntry adds an email to the whitelist (case-insensitive dedup).
func (s *AgentService) AddEmailWhitelistEntry(ctx context.Context, email string) error {
	emailLower := toLower(email)

	// Check for existing entry
	existing, err := s.GetEmailWhitelist(ctx)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if toLower(e) == emailLower {
			return nil // Already whitelisted
		}
	}

	entry := &EmailWhitelistEntry{
		ID:        newID(),
		AgentID:   s.agentID,
		Email:     emailLower,
		CreatedAt: time.Now(),
	}
	return s.db.Table(EmailWhitelistEntry{}).Insert(ctx, entry)
}

// RemoveEmailWhitelistEntry removes an email from the whitelist.
func (s *AgentService) RemoveEmailWhitelistEntry(ctx context.Context, email string) error {
	emailLower := toLower(email)
	dao := s.db.Table(EmailWhitelistEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		entry := r.(*EmailWhitelistEntry)
		if toLower(entry.Email) == emailLower {
			return dao.Delete(ctx, entry.ID)
		}
	}
	return nil
}

// IsEmailWhitelisted checks if ALL given recipients are whitelisted.
func (s *AgentService) IsEmailWhitelisted(ctx context.Context, recipients []string) (bool, error) {
	if len(recipients) == 0 {
		return false, nil
	}

	whitelist, err := s.GetEmailWhitelist(ctx)
	if err != nil {
		return false, err
	}

	whiteset := make(map[string]bool, len(whitelist))
	for _, e := range whitelist {
		whiteset[toLower(e)] = true
	}

	for _, r := range recipients {
		if !whiteset[toLower(r)] {
			return false, nil
		}
	}
	return true, nil
}
