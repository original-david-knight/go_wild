package data

import "time"

// PaywallProduct is a digital product for sale via crypto paywall.
type PaywallProduct struct {
	ID            string    `json:"id"`
	AgentID       string    `json:"agent_id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	PriceUSDC     string    `json:"price_usdc"`
	Chain         string    `json:"chain"`
	WalletAddress string    `json:"wallet_address"`
	StoragePath   string    `json:"-"` // server-side only, never exposed via API
	FileName      string    `json:"file_name"`
	FileSize      int64     `json:"file_size"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// PaywallPurchase is a verified purchase of a paywall product.
type PaywallPurchase struct {
	ID             string     `json:"id"`
	ProductID      string     `json:"product_id"`
	TxHash         string     `json:"tx_hash"`
	Chain          string     `json:"chain"`
	PayerAddress   string     `json:"payer_address"`
	AmountUSDC     string     `json:"amount_usdc"`
	BlockTime      *time.Time `json:"block_time,omitempty"`
	DownloadToken  string     `json:"download_token"`
	TokenExpiresAt time.Time  `json:"token_expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
}
