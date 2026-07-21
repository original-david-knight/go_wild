package data

import (
	"context"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// Pending Email operations

// AddPendingEmail adds an email to the database.
// If Status is not already set, it defaults to "pending".
func (s *AgentService) AddPendingEmail(ctx context.Context, pe *PendingEmail) error {
	pe.ID = newID()
	pe.AgentID = s.agentID
	if pe.Status == "" {
		pe.Status = "pending"
	}
	pe.CreatedAt = time.Now()
	return s.db.Table(PendingEmail{}).Insert(ctx, pe)
}

// GetPendingEmails retrieves all pending emails for the agent.
func (s *AgentService) GetPendingEmails(ctx context.Context) ([]*PendingEmail, error) {
	dao := s.db.Table(PendingEmail{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": s.agentID, "status": "pending"},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}

	emails := make([]*PendingEmail, len(results))
	for i, r := range results {
		emails[i] = r.(*PendingEmail)
	}
	return emails, nil
}

// GetPendingEmailByID retrieves a pending email by ID.
func (s *AgentService) GetPendingEmailByID(ctx context.Context, id string) (*PendingEmail, error) {
	dao := s.db.Table(PendingEmail{})
	var pe PendingEmail
	if err := dao.Get(ctx, id, &pe); err != nil {
		return nil, err
	}
	if pe.AgentID != s.agentID {
		return nil, fmt.Errorf("pending email not found")
	}
	return &pe, nil
}

// UpdatePendingEmailStatus updates the status of a pending email.
func (s *AgentService) UpdatePendingEmailStatus(ctx context.Context, id, status string) error {
	pe, err := s.GetPendingEmailByID(ctx, id)
	if err != nil {
		return err
	}
	pe.Status = status
	return s.db.Table(PendingEmail{}).Update(ctx, pe)
}

// DeletePendingEmail deletes a pending email.
func (s *AgentService) DeletePendingEmail(ctx context.Context, id string) error {
	pe, err := s.GetPendingEmailByID(ctx, id)
	if err != nil {
		return err
	}
	return s.db.Table(PendingEmail{}).Delete(ctx, pe.ID)
}

// deleteAllPendingEmails deletes all pending emails for the agent.
func (s *AgentService) deleteAllPendingEmails(ctx context.Context) error {
	dao := s.db.Table(PendingEmail{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": s.agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		pe := r.(*PendingEmail)
		if err := dao.Delete(ctx, pe.ID); err != nil {
			return err
		}
	}
	return nil
}
