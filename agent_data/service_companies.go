package data

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

var ErrCompanyMembershipConflict = errors.New("agent already belongs to another company")

// ListCompanies returns all companies.
func ListCompanies(ctx context.Context, db gowild_data.Database) ([]Company, error) {
	dao := db.Table(Company{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	out := make([]Company, len(results))
	for i, r := range results {
		out[i] = *r.(*Company)
	}
	return out, nil
}

// GetCompany returns a company by ID.
func GetCompany(ctx context.Context, db gowild_data.Database, companyID string) (*Company, error) {
	var c Company
	if err := db.Table(Company{}).Get(ctx, companyID, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCompany creates a company and optionally assigns a CEO member.
func CreateCompany(ctx context.Context, db gowild_data.Database, name, description, ceoAgentID string) (*Company, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	ceoAgentID = strings.TrimSpace(ceoAgentID)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	seedPhrase, err := generateSeedPhrase()
	if err != nil {
		return nil, fmt.Errorf("failed to generate company wallet seed phrase: %w", err)
	}
	webhookIngressKey, err := generateCompanyWebhookIngressKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate company webhook ingress key: %w", err)
	}

	now := time.Now()
	company := &Company{
		ID:                newID(),
		Name:              name,
		Description:       description,
		WebhookIngressKey: webhookIngressKey,
		// Companies own shared wallets; generate one by default.
		WalletSeedPhrase: seedPhrase,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		if err := tx.Table(Company{}).Insert(ctx, company); err != nil {
			return err
		}
		if ceoAgentID != "" {
			if err := AddAgentToCompany(ctx, tx, company.ID, ceoAgentID, "ceo"); err != nil {
				return err
			}
			company.CEOAgentID = ceoAgentID
			company.UpdatedAt = time.Now()
			if err := tx.Table(Company{}).Update(ctx, company); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return company, nil
}

// EnsureCompanyWalletSeedPhrase returns a company's wallet seed phrase.
// If it is missing, a new seed phrase is generated and persisted.
func EnsureCompanyWalletSeedPhrase(ctx context.Context, db gowild_data.Database, companyID string) (string, error) {
	company, err := GetCompany(ctx, db, strings.TrimSpace(companyID))
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(company.WalletSeedPhrase) != "" {
		return company.WalletSeedPhrase, nil
	}

	seedPhrase, err := generateSeedPhrase()
	if err != nil {
		return "", fmt.Errorf("failed to generate company wallet seed phrase: %w", err)
	}
	company.WalletSeedPhrase = seedPhrase
	company.UpdatedAt = time.Now()
	if err := db.Table(Company{}).Update(ctx, company); err != nil {
		return "", err
	}
	return seedPhrase, nil
}

// UpdateCompany updates a company record.
func UpdateCompany(ctx context.Context, db gowild_data.Database, company *Company) error {
	if company == nil {
		return fmt.Errorf("company is nil")
	}
	company.Name = strings.TrimSpace(company.Name)
	company.Description = strings.TrimSpace(company.Description)
	if company.ID == "" {
		return fmt.Errorf("company id is required")
	}
	if company.Name == "" {
		return fmt.Errorf("name is required")
	}
	company.UpdatedAt = time.Now()
	return db.Table(Company{}).Update(ctx, company)
}

// DeleteCompany deletes a company and all related memberships/connections/knowledge.
func DeleteCompany(ctx context.Context, db gowild_data.Database, companyID string) error {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return fmt.Errorf("company id is required")
	}
	return db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		memberDAO := tx.Table(CompanyMember{})
		members, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range members {
			if err := memberDAO.Delete(ctx, r.(*CompanyMember).ID); err != nil {
				return err
			}
		}

		shopDAO := tx.Table(CompanyShopifyConnection{})
		shops, err := shopDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range shops {
			if err := shopDAO.Delete(ctx, r.(*CompanyShopifyConnection).ID); err != nil {
				return err
			}
		}

		polyDAO := tx.Table(CompanyPolymarketConnection{})
		polys, err := polyDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range polys {
			if err := polyDAO.Delete(ctx, r.(*CompanyPolymarketConnection).ID); err != nil {
				return err
			}
		}

		topdawgDAO := tx.Table(CompanyTopDawgConnection{})
		topdawgs, err := topdawgDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range topdawgs {
			if err := topdawgDAO.Delete(ctx, r.(*CompanyTopDawgConnection).ID); err != nil {
				return err
			}
		}

		cjDAO := tx.Table(CompanyCJDropshippingConnection{})
		cjConnections, err := cjDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range cjConnections {
			if err := cjDAO.Delete(ctx, r.(*CompanyCJDropshippingConnection).ID); err != nil {
				return err
			}
		}

		amazonDAO := tx.Table(CompanyAmazonConnection{})
		amazonConns, err := amazonDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range amazonConns {
			if err := amazonDAO.Delete(ctx, r.(*CompanyAmazonConnection).ID); err != nil {
				return err
			}
		}

		knowledgeDAO := tx.Table(CompanyKnowledgeEntry{})
		entries, err := knowledgeDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range entries {
			if err := knowledgeDAO.Delete(ctx, r.(*CompanyKnowledgeEntry).ID); err != nil {
				return err
			}
		}

		webhookConfigDAO := tx.Table(WebhookConfig{})
		webhookConfigs, err := webhookConfigDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range webhookConfigs {
			if err := webhookConfigDAO.Delete(ctx, r.(*WebhookConfig).ID); err != nil {
				return err
			}
		}

		webhookEventDAO := tx.Table(WebhookEvent{})
		webhookEvents, err := webhookEventDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range webhookEvents {
			if err := webhookEventDAO.Delete(ctx, r.(*WebhookEvent).ID); err != nil {
				return err
			}
		}

		listingDAO := tx.Table(ProductListing{})
		listingRows, err := listingDAO.Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{"company_id": companyID},
		})
		if err != nil {
			return err
		}
		for _, r := range listingRows {
			if err := listingDAO.Delete(ctx, r.(*ProductListing).ID); err != nil {
				return err
			}
		}

		return tx.Table(Company{}).Delete(ctx, companyID)
	})
}

