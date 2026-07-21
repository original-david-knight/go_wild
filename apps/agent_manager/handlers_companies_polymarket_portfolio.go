package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
	"github.com/original-david-knight/go_wild/tools"
)

const managerPolymarketPortfolioActorID = "manager_polymarket_portfolio"

type companyPolymarketClient interface {
	GetPositions(context.Context) ([]polymarket.Position, error)
	GetOrders(context.Context, string) ([]polymarket.Order, error)
	GetMarket(context.Context, string) (*polymarket.Market, error)
	GetOrder(context.Context, string) (*polymarket.Order, error)
	GetOrderBook(context.Context, string) (*polymarket.OrderBook, error)
	PlaceOrder(context.Context, string, float64, float64, string, string, bool) (*polymarket.PlaceOrderResponse, error)
	CancelOrder(context.Context, string) error
}

type liveCompanyPolymarketClient struct {
	client *polymarket.Client
	helper *BrokerPolymarketHandler
}

func (c *liveCompanyPolymarketClient) GetPositions(ctx context.Context) ([]polymarket.Position, error) {
	return c.client.GetPositions(ctx)
}

func (c *liveCompanyPolymarketClient) GetOrders(ctx context.Context, market string) ([]polymarket.Order, error) {
	return c.client.GetOrders(ctx, market)
}

func (c *liveCompanyPolymarketClient) GetMarket(ctx context.Context, conditionID string) (*polymarket.Market, error) {
	return c.client.GetMarket(ctx, conditionID)
}

func (c *liveCompanyPolymarketClient) GetOrder(ctx context.Context, orderID string) (*polymarket.Order, error) {
	return c.client.GetOrder(ctx, orderID)
}

func (c *liveCompanyPolymarketClient) GetOrderBook(ctx context.Context, tokenID string) (*polymarket.OrderBook, error) {
	if c.helper != nil {
		return c.helper.getOrderBook(ctx, c.client, tokenID)
	}
	return c.client.GetOrderBook(ctx, tokenID)
}

func (c *liveCompanyPolymarketClient) PlaceOrder(ctx context.Context, tokenID string, price, size float64, side, orderType string, negRisk bool) (*polymarket.PlaceOrderResponse, error) {
	return c.client.PlaceOrder(ctx, tokenID, price, size, side, orderType, negRisk)
}

func (c *liveCompanyPolymarketClient) CancelOrder(ctx context.Context, orderID string) error {
	return c.client.CancelOrder(ctx, orderID)
}

func (h *Handlers) newCompanyPolymarketClient(ctx context.Context, companyID string) (companyPolymarketClient, error) {
	if h.polymarketHelper == nil {
		h.polymarketHelper = NewBrokerPolymarketHandler(h.service)
	}
	if h.polymarketHelper == nil {
		return nil, fmt.Errorf("polymarket service is not configured")
	}
	client, _, err := h.polymarketHelper.getClientForCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	return &liveCompanyPolymarketClient{client: client, helper: h.polymarketHelper}, nil
}

func (h *Handlers) getCompanyPolymarketClient(ctx context.Context, companyID string) (companyPolymarketClient, error) {
	if h.companyPolymarketClientFactory != nil {
		return h.companyPolymarketClientFactory(ctx, companyID)
	}
	return h.newCompanyPolymarketClient(ctx, companyID)
}

