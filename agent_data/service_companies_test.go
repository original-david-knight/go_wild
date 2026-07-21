package data

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompanyMembershipOneCompanyMax(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	agentSvc := NewAgentService(db, "alice")
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	c1, err := CreateCompany(ctx, db, "Alpha", "", "")
	if err != nil {
		t.Fatalf("CreateCompany alpha failed: %v", err)
	}
	c2, err := CreateCompany(ctx, db, "Beta", "", "")
	if err != nil {
		t.Fatalf("CreateCompany beta failed: %v", err)
	}

	if err := AddAgentToCompany(ctx, db, c1.ID, "alice", "member"); err != nil {
		t.Fatalf("AddAgentToCompany alpha failed: %v", err)
	}
	if err := AddAgentToCompany(ctx, db, c2.ID, "alice", "member"); err == nil {
		t.Fatalf("expected one-company-max conflict, got nil error")
	} else if !errors.Is(err, ErrCompanyMembershipConflict) {
		t.Fatalf("expected ErrCompanyMembershipConflict, got %v", err)
	}
}

func TestSetCompanyCEORequiresMembership(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	aliceSvc := NewAgentService(db, "alice")
	if _, err := aliceSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(alice) failed: %v", err)
	}
	bobSvc := NewAgentService(db, "bob")
	if _, err := bobSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(bob) failed: %v", err)
	}

	c, err := CreateCompany(ctx, db, "Gamma", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := AddAgentToCompany(ctx, db, c.ID, "alice", "member"); err != nil {
		t.Fatalf("AddAgentToCompany(alice) failed: %v", err)
	}

	if err := SetCompanyCEO(ctx, db, c.ID, "bob"); err == nil {
		t.Fatalf("expected membership validation error, got nil")
	}
	if err := SetCompanyCEO(ctx, db, c.ID, "alice"); err != nil {
		t.Fatalf("SetCompanyCEO(alice) failed: %v", err)
	}
}

func TestCompanyWebhookIngressKeyLifecycle(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	company, err := CreateCompany(ctx, db, "Webhook Key Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if strings.TrimSpace(company.WebhookIngressKey) == "" {
		t.Fatalf("expected webhook ingress key to be generated")
	}

	byKey, err := GetCompanyByWebhookIngressKey(ctx, db, company.WebhookIngressKey)
	if err != nil {
		t.Fatalf("GetCompanyByWebhookIngressKey failed: %v", err)
	}
	if byKey == nil || byKey.ID != company.ID {
		t.Fatalf("expected company lookup by webhook key")
	}

	ensured, err := EnsureCompanyWebhookIngressKey(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("EnsureCompanyWebhookIngressKey failed: %v", err)
	}
	if ensured != company.WebhookIngressKey {
		t.Fatalf("expected ensure to keep existing key")
	}

	rotated, err := RotateCompanyWebhookIngressKey(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("RotateCompanyWebhookIngressKey failed: %v", err)
	}
	if strings.TrimSpace(rotated) == "" || rotated == company.WebhookIngressKey {
		t.Fatalf("expected rotated key to differ from original")
	}
}

func TestCompanyShopifyConnectionUpsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "Delta", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyShopifyConnection(ctx, db, &CompanyShopifyConnection{
		CompanyID:       c.ID,
		ShopURL:         "demo.myshopify.com",
		APIVersion:      "2025-01",
		ClientID:        "client-1",
		ClientSecretEnc: "secret-1",
		Enabled:         true,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyShopifyConnection(insert) failed: %v", err)
	}

	conn, err := GetCompanyShopifyConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyShopifyConnection failed: %v", err)
	}
	if conn == nil {
		t.Fatalf("expected non-nil connection")
	}
	if conn.ShopURL != "demo.myshopify.com" {
		t.Fatalf("unexpected shop url: %q", conn.ShopURL)
	}
	if conn.ClientID != "client-1" {
		t.Fatalf("unexpected client_id: %q", conn.ClientID)
	}

	err = UpsertCompanyShopifyConnection(ctx, db, &CompanyShopifyConnection{
		CompanyID:       c.ID,
		ShopURL:         "new-demo.myshopify.com",
		APIVersion:      "2025-01",
		ClientID:        "client-2",
		ClientSecretEnc: "secret-2",
		AccessTokenEnc:  "token-2",
		Enabled:         false,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyShopifyConnection(update) failed: %v", err)
	}

	conn, err = GetCompanyShopifyConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyShopifyConnection after update failed: %v", err)
	}
	if conn.ShopURL != "new-demo.myshopify.com" {
		t.Fatalf("expected updated shop url, got %q", conn.ShopURL)
	}
	if conn.AccessTokenEnc != "token-2" {
		t.Fatalf("expected updated token")
	}
	if conn.ClientID != "client-2" {
		t.Fatalf("expected updated client_id, got %q", conn.ClientID)
	}
	if conn.Enabled {
		t.Fatalf("expected enabled=false after update")
	}
}

