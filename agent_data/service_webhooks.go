package data

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// SaveWebhookEvent saves a webhook event, deduplicating by event_id.
// If an event with the same event_id already exists, this is a no-op.
func (s *AgentService) SaveWebhookEvent(ctx context.Context, event *WebhookEvent) error {
	dao := s.db.Table(WebhookEvent{})

	// Check for existing event with same event_id (dedup)
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"event_id": event.EventID},
		Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(results) > 0 {
		return nil // Already exists, deduplicated
	}

	now := time.Now()
	if event.ID == "" {
		event.ID = newID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	}
	event.UpdatedAt = now
	if err := dao.Insert(ctx, event); err != nil {
		if IsUniqueConstraintError(err) {
			return nil
		}
		return err
	}
	return nil
}

// GetPendingWebhookEvents retrieves pending webhook events up to the given limit.
func (s *AgentService) GetPendingWebhookEvents(ctx context.Context, limit int) ([]WebhookEvent, error) {
	dao := s.db.Table(WebhookEvent{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"status": "pending"},
		OrderBy: "created_at",
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}

	events := make([]WebhookEvent, len(results))
	for i, r := range results {
		events[i] = *r.(*WebhookEvent)
	}
	return events, nil
}

// UpdateWebhookEvent updates an existing webhook event.
func (s *AgentService) UpdateWebhookEvent(ctx context.Context, event *WebhookEvent) error {
	event.UpdatedAt = time.Now()
	return s.db.Table(WebhookEvent{}).Update(ctx, event)
}

// GetWebhookConfig retrieves the webhook configuration for a given source and event.
func (s *AgentService) GetWebhookConfig(ctx context.Context, source, event string) (*WebhookConfig, error) {
	dao := s.db.Table(WebhookConfig{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"source": source, "event": event, "enabled": true},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no webhook config found for %s/%s", source, event)
	}
	return results[0].(*WebhookConfig), nil
}

// ListCompanyWebhookConfigs returns webhook configs for a company.
func ListCompanyWebhookConfigs(ctx context.Context, db gowild_data.Database, companyID string) ([]WebhookConfig, error) {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}
	dao := db.Table(WebhookConfig{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"company_id": companyID},
		OrderBy: "source,event_path",
	})
	if err != nil {
		return nil, err
	}
	out := make([]WebhookConfig, len(results))
	for i, r := range results {
		out[i] = *r.(*WebhookConfig)
	}
	return out, nil
}

// GetCompanyWebhookConfigByPath looks up one company-scoped webhook config.
func GetCompanyWebhookConfigByPath(ctx context.Context, db gowild_data.Database, companyID, source, eventPath string) (*WebhookConfig, error) {
	companyID = strings.TrimSpace(companyID)
	source = strings.TrimSpace(strings.ToLower(source))
	eventPath = normalizeWebhookEventPath(eventPath)
	if companyID == "" || source == "" || eventPath == "" {
		return nil, fmt.Errorf("company_id, source, and event_path are required")
	}
	dao := db.Table(WebhookConfig{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"company_id": companyID,
			"source":     source,
			"event_path": eventPath,
		},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*WebhookConfig), nil
}

// UpsertCompanyWebhookConfig inserts or updates one company-scoped webhook config.
func UpsertCompanyWebhookConfig(ctx context.Context, db gowild_data.Database, cfg *WebhookConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	cfg.CompanyID = strings.TrimSpace(cfg.CompanyID)
	cfg.Source = strings.TrimSpace(strings.ToLower(cfg.Source))
	cfg.Event = strings.TrimSpace(cfg.Event)
	cfg.EventPath = normalizeWebhookEventPath(cfg.EventPath)
	cfg.TargetRole = strings.TrimSpace(cfg.TargetRole)
	cfg.TargetMethod = strings.TrimSpace(cfg.TargetMethod)
	cfg.HMACSecret = strings.TrimSpace(cfg.HMACSecret)
	if cfg.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if cfg.Source == "" {
		return fmt.Errorf("source is required")
	}
	if cfg.Event == "" {
		return fmt.Errorf("event is required")
	}
	if cfg.EventPath == "" {
		return fmt.Errorf("event_path is required")
	}
	if cfg.TargetRole == "" {
		return fmt.Errorf("target_role is required")
	}
	if cfg.TargetMethod == "" {
		return fmt.Errorf("target_method is required")
	}
	if cfg.Source == "shopify" && cfg.HMACSecret == "" {
		return fmt.Errorf("hmac_secret is required for shopify")
	}
	if _, err := GetCompany(ctx, db, cfg.CompanyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}

	dao := db.Table(WebhookConfig{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"company_id": cfg.CompanyID,
			"source":     cfg.Source,
			"event_path": cfg.EventPath,
		},
		Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(results) == 1 {
		existing := results[0].(*WebhookConfig)
		existing.Event = cfg.Event
		existing.TargetRole = cfg.TargetRole
		existing.TargetMethod = cfg.TargetMethod
		existing.Enabled = cfg.Enabled
		existing.HMACSecret = cfg.HMACSecret
		if strings.TrimSpace(existing.ID) == "" {
			existing.ID = cfg.ID
		}
		if strings.TrimSpace(existing.ID) == "" {
			existing.ID = newID()
		}
		return dao.Update(ctx, existing)
	}
	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = newID()
	}
	return dao.Insert(ctx, cfg)
}

func normalizeWebhookEventPath(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

// IsUniqueConstraintError returns true if the error is a unique constraint violation
// from either SQLite or PostgreSQL.
func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "constraint failed")
}
