package gowild_polymarket

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	tokenMetadataCacheTTL = 10 * time.Minute
	priceCacheTTL         = 2 * time.Second
)

type cachedTokenMetadata struct {
	negRisk     bool
	tickSize    float64
	hasNegRisk  bool
	hasTickSize bool
	expiresAt   time.Time
}

type cachedPriceQuote struct {
	price     string
	expiresAt time.Time
}

// Signature type constants for Polymarket orders.
const (
	SigTypeEOA            = 0 // Direct EOA signature
	SigTypePolyProxy      = 1 // Polymarket magic/email proxy wallet
	SigTypePolyGnosisSafe = 2 // Polymarket Gnosis Safe (browser wallet)
)

// Client is the Polymarket CLOB API client.
type Client struct {
	httpClient    *http.Client // Used for CLOB API (may be proxied)
	publicClient  *http.Client // Used for public/Gamma API (never proxied)
	privateKey    *ecdsa.PrivateKey
	address       string // EOA address (signer)
	funder        string // Funder address (maker) — equals address if no proxy
	signatureType int    // 0=EOA, 1=POLY_PROXY, 2=POLY_GNOSIS_SAFE
	chainID       int
	onchainRPCURL string          // Polygon RPC endpoint used for on-chain settlement transactions
	creds         *apiCredentials // L2 API credentials (derived from L1 signing)
	credsErr      error           // Non-nil if CLOB auth failed (public/on-chain ops still work)

	approvalMu       sync.Mutex
	approvalsEnsured bool

	cacheMu            sync.Mutex
	tokenMetadataCache map[string]cachedTokenMetadata
	priceCache         map[string]cachedPriceQuote

	// Test seams.
	getNegRiskFn             func(context.Context, string) (bool, error)
	getTickSizeFn            func(context.Context, string) (float64, error)
	submitOrderFn            func(context.Context, placeOrderRequest) (*PlaceOrderResponse, error)
	ensureAllowancesFn       func(context.Context) error
	updateBalanceAllowanceFn func(context.Context, string, string) error
}

// Option configures the Client.
type Option func(*Client)