func TestCompanyShopifyConnectionValidation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "Delta Validation", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyShopifyConnection(ctx, db, &CompanyShopifyConnection{
		CompanyID:  c.ID,
		ShopURL:    "demo.myshopify.com",
		APIVersion: "2025-01",
		Enabled:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "client credentials are required") {
		t.Fatalf("expected missing credentials validation error, got: %v", err)
	}

	err = UpsertCompanyShopifyConnection(ctx, db, &CompanyShopifyConnection{
		CompanyID:  c.ID,
		ShopURL:    "demo.myshopify.com",
		APIVersion: "2025-01",
		ClientID:   "client-1",
		Enabled:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "client_id and client_secret_enc must both be set") {
		t.Fatalf("expected partial client credentials validation error, got: %v", err)
	}

	err = UpsertCompanyShopifyConnection(ctx, db, &CompanyShopifyConnection{
		CompanyID:      c.ID,
		ShopURL:        "demo.myshopify.com",
		APIVersion:     "2025-01",
		AccessTokenEnc: "legacy-token",
		Enabled:        true,
	})
	if err == nil || !strings.Contains(err.Error(), "client credentials are required") {
		t.Fatalf("expected legacy token-only validation error, got: %v", err)
	}
}

func TestCompanyPolymarketConnectionUpsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "Poly Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyPolymarketConnection(ctx, db, &CompanyPolymarketConnection{
		CompanyID:     c.ID,
		ProxyURL:      "socks5://127.0.0.1:9050",
		OnchainRPCURL: "https://polygon-rpc.example",
		FunderAddress: "0xabc",
		SignatureType: 1,
		ChainID:       137,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyPolymarketConnection(insert) failed: %v", err)
	}

	conn, err := GetCompanyPolymarketConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyPolymarketConnection failed: %v", err)
	}
	if conn == nil {
		t.Fatalf("expected non-nil connection")
	}
	if conn.ProxyURL != "socks5://127.0.0.1:9050" {
		t.Fatalf("unexpected proxy url: %q", conn.ProxyURL)
	}

	err = UpsertCompanyPolymarketConnection(ctx, db, &CompanyPolymarketConnection{
		CompanyID:     c.ID,
		ProxyURL:      "socks5://127.0.0.1:9051",
		OnchainRPCURL: "https://polygon-rpc.alt",
		FunderAddress: "0xdef",
		SignatureType: 2,
		ChainID:       1,
		Enabled:       false,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyPolymarketConnection(update) failed: %v", err)
	}

	conn, err = GetCompanyPolymarketConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyPolymarketConnection after update failed: %v", err)
	}
	if conn.ProxyURL != "socks5://127.0.0.1:9051" {
		t.Fatalf("expected updated proxy, got %q", conn.ProxyURL)
	}
	if conn.SignatureType != 2 {
		t.Fatalf("expected updated signature type 2, got %d", conn.SignatureType)
	}
	if conn.Enabled {
		t.Fatalf("expected enabled=false after update")
	}
}

func TestCompanyTopDawgConnectionUpsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "TopDawg Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyTopDawgConnection(ctx, db, &CompanyTopDawgConnection{
		CompanyID:  c.ID,
		APIKeyEnc:  "td-key-1",
		SupplierID: "supplier-1",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyTopDawgConnection(insert) failed: %v", err)
	}

	conn, err := GetCompanyTopDawgConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyTopDawgConnection failed: %v", err)
	}
	if conn == nil {
		t.Fatalf("expected non-nil connection")
	}
	if conn.SupplierID != "supplier-1" {
		t.Fatalf("unexpected supplier_id: %q", conn.SupplierID)
	}

	err = UpsertCompanyTopDawgConnection(ctx, db, &CompanyTopDawgConnection{
		CompanyID:  c.ID,
		APIKeyEnc:  "td-key-2",
		SupplierID: "supplier-2",
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyTopDawgConnection(update) failed: %v", err)
	}

	conn, err = GetCompanyTopDawgConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyTopDawgConnection after update failed: %v", err)
	}
	if conn.SupplierID != "supplier-2" {
		t.Fatalf("expected updated supplier_id, got %q", conn.SupplierID)
	}
	if conn.APIKeyEnc != "td-key-2" {
		t.Fatalf("expected updated api key")
	}
	if conn.Enabled {
		t.Fatalf("expected enabled=false after update")
	}
}