// AddAgentToCompany adds an agent to a company.
func AddAgentToCompany(ctx context.Context, db gowild_data.Database, companyID, agentID, role string) error {
	companyID = strings.TrimSpace(companyID)
	agentID = strings.TrimSpace(agentID)
	role = strings.TrimSpace(role)
	if companyID == "" || agentID == "" {
		return fmt.Errorf("company_id and agent_id are required")
	}

	// Validate company exists.
	if _, err := GetCompany(ctx, db, companyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}
	// Validate agent exists.
	var agent Agent
	if err := db.Table(Agent{}).Get(ctx, agentID, &agent); err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	memberDAO := db.Table(CompanyMember{})
	// Enforce one-company-max per agent.
	existingByAgent, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": agentID},
		Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(existingByAgent) > 0 {
		existing := existingByAgent[0].(*CompanyMember)
		if existing.CompanyID != companyID {
			return fmt.Errorf("%w: agent %s already belongs to company %s", ErrCompanyMembershipConflict, agentID, existing.CompanyID)
		}
		if role != "" && existing.Role != role {
			existing.Role = role
			return memberDAO.Update(ctx, existing)
		}
		return nil
	}

	member := &CompanyMember{
		ID:        newID(),
		CompanyID: companyID,
		AgentID:   agentID,
		Role:      role,
		CreatedAt: time.Now(),
	}
	return memberDAO.Insert(ctx, member)
}

// RemoveAgentFromCompany removes an agent from a company.
func RemoveAgentFromCompany(ctx context.Context, db gowild_data.Database, companyID, agentID string) error {
	companyID = strings.TrimSpace(companyID)
	agentID = strings.TrimSpace(agentID)
	if companyID == "" || agentID == "" {
		return fmt.Errorf("company_id and agent_id are required")
	}

	company, err := GetCompany(ctx, db, companyID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(company.CEOAgentID) == agentID {
		return fmt.Errorf("cannot remove current ceo; assign a new ceo first")
	}

	memberDAO := db.Table(CompanyMember{})
	results, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID, "agent_id": agentID},
	})
	if err != nil {
		return err
	}
	for _, r := range results {
		if err := memberDAO.Delete(ctx, r.(*CompanyMember).ID); err != nil {
			return err
		}
	}
	return nil
}

