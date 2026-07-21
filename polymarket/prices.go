package gowild_polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type float64OrString float64

func (v *float64OrString) UnmarshalJSON(data []byte) error {
	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*v = float64OrString(number)
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*v = float64OrString(number)
	return nil
}

// GetPrice returns the best price for a token on a given side (public, no auth).
// side must be "buy" or "sell".
func (c *Client) GetPrice(ctx context.Context, tokenID, side string) (string, error) {
	if price, ok := c.cachedPrice(tokenID, side); ok {
		return price, nil
	}

	params := url.Values{}
	params.Set("token_id", tokenID)
	params.Set("side", side)

	data, err := c.getPublic(ctx, clobBaseURL, "/price", params)
	if err != nil {
		return "", fmt.Errorf("get price failed: %w", err)
	}

	var result struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("failed to decode price: %w", err)
	}
	c.storeCachedPrice(tokenID, side, result.Price)
	return result.Price, nil
}

// GetPriceHistory returns timestamped historical prices for a token from the CLOB API (public, no auth).
// tokenID maps to the CLOB API "market" query parameter.
// interval is mutually exclusive with explicit startTs/endTs.
func (c *Client) GetPriceHistory(ctx context.Context, tokenID, interval string, startTs, endTs int64, fidelity int) ([]PriceHistoryPoint, error) {
	if tokenID == "" {
		return nil, fmt.Errorf("token ID is required")
	}
	if interval != "" && (startTs > 0 || endTs > 0) {
		return nil, fmt.Errorf("interval cannot be used with startTs/endTs")
	}

	params := url.Values{}
	params.Set("market", tokenID)

	if interval == "" && startTs <= 0 && endTs <= 0 {
		interval = "1d"
	}

	if interval != "" {
		switch interval {
		case "1m", "1h", "6h", "1d", "1w", "max":
			params.Set("interval", interval)
		default:
			return nil, fmt.Errorf("invalid interval: %s", interval)
		}
	}
	if startTs > 0 {
		params.Set("startTs", strconv.FormatInt(startTs, 10))
	}
	if endTs > 0 {
		params.Set("endTs", strconv.FormatInt(endTs, 10))
	}
	if fidelity > 0 {
		params.Set("fidelity", strconv.Itoa(fidelity))
	}

	data, err := c.getPublic(ctx, clobBaseURL, "/prices-history", params)
	if err != nil {
		return nil, fmt.Errorf("get price history failed: %w", err)
	}

	var result struct {
		History []PriceHistoryPoint `json:"history"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to decode price history: %w", err)
	}
	return result.History, nil
}

// BuildCandles aggregates price samples into fixed-size OHLC candles.
// candleMinutes must be > 0.
func BuildCandles(points []PriceHistoryPoint, candleMinutes int) ([]CandleBar, error) {
	if candleMinutes <= 0 {
		return nil, fmt.Errorf("candleMinutes must be > 0")
	}
	if len(points) == 0 {
		return []CandleBar{}, nil
	}

	bucketSeconds := int64(candleMinutes * 60)
	sorted := make([]PriceHistoryPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	candles := make([]CandleBar, 0, len(sorted))
	var current *CandleBar
	var currentBucketStart int64

	for _, point := range sorted {
		bucketStart := (point.Timestamp / bucketSeconds) * bucketSeconds
		bucketEnd := bucketStart + bucketSeconds

		if current == nil || bucketStart != currentBucketStart {
			candle := CandleBar{
				StartTS:     bucketStart,
				EndTS:       bucketEnd,
				Open:        point.Price,
				High:        point.Price,
				Low:         point.Price,
				Close:       point.Price,
				SampleCount: 1,
			}
			candles = append(candles, candle)
			current = &candles[len(candles)-1]
			currentBucketStart = bucketStart
			continue
		}

		if point.Price > current.High {
			current.High = point.Price
		}
		if point.Price < current.Low {
			current.Low = point.Price
		}
		current.Close = point.Price
		current.SampleCount++
	}

	return candles, nil
}

// GetOrderBook returns the order book for a token (public, no auth).
func (c *Client) GetOrderBook(ctx context.Context, tokenID string) (*OrderBook, error) {
	params := url.Values{}
	params.Set("token_id", tokenID)

	data, err := c.getPublic(ctx, clobBaseURL, "/book", params)
	if err != nil {
		return nil, fmt.Errorf("get order book failed: %w", err)
	}

	var book OrderBook
	if err := json.Unmarshal(data, &book); err != nil {
		return nil, fmt.Errorf("failed to decode order book: %w", err)
	}
	return &book, nil
}

// getNegRisk queries the CLOB API to determine if a token uses the NegRisk exchange.
// This is the authoritative source for negRisk — the Gamma API's neg_risk field may disagree.
func (c *Client) getNegRisk(ctx context.Context, tokenID string) (bool, error) {
	if c.getNegRiskFn != nil {
		return c.getNegRiskFn(ctx, tokenID)
	}
	if cached, ok := c.cachedTokenMetadata(tokenID); ok && cached.hasNegRisk {
		return cached.negRisk, nil
	}

	params := url.Values{}
	params.Set("token_id", tokenID)

	data, err := c.getPublic(ctx, clobBaseURL, "/neg-risk", params)
	if err != nil {
		return false, fmt.Errorf("get neg-risk failed: %w", err)
	}

	var result struct {
		NegRisk bool `json:"neg_risk"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("failed to decode neg-risk: %w", err)
	}
	c.storeCachedTokenMetadata(tokenID, func(entry cachedTokenMetadata) cachedTokenMetadata {
		entry.negRisk = result.NegRisk
		entry.hasNegRisk = true
		return entry
	})
	return result.NegRisk, nil
}

