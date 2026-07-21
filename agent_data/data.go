package data

import (
	"github.com/original-david-knight/go_wild/data"
)

func init() {
	gowild_data.RegisterFunc(RegisterTables)
}

// RegisterTables registers all agent data tables with the database.
func RegisterTables(db gowild_data.Database) error {
	if err := db.AddTable(Agent{}); err != nil {
		return err
	}
	if err := db.AddTable(MCPServer{}); err != nil {
		return err
	}
	if err := db.AddTable(AgentMCPServer{}); err != nil {
		return err
	}
	if err := db.AddTable(MemoryEntry{}); err != nil {
		return err
	}
	if err := db.AddTable(ArchiveEntry{}); err != nil {
		return err
	}
	if err := db.AddTable(Soul{}); err != nil {
		return err
	}
	if err := db.AddTable(Skill{}); err != nil {
		return err
	}
	if err := db.AddTable(WalletTransaction{}); err != nil {
		return err
	}
	if err := db.AddTable(Task{}); err != nil {
		return err
	}
	if err := db.AddTable(RecurringTask{}); err != nil {
		return err
	}
	if err := db.AddTable(HistorySnapshot{}); err != nil {
		return err
	}
	if err := db.AddTable(HistorySummary{}); err != nil {
		return err
	}
	if err := db.AddTable(PendingEmail{}); err != nil {
		return err
	}
	if err := db.AddTable(EmailWhitelistEntry{}); err != nil {
		return err
	}
	if err := db.AddTable(ChatMessage{}); err != nil {
		return err
	}
	if err := db.AddTable(TelegramMessageRecord{}); err != nil {
		return err
	}
	if err := db.AddTable(PeerGroup{}); err != nil {
		return err
	}
	if err := db.AddTable(PeerGroupMember{}); err != nil {
		return err
	}
	if err := db.AddTable(Company{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyMember{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyKnowledgeEntry{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyShopifyConnection{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyPolymarketConnection{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyTopDawgConnection{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyCJDropshippingConnection{}); err != nil {
		return err
	}
	if err := db.AddTable(CompanyAmazonConnection{}); err != nil {
		return err
	}
	if err := db.AddTable(AgentMessage{}); err != nil {
		return err
	}
	if err := db.AddTable(A2AMethod{}); err != nil {
		return err
	}
	if err := db.AddTable(AgentCapability{}); err != nil {
		return err
	}
	if err := db.AddTable(SpendEntry{}); err != nil {
		return err
	}
	if err := db.AddTable(SpendLimit{}); err != nil {
		return err
	}
	if err := db.AddTable(PipelineDefinition{}); err != nil {
		return err
	}
	if err := db.AddTable(PipelineRun{}); err != nil {
		return err
	}
	if err := db.AddTable(PipelineStepRun{}); err != nil {
		return err
	}
	if err := db.AddTable(WebhookConfig{}); err != nil {
		return err
	}
	if err := db.AddTable(WebhookEvent{}); err != nil {
		return err
	}
	if err := db.AddTable(MarketNote{}); err != nil {
		return err
	}
	if err := db.AddTable(MarketProperty{}); err != nil {
		return err
	}
	if err := gowild_data.EnsureUniqueIndex(db, MarketProperty{}, "market_properties_company_condition_key_uidx", "company_id", "condition_id", "key"); err != nil {
		return err
	}
	if err := db.AddTable(ProductListing{}); err != nil {
		return err
	}
	if err := db.AddTable(PaywallProduct{}); err != nil {
		return err
	}
	if err := db.AddTable(PaywallPurchase{}); err != nil {
		return err
	}
	if err := gowild_data.EnsureUniqueIndex(db, PaywallPurchase{}, "paywall_purchases_tx_hash_uidx", "tx_hash"); err != nil {
		return err
	}
	if err := db.AddTable(AgentSite{}); err != nil {
		return err
	}
	return nil
}