// ListCompanyMembers lists all members in a company.
func ListCompanyMembers(ctx context.Context, db gowild_data.Database, companyID string) ([]CompanyMember, error) {
	memberDAO := db.Table(CompanyMember{})
	results, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"company_id": companyID},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	out := make([]CompanyMember, len(results))
	for i, r := range results {
		out[i] = *r.(*CompanyMember)
	}
	return out, nil
}

// GetCompanyMemberForAgent returns the membership row for an agent, or nil if not in a company.
func GetCompanyMemberForAgent(ctx context.Context, db gowild_data.Database, agentID string) (*CompanyMember, error) {
	memberDAO := db.Table(CompanyMember{})
	results, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"agent_id": agentID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*CompanyMember), nil
}

// GetCompanyForAgent returns the company for an agent, or nil if not in a company.
func GetCompanyForAgent(ctx context.Context, db gowild_data.Database, agentID string) (*Company, error) {
	member, err := GetCompanyMemberForAgent(ctx, db, agentID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, nil
	}
	return GetCompany(ctx, db, member.CompanyID)
}

// SetCompanyCEO sets the CEO for a company, requiring membership.
func SetCompanyCEO(ctx context.Context, db gowild_data.Database, companyID, agentID string) error {
	companyID = strings.TrimSpace(companyID)
	agentID = strings.TrimSpace(agentID)
	if companyID == "" || agentID == "" {
		return fmt.Errorf("company_id and agent_id are required")
	}

	members, err := ListCompanyMembers(ctx, db, companyID)
	if err != nil {
		return err
	}
	isMember := false
	for _, m := range members {
		if m.AgentID == agentID {
			isMember = true
			break
		}
	}
	if !isMember {
		return fmt.Errorf("agent %s is not a member of company %s", agentID, companyID)
	}

	company, err := GetCompany(ctx, db, companyID)
	if err != nil {
		return err
	}
	company.CEOAgentID = agentID
	company.UpdatedAt = time.Now()
	if err := db.Table(Company{}).Update(ctx, company); err != nil {
		return err
	}

	memberDAO := db.Table(CompanyMember{})
	memberRows, err := memberDAO.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID, "agent_id": agentID},
		Limit: 1,
	})
	if err == nil && len(memberRows) == 1 {
		m := memberRows[0].(*CompanyMember)
		if strings.TrimSpace(m.Role) != "ceo" {
			m.Role = "ceo"
			_ = memberDAO.Update(ctx, m)
		}
	}
	return nil
}

// GetCompanyByWebhookIngressKey returns the company for a webhook ingress key.
func GetCompanyByWebhookIngressKey(ctx context.Context, db gowild_data.Database, webhookIngressKey string) (*Company, error) {
	webhookIngressKey = strings.TrimSpace(webhookIngressKey)
	if webhookIngressKey == "" {
		return nil, nil
	}
	dao := db.Table(Company{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"webhook_ingress_key": webhookIngressKey},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*Company), nil
}

// EnsureCompanyWebhookIngressKey returns a company's webhook ingress key, generating one if missing.
func EnsureCompanyWebhookIngressKey(ctx context.Context, db gowild_data.Database, companyID string) (string, error) {
	company, err := GetCompany(ctx, db, strings.TrimSpace(companyID))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(company.WebhookIngressKey) != "" {
		return company.WebhookIngressKey, nil
	}
	key, err := generateCompanyWebhookIngressKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate company webhook ingress key: %w", err)
	}
	company.WebhookIngressKey = key
	company.UpdatedAt = time.Now()
	if err := db.Table(Company{}).Update(ctx, company); err != nil {
		return "", err
	}
	return key, nil
}

// RotateCompanyWebhookIngressKey rotates and persists a company's webhook ingress key.
func RotateCompanyWebhookIngressKey(ctx context.Context, db gowild_data.Database, companyID string) (string, error) {
	company, err := GetCompany(ctx, db, strings.TrimSpace(companyID))
	if err != nil {
		return "", err
	}
	key, err := generateCompanyWebhookIngressKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate company webhook ingress key: %w", err)
	}
	company.WebhookIngressKey = key
	company.UpdatedAt = time.Now()
	if err := db.Table(Company{}).Update(ctx, company); err != nil {
		return "", err
	}
	return key, nil
}

