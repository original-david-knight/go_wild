package main

import (
	"context"
	"sync"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

const (
	// polymarketClientCacheTTL is how long a cached Polymarket client remains valid.
	polymarketClientCacheTTL = 10 * time.Minute

	// polymarketAuthErrorCooldown is how long to suppress retries after an auth error
	// (e.g. "tenant disabled"). Prevents hammering Polymarket auth endpoints.
	polymarketAuthErrorCooldown = 5 * time.Minute
)

// cachedPolymarketClient holds a cached Polymarket client for a company.
type cachedPolymarketClient struct {
	client    *polymarket.Client
	companyID string
	createdAt time.Time
}

// polymarketAuthError records a recent auth failure for a company.
type polymarketAuthError struct {
	err      error
	failedAt time.Time
}

// BrokerPolymarketHandler handles Polymarket proxy requests.
type BrokerPolymarketHandler struct {
	service *AgentService

	mu sync.Mutex

	// Client cache: companyID -> cached client. Avoids re-deriving API credentials
	// on every tool call (each derivation hits Polymarket auth endpoints).
	clientCache map[string]*cachedPolymarketClient

	// Auth error cache: companyID -> recent auth failure. Prevents repeatedly
	// hitting Polymarket auth endpoints when the tenant is disabled.
	authErrors map[string]*polymarketAuthError

	// Proxy URL for SOCKS5 routing (from POLYMARKET_PROXY_URL env var)
	proxyURL string

	// Test seams: when set, these override default client/orderbook behavior.
	getClientFn    func(context.Context, string) (*polymarket.Client, error)
	getOrderBookFn func(context.Context, *polymarket.Client, string) (*polymarket.OrderBook, error)
}

// NewBrokerPolymarketHandler creates a new Polymarket broker handler.
func NewBrokerPolymarketHandler(service *AgentService) *BrokerPolymarketHandler {
	return &BrokerPolymarketHandler{
		service:     service,
		clientCache: make(map[string]*cachedPolymarketClient),
		authErrors:  make(map[string]*polymarketAuthError),
	}
}