// socksProxyTransport builds an HTTP transport that dials through the given
// SOCKS5 proxy. It returns nil if the proxy URL is unusable.
func socksProxyTransport(proxyURL string) *http.Transport {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil
	}
	dialer, err := proxy.FromURL(u, proxy.Direct)
	if err != nil {
		return nil
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil
	}
	return &http.Transport{
		DialContext:         contextDialer.DialContext,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

// WithProxy routes the CLOB API HTTP client through a SOCKS5 proxy. The public
// Gamma/Data API client is left direct.
func WithProxy(proxyURL string) Option {
	return func(c *Client) {
		if t := socksProxyTransport(proxyURL); t != nil {
			c.httpClient.Transport = t
		}
	}
}

// WithFullProxy routes ALL Polymarket HTTP traffic through a SOCKS5 proxy — the
// CLOB API as well as the public Gamma and Data APIs. Use this when every
// Polymarket request must egress through the proxy/VPN (e.g. for geo-compliance),
// not just the trading calls. The Polygon JSON-RPC used for on-chain settlement is
// a separate public node and is not affected by this option.
func WithFullProxy(proxyURL string) Option {
	return func(c *Client) {
		t := socksProxyTransport(proxyURL)
		if t == nil {
			return
		}
		c.httpClient.Transport = t
		c.publicClient.Transport = t
	}
}

// WithChainID sets the chain ID (default: 137 for Polygon).
func WithChainID(chainID int) Option {
	return func(c *Client) {
		c.chainID = chainID
	}
}

// WithOnchainRPC sets the Polygon RPC endpoint used for on-chain settlement transactions.
func WithOnchainRPC(rpcURL string) Option {
	return func(c *Client) {
		if trimmed := strings.TrimSpace(rpcURL); trimmed != "" {
			c.onchainRPCURL = trimmed
		}
	}
}

// NewClient creates a new Polymarket CLOB client.
// The private key is used for L1 EIP-712 signing (API key derivation) and order signing.
func NewClient(privateKey *ecdsa.PrivateKey, opts ...Option) (*Client, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("private key is required")
	}

	address := privateKeyToAddress(privateKey)

	c := &Client{
		httpClient:         &http.Client{Timeout: 30 * time.Second},
		publicClient:       &http.Client{Timeout: 30 * time.Second},
		privateKey:         privateKey,
		address:            address,
		funder:             address, // Default: EOA is the funder
		signatureType:      SigTypeEOA,
		chainID:            polygonChainID,
		onchainRPCURL:      PolygonRPCURL,
		tokenMetadataCache: make(map[string]cachedTokenMetadata),
		priceCache:         make(map[string]cachedPriceQuote),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Create or derive L2 API credentials from L1 signature (uses the proxy-configured HTTP client).
	// Auth failure is non-fatal: public API calls (GetPositions, market data) and on-chain
	// operations (RedeemWinnings) still work without CLOB credentials. Only authenticated
	// CLOB operations (PlaceOrder, CancelOrder, GetOrders) will fail.
	creds, err := createOrDeriveAPICredentials(privateKey, c.chainID, c.httpClient)
	if err != nil {
		c.credsErr = fmt.Errorf("CLOB API authentication failed (trading/authenticated operations unavailable, but public data and on-chain operations still work): %w", err)
	} else {
		c.creds = creds
	}

	return c, nil
}

func (c *Client) ensureCaches() {
	if c == nil {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.tokenMetadataCache == nil {
		c.tokenMetadataCache = make(map[string]cachedTokenMetadata)
	}
	if c.priceCache == nil {
		c.priceCache = make(map[string]cachedPriceQuote)
	}
}

// NewClientWithFunder creates a new Polymarket CLOB client with a specific funder (proxy wallet) address.
// This reuses existing API credentials from a standard client creation.
func NewClientWithFunder(privateKey *ecdsa.PrivateKey, funderAddr string, sigType int, opts ...Option) (*Client, error) {
	c, err := NewClient(privateKey, opts...)
	if err != nil {
		return nil, err
	}
	c.funder = funderAddr
	c.signatureType = sigType
	return c, nil
}

// Address returns the Ethereum address of the client.
func (c *Client) Address() string {
	return c.address
}

// FunderAddress returns the address that holds funds and positions for orders.
// In EOA mode it is the same as Address; in proxy/safe mode it is the configured
// proxy wallet / safe address.
func (c *Client) FunderAddress() string {
	return c.funder
}

// AccountAddress returns the address whose Polymarket portfolio should be read.
func (c *Client) AccountAddress() string {
	return c.accountAddress()
}

func (c *Client) accountAddress() string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(c.funder) != "" {
		return c.funder
	}
	return c.address
}

// hasCLOBAuth returns true if the client has valid CLOB API credentials.
func (c *Client) hasCLOBAuth() bool {
	return c.creds != nil
}

// CLOBAuthError returns the CLOB authentication error, if any.
// Returns nil if auth succeeded or was not attempted.
func (c *Client) CLOBAuthError() error {
	return c.credsErr
}

// authUnavailableError returns a descriptive error for methods that require CLOB auth.
func (c *Client) authUnavailableError(method string) error {
	if c.credsErr != nil {
		return fmt.Errorf("%s requires CLOB API credentials: %w", method, c.credsErr)
	}
	return fmt.Errorf("%s requires CLOB API credentials (not initialized)", method)
}

// getPublic makes an unauthenticated GET request.
// Uses the non-proxied client for Gamma API, proxied client for CLOB API.
func (c *Client) getPublic(ctx context.Context, baseURL, path string, params url.Values) (json.RawMessage, error) {
	u := baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	// Route CLOB API through proxy, Gamma API direct (not geo-restricted)
	client := c.publicClient
	if baseURL == clobBaseURL {
		client = c.httpClient
	}
	return c.doRequestWith(client, req)
}

// getAuthenticated makes an authenticated GET request with L2 HMAC signing.
func (c *Client) getAuthenticated(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	u := clobBaseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	if err := c.signRequest(req, ""); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	return c.doRequest(req)
}

// postAuthenticated makes an authenticated POST request with L2 HMAC signing.
func (c *Client) postAuthenticated(ctx context.Context, path string, body any) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	u := clobBaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if err := c.signRequest(req, string(data)); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	return c.doRequest(req)
}

// deleteAuthenticated makes an authenticated DELETE request with L2 HMAC signing.
func (c *Client) deleteAuthenticated(ctx context.Context, path string, body any) (json.RawMessage, error) {
	var bodyReader io.Reader
	var bodyStr string
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
		bodyStr = string(data)
	}

	u := clobBaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	if err := c.signRequest(req, bodyStr); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	return c.doRequest(req)
}

// doRequest executes the HTTP request using the proxied client.
func (c *Client) doRequest(req *http.Request) (json.RawMessage, error) {
	return c.doRequestWith(c.httpClient, req)
}

// doRequestWith executes the HTTP request using the specified client.
// Retries on 502/503/504 gateway errors (up to 3 attempts with backoff).
func (c *Client) doRequestWith(client *http.Client, req *http.Request) (json.RawMessage, error) {
	// Buffer the request body so it can be replayed on retry.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	const maxRetries = 3
	backoff := 2 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			// Reset body for retry.
			if bodyBytes != nil {
				req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504 {
			if attempt < maxRetries {
				continue
			}
		}

		if resp.StatusCode >= 400 {
			return nil, &apiError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("API error %d: %s", resp.StatusCode, string(body)),
			}
		}

		return json.RawMessage(body), nil
	}

	return nil, fmt.Errorf("request failed after %d retries", maxRetries+1)
}
