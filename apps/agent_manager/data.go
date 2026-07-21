package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentdata "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	_ "github.com/original-david-knight/go_wild/knowledge_graph"
	_ "github.com/original-david-knight/go_wild/objectives"
)

// Setting stores manager configuration key-value pairs.
type Setting struct {
	ID    string `json:"id"` // Key name (e.g., "broker_secret")
	Value string `json:"value"`
}

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		return db.AddTable(Setting{})
	})
}

// RegisterTables registers all tables via the auto-registration system.
func RegisterTables(db gowild_data.Database) error {
	return gowild_data.AddAllTables(db)
}

// EnsureSchema registers tables and enforces manager-specific constraints.
func EnsureSchema(db gowild_data.Database) error {
	if err := RegisterTables(db); err != nil {
		return err
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.WebhookEvent{}, "webhook_events_event_id_uidx", "event_id"); err != nil {
		return fmt.Errorf("failed to ensure webhook event dedupe index: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.CompanyMember{}, "company_members_agent_id_uidx", "agent_id"); err != nil {
		return fmt.Errorf("failed to ensure one-company-per-agent constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.CompanyMember{}, "company_members_company_agent_uidx", "company_id", "agent_id"); err != nil {
		return fmt.Errorf("failed to ensure company member uniqueness constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.CompanyShopifyConnection{}, "company_shopify_connections_company_uidx", "company_id"); err != nil {
		return fmt.Errorf("failed to ensure single shopify connection per company constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.CompanyPolymarketConnection{}, "company_polymarket_connections_company_uidx", "company_id"); err != nil {
		return fmt.Errorf("failed to ensure single polymarket connection per company constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.CompanyTopDawgConnection{}, "company_topdawg_connections_company_uidx", "company_id"); err != nil {
		return fmt.Errorf("failed to ensure single topdawg connection per company constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.CompanyCJDropshippingConnection{}, "company_cjdropshipping_connections_company_uidx", "company_id"); err != nil {
		return fmt.Errorf("failed to ensure single cjdropshipping connection per company constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.Company{}, "companies_webhook_ingress_key_uidx", "webhook_ingress_key"); err != nil {
		return fmt.Errorf("failed to ensure unique company webhook ingress key constraint: %w", err)
	}
	if err := gowild_data.EnsureUniqueIndex(db, agentdata.WebhookConfig{}, "webhook_configs_company_source_event_path_uidx", "company_id", "source", "event_path"); err != nil {
		return fmt.Errorf("failed to ensure unique company webhook config route constraint: %w", err)
	}
	if err := backfillA2AMethodsFromCapabilities(context.Background(), db); err != nil {
		return fmt.Errorf("failed to backfill a2a methods: %w", err)
	}
	return nil
}

func backfillA2AMethodsFromCapabilities(ctx context.Context, db gowild_data.Database) error {
	capDAO := db.Table(agentdata.AgentCapability{})
	methodDAO := db.Table(agentdata.A2AMethod{})
	if capDAO == nil || methodDAO == nil {
		return nil
	}

	existingRows, err := methodDAO.GetAll(ctx)
	if err != nil {
		return err
	}
	existingMethods := make(map[string]struct{}, len(existingRows))
	for _, row := range existingRows {
		method := strings.TrimSpace(row.(*agentdata.A2AMethod).Method)
		if method == "" {
			continue
		}
		existingMethods[method] = struct{}{}
	}

	capRows, err := capDAO.GetAll(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	pending := map[string]*agentdata.A2AMethod{}
	for _, row := range capRows {
		cap := row.(*agentdata.AgentCapability)
		method := strings.TrimSpace(cap.Method)
		if method == "" {
			continue
		}
		if _, ok := existingMethods[method]; ok {
			continue
		}

		entry, ok := pending[method]
		if !ok {
			entry = &agentdata.A2AMethod{
				Method:           method,
				Description:      "",
				InputSchemaJSON:  "",
				OutputSchemaJSON: "",
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			pending[method] = entry
		}

		if entry.Description == "" && strings.TrimSpace(cap.Description) != "" {
			entry.Description = strings.TrimSpace(cap.Description)
		}
		if entry.InputSchemaJSON == "" && strings.TrimSpace(cap.InputSchemaJSON) != "" {
			entry.InputSchemaJSON = strings.TrimSpace(cap.InputSchemaJSON)
		}
		if entry.OutputSchemaJSON == "" && strings.TrimSpace(cap.OutputSchemaJSON) != "" {
			entry.OutputSchemaJSON = strings.TrimSpace(cap.OutputSchemaJSON)
		}
	}

	for method, entry := range pending {
		if err := methodDAO.Insert(ctx, entry); err != nil {
			var existing agentdata.A2AMethod
			if getErr := methodDAO.Get(ctx, method, &existing); getErr == nil {
				continue
			}
			return fmt.Errorf("failed to insert backfilled method %q: %w", method, err)
		}
	}
	return nil
}

// GetSetting retrieves a setting value by key.
func GetSetting(ctx context.Context, db gowild_data.Database, key string) (string, error) {
	table := db.Table(Setting{})
	if table == nil {
		return "", nil
	}
	var setting Setting
	if err := table.Get(ctx, key, &setting); err != nil {
		return "", err
	}
	return setting.Value, nil
}

// SetSetting stores a setting value (upsert via delete+insert).
func SetSetting(ctx context.Context, db gowild_data.Database, key, value string) error {
	table := db.Table(Setting{})
	if table == nil {
		return nil
	}
	// Delete existing, then insert new
	if err := table.Delete(ctx, key); err != nil {
		return err
	}
	return table.Insert(ctx, &Setting{ID: key, Value: value})
}