func (h *Handlers) loadCompanyWalletBalances(ctx context.Context, companyID string) (map[string]any, error) {
	if h.walletHelper == nil {
		h.walletHelper = NewBrokerWalletHandler(h.service)
	}
	if h.walletHelper == nil {
		return nil, fmt.Errorf("wallet service is not configured")
	}

	config, err := h.walletHelper.getWalletConfigForCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}

	defaultWallet, err := tools.NewWalletToolsWithConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet tools: %w", err)
	}

	solanaResult, solanaErr := defaultWallet.GetBalanceTool(ctx, tools.GetBalanceInput{Chain: "solana"})
	ethResult, ethErr := defaultWallet.GetBalanceTool(ctx, tools.GetBalanceInput{Chain: "ethereum"})

	polygonRow := map[string]any{"ok": false, "error": "polygon rpc is not configured"}
	polygonUSDTeRow := map[string]any{"ok": false, "error": "polygon rpc is not configured"}
	polygonUSDCeRow := map[string]any{"ok": false, "error": "polygon rpc is not configured"}

	polygonRPCURL := h.walletHelper.resolvePolygonRPCURL(ctx, companyID)
	polygonUSDTeToken := polygonUSDTeTokenAddress()
	polygonUSDCeToken := polygonUSDCeTokenAddress()
	polygonUSDTeRow["token_address"] = polygonUSDTeToken
	polygonUSDCeRow["token_address"] = polygonUSDCeToken

	if polygonRPCURL != "" {
		polygonConfig := config
		polygonConfig.EthRPCURL = polygonRPCURL
		polygonWallet, walletErr := tools.NewWalletToolsWithConfig(polygonConfig)
		if walletErr != nil {
			msg := "failed to create polygon wallet tools: " + walletErr.Error()
			polygonRow = map[string]any{"ok": false, "error": msg}
			polygonUSDTeRow = map[string]any{
				"ok":            false,
				"error":         msg,
				"token_address": polygonUSDTeToken,
			}
			polygonUSDCeRow = map[string]any{
				"ok":            false,
				"error":         msg,
				"token_address": polygonUSDCeToken,
			}
		} else {
			polygonBalanceResult, polygonBalanceErr := polygonWallet.GetBalanceTool(ctx, tools.GetBalanceInput{Chain: "ethereum"})
			polygonUSDTeResult, polygonUSDTeErr := polygonWallet.GetBalanceTool(ctx, tools.GetBalanceInput{
				Chain:        "ethereum",
				TokenAddress: polygonUSDTeToken,
			})
			polygonUSDCeResult, polygonUSDCeErr := polygonWallet.GetBalanceTool(ctx, tools.GetBalanceInput{
				Chain:        "ethereum",
				TokenAddress: polygonUSDCeToken,
			})
			polygonRow = compactBalanceSnapshot(polygonBalanceResult, polygonBalanceErr)
			polygonUSDTeRow = compactBalanceSnapshot(polygonUSDTeResult, polygonUSDTeErr)
			polygonUSDCeRow = compactBalanceSnapshot(polygonUSDCeResult, polygonUSDCeErr)
			polygonRow["chain"] = "polygon"
			polygonUSDTeRow["chain"] = "polygon"
			polygonUSDCeRow["chain"] = "polygon"
			polygonUSDTeRow["token_address"] = polygonUSDTeToken
			polygonUSDCeRow["token_address"] = polygonUSDCeToken
		}
	}

	return map[string]any{
		"solana":         compactBalanceSnapshot(solanaResult, solanaErr),
		"eth":            compactBalanceSnapshot(ethResult, ethErr),
		"polygon":        polygonRow,
		"polygon_usdte":  polygonUSDTeRow,
		"polygon_usdce":  polygonUSDCeRow,
		"identity_scope": "company",
		"company_id":     companyID,
	}, nil
}

func (h *Handlers) getCompanyWalletBalances(ctx context.Context, companyID string) (map[string]any, error) {
	if h.companyWalletBalancesLoader != nil {
		return h.companyWalletBalancesLoader(ctx, companyID)
	}
	return h.loadCompanyWalletBalances(ctx, companyID)
}