// GetCompanyShopifyConnection returns the Shopify connection for a company.
func GetCompanyShopifyConnection(ctx context.Context, db gowild_data.Database, companyID string) (*CompanyShopifyConnection, error) {
	dao := db.Table(CompanyShopifyConnection{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*CompanyShopifyConnection), nil
}

// UpsertCompanyShopifyConnection inserts or updates a company's Shopify connection.
func UpsertCompanyShopifyConnection(ctx context.Context, db gowild_data.Database, conn *CompanyShopifyConnection) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	conn.CompanyID = strings.TrimSpace(conn.CompanyID)
	conn.ShopURL = strings.TrimSpace(conn.ShopURL)
	conn.APIVersion = strings.TrimSpace(conn.APIVersion)
	conn.ClientID = strings.TrimSpace(conn.ClientID)
	conn.ClientSecretEnc = strings.TrimSpace(conn.ClientSecretEnc)
	conn.AccessTokenEnc = strings.TrimSpace(conn.AccessTokenEnc)
	conn.WebhookSecretEnc = strings.TrimSpace(conn.WebhookSecretEnc)
	if conn.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if conn.ShopURL == "" {
		return fmt.Errorf("shop_url is required")
	}
	if conn.APIVersion == "" {
		conn.APIVersion = "2025-01"
	}
	hasClientID := conn.ClientID != ""
	hasClientSecret := conn.ClientSecretEnc != ""
	if hasClientID != hasClientSecret {
		return fmt.Errorf("client_id and client_secret_enc must both be set")
	}
	if !hasClientID || !hasClientSecret {
		return fmt.Errorf("client credentials are required")
	}
	if _, err := GetCompany(ctx, db, conn.CompanyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}

	dao := db.Table(CompanyShopifyConnection{})
	now := time.Now()
	existing, err := GetCompanyShopifyConnection(ctx, db, conn.CompanyID)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.ShopURL = conn.ShopURL
		existing.APIVersion = conn.APIVersion
		existing.ClientID = conn.ClientID
		existing.ClientSecretEnc = conn.ClientSecretEnc
		existing.AccessTokenEnc = conn.AccessTokenEnc
		existing.AccessTokenExpAt = conn.AccessTokenExpAt
		existing.WebhookSecretEnc = conn.WebhookSecretEnc
		existing.Enabled = conn.Enabled
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	if conn.ID == "" {
		conn.ID = newID()
	}
	conn.CreatedAt = now
	conn.UpdatedAt = now
	return dao.Insert(ctx, conn)
}

// DeleteCompanyShopifyConnection deletes a company's Shopify connection.
func DeleteCompanyShopifyConnection(ctx context.Context, db gowild_data.Database, companyID string) error {
	dao := db.Table(CompanyShopifyConnection{})
	rows, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := dao.Delete(ctx, r.(*CompanyShopifyConnection).ID); err != nil {
			return err
		}
	}
	return nil
}

func generateCompanyWebhookIngressKey() (string, error) {
	var key [24]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key[:]), nil
}

// GetCompanyPolymarketConnection returns the Polymarket connection for a company.
func GetCompanyPolymarketConnection(ctx context.Context, db gowild_data.Database, companyID string) (*CompanyPolymarketConnection, error) {
	dao := db.Table(CompanyPolymarketConnection{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*CompanyPolymarketConnection), nil
}

// UpsertCompanyPolymarketConnection inserts or updates a company's Polymarket connection.
func UpsertCompanyPolymarketConnection(ctx context.Context, db gowild_data.Database, conn *CompanyPolymarketConnection) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	conn.CompanyID = strings.TrimSpace(conn.CompanyID)
	conn.ProxyURL = strings.TrimSpace(conn.ProxyURL)
	conn.OnchainRPCURL = strings.TrimSpace(conn.OnchainRPCURL)
	conn.FunderAddress = strings.TrimSpace(conn.FunderAddress)
	if conn.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if conn.SignatureType < 0 || conn.SignatureType > 2 {
		return fmt.Errorf("signature_type must be 0, 1, or 2")
	}
	if conn.ChainID < 0 {
		return fmt.Errorf("chain_id must be >= 0")
	}
	if _, err := GetCompany(ctx, db, conn.CompanyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}

	dao := db.Table(CompanyPolymarketConnection{})
	now := time.Now()
	existing, err := GetCompanyPolymarketConnection(ctx, db, conn.CompanyID)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.ProxyURL = conn.ProxyURL
		existing.OnchainRPCURL = conn.OnchainRPCURL
		existing.FunderAddress = conn.FunderAddress
		existing.SignatureType = conn.SignatureType
		existing.ChainID = conn.ChainID
		existing.Enabled = conn.Enabled
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	if conn.ID == "" {
		conn.ID = newID()
	}
	conn.CreatedAt = now
	conn.UpdatedAt = now
	return dao.Insert(ctx, conn)
}

