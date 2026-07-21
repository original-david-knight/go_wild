package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
	"github.com/original-david-knight/go_wild/tools"
)

func (h *BrokerPolymarketHandler) handleSearchMarkets(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketSearchMarketsInput) (map[string]any, error) {
		markets, err := c.SearchMarkets(r.Context(), input.Query, input.Limit)
		if err != nil {
			return nil, err
		}
		result := map[string]any{"markets": markets}
		if h.currentExecutionDisablesPolymarketNoteAugmentation(r.Context()) {
			return result, nil
		}

		// Augment with note counts per market.
		agentID := BrokerAgentID(r.Context())
		member, memberErr := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
		if memberErr == nil && member != nil && strings.TrimSpace(member.CompanyID) != "" {
			companyID := strings.TrimSpace(member.CompanyID)
			noteCounts := map[string]int{}
			conditionIDs := make([]string, 0, len(markets))
			for _, market := range markets {
				conditionIDs = append(conditionIDs, strings.TrimSpace(market.ConditionID))
			}
			summaries, err := data.GetMarketNoteSummaries(r.Context(), h.service.db, companyID, conditionIDs)
			if err == nil {
				for conditionID, summary := range summaries {
					if summary.Count > 0 {
						noteCounts[conditionID] = summary.Count
					}
				}
			}
			if len(noteCounts) > 0 {
				result["note_counts"] = noteCounts
			}
		}

		return result, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetMarket(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetMarketInput) (map[string]any, error) {
		market, err := c.GetMarket(r.Context(), input.ConditionID)
		if err != nil {
			return nil, err
		}
		result := map[string]any{"market": market}
		if h.currentExecutionDisablesPolymarketNoteAugmentation(r.Context()) {
			return result, nil
		}

		// Augment with notes for this market.
		agentID := BrokerAgentID(r.Context())
		member, memberErr := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
		if memberErr == nil && member != nil && strings.TrimSpace(member.CompanyID) != "" {
			companyID := strings.TrimSpace(member.CompanyID)
			notes, notesErr := data.ListMarketNotes(r.Context(), h.service.db, companyID, input.ConditionID, 6)
			if notesErr == nil && len(notes) > 0 {
				out := make([]map[string]any, len(notes))
				for i, n := range notes {
					out[i] = marketNoteToMap(n)
				}
				totalCount, countErr := data.CountMarketNotes(r.Context(), h.service.db, companyID, input.ConditionID)
				if countErr == nil && totalCount > len(notes) {
					out = append([]map[string]any{{
						"_truncated": fmt.Sprintf("Showing last %d of %d notes. Use list_market_notes to fetch more.", len(notes), totalCount),
					}}, out...)
				}
				result["notes"] = out
			}
		}

		return result, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetPrices(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetPricesInput) (map[string]any, error) {
		price, err := c.GetPrice(r.Context(), input.TokenID, input.Side)
		if err != nil {
			return nil, err
		}
		return map[string]any{"price": price}, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetPriceHistory(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetPriceHistoryInput) (map[string]any, error) {
		history, err := c.GetPriceHistory(r.Context(), input.TokenID, input.Interval, input.StartTS, input.EndTS, input.Fidelity)
		if err != nil {
			return nil, err
		}
		return map[string]any{"history": history}, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetCandles(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetCandlesInput) (map[string]any, error) {
		candleMinutes := input.CandleMinutes
		if candleMinutes <= 0 {
			candleMinutes = 60
		}

		history, err := c.GetPriceHistory(r.Context(), input.TokenID, input.Interval, input.StartTS, input.EndTS, input.Fidelity)
		if err != nil {
			return nil, err
		}
		candles, err := polymarket.BuildCandles(history, candleMinutes)
		if err != nil {
			return nil, err
		}

		return map[string]any{
			"candles":            candles,
			"candle_minutes":     candleMinutes,
			"volume_available":   false,
			"volume_note":        "Volume is not provided by Polymarket /prices-history endpoint.",
			"price_sample_count": len(history),
		}, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetOrderbook(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetOrderbookInput) (map[string]any, error) {
		book, err := h.getOrderBook(r.Context(), c, input.TokenID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"orderbook": book}, nil
	})
}

func (h *BrokerPolymarketHandler) handleOrderBookDepth(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketOrderBookDepthInput) (map[string]any, error) {
		book, err := h.getOrderBook(r.Context(), c, input.TokenID)
		if err != nil {
			return nil, err
		}

		depth, err := buildOrderBookDepth(book, input.TokenID, input.Levels)
		if err != nil {
			return nil, err
		}
		return map[string]any{"depth": depth}, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetPositions(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetPositionsInput) (map[string]any, error) {
		positions, err := c.GetPositions(r.Context())
		if err != nil {
			return nil, err
		}
		result := map[string]any{"positions": positions}
		if h.currentExecutionDisablesPolymarketNoteAugmentation(r.Context()) {
			return result, nil
		}

		// Augment with notes for each position's market.
		agentID := BrokerAgentID(r.Context())
		member, memberErr := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
		if memberErr == nil && member != nil && strings.TrimSpace(member.CompanyID) != "" {
			companyID := strings.TrimSpace(member.CompanyID)
			conditionIDs := make([]string, 0, len(positions))
			for _, p := range positions {
				conditionIDs = append(conditionIDs, strings.TrimSpace(p.ConditionID))
			}
			if notesByCondition, notesErr := data.ListMarketNotesByMarkets(r.Context(), h.service.db, companyID, conditionIDs, 6); notesErr == nil {
				totalCounts := noteCountsForConditions(r.Context(), h.service.db, companyID, conditionIDs)
				allNotes := marketNotesByConditionToMaps(notesByCondition, totalCounts)
				if len(allNotes) > 0 {
					result["notes_by_market"] = allNotes
				}
			}
		}

		return result, nil
	})
}

func (h *BrokerPolymarketHandler) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketPlaceOrderInput) (map[string]any, error) {
		resp, err := c.PlaceOrder(r.Context(), input.TokenID, input.Price, input.Size, input.Side, input.OrderType, input.NegRisk)
		if err != nil {
			return nil, err
		}
		data, _ := json.Marshal(resp)
		var result map[string]any
		json.Unmarshal(data, &result)
		return result, nil
	})
}