// getTickSize returns the minimum tick size for a token (public, no auth).
// The tick size determines price and amount precision for orders.
func (c *Client) getTickSize(ctx context.Context, tokenID string) (float64, error) {
	if c.getTickSizeFn != nil {
		return c.getTickSizeFn(ctx, tokenID)
	}
	if cached, ok := c.cachedTokenMetadata(tokenID); ok && cached.hasTickSize {
		return cached.tickSize, nil
	}

	params := url.Values{}
	params.Set("token_id", tokenID)

	data, err := c.getPublic(ctx, clobBaseURL, "/tick-size", params)
	if err != nil {
		return 0, fmt.Errorf("get tick size failed: %w", err)
	}

	var result struct {
		TickSize float64OrString `json:"minimum_tick_size"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, fmt.Errorf("failed to decode tick size: %w", err)
	}
	if result.TickSize <= 0 {
		return 0, fmt.Errorf("invalid minimum_tick_size %q", string(data))
	}
	tickSize := float64(result.TickSize)
	c.storeCachedTokenMetadata(tokenID, func(entry cachedTokenMetadata) cachedTokenMetadata {
		entry.tickSize = tickSize
		entry.hasTickSize = true
		return entry
	})
	return tickSize, nil
}

func normalizeTokenCacheKey(tokenID string) string {
	return strings.TrimSpace(tokenID)
}

func normalizePriceCacheKey(tokenID, side string) string {
	return normalizeTokenCacheKey(tokenID) + "|" + strings.ToLower(strings.TrimSpace(side))
}

func (c *Client) cachedPrice(tokenID, side string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.ensureCaches()
	key := normalizePriceCacheKey(tokenID, side)
	if key == "|" {
		return "", false
	}

	now := time.Now().UTC()
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	entry, ok := c.priceCache[key]
	if !ok {
		return "", false
	}
	if now.After(entry.expiresAt) {
		delete(c.priceCache, key)
		return "", false
	}
	return entry.price, true
}

func (c *Client) storeCachedPrice(tokenID, side, price string) {
	if c == nil {
		return
	}
	c.ensureCaches()
	key := normalizePriceCacheKey(tokenID, side)
	if key == "|" || strings.TrimSpace(price) == "" {
		return
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.priceCache[key] = cachedPriceQuote{
		price:     strings.TrimSpace(price),
		expiresAt: time.Now().UTC().Add(priceCacheTTL),
	}
}

func (c *Client) cachedTokenMetadata(tokenID string) (cachedTokenMetadata, bool) {
	if c == nil {
		return cachedTokenMetadata{}, false
	}
	c.ensureCaches()
	key := normalizeTokenCacheKey(tokenID)
	if key == "" {
		return cachedTokenMetadata{}, false
	}

	now := time.Now().UTC()
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	entry, ok := c.tokenMetadataCache[key]
	if !ok {
		return cachedTokenMetadata{}, false
	}
	if now.After(entry.expiresAt) {
		delete(c.tokenMetadataCache, key)
		return cachedTokenMetadata{}, false
	}
	return entry, true
}

func (c *Client) storeCachedTokenMetadata(tokenID string, update func(cachedTokenMetadata) cachedTokenMetadata) {
	if c == nil || update == nil {
		return
	}
	c.ensureCaches()
	key := normalizeTokenCacheKey(tokenID)
	if key == "" {
		return
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	entry := c.tokenMetadataCache[key]
	entry = update(entry)
	entry.expiresAt = time.Now().UTC().Add(tokenMetadataCacheTTL)
	c.tokenMetadataCache[key] = entry
}