// DeleteCompanyPolymarketConnection deletes a company's Polymarket connection.
func DeleteCompanyPolymarketConnection(ctx context.Context, db gowild_data.Database, companyID string) error {
	dao := db.Table(CompanyPolymarketConnection{})
	rows, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := dao.Delete(ctx, r.(*CompanyPolymarketConnection).ID); err != nil {
			return err
		}
	}
	return nil
}

// GetCompanyTopDawgConnection returns the TopDawg connection for a company.
func GetCompanyTopDawgConnection(ctx context.Context, db gowild_data.Database, companyID string) (*CompanyTopDawgConnection, error) {
	dao := db.Table(CompanyTopDawgConnection{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*CompanyTopDawgConnection), nil
}

// UpsertCompanyTopDawgConnection inserts or updates a company's TopDawg connection.
func UpsertCompanyTopDawgConnection(ctx context.Context, db gowild_data.Database, conn *CompanyTopDawgConnection) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	conn.CompanyID = strings.TrimSpace(conn.CompanyID)
	conn.APIKeyEnc = strings.TrimSpace(conn.APIKeyEnc)
	conn.SupplierID = strings.TrimSpace(conn.SupplierID)
	if conn.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if conn.Enabled {
		if conn.APIKeyEnc == "" {
			return fmt.Errorf("api_key is required")
		}
		if conn.SupplierID == "" {
			return fmt.Errorf("supplier_id is required")
		}
	}
	if _, err := GetCompany(ctx, db, conn.CompanyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}

	dao := db.Table(CompanyTopDawgConnection{})
	now := time.Now()
	existing, err := GetCompanyTopDawgConnection(ctx, db, conn.CompanyID)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.APIKeyEnc = conn.APIKeyEnc
		existing.SupplierID = conn.SupplierID
		existing.Enabled = conn.Enabled
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	if conn.ID == "" {
		conn.ID = newID()
	}
	conn.CreatedAt = now
	conn.UpdatedAt = now
	return dao.Insert(ctx, conn)
}

// DeleteCompanyTopDawgConnection deletes a company's TopDawg connection.
func DeleteCompanyTopDawgConnection(ctx context.Context, db gowild_data.Database, companyID string) error {
	dao := db.Table(CompanyTopDawgConnection{})
	rows, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := dao.Delete(ctx, r.(*CompanyTopDawgConnection).ID); err != nil {
			return err
		}
	}
	return nil
}