func TestCompanyTopDawgConnectionValidation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "TopDawg Validation", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyTopDawgConnection(ctx, db, &CompanyTopDawgConnection{
		CompanyID:  c.ID,
		SupplierID: "supplier-1",
		Enabled:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "api_key is required") {
		t.Fatalf("expected api_key validation error, got: %v", err)
	}

	err = UpsertCompanyTopDawgConnection(ctx, db, &CompanyTopDawgConnection{
		CompanyID: c.ID,
		APIKeyEnc: "td-key-1",
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "supplier_id is required") {
		t.Fatalf("expected supplier_id validation error, got: %v", err)
	}
}

func TestCompanyCJDropshippingConnectionUpsertAndGet(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "CJ Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyCJDropshippingConnection(ctx, db, &CompanyCJDropshippingConnection{
		CompanyID:              c.ID,
		APIKeyEnc:              "cj-key-1",
		DefaultFromCountryCode: "cn",
		Enabled:                true,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyCJDropshippingConnection(insert) failed: %v", err)
	}

	conn, err := GetCompanyCJDropshippingConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyCJDropshippingConnection failed: %v", err)
	}
	if conn == nil {
		t.Fatalf("expected non-nil connection")
	}
	if conn.DefaultFromCountryCode != "CN" {
		t.Fatalf("expected normalized default country CN, got %q", conn.DefaultFromCountryCode)
	}
	if conn.APIKeyEnc != "cj-key-1" {
		t.Fatalf("expected api key cj-key-1, got %q", conn.APIKeyEnc)
	}

	accessExp := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	refreshExp := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	err = UpsertCompanyCJDropshippingConnection(ctx, db, &CompanyCJDropshippingConnection{
		CompanyID:              c.ID,
		APIKeyEnc:              "cj-key-2",
		AccessTokenEnc:         "access-2",
		AccessTokenExpAt:       accessExp,
		RefreshTokenEnc:        "refresh-2",
		RefreshTokenExpAt:      refreshExp,
		PlatformTokenEnc:       "platform-2",
		DefaultFromCountryCode: "us",
		Enabled:                false,
	})
	if err != nil {
		t.Fatalf("UpsertCompanyCJDropshippingConnection(update) failed: %v", err)
	}

	conn, err = GetCompanyCJDropshippingConnection(ctx, db, c.ID)
	if err != nil {
		t.Fatalf("GetCompanyCJDropshippingConnection after update failed: %v", err)
	}
	if conn.APIKeyEnc != "cj-key-2" {
		t.Fatalf("expected updated api key, got %q", conn.APIKeyEnc)
	}
	if conn.AccessTokenEnc != "access-2" {
		t.Fatalf("expected updated access token, got %q", conn.AccessTokenEnc)
	}
	if conn.RefreshTokenEnc != "refresh-2" {
		t.Fatalf("expected updated refresh token, got %q", conn.RefreshTokenEnc)
	}
	if conn.PlatformTokenEnc != "platform-2" {
		t.Fatalf("expected updated platform token, got %q", conn.PlatformTokenEnc)
	}
	if conn.DefaultFromCountryCode != "US" {
		t.Fatalf("expected updated default country US, got %q", conn.DefaultFromCountryCode)
	}
	if conn.Enabled {
		t.Fatalf("expected enabled=false after update")
	}
	if conn.AccessTokenExpAt.IsZero() {
		t.Fatalf("expected persisted access token expiry")
	}
	if conn.RefreshTokenExpAt.IsZero() {
		t.Fatalf("expected persisted refresh token expiry")
	}
}

