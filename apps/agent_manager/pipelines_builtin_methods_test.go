package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

type testBuiltinPlacedOrder struct {
	TokenID   string
	Price     float64
	Size      float64
	Side      string
	OrderType string
	NegRisk   bool
}

type testBuiltinPolymarketClient struct {
	events     []polymarket.Event
	markets    []polymarket.Market
	positions  []polymarket.Position
	orders     []polymarket.Order
	orderBooks map[string]*polymarket.OrderBook
	prices     map[string]string
	priceErrs  map[string]error

	listEventsErr    error
	searchMarketsErr error
	getPositionsErr  error
	getOrdersErr     error
	getOrderBookErr  error
	placeOrderErr    error
	cancelOrderErr   error

	listEventsFn    func(limit, offset int) ([]polymarket.Event, error)
	searchMarketsFn func(query string, limit int) ([]polymarket.Market, error)
	listMarketsFn   func(limit, offset int) ([]polymarket.Market, error)
	getMarketFn     func(conditionID string) (*polymarket.Market, error)
	placeOrderResp  *polymarket.PlaceOrderResponse
	placeOrderFn    func(tokenID string, price, size float64, side, orderType string, negRisk bool) (*polymarket.PlaceOrderResponse, error)
	cancelOrderFn   func(orderID string) error

	gotPositionsCalls int
	gotEventLimits    []int
	gotEventOffsets   []int
	gotSearchQueries  []string
	gotSearchLimits   []int
	gotListLimits     []int
	gotListOffsets    []int
	gotMarketIDs      []string
	gotOrdersMarket   string
	placedOrders      []testBuiltinPlacedOrder
	cancelledOrderIDs []string
}

func (c *testBuiltinPolymarketClient) ListEvents(_ context.Context, limit, offset int) ([]polymarket.Event, error) {
	c.gotEventLimits = append(c.gotEventLimits, limit)
	c.gotEventOffsets = append(c.gotEventOffsets, offset)
	if c.listEventsFn != nil {
		return c.listEventsFn(limit, offset)
	}
	if offset >= len(c.events) {
		return []polymarket.Event{}, nil
	}
	end := offset + limit
	if end > len(c.events) {
		end = len(c.events)
	}
	return append([]polymarket.Event(nil), c.events[offset:end]...), c.listEventsErr
}

func (c *testBuiltinPolymarketClient) SearchMarkets(_ context.Context, query string, limit int) ([]polymarket.Market, error) {
	c.gotSearchQueries = append(c.gotSearchQueries, strings.TrimSpace(query))
	c.gotSearchLimits = append(c.gotSearchLimits, limit)
	if c.searchMarketsFn != nil {
		return c.searchMarketsFn(query, limit)
	}
	return c.markets, c.searchMarketsErr
}

func (c *testBuiltinPolymarketClient) ListMarkets(_ context.Context, limit, offset int) ([]polymarket.Market, error) {
	c.gotListLimits = append(c.gotListLimits, limit)
	c.gotListOffsets = append(c.gotListOffsets, offset)
	if c.listMarketsFn != nil {
		return c.listMarketsFn(limit, offset)
	}
	if offset >= len(c.markets) {
		return []polymarket.Market{}, nil
	}
	end := offset + limit
	if end > len(c.markets) {
		end = len(c.markets)
	}
	return append([]polymarket.Market(nil), c.markets[offset:end]...), nil
}

func (c *testBuiltinPolymarketClient) GetMarket(_ context.Context, conditionID string) (*polymarket.Market, error) {
	conditionID = strings.TrimSpace(conditionID)
	c.gotMarketIDs = append(c.gotMarketIDs, conditionID)
	if c.getMarketFn != nil {
		return c.getMarketFn(conditionID)
	}
	for i := range c.markets {
		if strings.EqualFold(strings.TrimSpace(c.markets[i].ConditionID), conditionID) {
			market := c.markets[i]
			return &market, nil
		}
	}
	return nil, fmt.Errorf("missing test market for %s", conditionID)
}

func (c *testBuiltinPolymarketClient) GetPositions(context.Context) ([]polymarket.Position, error) {
	c.gotPositionsCalls++
	return c.positions, c.getPositionsErr
}

func (c *testBuiltinPolymarketClient) GetOrders(_ context.Context, market string) ([]polymarket.Order, error) {
	c.gotOrdersMarket = strings.TrimSpace(market)
	return c.orders, c.getOrdersErr
}