// GetCompanyCJDropshippingConnection returns the CJ Dropshipping connection for a company.
func GetCompanyCJDropshippingConnection(ctx context.Context, db gowild_data.Database, companyID string) (*CompanyCJDropshippingConnection, error) {
	dao := db.Table(CompanyCJDropshippingConnection{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*CompanyCJDropshippingConnection), nil
}

// UpsertCompanyCJDropshippingConnection inserts or updates a company's CJ Dropshipping connection.
func UpsertCompanyCJDropshippingConnection(ctx context.Context, db gowild_data.Database, conn *CompanyCJDropshippingConnection) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	conn.CompanyID = strings.TrimSpace(conn.CompanyID)
	conn.APIKeyEnc = strings.TrimSpace(conn.APIKeyEnc)
	conn.AccessTokenEnc = strings.TrimSpace(conn.AccessTokenEnc)
	conn.RefreshTokenEnc = strings.TrimSpace(conn.RefreshTokenEnc)
	conn.PlatformTokenEnc = strings.TrimSpace(conn.PlatformTokenEnc)
	conn.DefaultFromCountryCode = strings.ToUpper(strings.TrimSpace(conn.DefaultFromCountryCode))
	if conn.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if conn.APIKeyEnc == "" && conn.AccessTokenEnc == "" {
		return fmt.Errorf("api_key or access_token is required")
	}
	if conn.DefaultFromCountryCode != "" && len(conn.DefaultFromCountryCode) != 2 {
		return fmt.Errorf("default_from_country_code must be a two-letter country code")
	}
	if _, err := GetCompany(ctx, db, conn.CompanyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}

	dao := db.Table(CompanyCJDropshippingConnection{})
	now := time.Now()
	existing, err := GetCompanyCJDropshippingConnection(ctx, db, conn.CompanyID)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.APIKeyEnc = conn.APIKeyEnc
		existing.AccessTokenEnc = conn.AccessTokenEnc
		existing.AccessTokenExpAt = conn.AccessTokenExpAt
		existing.RefreshTokenEnc = conn.RefreshTokenEnc
		existing.RefreshTokenExpAt = conn.RefreshTokenExpAt
		existing.PlatformTokenEnc = conn.PlatformTokenEnc
		existing.DefaultFromCountryCode = conn.DefaultFromCountryCode
		existing.Enabled = conn.Enabled
		existing.LastTestedAt = conn.LastTestedAt
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	if conn.ID == "" {
		conn.ID = newID()
	}
	conn.CreatedAt = now
	conn.UpdatedAt = now
	return dao.Insert(ctx, conn)
}

// DeleteCompanyCJDropshippingConnection deletes a company's CJ Dropshipping connection.
func DeleteCompanyCJDropshippingConnection(ctx context.Context, db gowild_data.Database, companyID string) error {
	dao := db.Table(CompanyCJDropshippingConnection{})
	rows, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := dao.Delete(ctx, r.(*CompanyCJDropshippingConnection).ID); err != nil {
			return err
		}
	}
	return nil
}

// --- Amazon connection CRUD ---

// GetCompanyAmazonConnection returns the Amazon PAAPI connection for a company.
func GetCompanyAmazonConnection(ctx context.Context, db gowild_data.Database, companyID string) (*CompanyAmazonConnection, error) {
	dao := db.Table(CompanyAmazonConnection{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*CompanyAmazonConnection), nil
}

// UpsertCompanyAmazonConnection inserts or updates a company's Amazon connection.
func UpsertCompanyAmazonConnection(ctx context.Context, db gowild_data.Database, conn *CompanyAmazonConnection) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	conn.CompanyID = strings.TrimSpace(conn.CompanyID)
	conn.AccessKeyEnc = strings.TrimSpace(conn.AccessKeyEnc)
	conn.SecretKeyEnc = strings.TrimSpace(conn.SecretKeyEnc)
	conn.PartnerTag = strings.TrimSpace(conn.PartnerTag)
	conn.Marketplace = strings.TrimSpace(conn.Marketplace)
	if conn.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if conn.Enabled {
		if conn.AccessKeyEnc == "" || conn.SecretKeyEnc == "" {
			return fmt.Errorf("access_key and secret_key are required")
		}
		if conn.PartnerTag == "" {
			return fmt.Errorf("partner_tag is required")
		}
	}
	if conn.Marketplace == "" {
		conn.Marketplace = "US"
	}
	if _, err := GetCompany(ctx, db, conn.CompanyID); err != nil {
		return fmt.Errorf("company not found: %w", err)
	}

	dao := db.Table(CompanyAmazonConnection{})
	now := time.Now()
	existing, err := GetCompanyAmazonConnection(ctx, db, conn.CompanyID)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.AccessKeyEnc = conn.AccessKeyEnc
		existing.SecretKeyEnc = conn.SecretKeyEnc
		existing.PartnerTag = conn.PartnerTag
		existing.Marketplace = conn.Marketplace
		existing.Enabled = conn.Enabled
		existing.UpdatedAt = now
		return dao.Update(ctx, existing)
	}

	if conn.ID == "" {
		conn.ID = newID()
	}
	conn.CreatedAt = now
	conn.UpdatedAt = now
	return dao.Insert(ctx, conn)
}