func (h *BrokerPolymarketHandler) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketCancelOrderInput) (map[string]any, error) {
		err := c.CancelOrder(r.Context(), input.OrderID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "cancelled", "order_id": input.OrderID}, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetOrdersInput) (map[string]any, error) {
		orders, err := c.GetOrders(r.Context(), input.Market)
		if err != nil {
			return nil, err
		}
		result := map[string]any{"orders": orders}
		if h.currentExecutionDisablesPolymarketNoteAugmentation(r.Context()) {
			return result, nil
		}

		agentID := BrokerAgentID(r.Context())
		member, memberErr := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
		if memberErr == nil && member != nil && strings.TrimSpace(member.CompanyID) != "" {
			companyID := strings.TrimSpace(member.CompanyID)
			conditionIDs := make([]string, 0, len(orders))
			for _, o := range orders {
				conditionIDs = append(conditionIDs, strings.TrimSpace(o.Market))
			}
			if notesByCondition, notesErr := data.ListMarketNotesByMarkets(r.Context(), h.service.db, companyID, conditionIDs, 6); notesErr == nil {
				totalCounts := noteCountsForConditions(r.Context(), h.service.db, companyID, conditionIDs)
				allNotes := marketNotesByConditionToMaps(notesByCondition, totalCounts)
				if len(allNotes) > 0 {
					result["notes_by_market"] = allNotes
				}
			}
		}

		return result, nil
	})
}

func (h *BrokerPolymarketHandler) handleGetTrades(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketGetTradesInput) (map[string]any, error) {
		trades, err := c.GetTrades(r.Context(), input.Limit)
		if err != nil {
			return nil, err
		}
		result := map[string]any{"trades": trades}
		if h.currentExecutionDisablesPolymarketNoteAugmentation(r.Context()) {
			return result, nil
		}

		agentID := BrokerAgentID(r.Context())
		member, memberErr := h.service.GetCompanyMemberForAgent(r.Context(), agentID)
		if memberErr == nil && member != nil && strings.TrimSpace(member.CompanyID) != "" {
			companyID := strings.TrimSpace(member.CompanyID)
			conditionIDs := make([]string, 0, len(trades))
			for _, t := range trades {
				conditionIDs = append(conditionIDs, strings.TrimSpace(t.ConditionID))
			}
			if notesByCondition, notesErr := data.ListMarketNotesByMarkets(r.Context(), h.service.db, companyID, conditionIDs, 6); notesErr == nil {
				totalCounts := noteCountsForConditions(r.Context(), h.service.db, companyID, conditionIDs)
				allNotes := marketNotesByConditionToMaps(notesByCondition, totalCounts)
				if len(allNotes) > 0 {
					result["notes_by_market"] = allNotes
				}
			}
		}

		return result, nil
	})
}

func marketNotesByConditionToMaps(notesByCondition map[string][]*data.MarketNote, totalCounts map[string]int) map[string][]map[string]any {
	if len(notesByCondition) == 0 {
		return nil
	}
	out := make(map[string][]map[string]any, len(notesByCondition))
	for conditionID, notes := range notesByCondition {
		if len(notes) == 0 {
			continue
		}
		rows := make([]map[string]any, 0, len(notes)+1)
		cid := strings.TrimSpace(conditionID)
		if total, ok := totalCounts[cid]; ok && total > len(notes) {
			rows = append(rows, map[string]any{
				"_truncated": fmt.Sprintf("Showing last %d of %d notes. Use list_market_notes to fetch more.", len(notes), total),
			})
		}
		for _, note := range notes {
			rows = append(rows, marketNoteToMap(note))
		}
		out[cid] = rows
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// noteCountsForConditions returns the total note count per condition_id using summaries.
func noteCountsForConditions(ctx context.Context, db gowild_data.Database, companyID string, conditionIDs []string) map[string]int {
	summaries, err := data.GetMarketNoteSummaries(ctx, db, companyID, conditionIDs)
	if err != nil {
		return nil
	}
	counts := make(map[string]int, len(summaries))
	for cid, s := range summaries {
		counts[cid] = s.Count
	}
	return counts
}

func (h *BrokerPolymarketHandler) handleRedeemWinnings(w http.ResponseWriter, r *http.Request) {
	polymarketToolHandler(h, w, r, func(c *polymarket.Client, input tools.PolymarketRedeemWinningsInput) (map[string]any, error) {
		resp, err := c.RedeemWinnings(r.Context(), "", nil, "", input.IncludeLosing)
		if resp != nil {
			// Return partial results even when some conditions failed.
			data, _ := json.Marshal(resp)
			var result map[string]any
			json.Unmarshal(data, &result)
			if err != nil {
				result["error"] = err.Error()
			}
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
}