func (c *testBuiltinPolymarketClient) GetPrice(_ context.Context, tokenID, side string) (string, error) {
	key := builtinPolymarketPriceKey(tokenID, side)
	if err := c.priceErrs[key]; err != nil {
		return "", err
	}
	if price, ok := c.prices[key]; ok {
		return price, nil
	}
	return "", fmt.Errorf("missing test price for %s", key)
}

func (c *testBuiltinPolymarketClient) GetOrderBook(_ context.Context, tokenID string) (*polymarket.OrderBook, error) {
	if c.getOrderBookErr != nil {
		return nil, c.getOrderBookErr
	}
	if book, ok := c.orderBooks[strings.TrimSpace(tokenID)]; ok && book != nil {
		return book, nil
	}
	return nil, fmt.Errorf("missing test order book for %s", strings.TrimSpace(tokenID))
}

func (c *testBuiltinPolymarketClient) PlaceOrder(_ context.Context, tokenID string, price, size float64, side, orderType string, negRisk bool) (*polymarket.PlaceOrderResponse, error) {
	c.placedOrders = append(c.placedOrders, testBuiltinPlacedOrder{
		TokenID:   tokenID,
		Price:     price,
		Size:      size,
		Side:      side,
		OrderType: orderType,
		NegRisk:   negRisk,
	})
	if c.placeOrderFn != nil {
		return c.placeOrderFn(tokenID, price, size, side, orderType, negRisk)
	}
	if c.placeOrderErr != nil {
		return nil, c.placeOrderErr
	}
	if c.placeOrderResp != nil {
		return c.placeOrderResp, nil
	}
	return &polymarket.PlaceOrderResponse{
		Success: true,
		OrderID: fmt.Sprintf("ord-%d", len(c.placedOrders)),
	}, nil
}

func (c *testBuiltinPolymarketClient) CancelOrder(_ context.Context, orderID string) error {
	orderID = strings.TrimSpace(orderID)
	c.cancelledOrderIDs = append(c.cancelledOrderIDs, orderID)
	if c.cancelOrderFn != nil {
		return c.cancelOrderFn(orderID)
	}
	if c.cancelOrderErr != nil {
		return c.cancelOrderErr
	}
	filtered := c.orders[:0]
	for _, order := range c.orders {
		if !strings.EqualFold(strings.TrimSpace(order.ID), orderID) {
			filtered = append(filtered, order)
		}
	}
	c.orders = filtered
	return nil
}

func builtinPolymarketPriceKey(tokenID, side string) string {
	return strings.TrimSpace(tokenID) + "|" + strings.ToLower(strings.TrimSpace(side))
}

func installTestBuiltinPolymarketClient(t *testing.T, client *testBuiltinPolymarketClient) {
	t.Helper()
	resetTestBuiltinPolymarketSizingCache(t)
	prevClient := getBuiltinPolymarketClient
	getBuiltinPolymarketClient = func(context.Context, *PipelineEngine, string) (builtinPolymarketClient, string, error) {
		return client, "company-1", nil
	}
	t.Cleanup(func() {
		getBuiltinPolymarketClient = prevClient
	})
}

func resetTestBuiltinPolymarketSizingCache(t *testing.T) {
	t.Helper()
	builtinPolymarketSizingCache.mu.Lock()
	builtinPolymarketSizingCache.entries = make(map[string]builtinPolymarketSizingCacheEntry)
	builtinPolymarketSizingCache.mu.Unlock()
	t.Cleanup(func() {
		builtinPolymarketSizingCache.mu.Lock()
		builtinPolymarketSizingCache.entries = make(map[string]builtinPolymarketSizingCacheEntry)
		builtinPolymarketSizingCache.mu.Unlock()
	})
}

func installTestBuiltinPolymarketUSDCBalance(t *testing.T, balance float64, err error) {
	t.Helper()
	prevBalance := getBuiltinPolymarketUSDCBalance
	getBuiltinPolymarketUSDCBalance = func(context.Context, *PipelineEngine, string) (float64, error) {
		return balance, err
	}
	t.Cleanup(func() {
		getBuiltinPolymarketUSDCBalance = prevBalance
	})
}

func installTestBuiltinPolymarketLiquidUSDBalance(t *testing.T, balance float64, err error) {
	t.Helper()
	prevBalance := getBuiltinPolymarketLiquidUSDBalance
	getBuiltinPolymarketLiquidUSDBalance = func(context.Context, *PipelineEngine, string) (float64, error) {
		return balance, err
	}
	t.Cleanup(func() {
		getBuiltinPolymarketLiquidUSDBalance = prevBalance
	})
}
