package data

import (
	"context"
	"time"
)

// Report operations

// GetReportHTML retrieves the agent's report HTML and updated time.
func (s *AgentService) GetReportHTML(ctx context.Context) (string, time.Time, error) {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	return agent.ReportHTML, agent.ReportUpdatedAt, nil
}

// SetReportHTML saves the agent's report HTML and updates the timestamp.
func (s *AgentService) SetReportHTML(ctx context.Context, html string) (time.Time, error) {
	agent, err := s.GetAgent(ctx)
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now()
	agent.ReportHTML = html
	agent.ReportUpdatedAt = now
	if err := s.UpdateAgent(ctx, agent); err != nil {
		return time.Time{}, err
	}
	return now, nil
}