// DeleteCompanyAmazonConnection deletes a company's Amazon connection.
func DeleteCompanyAmazonConnection(ctx context.Context, db gowild_data.Database, companyID string) error {
	dao := db.Table(CompanyAmazonConnection{})
	rows, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"company_id": companyID},
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := dao.Delete(ctx, r.(*CompanyAmazonConnection).ID); err != nil {
			return err
		}
	}
	return nil
}

// --- ProductListing CRUD ---

// ListProductListings returns all active product listings for a company.
func ListProductListings(ctx context.Context, db gowild_data.Database, companyID string) ([]ProductListing, error) {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}
	dao := db.Table(ProductListing{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"company_id": companyID, "status": "active"},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil, err
	}
	out := make([]ProductListing, len(results))
	for i, r := range results {
		out[i] = *r.(*ProductListing)
	}
	return out, nil
}

// GetProductListing returns a single product listing by ID.
func GetProductListing(ctx context.Context, db gowild_data.Database, id string) (*ProductListing, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	var listing ProductListing
	if err := db.Table(ProductListing{}).Get(ctx, id, &listing); err != nil {
		return nil, err
	}
	return &listing, nil
}

// UpsertProductListing inserts or updates a product listing.
func UpsertProductListing(ctx context.Context, db gowild_data.Database, listing *ProductListing) error {
	if listing == nil {
		return fmt.Errorf("listing is nil")
	}
	listing.CompanyID = strings.TrimSpace(listing.CompanyID)
	listing.ShopifyProductID = strings.TrimSpace(listing.ShopifyProductID)
	listing.ShopifyVariantID = strings.TrimSpace(listing.ShopifyVariantID)
	listing.SupplierName = strings.TrimSpace(listing.SupplierName)
	listing.SupplierProductID = strings.TrimSpace(listing.SupplierProductID)
	listing.SupplierVariantID = strings.TrimSpace(listing.SupplierVariantID)
	if listing.CompanyID == "" {
		return fmt.Errorf("company_id is required")
	}
	if listing.ShopifyVariantID == "" {
		return fmt.Errorf("shopify_variant_id is required")
	}
	if listing.SupplierName == "" {
		return fmt.Errorf("supplier_name is required")
	}
	if listing.SupplierVariantID == "" {
		return fmt.Errorf("supplier_variant_id is required")
	}
	if listing.Status == "" {
		listing.Status = "active"
	}

	dao := db.Table(ProductListing{})
	now := time.Now()

	if listing.ID != "" {
		existing, err := GetProductListing(ctx, db, listing.ID)
		if err == nil && existing != nil {
			listing.CreatedAt = existing.CreatedAt
			listing.UpdatedAt = now
			return dao.Update(ctx, listing)
		}
	}

	if listing.ID == "" {
		listing.ID = newID()
	}
	listing.CreatedAt = now
	listing.UpdatedAt = now
	return dao.Insert(ctx, listing)
}

// DeleteProductListing removes a product listing by ID.
func DeleteProductListing(ctx context.Context, db gowild_data.Database, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	return db.Table(ProductListing{}).Delete(ctx, id)
}