func (h *Handlers) getCompanyPolymarketPortfolio(w http.ResponseWriter, r *http.Request, companyID string) {
	client, err := h.getCompanyPolymarketClient(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create polymarket client: "+err.Error())
		return
	}

	resp, err := h.buildCompanyPolymarketPortfolio(r.Context(), companyID, client)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load polymarket portfolio: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) buildCompanyPolymarketPortfolio(ctx context.Context, companyID string, client companyPolymarketClient) (map[string]any, error) {
	var (
		positions    []polymarket.Position
		positionsErr error
		allOrders    []polymarket.Order
		ordersErr    error
		balances     map[string]any
		balancesErr  error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		positions, positionsErr = client.GetPositions(ctx)
	}()
	go func() {
		defer wg.Done()
		allOrders, ordersErr = client.GetOrders(ctx, "")
	}()
	go func() {
		defer wg.Done()
		balances, balancesErr = h.getCompanyWalletBalances(ctx, companyID)
	}()
	wg.Wait()

	if positionsErr == nil {
		filtered := positions[:0]
		for _, position := range positions {
			if roundBuiltinPolymarketFloat(position.Size, 4) <= 0 {
				continue
			}
			filtered = append(filtered, position)
		}
		positions = filtered
		sort.Slice(positions, func(i, j int) bool {
			if positions[i].CurrentValue == positions[j].CurrentValue {
				return strings.Compare(strings.TrimSpace(positions[i].Title), strings.TrimSpace(positions[j].Title)) < 0
			}
			return positions[i].CurrentValue > positions[j].CurrentValue
		})
	}

	openOrders := make([]polymarket.Order, 0, len(allOrders))
	if ordersErr == nil {
		for _, order := range allOrders {
			if !isBuiltinPolymarketOrderOpen(order) {
				continue
			}
			openOrders = append(openOrders, order)
		}
		sort.Slice(openOrders, func(i, j int) bool {
			left := strings.TrimSpace(openOrders[i].CreatedAt.String())
			right := strings.TrimSpace(openOrders[j].CreatedAt.String())
			if left == right {
				return strings.Compare(strings.TrimSpace(openOrders[i].ID), strings.TrimSpace(openOrders[j].ID)) < 0
			}
			return left > right
		})
	}

	conditionIDs := make([]string, 0)
	seen := map[string]struct{}{}
	addConditionID := func(conditionID string) {
		conditionID = strings.TrimSpace(conditionID)
		if conditionID == "" {
			return
		}
		if _, ok := seen[conditionID]; ok {
			return
		}
		seen[conditionID] = struct{}{}
		conditionIDs = append(conditionIDs, conditionID)
	}
	for _, position := range positions {
		addConditionID(position.ConditionID)
	}
	for _, order := range openOrders {
		addConditionID(order.Market)
	}

	noteSummaries, noteSummaryErr := data.GetMarketNoteSummaries(ctx, h.service.db, companyID, conditionIDs)
	cachedMarkets, cachedMarketsErr := loadPolymarketCachedMarketsByConditionIDs(ctx, h.service.db, conditionIDs)

	markets := map[string]map[string]any{}
	notesByMarket := map[string]map[string]any{}
	for _, conditionID := range conditionIDs {
		if summary, ok := noteSummaries[conditionID]; ok {
			noteMeta := map[string]any{"count": summary.Count}
			if summary.Latest != nil {
				noteMeta["latest_note"] = marketNoteToMap(summary.Latest)
			}
			notesByMarket[conditionID] = noteMeta
		}

		var (
			market    *polymarket.Market
			marketErr error
		)
		if cached, ok := cachedMarkets[conditionID]; ok {
			cachedCopy := cached
			market = &cachedCopy
		} else {
			market, marketErr = client.GetMarket(ctx, conditionID)
		}
		if marketErr != nil || market == nil {
			markets[conditionID] = map[string]any{
				"condition_id": conditionID,
			}
			continue
		}
		tokens, probability := buildBuiltinPolymarketFindMarketTokens(*market)
		markets[conditionID] = map[string]any{
			"condition_id":      strings.TrimSpace(market.ConditionID),
			"question":          strings.TrimSpace(market.Question),
			"slug":              strings.TrimSpace(market.Slug),
			"description":       strings.TrimSpace(market.Description),
			"image":             strings.TrimSpace(market.Image),
			"icon":              strings.TrimSpace(market.Icon),
			"end_date":          strings.TrimSpace(market.EndDate),
			"best_bid":          roundBuiltinPolymarketFloat(market.BestBid, 4),
			"best_ask":          roundBuiltinPolymarketFloat(market.BestAsk, 4),
			"probability":       roundBuiltinPolymarketFloat(probability, 4),
			"accepting_orders":  market.AcceptingOrders,
			"active":            market.Active,
			"closed":            market.Closed,
			"negative_risk":     market.NegRisk,
			"tokens":            tokens,
			"latest_note_count": noteIntValue(notesByMarket[conditionID], "count"),
			"latest_note":       notesByMarket[conditionID]["latest_note"],
		}
	}

	positionsOut := make([]map[string]any, 0, len(positions))
	polymarketAssets := 0.0
	for _, position := range positions {
		polymarketAssets += position.CurrentValue
		row := map[string]any{
			"asset":         strings.TrimSpace(position.Asset),
			"condition_id":  strings.TrimSpace(position.ConditionID),
			"title":         strings.TrimSpace(position.Title),
			"slug":          strings.TrimSpace(position.Slug),
			"outcome":       strings.TrimSpace(position.Outcome),
			"outcome_index": position.OutcomeIndex,
			"size":          roundBuiltinPolymarketFloat(position.Size, 4),
			"avg_price":     roundBuiltinPolymarketFloat(position.AvgPrice, 4),
			"cur_price":     roundBuiltinPolymarketFloat(position.CurPrice, 4),
			"initial_value": roundBuiltinPolymarketFloat(position.InitialValue, 2),
			"current_value": roundBuiltinPolymarketFloat(position.CurrentValue, 2),
			"cash_pnl":      roundBuiltinPolymarketFloat(position.CashPnl, 2),
			"percent_pnl":   roundBuiltinPolymarketFloat(position.PercentPnl, 4),
			"realized_pnl":  roundBuiltinPolymarketFloat(position.RealizedPnl, 2),
			"total_bought":  roundBuiltinPolymarketFloat(position.TotalBought, 4),
			"redeemable":    position.Redeemable,
			"negative_risk": position.NegativeRisk,
			"end_date":      strings.TrimSpace(position.EndDate),
			"event_slug":    strings.TrimSpace(position.EventSlug),
		}
		if market, ok := markets[strings.TrimSpace(position.ConditionID)]; ok {
			if question := strings.TrimSpace(fmt.Sprint(market["question"])); question != "" {
				row["market_question"] = question
			}
		}
		if noteMeta, ok := notesByMarket[strings.TrimSpace(position.ConditionID)]; ok {
			row["note_count"] = noteIntValue(noteMeta, "count")
			if latest, ok := noteMeta["latest_note"]; ok && latest != nil {
				row["latest_note"] = latest
			}
		}
		positionsOut = append(positionsOut, row)
	}

	ordersOut := make([]map[string]any, 0, len(openOrders))
	for _, order := range openOrders {
		row := map[string]any{
			"id":             strings.TrimSpace(order.ID),
			"market":         strings.TrimSpace(order.Market),
			"asset_id":       strings.TrimSpace(order.AssetID),
			"side":           strings.ToUpper(strings.TrimSpace(order.Side)),
			"original_size":  strings.TrimSpace(order.OriginalSize),
			"size_matched":   strings.TrimSpace(order.SizeMatched),
			"remaining_size": roundBuiltinPolymarketFloat(builtinPolymarketOrderRemainingSize(order), 4),
			"price":          strings.TrimSpace(order.Price),
			"status":         strings.TrimSpace(order.Status),
			"owner":          strings.TrimSpace(order.Owner),
			"outcome":        strings.TrimSpace(order.Outcome),
			"maker_address":  strings.TrimSpace(order.MakerAddress),
			"order_type":     strings.TrimSpace(order.Type),
			"created_at":     strings.TrimSpace(order.CreatedAt.String()),
			"expiration":     strings.TrimSpace(order.Expiration),
		}
		if market, ok := markets[strings.TrimSpace(order.Market)]; ok {
			if question := strings.TrimSpace(fmt.Sprint(market["question"])); question != "" {
				row["market_question"] = question
			}
		}
		if noteMeta, ok := notesByMarket[strings.TrimSpace(order.Market)]; ok {
			row["note_count"] = noteIntValue(noteMeta, "count")
			if latest, ok := noteMeta["latest_note"]; ok && latest != nil {
				row["latest_note"] = latest
			}
		}
		ordersOut = append(ordersOut, row)
	}

	usdceAssets := balanceAmountFromSnapshot(balances, "polygon_usdce")
	usdteAssets := balanceAmountFromSnapshot(balances, "polygon_usdte")
	usdAssets := roundBuiltinPolymarketFloat(usdceAssets+usdteAssets, 2)
	polymarketAssets = roundBuiltinPolymarketFloat(polymarketAssets, 2)
	totalAssets := roundBuiltinPolymarketFloat(usdAssets+polymarketAssets, 2)

	errorsBySection := map[string]string{}
	if positionsErr != nil {
		errorsBySection["positions"] = strings.TrimSpace(positionsErr.Error())
	}
	if ordersErr != nil {
		errorsBySection["orders"] = strings.TrimSpace(ordersErr.Error())
	}
	if balancesErr != nil {
		errorsBySection["balances"] = strings.TrimSpace(balancesErr.Error())
	}
	if noteSummaryErr != nil {
		errorsBySection["notes"] = strings.TrimSpace(noteSummaryErr.Error())
	}
	if cachedMarketsErr != nil {
		errorsBySection["market_cache"] = strings.TrimSpace(cachedMarketsErr.Error())
	}

	resp := map[string]any{
		"company_id": companyID,
		"summary": map[string]any{
			"total_assets":       totalAssets,
			"usd_assets":         usdAssets,
			"usdce_assets":       roundBuiltinPolymarketFloat(usdceAssets, 2),
			"usdte_assets":       roundBuiltinPolymarketFloat(usdteAssets, 2),
			"polymarket_assets":  polymarketAssets,
			"positions_count":    len(positionsOut),
			"open_orders_count":  len(ordersOut),
			"shares_owned_total": roundBuiltinPolymarketFloat(sumPolymarketPositionShares(positions), 4),
		},
		"wallet_balances": balances,
		"positions":       positionsOut,
		"orders":          ordersOut,
		"markets":         markets,
		"notes_by_market": notesByMarket,
	}
	if len(errorsBySection) > 0 {
		resp["errors"] = errorsBySection
	}
	return resp, nil
}

