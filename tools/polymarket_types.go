package tools

// PolymarketSearchMarketsInput defines the input for searching Polymarket prediction markets.
type PolymarketSearchMarketsInput struct {
	Query string `json:"query" description:"Search query for prediction markets (e.g., 'election', 'bitcoin', 'fed rate')" required:"true"`
	Limit int    `json:"limit,omitempty" description:"Maximum number of results to return (default 10, max 100)"`
}

// PolymarketGetMarketInput defines the input for getting a specific market by condition ID.
type PolymarketGetMarketInput struct {
	ConditionID string `json:"condition_id" description:"The condition ID (hex string starting with 0x) that identifies a market. This is NOT the same as a token_id — condition_id identifies the market itself, while token_id identifies a specific outcome token within that market. Use the condition_id from search results." required:"true"`
}

// PolymarketGetPricesInput defines the input for getting the current price of a token.
type PolymarketGetPricesInput struct {
	TokenID string `json:"token_id" description:"The CLOB token ID to get prices for" required:"true"`
	Side    string `json:"side" description:"Order side to get the best price for" required:"true" enum:"buy,sell"`
}

// PolymarketGetPriceHistoryInput defines the input for getting historical prices of a token.
type PolymarketGetPriceHistoryInput struct {
	TokenID  string `json:"token_id" description:"The CLOB token ID to get historical prices for" required:"true"`
	Interval string `json:"interval,omitempty" description:"Relative window ending now (default: 1d). Mutually exclusive with start_ts/end_ts." enum:"1m,1h,6h,1d,1w,max"`
	StartTS  int64  `json:"start_ts,omitempty" description:"Unix start timestamp in seconds (UTC). Use with end_ts, mutually exclusive with interval."`
	EndTS    int64  `json:"end_ts,omitempty" description:"Unix end timestamp in seconds (UTC). Use with start_ts, mutually exclusive with interval."`
	Fidelity int    `json:"fidelity,omitempty" description:"Resolution in minutes between points (for example: 1, 5, 15, 60)."`
}

// PolymarketGetCandlesInput defines the input for getting aggregated OHLC candles.
type PolymarketGetCandlesInput struct {
	TokenID       string `json:"token_id" description:"The CLOB token ID to build candles for" required:"true"`
	CandleMinutes int    `json:"candle_minutes,omitempty" description:"Candle size in minutes (default 60)."`
	Interval      string `json:"interval,omitempty" description:"Relative window ending now (default: 1d). Mutually exclusive with start_ts/end_ts." enum:"1m,1h,6h,1d,1w,max"`
	StartTS       int64  `json:"start_ts,omitempty" description:"Unix start timestamp in seconds (UTC). Use with end_ts, mutually exclusive with interval."`
	EndTS         int64  `json:"end_ts,omitempty" description:"Unix end timestamp in seconds (UTC). Use with start_ts, mutually exclusive with interval."`
	Fidelity      int    `json:"fidelity,omitempty" description:"Resolution in minutes between samples fetched from Polymarket."`
}

// PolymarketGetOrderbookInput defines the input for getting the order book of a token.
type PolymarketGetOrderbookInput struct {
	TokenID string `json:"token_id" description:"The CLOB token ID to get the order book for" required:"true"`
}

// PolymarketOrderBookDepthInput defines the input for getting L2 depth of a token order book.
type PolymarketOrderBookDepthInput struct {
	TokenID string `json:"token_id" description:"The CLOB token ID to get order book depth for" required:"true"`
	Levels  int    `json:"levels,omitempty" description:"Number of price levels per side to return (default 10, max 100)"`
}

// PolymarketGetPositionsInput defines the input for getting the user's positions.
type PolymarketGetPositionsInput struct{}

// PolymarketPlaceOrderInput defines the input for placing an order on Polymarket.
type PolymarketPlaceOrderInput struct {
	TokenID   string  `json:"token_id" description:"The CLOB token ID to trade" required:"true"`
	Price     float64 `json:"price" description:"Price per share between 0 and 1 (e.g., 0.55 for 55 cents)" required:"true"`
	Size      float64 `json:"size" description:"Number of shares to buy or sell" required:"true"`
	Side      string  `json:"side" description:"Order side" required:"true" enum:"BUY,SELL"`
	OrderType string  `json:"order_type,omitempty" description:"Order time-in-force type (default GTC)" enum:"GTC,FOK,GTD"`
	NegRisk   bool    `json:"neg_risk,omitempty" description:"Whether this market uses the NegRisk exchange (check market data)"`
}

// PolymarketPlaceBuyOrderInput defines input for a BUY order.
// Side is fixed to BUY by the tool implementation.
type PolymarketPlaceBuyOrderInput struct {
	TokenID   string  `json:"token_id" description:"The CLOB token ID to buy" required:"true"`
	Price     float64 `json:"price" description:"Price per share between 0 and 1 (e.g., 0.55 for 55 cents)" required:"true"`
	Size      float64 `json:"size" description:"Number of shares to buy" required:"true"`
	OrderType string  `json:"order_type,omitempty" description:"Order time-in-force type (default GTC)" enum:"GTC,FOK,GTD"`
	NegRisk   bool    `json:"neg_risk,omitempty" description:"Whether this market uses the NegRisk exchange (check market data)"`
}

// PolymarketPlaceSellOrderInput defines input for a SELL order.
// Side is fixed to SELL by the tool implementation.
type PolymarketPlaceSellOrderInput struct {
	TokenID   string  `json:"token_id" description:"The CLOB token ID to sell" required:"true"`
	Price     float64 `json:"price" description:"Price per share between 0 and 1 (e.g., 0.55 for 55 cents)" required:"true"`
	Size      float64 `json:"size" description:"Number of shares to sell" required:"true"`
	OrderType string  `json:"order_type,omitempty" description:"Order time-in-force type (default GTC)" enum:"GTC,FOK,GTD"`
	NegRisk   bool    `json:"neg_risk,omitempty" description:"Whether this market uses the NegRisk exchange (check market data)"`
}

// PolymarketCancelOrderInput defines the input for cancelling an order.
type PolymarketCancelOrderInput struct {
	OrderID string `json:"order_id" description:"The ID of the order to cancel" required:"true"`
}

// PolymarketGetOrdersInput defines the input for getting the user's orders.
type PolymarketGetOrdersInput struct {
	Market string `json:"market,omitempty" description:"Filter orders by market condition ID. Leave empty for all orders."`
}

// PolymarketGetTradesInput defines the input for getting the user's trade history.
type PolymarketGetTradesInput struct {
	Limit int `json:"limit,omitempty" description:"Maximum number of trades to return (default 20)"`
}

// PolymarketRedeemWinningsInput controls which resolved positions to redeem.
type PolymarketRedeemWinningsInput struct {
	IncludeLosing bool `json:"include_losing,omitempty" description:"If true, also redeem losing positions (zero payout) to burn the dead tokens and clean up the portfolio. Costs gas but returns no USDC."`
}

// PolymarketAddMarketNoteInput defines the input for adding a note to a market.
type PolymarketAddMarketNoteInput struct {
	ConditionID string `json:"condition_id" description:"The condition ID of the market to add a note to" required:"true"`
	Content     string `json:"content" description:"The note content (max 2000 characters)" required:"true"`
}

// PolymarketListMarketNotesInput defines the input for listing notes on a market.
type PolymarketListMarketNotesInput struct {
	ConditionID string `json:"condition_id" description:"The condition ID of the market to list notes for" required:"true"`
	Limit       int    `json:"limit,omitempty" description:"Maximum number of notes to return (default 50)"`
}
