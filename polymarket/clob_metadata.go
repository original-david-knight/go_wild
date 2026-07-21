package gowild_polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Sources a resolved venue minimum order size can be read from.
const (
	MinOrderSizeSourceOrderBook = "order_book"
	MinOrderSizeSourceClobMos   = "clob_markets_mos"
)

// ClobToken is one outcome token in a CLOB market's metadata, pairing an outcome
// label with its CLOB token ID.
type ClobToken struct {
	TokenID string  `json:"token_id"`
	Outcome string  `json:"outcome"`
	Price   float64 `json:"price"`
	Winner  bool    `json:"winner"`
}

// ClobMarket is the typed CLOB market metadata returned by
// GET /markets/{condition_id}. It exposes the venue minimum order size (mos),
// tick size, neg-risk flags, and the ordered outcome/token-ID pairs needed for
// deterministic YES/NO token mapping — without callers re-parsing raw JSON.
type ClobMarket struct {
	ConditionID     string `json:"condition_id"`
	QuestionID      string `json:"question_id"`
	Question        string `json:"question"`
	Active          bool   `json:"active"`
	Closed          bool   `json:"closed"`
	AcceptingOrders bool   `json:"accepting_orders"`
	NegRisk         bool   `json:"neg_risk"`
	NegRiskMarketID string `json:"neg_risk_market_id"`

	// MinimumOrderSize is the venue minimum order size as reported under the
	// "minimum_order_size" key; MosField captures the alternate "mos" key. Use
	// MinOrderSize() to read whichever is present.
	MinimumOrderSize float64OrString `json:"minimum_order_size"`
	MosField         float64OrString `json:"mos"`
	MinimumTickSize  float64OrString `json:"minimum_tick_size"`

	Tokens []ClobToken `json:"tokens"`
}

// MinOrderSize returns the venue minimum order size, preferring minimum_order_size
// and falling back to the mos field. Returns 0 when neither is a positive value.
func (m *ClobMarket) MinOrderSize() float64 {
	if m == nil {
		return 0
	}
	if v := float64(m.MinimumOrderSize); v > 0 {
		return v
	}
	if v := float64(m.MosField); v > 0 {
		return v
	}
	return 0
}

// TickSize returns the market minimum tick size, or 0 when absent.
func (m *ClobMarket) TickSize() float64 {
	if m == nil {
		return 0
	}
	return float64(m.MinimumTickSize)
}

// GetClobMarket fetches CLOB market metadata for a condition ID via
// GET /markets/{condition_id} (public, no auth).
func (c *Client) GetClobMarket(ctx context.Context, conditionID string) (*ClobMarket, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, fmt.Errorf("condition ID is required")
	}

	data, err := c.getPublic(ctx, clobBaseURL, "/markets/"+conditionID, nil)
	if err != nil {
		return nil, fmt.Errorf("get clob market failed: %w", err)
	}

	var m ClobMarket
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to decode clob market: %w", err)
	}
	return &m, nil
}

// OrderBookDetail extends OrderBook with the venue metadata fields the /book
// endpoint returns alongside bids and asks: min order size, tick size, neg-risk.
type OrderBookDetail struct {
	Market       string           `json:"market"`
	AssetID      string           `json:"asset_id"`
	Bids         []OrderBookEntry `json:"bids"`
	Asks         []OrderBookEntry `json:"asks"`
	Hash         string           `json:"hash"`
	MinOrderSize float64OrString  `json:"min_order_size"`
	TickSize     float64OrString  `json:"tick_size"`
	NegRisk      bool             `json:"neg_risk"`
}

// Book returns the bid/ask portion of the detailed book as a plain OrderBook.
func (b *OrderBookDetail) Book() *OrderBook {
	if b == nil {
		return nil
	}
	return &OrderBook{
		Market:  b.Market,
		AssetID: b.AssetID,
		Bids:    b.Bids,
		Asks:    b.Asks,
		Hash:    b.Hash,
	}
}

// GetOrderBookDetailed fetches the order book for a token along with the venue
// metadata fields (min order size, tick size, neg-risk) that the /book response
// carries (public, no auth).
func (c *Client) GetOrderBookDetailed(ctx context.Context, tokenID string) (*OrderBookDetail, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)

	data, err := c.getPublic(ctx, clobBaseURL, "/book", params)
	if err != nil {
		return nil, fmt.Errorf("get order book failed: %w", err)
	}

	var book OrderBookDetail
	if err := json.Unmarshal(data, &book); err != nil {
		return nil, fmt.Errorf("failed to decode order book: %w", err)
	}
	return &book, nil
}

// ResolveMinOrderSize returns the venue minimum order size for a market token,
// preferring the order book's min_order_size and falling back to the clob-market
// mos. It short-circuits and does NOT call /markets when the book already
// provides a positive value. A return of (0, "", nil) means the minimum order
// size is undeterminable from venue metadata: callers must fail closed (skip)
// rather than guess a value.
func (c *Client) ResolveMinOrderSize(ctx context.Context, tokenID, conditionID string) (float64, string, error) {
	book, err := c.GetOrderBookDetailed(ctx, tokenID)
	if err != nil {
		return 0, "", err
	}
	if v := float64(book.MinOrderSize); v > 0 {
		return v, MinOrderSizeSourceOrderBook, nil
	}

	if strings.TrimSpace(conditionID) == "" {
		return 0, "", nil
	}
	market, err := c.GetClobMarket(ctx, conditionID)
	if err != nil {
		return 0, "", err
	}
	if v := market.MinOrderSize(); v > 0 {
		return v, MinOrderSizeSourceClobMos, nil
	}
	return 0, "", nil
}