func (h *Handlers) listCompanyPolymarketNotes(w http.ResponseWriter, r *http.Request, companyID string) {
	conditionID := strings.TrimSpace(r.URL.Query().Get("condition_id"))
	if conditionID == "" {
		writeError(w, http.StatusBadRequest, "condition_id is required")
		return
	}

	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	notes, err := data.ListMarketNotes(r.Context(), h.service.db, companyID, conditionID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load market notes: "+err.Error())
		return
	}
	totalCount, countErr := data.CountMarketNotes(r.Context(), h.service.db, companyID, conditionID)
	if countErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to count market notes: "+countErr.Error())
		return
	}

	out := make([]map[string]any, len(notes))
	for i, note := range notes {
		out[i] = marketNoteToMap(note)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"company_id":   companyID,
		"condition_id": conditionID,
		"count":        totalCount,
		"notes":        out,
	})
}

func findCompanyPolymarketPosition(positions []polymarket.Position, conditionID, asset string) *polymarket.Position {
	conditionID = strings.TrimSpace(conditionID)
	asset = strings.TrimSpace(asset)
	for i := range positions {
		current := &positions[i]
		if strings.TrimSpace(current.ConditionID) != conditionID {
			continue
		}
		if strings.TrimSpace(current.Asset) != asset {
			continue
		}
		return current
	}
	return nil
}