func TestCompanyCJDropshippingConnectionValidation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	c, err := CreateCompany(ctx, db, "CJ Validation", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	err = UpsertCompanyCJDropshippingConnection(ctx, db, &CompanyCJDropshippingConnection{
		CompanyID: c.ID,
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "api_key or access_token is required") {
		t.Fatalf("expected api_key/access_token validation error, got: %v", err)
	}

	err = UpsertCompanyCJDropshippingConnection(ctx, db, &CompanyCJDropshippingConnection{
		CompanyID:              c.ID,
		APIKeyEnc:              "cj-key",
		DefaultFromCountryCode: "USA",
		Enabled:                true,
	})
	if err == nil || !strings.Contains(err.Error(), "default_from_country_code must be a two-letter country code") {
		t.Fatalf("expected country-code validation error, got: %v", err)
	}
}

func TestCreateCompanyAndEnsureWalletSeedPhrase(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	company, err := CreateCompany(ctx, db, "Wallet Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if strings.TrimSpace(company.WalletSeedPhrase) == "" {
		t.Fatalf("expected CreateCompany to set wallet seed phrase")
	}

	// Clear it to simulate legacy rows, then verify ensure repopulates.
	company.WalletSeedPhrase = ""
	if err := UpdateCompany(ctx, db, company); err != nil {
		t.Fatalf("UpdateCompany failed: %v", err)
	}
	seed, err := EnsureCompanyWalletSeedPhrase(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("EnsureCompanyWalletSeedPhrase failed: %v", err)
	}
	if strings.TrimSpace(seed) == "" {
		t.Fatalf("expected non-empty ensured seed phrase")
	}

	reloaded, err := GetCompany(ctx, db, company.ID)
	if err != nil {
		t.Fatalf("GetCompany failed: %v", err)
	}
	if strings.TrimSpace(reloaded.WalletSeedPhrase) == "" {
		t.Fatalf("expected ensured seed phrase persisted")
	}
}

func TestCompanyKnowledgeCRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	agentSvc := NewAgentService(db, "alice")
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(alice) failed: %v", err)
	}
	bobSvc := NewAgentService(db, "bob")
	if _, err := bobSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(bob) failed: %v", err)
	}

	company, err := CreateCompany(ctx, db, "Knowledge Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := AddAgentToCompany(ctx, db, company.ID, "alice", "member"); err != nil {
		t.Fatalf("AddAgentToCompany(alice) failed: %v", err)
	}

	if _, err := AddCompanyKnowledgeEntry(ctx, db, company.ID, "bob", "policy", "Bad creator", "content", nil, nil); err == nil {
		t.Fatalf("expected non-member creator validation error")
	}

	entry, err := AddCompanyKnowledgeEntry(
		ctx,
		db,
		company.ID,
		"alice",
		"policy",
		"Refund policy",
		"Refunds require receipt.",
		[]string{"refunds", "support"},
		map[string]any{"source": "ops"},
	)
	if err != nil {
		t.Fatalf("AddCompanyKnowledgeEntry failed: %v", err)
	}
	if entry.ID == "" {
		t.Fatalf("expected created entry id")
	}

	got, err := GetCompanyKnowledgeEntry(ctx, db, company.ID, entry.ID)
	if err != nil {
		t.Fatalf("GetCompanyKnowledgeEntry failed: %v", err)
	}
	if got.Title != "Refund policy" {
		t.Fatalf("unexpected title: %q", got.Title)
	}
	if !strings.Contains(got.TagsJSON, "refunds") {
		t.Fatalf("expected tags json to contain refunds")
	}

	list, err := ListCompanyKnowledgeEntries(ctx, db, company.ID, "receipt", "policy", 10)
	if err != nil {
		t.Fatalf("ListCompanyKnowledgeEntries failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one entry from filtered list, got %d", len(list))
	}

	updated, err := UpdateCompanyKnowledgeEntry(
		ctx,
		db,
		company.ID,
		entry.ID,
		"procedure",
		"Refund procedure",
		"",
		[]string{},
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("UpdateCompanyKnowledgeEntry failed: %v", err)
	}
	if updated.Kind != "procedure" {
		t.Fatalf("expected updated kind procedure, got %q", updated.Kind)
	}
	if updated.TagsJSON != "" {
		t.Fatalf("expected tags to be cleared")
	}
	if updated.MetadataJSON != "" {
		t.Fatalf("expected metadata to be cleared")
	}

	if err := DeleteCompanyKnowledgeEntry(ctx, db, company.ID, entry.ID); err != nil {
		t.Fatalf("DeleteCompanyKnowledgeEntry failed: %v", err)
	}
	if _, err := GetCompanyKnowledgeEntry(ctx, db, company.ID, entry.ID); err == nil {
		t.Fatalf("expected get to fail after delete")
	}
}

func TestRemoveCompanyMemberBlocksCurrentCEO(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for _, agentID := range []string{"ceo-agent", "next-ceo"} {
		agentSvc := NewAgentService(db, agentID)
		if _, err := agentSvc.EnsureAgent(ctx); err != nil {
			t.Fatalf("EnsureAgent(%s) failed: %v", agentID, err)
		}
	}

	company, err := CreateCompany(ctx, db, "CEO Lock Co", "", "ceo-agent")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := AddAgentToCompany(ctx, db, company.ID, "next-ceo", "member"); err != nil {
		t.Fatalf("AddAgentToCompany(next-ceo) failed: %v", err)
	}

	if err := RemoveAgentFromCompany(ctx, db, company.ID, "ceo-agent"); err == nil {
		t.Fatalf("expected removing current ceo to fail")
	}

	if err := SetCompanyCEO(ctx, db, company.ID, "next-ceo"); err != nil {
		t.Fatalf("SetCompanyCEO(next-ceo) failed: %v", err)
	}
	if err := RemoveAgentFromCompany(ctx, db, company.ID, "ceo-agent"); err != nil {
		t.Fatalf("RemoveAgentFromCompany(former ceo) failed: %v", err)
	}

	member, err := GetCompanyMemberForAgent(ctx, db, "ceo-agent")
	if err != nil {
		t.Fatalf("GetCompanyMemberForAgent(former ceo) failed: %v", err)
	}
	if member != nil {
		t.Fatalf("expected former ceo membership to be removed")
	}
}