// AddCompanyKnowledgeEntry creates a company-shared knowledge entry.
func AddCompanyKnowledgeEntry(ctx context.Context, db gowild_data.Database, companyID, createdByAgentID, kind, title, content string, tags []string, metadata map[string]any) (*CompanyKnowledgeEntry, error) {
	companyID = strings.TrimSpace(companyID)
	createdByAgentID = strings.TrimSpace(createdByAgentID)
	kind = strings.TrimSpace(kind)
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if _, err := GetCompany(ctx, db, companyID); err != nil {
		return nil, fmt.Errorf("company not found: %w", err)
	}

	// Validate creator membership when provided.
	if createdByAgentID != "" {
		member, err := GetCompanyMemberForAgent(ctx, db, createdByAgentID)
		if err != nil {
			return nil, err
		}
		if member == nil || member.CompanyID != companyID {
			return nil, fmt.Errorf("agent %s is not a member of company %s", createdByAgentID, companyID)
		}
	}

	tagsJSON := ""
	if len(tags) > 0 {
		if b, err := json.Marshal(tags); err == nil {
			tagsJSON = string(b)
		}
	}
	metadataJSON := ""
	if len(metadata) > 0 {
		if b, err := json.Marshal(metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	now := time.Now()
	entry := &CompanyKnowledgeEntry{
		ID:               newID(),
		CompanyID:        companyID,
		Kind:             kind,
		Title:            title,
		Content:          content,
		TagsJSON:         tagsJSON,
		MetadataJSON:     metadataJSON,
		CreatedByAgentID: createdByAgentID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := db.Table(CompanyKnowledgeEntry{}).Insert(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// ListCompanyKnowledgeEntries returns knowledge entries for a company with optional filters.
func ListCompanyKnowledgeEntries(ctx context.Context, db gowild_data.Database, companyID, query, kind string, limit int) ([]CompanyKnowledgeEntry, error) {
	companyID = strings.TrimSpace(companyID)
	query = strings.TrimSpace(strings.ToLower(query))
	kind = strings.TrimSpace(kind)
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}
	if limit <= 0 {
		limit = 50
	}

	dao := db.Table(CompanyKnowledgeEntry{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"company_id": companyID},
		OrderBy:   "updated_at",
		OrderDesc: true,
		Limit:     limit * 4, // fetch more if filtering by query in-memory
	})
	if err != nil {
		return nil, err
	}

	out := make([]CompanyKnowledgeEntry, 0, len(results))
	for _, r := range results {
		entry := r.(*CompanyKnowledgeEntry)
		if kind != "" && strings.TrimSpace(entry.Kind) != kind {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(entry.Title + "\n" + entry.Content + "\n" + entry.Kind + "\n" + entry.TagsJSON)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, *entry)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// GetCompanyKnowledgeEntry returns a specific knowledge entry by company+id.
func GetCompanyKnowledgeEntry(ctx context.Context, db gowild_data.Database, companyID, entryID string) (*CompanyKnowledgeEntry, error) {
	companyID = strings.TrimSpace(companyID)
	entryID = strings.TrimSpace(entryID)
	if companyID == "" || entryID == "" {
		return nil, fmt.Errorf("company_id and entry_id are required")
	}
	dao := db.Table(CompanyKnowledgeEntry{})
	var entry CompanyKnowledgeEntry
	if err := dao.Get(ctx, entryID, &entry); err != nil {
		return nil, err
	}
	if entry.CompanyID != companyID {
		return nil, fmt.Errorf("entry not found in company")
	}
	return &entry, nil
}

// UpdateCompanyKnowledgeEntry updates fields on a company knowledge entry.
func UpdateCompanyKnowledgeEntry(ctx context.Context, db gowild_data.Database, companyID, entryID, kind, title, content string, tags []string, metadata map[string]any) (*CompanyKnowledgeEntry, error) {
	entry, err := GetCompanyKnowledgeEntry(ctx, db, companyID, entryID)
	if err != nil {
		return nil, err
	}

	kind = strings.TrimSpace(kind)
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if kind != "" {
		entry.Kind = kind
	}
	if title != "" {
		entry.Title = title
	}
	if content != "" {
		entry.Content = content
	}
	if tags != nil {
		if len(tags) == 0 {
			entry.TagsJSON = ""
		} else if b, err := json.Marshal(tags); err == nil {
			entry.TagsJSON = string(b)
		}
	}
	if metadata != nil {
		if len(metadata) == 0 {
			entry.MetadataJSON = ""
		} else if b, err := json.Marshal(metadata); err == nil {
			entry.MetadataJSON = string(b)
		}
	}
	entry.UpdatedAt = time.Now()
	if err := db.Table(CompanyKnowledgeEntry{}).Update(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// DeleteCompanyKnowledgeEntry deletes a company knowledge entry.
func DeleteCompanyKnowledgeEntry(ctx context.Context, db gowild_data.Database, companyID, entryID string) error {
	entry, err := GetCompanyKnowledgeEntry(ctx, db, companyID, entryID)
	if err != nil {
		return err
	}
	return db.Table(CompanyKnowledgeEntry{}).Delete(ctx, entry.ID)
}