func resolveCompanyPolymarketNegRisk(ctx context.Context, client companyPolymarketClient, conditionID, asset string) (bool, error) {
	positions, positionsErr := client.GetPositions(ctx)
	if positionsErr == nil {
		if position := findCompanyPolymarketPosition(positions, conditionID, asset); position != nil {
			return position.NegativeRisk, nil
		}
	}

	market, marketErr := client.GetMarket(ctx, conditionID)
	switch {
	case marketErr == nil && market != nil:
		return market.NegRisk, nil
	case positionsErr != nil && marketErr != nil:
		return false, fmt.Errorf("failed to resolve negRisk metadata: positions: %v; market: %v", positionsErr, marketErr)
	case marketErr != nil:
		return false, fmt.Errorf("failed to load market metadata: %w", marketErr)
	default:
		return false, nil
	}
}

func (h *Handlers) sellCompanyPolymarketPosition(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		Asset       string   `json:"asset"`
		ConditionID string   `json:"condition_id"`
		Outcome     string   `json:"outcome,omitempty"`
		Size        float64  `json:"size"`
		Price       *float64 `json:"price,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	req.Asset = strings.TrimSpace(req.Asset)
	req.ConditionID = strings.TrimSpace(req.ConditionID)
	req.Outcome = strings.TrimSpace(req.Outcome)
	if req.Asset == "" {
		writeError(w, http.StatusBadRequest, "asset is required")
		return
	}
	if req.ConditionID == "" {
		writeError(w, http.StatusBadRequest, "condition_id is required")
		return
	}
	if req.Size <= 0 {
		writeError(w, http.StatusBadRequest, "size must be greater than zero")
		return
	}

	client, err := h.getCompanyPolymarketClient(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create polymarket client: "+err.Error())
		return
	}

	price := 0.0
	if req.Price != nil {
		price = roundBuiltinPolymarketFloat(*req.Price, 4)
	}
	if price <= 0 {
		book, bookErr := client.GetOrderBook(r.Context(), req.Asset)
		if bookErr == nil {
			price = roundBuiltinPolymarketFloat(bestBidFromOrderBook(book), 4)
		}
	}
	if price <= 0 {
		writeError(w, http.StatusBadRequest, "price is required when no live best bid is available")
		return
	}

	negRisk, err := resolveCompanyPolymarketNegRisk(r.Context(), client, req.ConditionID, req.Asset)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to resolve negRisk metadata: "+err.Error())
		return
	}

	resp, err := client.PlaceOrder(r.Context(), req.Asset, price, roundBuiltinPolymarketFloat(req.Size, 4), polymarket.Sell, polymarket.GTC, negRisk)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to place sell order: "+err.Error())
		return
	}
	if resp == nil {
		writeError(w, http.StatusBadRequest, "polymarket sell order returned no response")
		return
	}
	if !resp.Success {
		msg := strings.TrimSpace(resp.ErrorMsg)
		if msg == "" {
			msg = "order rejected"
		}
		writeError(w, http.StatusBadRequest, "polymarket sell order rejected: "+msg)
		return
	}

	noteContent := buildManagerPolymarketNote("sell", req.ConditionID, req.Outcome, roundBuiltinPolymarketFloat(req.Size, 4), price, strings.TrimSpace(resp.OrderID))
	note, noteErr := data.AddMarketNote(r.Context(), h.service.db, companyID, managerPolymarketPortfolioActorID, req.ConditionID, noteContent)

	out := map[string]any{
		"status":       "placed",
		"company_id":   companyID,
		"condition_id": req.ConditionID,
		"asset":        req.Asset,
		"outcome":      req.Outcome,
		"order_id":     strings.TrimSpace(resp.OrderID),
		"price":        price,
		"size":         roundBuiltinPolymarketFloat(req.Size, 4),
	}
	if note != nil {
		out["market_note"] = marketNoteToMap(note)
	}
	if noteErr != nil {
		out["market_note_error"] = strings.TrimSpace(noteErr.Error())
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) exitCompanyPolymarketPosition(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		Asset       string `json:"asset"`
		ConditionID string `json:"condition_id"`
		Outcome     string `json:"outcome,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	req.Asset = strings.TrimSpace(req.Asset)
	req.ConditionID = strings.TrimSpace(req.ConditionID)
	req.Outcome = strings.TrimSpace(req.Outcome)
	if req.Asset == "" {
		writeError(w, http.StatusBadRequest, "asset is required")
		return
	}
	if req.ConditionID == "" {
		writeError(w, http.StatusBadRequest, "condition_id is required")
		return
	}

	client, err := h.getCompanyPolymarketClient(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create polymarket client: "+err.Error())
		return
	}

	positions, err := client.GetPositions(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to load positions: "+err.Error())
		return
	}

	position := findCompanyPolymarketPosition(positions, req.ConditionID, req.Asset)
	if position == nil || roundBuiltinPolymarketFloat(position.Size, 4) <= 0 {
		writeError(w, http.StatusBadRequest, "no held position found for asset")
		return
	}

	orders, err := client.GetOrders(r.Context(), req.ConditionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to load orders: "+err.Error())
		return
	}

	cancelledOrderIDs := make([]string, 0)
	for _, order := range orders {
		if strings.TrimSpace(order.Market) != req.ConditionID {
			continue
		}
		if !isBuiltinPolymarketOrderOpen(order) {
			continue
		}
		orderID := strings.TrimSpace(order.ID)
		if orderID == "" {
			continue
		}
		if err := client.CancelOrder(r.Context(), orderID); err != nil {
			writeError(w, http.StatusBadRequest, "failed to cancel order "+orderID+": "+err.Error())
			return
		}
		cancelledOrderIDs = append(cancelledOrderIDs, orderID)
	}

	book, err := client.GetOrderBook(r.Context(), req.Asset)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to load order book: "+err.Error())
		return
	}
	price := roundBuiltinPolymarketFloat(bestBidFromOrderBook(book), 4)
	if price <= 0 {
		writeError(w, http.StatusBadRequest, "no live best bid available to sell the position")
		return
	}

	size := roundBuiltinPolymarketFloat(position.Size, 4)
	resp, err := client.PlaceOrder(r.Context(), req.Asset, price, size, polymarket.Sell, polymarket.GTC, position.NegativeRisk)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to place exit sell order: "+err.Error())
		return
	}
	if resp == nil {
		writeError(w, http.StatusBadRequest, "polymarket exit sell order returned no response")
		return
	}
	if !resp.Success {
		msg := strings.TrimSpace(resp.ErrorMsg)
		if msg == "" {
			msg = "order rejected"
		}
		writeError(w, http.StatusBadRequest, "polymarket exit sell order rejected: "+msg)
		return
	}

	noteContent := buildManagerPolymarketExitNote(req.ConditionID, firstNonEmpty(req.Outcome, position.Outcome), size, price, strings.TrimSpace(resp.OrderID), cancelledOrderIDs)
	note, noteErr := data.AddMarketNote(r.Context(), h.service.db, companyID, managerPolymarketPortfolioActorID, req.ConditionID, noteContent)

	out := map[string]any{
		"status":              "exiting",
		"company_id":          companyID,
		"condition_id":        req.ConditionID,
		"asset":               req.Asset,
		"outcome":             firstNonEmpty(req.Outcome, position.Outcome),
		"sold_size":           size,
		"sell_price":          price,
		"sell_order_id":       strings.TrimSpace(resp.OrderID),
		"cancelled_order_ids": cancelledOrderIDs,
		"cancelled_orders":    len(cancelledOrderIDs),
	}
	if note != nil {
		out["market_note"] = marketNoteToMap(note)
	}
	if noteErr != nil {
		out["market_note_error"] = strings.TrimSpace(noteErr.Error())
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) cancelCompanyPolymarketOrder(w http.ResponseWriter, r *http.Request, companyID string) {
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.OrderID = strings.TrimSpace(req.OrderID)
	if req.OrderID == "" {
		writeError(w, http.StatusBadRequest, "order_id is required")
		return
	}

	client, err := h.getCompanyPolymarketClient(r.Context(), companyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create polymarket client: "+err.Error())
		return
	}

	order, orderErr := client.GetOrder(r.Context(), req.OrderID)
	if orderErr != nil {
		writeError(w, http.StatusBadRequest, "failed to load order: "+orderErr.Error())
		return
	}
	if order == nil {
		writeError(w, http.StatusBadRequest, "order not found")
		return
	}

	if err := client.CancelOrder(r.Context(), req.OrderID); err != nil {
		writeError(w, http.StatusBadRequest, "failed to cancel order: "+err.Error())
		return
	}

	noteContent := buildManagerPolymarketNote("cancel", strings.TrimSpace(order.Market), strings.TrimSpace(order.Outcome), roundBuiltinPolymarketFloat(builtinPolymarketOrderRemainingSize(*order), 4), parsePolymarketOrderPrice(order), strings.TrimSpace(order.ID))
	note, noteErr := data.AddMarketNote(r.Context(), h.service.db, companyID, managerPolymarketPortfolioActorID, strings.TrimSpace(order.Market), noteContent)

	out := map[string]any{
		"status":       "cancelled",
		"company_id":   companyID,
		"condition_id": strings.TrimSpace(order.Market),
		"order_id":     strings.TrimSpace(order.ID),
		"asset_id":     strings.TrimSpace(order.AssetID),
		"side":         strings.ToUpper(strings.TrimSpace(order.Side)),
		"outcome":      strings.TrimSpace(order.Outcome),
	}
	if note != nil {
		out["market_note"] = marketNoteToMap(note)
	}
	if noteErr != nil {
		out["market_note_error"] = strings.TrimSpace(noteErr.Error())
	}
	writeJSON(w, http.StatusOK, out)
}

