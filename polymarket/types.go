package gowild_polymarket

import (
	"encoding/json"
	"math/big"
)

// Market represents a Polymarket prediction market from the Gamma API.
// Note: the Gamma API uses camelCase for most fields.
type Market struct {
	ID               string  `json:"id"`
	Question         string  `json:"question"`
	Description      string  `json:"description"`
	ConditionID      string  `json:"conditionId"`
	CreatedAt        string  `json:"createdAt"`
	CreationDate     string  `json:"creationDate"`
	StartDate        string  `json:"startDate"`
	StartDateISO     string  `json:"startDateIso"`
	Slug             string  `json:"slug"`
	Image            string  `json:"image"`
	Icon             string  `json:"icon"`
	Active           bool    `json:"active"`
	Closed           bool    `json:"closed"`
	EndDate          string  `json:"endDateIso"`
	Volume           string  `json:"volume"`
	Liquidity        string  `json:"liquidity"`
	OutcomePrices    string  `json:"outcomePrices"`
	Outcomes         string  `json:"outcomes"`
	ClobTokenIDs     string  `json:"clobTokenIds"`
	AcceptingOrders  bool    `json:"acceptingOrders"`
	NegRisk          bool    `json:"negRisk"`
	NegRiskMarketID  string  `json:"negRiskMarketID"`
	NegRiskRequestID string  `json:"negRiskRequestID"`
	BestBid          float64 `json:"bestBid"`
	BestAsk          float64 `json:"bestAsk"`
	Volume24hr       float64 `json:"volume24hr"`
	Tags             []Tag   `json:"tags,omitempty"`
}

// Tag represents a market or event tag from the Gamma API.
type Tag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Slug  string `json:"slug"`
}

// PriceHistoryPoint represents a timestamped market price sample from /prices-history.
type PriceHistoryPoint struct {
	Timestamp int64   `json:"t"`
	Price     float64 `json:"p"`
}

// CandleBar represents an OHLC candle built from price samples.
// Volume is not available from /prices-history; SampleCount indicates sample density.
type CandleBar struct {
	StartTS     int64   `json:"start_ts"`
	EndTS       int64   `json:"end_ts"`
	Open        float64 `json:"open"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Close       float64 `json:"close"`
	SampleCount int     `json:"sample_count"`
}

// OrderBookEntry represents a single price/size entry in the order book.
type OrderBookEntry struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// OrderBook represents the order book for a token.
type OrderBook struct {
	Market  string           `json:"market"`
	AssetID string           `json:"asset_id"`
	Bids    []OrderBookEntry `json:"bids"`
	Asks    []OrderBookEntry `json:"asks"`
	Hash    string           `json:"hash"`
}

// Order represents an order on the CLOB.
type Order struct {
	ID              string          `json:"id"`
	Market          string          `json:"market"`
	AssetID         string          `json:"asset_id"`
	Side            string          `json:"side"` // "BUY" or "SELL"
	OriginalSize    string          `json:"original_size"`
	SizeMatched     string          `json:"size_matched"`
	Price           string          `json:"price"`
	Status          string          `json:"status"`
	Owner           string          `json:"owner"`
	Outcome         string          `json:"outcome"`
	MakerAddress    string          `json:"maker_address"`
	Type            string          `json:"order_type"` // "GTC", "FOK", "GTD"
	CreatedAt       json.Number     `json:"created_at"`
	Expiration      string          `json:"expiration,omitempty"`
	AssociateTrades json.RawMessage `json:"associate_trades,omitempty"`
}

// signedOrder is the EIP-712 signed order sent to the CLOB.
// Side is "BUY" or "SELL" in the API payload. SignatureType: 0 = EOA, 1 = POLY_PROXY, 2 = POLY_GNOSIS_SAFE.
type signedOrder struct {
	Salt          *big.Int `json:"salt"`
	Maker         string   `json:"maker"`
	Signer        string   `json:"signer"`
	Taker         string   `json:"taker,omitempty"`
	TokenID       string   `json:"tokenId"`
	MakerAmount   string   `json:"makerAmount"`
	TakerAmount   string   `json:"takerAmount"`
	Expiration    string   `json:"expiration"`
	Nonce         string   `json:"nonce,omitempty"`
	FeeRateBps    string   `json:"feeRateBps,omitempty"`
	Side          string   `json:"side"`          // "BUY" or "SELL"
	SignatureType int      `json:"signatureType"` // 0 = EOA, 1 = POLY_PROXY, 2 = POLY_GNOSIS_SAFE
	Timestamp     string   `json:"timestamp"`
	Metadata      string   `json:"metadata"`
	Builder       string   `json:"builder"`
	Signature     string   `json:"signature"`
}

// placeOrderRequest is the request body for placing an order.
type placeOrderRequest struct {
	Order     *signedOrder `json:"order"`
	OrderType string       `json:"orderType"` // "GTC", "FOK", "GTD"
	Owner     string       `json:"owner"`
}

// PlaceOrderResponse is the response from placing an order.
type PlaceOrderResponse struct {
	Success  bool   `json:"success"`
	OrderID  string `json:"orderID"`
	ErrorMsg string `json:"errorMsg,omitempty"`
}

// Position represents a user's position from the Data API.
// Note: the Data API returns numeric types for size/price fields.
type Position struct {
	Asset        string  `json:"asset"`
	ProxyWallet  string  `json:"proxyWallet"`
	ConditionID  string  `json:"conditionId"`
	Title        string  `json:"title"`
	Slug         string  `json:"slug"`
	Outcome      string  `json:"outcome"`
	OutcomeIndex int     `json:"outcomeIndex"`
	Size         float64 `json:"size"`
	AvgPrice     float64 `json:"avgPrice"`
	CurPrice     float64 `json:"curPrice"`
	CurrPrice    float64 `json:"currPrice"`
	InitialValue float64 `json:"initialValue"`
	CurrentValue float64 `json:"currentValue"`
	CashPnl      float64 `json:"cashPnl"`
	PercentPnl   float64 `json:"percentPnl"`
	RealizedPnl  float64 `json:"realizedPnl"`
	TotalBought  float64 `json:"totalBought"`
	Redeemable   bool    `json:"redeemable"`
	NegativeRisk bool    `json:"negativeRisk"`
	EndDate      string  `json:"endDate"`
	EventSlug    string  `json:"eventSlug"`
}

// Trade represents a completed trade from the Data API.
type Trade struct {
	Asset           string  `json:"asset"`
	ConditionID     string  `json:"conditionId"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Side            string  `json:"side"`
	Outcome         string  `json:"outcome"`
	OutcomeIndex    int     `json:"outcomeIndex"`
	Size            float64 `json:"size"`
	Price           float64 `json:"price"`
	Timestamp       int64   `json:"timestamp"`
	TransactionHash string  `json:"transactionHash"`
	EventSlug       string  `json:"eventSlug"`
}

// apiCredentials holds the CLOB API key credentials derived from L1 signing.
type apiCredentials struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// apiError is a structured error from the Polymarket API.
type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return e.Message
}
