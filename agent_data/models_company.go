package data

import "time"

// Company is a first-class grouping of agents with shared resources.
type Company struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	CEOAgentID        string    `json:"ceo_agent_id"`
	WebhookIngressKey string    `json:"webhook_ingress_key,omitempty"`
	WalletSeedPhrase  string    `json:"wallet_seed_phrase,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (Company) TableName() string { return "companies" }

// CompanyMember links an agent to a company.
type CompanyMember struct {
	ID        string    `json:"id"`
	CompanyID string    `json:"company_id"`
	AgentID   string    `json:"agent_id"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CompanyKnowledgeEntry stores shared company knowledge.
type CompanyKnowledgeEntry struct {
	ID               string    `json:"id"`
	CompanyID        string    `json:"company_id"`
	Kind             string    `json:"kind"`
	Title            string    `json:"title"`
	Content          string    `json:"content"`
	TagsJSON         string    `json:"tags_json,omitempty"`
	MetadataJSON     string    `json:"metadata_json,omitempty"`
	CreatedByAgentID string    `json:"created_by_agent_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (CompanyKnowledgeEntry) TableName() string { return "company_knowledge_entries" }

// CompanyShopifyConnection stores Shopify credentials/config for a company.
type CompanyShopifyConnection struct {
	ID               string    `json:"id"`
	CompanyID        string    `json:"company_id"`
	ShopURL          string    `json:"shop_url"`
	APIVersion       string    `json:"api_version"`
	ClientID         string    `json:"client_id,omitempty"`
	ClientSecretEnc  string    `json:"client_secret_enc,omitempty"`
	AccessTokenEnc   string    `json:"access_token_enc"`
	AccessTokenExpAt time.Time `json:"access_token_expires_at,omitempty"`
	WebhookSecretEnc string    `json:"webhook_secret_enc,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastTestedAt     time.Time `json:"last_tested_at,omitempty"`
}

// CompanyPolymarketConnection stores Polymarket execution/config settings for a company.
type CompanyPolymarketConnection struct {
	ID            string    `json:"id"`
	CompanyID     string    `json:"company_id"`
	ProxyURL      string    `json:"proxy_url,omitempty"`
	OnchainRPCURL string    `json:"onchain_rpc_url,omitempty"`
	FunderAddress string    `json:"funder_address,omitempty"`
	SignatureType int       `json:"signature_type,omitempty"`
	ChainID       int       `json:"chain_id,omitempty"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LastTestedAt  time.Time `json:"last_tested_at,omitempty"`
}

func (CompanyPolymarketConnection) TableName() string { return "company_polymarket_connections" }

// CompanyTopDawgConnection stores TopDawg supplier settings for a company.
type CompanyTopDawgConnection struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	APIKeyEnc    string    `json:"api_key_enc,omitempty"`
	SupplierID   string    `json:"supplier_id"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastTestedAt time.Time `json:"last_tested_at,omitempty"`
}

func (CompanyTopDawgConnection) TableName() string { return "company_topdawg_connections" }

// CompanyCJDropshippingConnection stores CJ Dropshipping settings for a company.
type CompanyCJDropshippingConnection struct {
	ID                     string    `json:"id"`
	CompanyID              string    `json:"company_id"`
	APIKeyEnc              string    `json:"api_key_enc,omitempty"`
	AccessTokenEnc         string    `json:"access_token_enc,omitempty"`
	AccessTokenExpAt       time.Time `json:"access_token_expires_at,omitempty"`
	RefreshTokenEnc        string    `json:"refresh_token_enc,omitempty"`
	RefreshTokenExpAt      time.Time `json:"refresh_token_expires_at,omitempty"`
	PlatformTokenEnc       string    `json:"platform_token_enc,omitempty"`
	DefaultFromCountryCode string    `json:"default_from_country_code,omitempty"`
	Enabled                bool      `json:"enabled"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
	LastTestedAt           time.Time `json:"last_tested_at,omitempty"`
}

func (CompanyCJDropshippingConnection) TableName() string {
	return "company_cjdropshipping_connections"
}

// CompanyAmazonConnection stores Amazon Product Advertising API credentials for a company.
type CompanyAmazonConnection struct {
	ID           string    `json:"id"`
	CompanyID    string    `json:"company_id"`
	AccessKeyEnc string    `json:"access_key_enc"`
	SecretKeyEnc string    `json:"secret_key_enc"`
	PartnerTag   string    `json:"partner_tag"`
	Marketplace  string    `json:"marketplace"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastTestedAt time.Time `json:"last_tested_at,omitempty"`
}

func (CompanyAmazonConnection) TableName() string { return "company_amazon_connections" }

// MarketNote stores a company-scoped note about a Polymarket market.
type MarketNote struct {
	ID               string    `json:"id"`
	CompanyID        string    `json:"company_id"`
	ConditionID      string    `json:"condition_id"`
	Content          string    `json:"content"`
	MetadataJSON     string    `json:"metadata_json,omitempty"`
	CreatedByAgentID string    `json:"created_by_agent_id"`
	CreatedAt        time.Time `json:"created_at"`
}

func (MarketNote) TableName() string { return "market_notes" }

// MarketProperty stores a typed key-value pair scoped to a company + market.
// ValueType is one of: "string", "datetime", "bool", "currency".
// Value is always stored as a string; callers parse according to ValueType.
type MarketProperty struct {
	ID          string    `json:"id"`
	CompanyID   string    `json:"company_id"`
	ConditionID string    `json:"condition_id"`
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	ValueType   string    `json:"value_type"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (MarketProperty) TableName() string { return "market_properties" }

// ProductListing links a Shopify variant to a supplier variant for inventory sync.
type ProductListing struct {
	ID                     string    `json:"id"`
	CompanyID              string    `json:"company_id"`
	ShopifyProductID       string    `json:"shopify_product_id"`
	ShopifyVariantID       string    `json:"shopify_variant_id"`
	ShopifyInventoryItemID string    `json:"shopify_inventory_item_id"`
	ShopifyLocationID      string    `json:"shopify_location_id"`
	SupplierName           string    `json:"supplier_name"`
	SupplierProductID      string    `json:"supplier_product_id"`
	SupplierVariantID      string    `json:"supplier_variant_id"`
	SupplierCountryCode    string    `json:"supplier_country_code,omitempty"`
	Status                 string    `json:"status"`
	LastSyncedAt           time.Time `json:"last_synced_at,omitempty"`
	LastSyncQuantity       int       `json:"last_sync_quantity"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (ProductListing) TableName() string { return "product_listings" }