func bestBidFromOrderBook(book *polymarket.OrderBook) float64 {
	if book == nil {
		return 0
	}
	best := 0.0
	for _, bid := range book.Bids {
		price, err := strconv.ParseFloat(strings.TrimSpace(bid.Price), 64)
		if err != nil {
			continue
		}
		best = math.Max(best, price)
	}
	return best
}

func parsePolymarketOrderPrice(order *polymarket.Order) float64 {
	if order == nil {
		return 0
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(order.Price), 64)
	if err != nil {
		return 0
	}
	return roundBuiltinPolymarketFloat(price, 4)
}

func buildManagerPolymarketNote(action, conditionID, outcome string, size, price float64, orderID string) string {
	action = strings.ToUpper(strings.TrimSpace(action))
	lines := []string{
		"Manager Polymarket portfolio action",
		"Instruction: Do not buy this market automatically until reviewed.",
		"Time: " + time.Now().UTC().Format(time.RFC3339),
	}
	if action != "" {
		lines = append(lines, "Action: "+action)
	}
	if conditionID = strings.TrimSpace(conditionID); conditionID != "" {
		lines = append(lines, "Condition ID: "+conditionID)
	}
	if outcome = strings.TrimSpace(outcome); outcome != "" {
		lines = append(lines, "Outcome: "+outcome)
	}
	if size > 0 {
		lines = append(lines, fmt.Sprintf("Amount: %.4f shares", roundBuiltinPolymarketFloat(size, 4)))
	}
	if price > 0 {
		lines = append(lines, fmt.Sprintf("Price: %.4f", roundBuiltinPolymarketFloat(price, 4)))
	}
	if orderID = strings.TrimSpace(orderID); orderID != "" {
		lines = append(lines, "Order ID: "+orderID)
	}
	return strings.Join(lines, "\n")
}

