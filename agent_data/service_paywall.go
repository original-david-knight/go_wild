package data

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// CreatePaywallProduct inserts a new paywall product.
func (s *AgentService) CreatePaywallProduct(ctx context.Context, product *PaywallProduct) error {
	product.AgentID = s.agentID
	product.CreatedAt = time.Now()
	if product.Status == "" {
		product.Status = "active"
	}
	return s.db.Table(PaywallProduct{}).Insert(ctx, product)
}

// GetPaywallProduct retrieves a paywall product by ID.
func (s *AgentService) GetPaywallProduct(ctx context.Context, productID string) (*PaywallProduct, error) {
	dao := s.db.Table(PaywallProduct{})
	var product PaywallProduct
	if err := dao.Get(ctx, productID, &product); err != nil {
		return nil, err
	}
	return &product, nil
}

// GetPaywallProductUnscoped retrieves a paywall product by ID without agent scoping.
// Used by public-facing endpoints where the agent ID isn't known.
func GetPaywallProductUnscoped(ctx context.Context, db gowild_data.Database, productID string) (*PaywallProduct, error) {
	dao := db.Table(PaywallProduct{})
	var product PaywallProduct
	if err := dao.Get(ctx, productID, &product); err != nil {
		return nil, err
	}
	return &product, nil
}

// CreatePaywallPurchase inserts a new purchase record with a download token.
func CreatePaywallPurchase(ctx context.Context, db gowild_data.Database, purchase *PaywallPurchase) error {
	purchase.CreatedAt = time.Now()
	if purchase.DownloadToken == "" {
		token, err := generateDownloadToken()
		if err != nil {
			return fmt.Errorf("failed to generate download token: %w", err)
		}
		purchase.DownloadToken = token
	}
	if purchase.TokenExpiresAt.IsZero() {
		purchase.TokenExpiresAt = time.Now().Add(24 * time.Hour)
	}
	return db.Table(PaywallPurchase{}).Insert(ctx, purchase)
}

// GetPaywallPurchaseByTxHash checks if a purchase with this tx_hash already exists (replay prevention).
func GetPaywallPurchaseByTxHash(ctx context.Context, db gowild_data.Database, txHash string) (*PaywallPurchase, error) {
	results, err := db.Table(PaywallPurchase{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"tx_hash": txHash},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*PaywallPurchase), nil
}

// GetPaywallPurchaseByToken retrieves a purchase by download token.
func GetPaywallPurchaseByToken(ctx context.Context, db gowild_data.Database, token string) (*PaywallPurchase, error) {
	results, err := db.Table(PaywallPurchase{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"download_token": token},
		Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].(*PaywallPurchase), nil
}

// CreatePaywallProductUnscoped inserts a new paywall product without agent scoping.
// Used by agent_net where the agent ID comes from auth middleware, not a service wrapper.
func CreatePaywallProductUnscoped(ctx context.Context, db gowild_data.Database, product *PaywallProduct) error {
	product.CreatedAt = time.Now()
	if product.Status == "" {
		product.Status = "active"
	}
	return db.Table(PaywallProduct{}).Insert(ctx, product)
}

// ListPaywallProductsUnscoped lists all active paywall products for an agent.
func ListPaywallProductsUnscoped(ctx context.Context, db gowild_data.Database, agentID string) ([]*PaywallProduct, error) {
	results, err := db.Table(PaywallProduct{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"agent_id": agentID, "status": "active"},
		OrderBy: "created_at DESC",
	})
	if err != nil {
		return nil, err
	}
	products := make([]*PaywallProduct, len(results))
	for i, r := range results {
		products[i] = r.(*PaywallProduct)
	}
	return products, nil
}

func generateDownloadToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
