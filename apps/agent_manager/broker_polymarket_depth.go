package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

const (
	defaultOrderBookDepthLevels = 10
	maxOrderBookDepthLevels     = 100
)

type orderBookDepthLevel struct {
	Level                 int     `json:"level"`
	Price                 float64 `json:"price"`
	PriceCents            float64 `json:"price_cents"`
	Size                  float64 `json:"size"`
	NotionalUSD           float64 `json:"notional_usd"`
	CumulativeSize        float64 `json:"cumulative_size"`
	CumulativeNotionalUSD float64 `json:"cumulative_notional_usd"`
}

type orderBookDepthTopOfBook struct {
	BestBid      float64 `json:"best_bid,omitempty"`
	BestBidCents float64 `json:"best_bid_cents,omitempty"`
	BestAsk      float64 `json:"best_ask,omitempty"`
	BestAskCents float64 `json:"best_ask_cents,omitempty"`
	Spread       float64 `json:"spread,omitempty"`
	SpreadCents  float64 `json:"spread_cents,omitempty"`
}

type orderBookDepthCounts struct {
	Bids int `json:"bids"`
	Asks int `json:"asks"`
}

type orderBookDepthResponse struct {
	TokenID                  string                  `json:"token_id"`
	Market                   string                  `json:"market,omitempty"`
	AssetID                  string                  `json:"asset_id,omitempty"`
	BookHash                 string                  `json:"book_hash,omitempty"`
	LevelsRequested          int                     `json:"levels_requested"`
	LevelsUsed               int                     `json:"levels_used"`
	LevelsReturned           orderBookDepthCounts    `json:"levels_returned"`
	TopOfBook                orderBookDepthTopOfBook `json:"top_of_book"`
	Bids                     []orderBookDepthLevel   `json:"bids"`
	Asks                     []orderBookDepthLevel   `json:"asks"`
	BidDepthTotalShares      float64                 `json:"bid_depth_total_shares"`
	BidDepthTotalNotionalUSD float64                 `json:"bid_depth_total_notional_usd"`
	AskDepthTotalShares      float64                 `json:"ask_depth_total_shares"`
	AskDepthTotalNotionalUSD float64                 `json:"ask_depth_total_notional_usd"`
}

type parsedOrderBookLevel struct {
	price float64
	size  float64
}

func normalizeOrderBookDepthLevels(requested int) int {
	if requested <= 0 {
		return defaultOrderBookDepthLevels
	}
	if requested > maxOrderBookDepthLevels {
		return maxOrderBookDepthLevels
	}
	return requested
}

func buildOrderBookDepth(book *polymarket.OrderBook, tokenID string, requestedLevels int) (*orderBookDepthResponse, error) {
	if book == nil {
		return nil, fmt.Errorf("order book is nil")
	}

	bids, err := parseOrderBookSide(book.Bids, "bid")
	if err != nil {
		return nil, err
	}
	asks, err := parseOrderBookSide(book.Asks, "ask")
	if err != nil {
		return nil, err
	}

	sort.Slice(bids, func(i, j int) bool {
		if bids[i].price == bids[j].price {
			return bids[i].size > bids[j].size
		}
		return bids[i].price > bids[j].price
	})
	sort.Slice(asks, func(i, j int) bool {
		if asks[i].price == asks[j].price {
			return asks[i].size > asks[j].size
		}
		return asks[i].price < asks[j].price
	})

	levelsUsed := normalizeOrderBookDepthLevels(requestedLevels)
	if len(bids) > levelsUsed {
		bids = bids[:levelsUsed]
	}
	if len(asks) > levelsUsed {
		asks = asks[:levelsUsed]
	}

	bidLevels, bidShares, bidNotional := buildDepthLevels(bids)
	askLevels, askShares, askNotional := buildDepthLevels(asks)

	response := &orderBookDepthResponse{
		TokenID:                  tokenID,
		Market:                   book.Market,
		AssetID:                  book.AssetID,
		BookHash:                 book.Hash,
		LevelsRequested:          requestedLevels,
		LevelsUsed:               levelsUsed,
		LevelsReturned:           orderBookDepthCounts{Bids: len(bidLevels), Asks: len(askLevels)},
		Bids:                     bidLevels,
		Asks:                     askLevels,
		BidDepthTotalShares:      roundFloat(bidShares),
		BidDepthTotalNotionalUSD: roundFloat(bidNotional),
		AskDepthTotalShares:      roundFloat(askShares),
		AskDepthTotalNotionalUSD: roundFloat(askNotional),
	}

	if len(bids) > 0 {
		response.TopOfBook.BestBid = roundFloat(bids[0].price)
		response.TopOfBook.BestBidCents = roundFloat(bids[0].price * 100)
	}
	if len(asks) > 0 {
		response.TopOfBook.BestAsk = roundFloat(asks[0].price)
		response.TopOfBook.BestAskCents = roundFloat(asks[0].price * 100)
	}
	if len(bids) > 0 && len(asks) > 0 {
		spread := asks[0].price - bids[0].price
		response.TopOfBook.Spread = roundFloat(spread)
		response.TopOfBook.SpreadCents = roundFloat(spread * 100)
	}

	return response, nil
}

func parseOrderBookSide(entries []polymarket.OrderBookEntry, side string) ([]parsedOrderBookLevel, error) {
	parsed := make([]parsedOrderBookLevel, 0, len(entries))
	for i, level := range entries {
		price, err := strconv.ParseFloat(strings.TrimSpace(level.Price), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid %s price at level %d: %w", side, i+1, err)
		}
		size, err := strconv.ParseFloat(strings.TrimSpace(level.Size), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid %s size at level %d: %w", side, i+1, err)
		}
		if size <= 0 {
			continue
		}
		if price < 0 || price > 1 {
			return nil, fmt.Errorf("invalid %s price at level %d: expected value between 0 and 1, got %f", side, i+1, price)
		}

		parsed = append(parsed, parsedOrderBookLevel{price: price, size: size})
	}
	return parsed, nil
}

func buildDepthLevels(levels []parsedOrderBookLevel) ([]orderBookDepthLevel, float64, float64) {
	result := make([]orderBookDepthLevel, 0, len(levels))
	var cumulativeSize float64
	var cumulativeNotional float64

	for i, level := range levels {
		notional := level.price * level.size
		cumulativeSize += level.size
		cumulativeNotional += notional

		result = append(result, orderBookDepthLevel{
			Level:                 i + 1,
			Price:                 roundFloat(level.price),
			PriceCents:            roundFloat(level.price * 100),
			Size:                  roundFloat(level.size),
			NotionalUSD:           roundFloat(notional),
			CumulativeSize:        roundFloat(cumulativeSize),
			CumulativeNotionalUSD: roundFloat(cumulativeNotional),
		})
	}

	return result, cumulativeSize, cumulativeNotional
}

func roundFloat(value float64) float64 {
	return math.Round(value*1e8) / 1e8
}