func buildManagerPolymarketExitNote(conditionID, outcome string, size, price float64, orderID string, cancelledOrderIDs []string) string {
	lines := []string{
		"Manager Polymarket portfolio action",
		"Instruction: Do not buy this market automatically until reviewed.",
		"Time: " + time.Now().UTC().Format(time.RFC3339),
		"Action: EXIT_POSITION",
	}
	if conditionID = strings.TrimSpace(conditionID); conditionID != "" {
		lines = append(lines, "Condition ID: "+conditionID)
	}
	if outcome = strings.TrimSpace(outcome); outcome != "" {
		lines = append(lines, "Outcome: "+outcome)
	}
	if size > 0 {
		lines = append(lines, fmt.Sprintf("Amount: %.4f shares", roundBuiltinPolymarketFloat(size, 4)))
	}
	if price > 0 {
		lines = append(lines, fmt.Sprintf("Price: %.4f", roundBuiltinPolymarketFloat(price, 4)))
	}
	if orderID = strings.TrimSpace(orderID); orderID != "" {
		lines = append(lines, "Sell Order ID: "+orderID)
	}
	lines = append(lines, fmt.Sprintf("Cancelled Orders: %d", len(cancelledOrderIDs)))
	if len(cancelledOrderIDs) > 0 {
		lines = append(lines, "Cancelled Order IDs: "+strings.Join(cancelledOrderIDs, ", "))
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func balanceAmountFromSnapshot(snapshot map[string]any, key string) float64 {
	if snapshot == nil {
		return 0
	}
	row, ok := snapshot[key].(map[string]any)
	if !ok {
		return 0
	}
	if okValue, ok := row["ok"].(bool); ok && !okValue {
		return 0
	}
	return anyFloat(row["balance"])
}

func anyFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func noteIntValue(noteMeta map[string]any, key string) int {
	if noteMeta == nil {
		return 0
	}
	switch typed := noteMeta[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return 0
}

func sumPolymarketPositionShares(positions []polymarket.Position) float64 {
	total := 0.0
	for _, position := range positions {
		total += position.Size
	}
	return total
}
