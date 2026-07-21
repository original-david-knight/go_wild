package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func pipelineBuiltinPolymarketFindMarkets(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, _ PipelineStep, params map[string]any) (map[string]any, error) {
	companyID, err := resolvePolymarketRunCompanyID(run, stringParam(params, "company_id"))
	if err != nil {
		return nil, err
	}
	client, resolvedCompanyID, err := getBuiltinPolymarketClient(ctx, pe, companyID)
	if err != nil {
		return nil, err
	}

	query := stringParam(params, "query")
	searchQueries := builtinPolymarketFindMarketQueries(query)
	queriesUsed := make([]string, 0, len(searchQueries))
	queriesUsed = append(queriesUsed, searchQueries...)

	positions, err := client.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list polymarket positions: %w", err)
	}
	orders, err := client.GetOrders(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list polymarket orders: %w", err)
	}

	positionMarkets := make(map[string]struct{})
	positionValue := 0.0
	for _, position := range positions {
		conditionID := strings.TrimSpace(position.ConditionID)
		if conditionID == "" || position.Size <= 0 {
			continue
		}
		positionMarkets[conditionID] = struct{}{}
		if position.CurrentValue > 0 {
			positionValue += position.CurrentValue
		}
	}

	openOrderMarkets := make(map[string]struct{})
	for _, order := range orders {
		if !isBuiltinPolymarketOrderOpen(order) {
			continue
		}
		conditionID := strings.TrimSpace(order.Market)
		if conditionID == "" {
			continue
		}
		openOrderMarkets[conditionID] = struct{}{}
	}

	now := time.Now().UTC()
	resolutionCutoff := now.AddDate(0, 6, 0)

	shareCapacity := 0.0
	aum := 0.0
	usdcBalance := 0.0
	if balance, balanceErr := getBuiltinPolymarketUSDCBalance(ctx, pe, resolvedCompanyID); balanceErr == nil && balance > 0 {
		usdcBalance = roundBuiltinPolymarketFloat(balance, 2)
	}
	if liquidUSDBalance, balanceErr := getBuiltinPolymarketLiquidUSDBalance(ctx, pe, resolvedCompanyID); balanceErr == nil {
		aum = roundBuiltinPolymarketFloat(liquidUSDBalance+positionValue, 2)
		if aum > 0 {
			shareCapacity = roundBuiltinPolymarketFloat(aum/20, 2)
		}
	}

	limit := builtinPolymarketFindMarketsLimit(params, usdcBalance, aum)
	searchLimit := builtinPolymarketFindMarketsSearchLimit(limit)

	selectedMarkets := make([]builtinPolymarketFindMarketCandidate, 0, limit)
	seenConditionIDs := make(map[string]struct{})
	candidatesExamined := 0
	pagesScanned := 0
	skippedSports := 0
	skippedCrypto := 0
	skippedStocks := 0
	skippedExistingOrders := 0
	skippedExistingShares := 0
	skippedLowVolume := 0
	skippedRecentNotes := 0
	skippedFarResolution := 0
	skippedExpired := 0
	skippedMissingEnd := 0
	skippedStale := 0
	staleOnly := true
	if v, ok := boolParam(params, "stale_only"); ok {
		staleOnly = v
	}
	cacheUsed := false
	cacheSyncPerformed := false
	cacheSyncQueued := false
	cacheSyncAt := ""
	cacheSyncError := ""
	cacheMarketsLoaded := 0
	scanMode := builtinPolymarketFindMarketsScanMode(query)
	var cacheDB gowild_data.Database
	if pe != nil {
		cacheDB = pe.db
	}
	loadNoteSummaries := func(markets []polymarket.Market) (map[string]data.MarketNoteSummary, error) {
		if pe == nil || pe.db == nil || strings.TrimSpace(resolvedCompanyID) == "" || len(markets) == 0 {
			return nil, nil
		}
		conditionIDs := make([]string, 0, len(markets))
		for _, market := range markets {
			conditionIDs = append(conditionIDs, strings.TrimSpace(market.ConditionID))
		}
		return data.GetMarketNoteSummaries(ctx, pe.db, resolvedCompanyID, conditionIDs)
	}
	loadMarketProperties := func(markets []polymarket.Market) (map[string][]*data.MarketProperty, error) {
		if !staleOnly || pe == nil || pe.db == nil || strings.TrimSpace(resolvedCompanyID) == "" || len(markets) == 0 {
			return nil, nil
		}
		conditionIDs := make([]string, 0, len(markets))
		for _, market := range markets {
			conditionIDs = append(conditionIDs, strings.TrimSpace(market.ConditionID))
		}
		return data.ListMarketPropertiesBulk(ctx, pe.db, resolvedCompanyID, conditionIDs)
	}
	staleCutoff := now.Add(-7 * 24 * time.Hour)

	considerMarket := func(market polymarket.Market, noteSummary data.MarketNoteSummary, props []*data.MarketProperty) error {
		conditionID := strings.TrimSpace(market.ConditionID)
		if conditionID == "" {
			return nil
		}
		if _, ok := seenConditionIDs[conditionID]; ok {
			return nil
		}
		seenConditionIDs[conditionID] = struct{}{}
		candidatesExamined++
		if !market.Active || market.Closed || !market.AcceptingOrders {
			return nil
		}
		if builtinPolymarketMarketLooksSports(market) {
			skippedSports++
			return nil
		}
		if builtinPolymarketMarketLooksCrypto(market) {
			skippedCrypto++
			return nil
		}
		if builtinPolymarketMarketLooksStocks(market) {
			skippedStocks++
			return nil
		}
		if _, ok := positionMarkets[conditionID]; ok {
			skippedExistingShares++
			return nil
		}
		if _, ok := openOrderMarkets[conditionID]; ok {
			skippedExistingOrders++
			return nil
		}
		if builtinPolymarketMarketVolume(market) < builtinPolymarketFindMarketsMinVolume {
			skippedLowVolume++
			return nil
		}

		endAt, ok := parseBuiltinPolymarketTime(market.EndDate)
		if !ok {
			skippedMissingEnd++
			return nil
		}
		if !endAt.After(now) {
			skippedExpired++
			return nil
		}
		if endAt.After(resolutionCutoff) {
			skippedFarResolution++
			return nil
		}

		if staleOnly && builtinPolymarketMarketRecentlyTouched(props, staleCutoff) {
			skippedStale++
			return nil
		}

		lastNoteAt := ""
		if noteSummary.Latest != nil {
			lastNoteAt = noteSummary.Latest.CreatedAt.UTC().Format(time.RFC3339)
		}
		score, spread, daysToEnd := builtinPolymarketFindMarketScore(now, market, endAt, lastNoteAt)

		selectedMarkets = append(selectedMarkets, builtinPolymarketFindMarketCandidate{
			market:     market,
			endAt:      endAt,
			lastNoteAt: lastNoteAt,
			noteCount:  noteSummary.Count,
			score:      score,
			spread:     spread,
			daysToEnd:  daysToEnd,
		})
		return nil
	}

	if cachedCandidates, cacheStatus, cacheErr := loadPolymarketMarketCacheCandidates(ctx, cacheDB, client, query); cacheStatus.Used {
		cacheUsed = true
		cacheSyncPerformed = cacheStatus.SyncPerformed
		cacheSyncQueued = cacheStatus.SyncQueued
		cacheSyncAt = strings.TrimSpace(cacheStatus.LastSyncAt)
		cacheSyncError = strings.TrimSpace(cacheStatus.SyncError)
		cacheMarketsLoaded = cacheStatus.MarketsLoaded
		if strings.TrimSpace(query) == "" {
			scanMode = "cached_events"
		} else {
			scanMode = "cached_query"
		}
		if cacheErr != nil && len(cachedCandidates) == 0 {
			return nil, fmt.Errorf("failed to load polymarket cache: %w", cacheErr)
		}
		noteSummaries, noteErr := loadNoteSummaries(cachedCandidates)
		if noteErr != nil {
			return nil, noteErr
		}
		propsByMarket, propErr := loadMarketProperties(cachedCandidates)
		if propErr != nil {
			return nil, propErr
		}
		for _, market := range cachedCandidates {
			cid := strings.TrimSpace(market.ConditionID)
			if err := considerMarket(market, noteSummaries[cid], propsByMarket[cid]); err != nil {
				return nil, err
			}
			if len(selectedMarkets) >= limit {
				break
			}
		}
	} else if len(searchQueries) > 0 {
		searchCandidates := make([]polymarket.Market, 0, searchLimit*len(searchQueries))
		for _, searchQuery := range searchQueries {
			batch, searchErr := client.SearchMarkets(ctx, searchQuery, searchLimit)
			if searchErr != nil {
				return nil, fmt.Errorf("failed to search polymarket markets: %w", searchErr)
			}
			searchCandidates = append(searchCandidates, batch...)
		}
		noteSummaries, noteErr := loadNoteSummaries(searchCandidates)
		if noteErr != nil {
			return nil, noteErr
		}
		propsByMarket, propErr := loadMarketProperties(searchCandidates)
		if propErr != nil {
			return nil, propErr
		}
		sortBuiltinPolymarketMarketsNewestFirst(searchCandidates)
		for _, market := range searchCandidates {
			cid := strings.TrimSpace(market.ConditionID)
			if err := considerMarket(market, noteSummaries[cid], propsByMarket[cid]); err != nil {
				return nil, err
			}
			if len(selectedMarkets) >= limit {
				break
			}
		}
	} else {
		offset := 0
		for {
			batch, listErr := client.ListMarkets(ctx, builtinPolymarketFindMarketsListPageSize, offset)
			if listErr != nil {
				return nil, fmt.Errorf("failed to list polymarket markets: %w", listErr)
			}
			if len(batch) == 0 {
				break
			}
			pagesScanned++
			noteSummaries, noteErr := loadNoteSummaries(batch)
			if noteErr != nil {
				return nil, noteErr
			}
			propsByMarket, propErr := loadMarketProperties(batch)
			if propErr != nil {
				return nil, propErr
			}
			sortBuiltinPolymarketMarketsNewestFirst(batch)
			for _, market := range batch {
				cid := strings.TrimSpace(market.ConditionID)
				if err := considerMarket(market, noteSummaries[cid], propsByMarket[cid]); err != nil {
					return nil, err
				}
				if len(selectedMarkets) >= limit {
					break
				}
			}
			if len(selectedMarkets) >= limit || len(batch) < builtinPolymarketFindMarketsListPageSize {
				break
			}
			offset += len(batch)
		}
	}

	sort.SliceStable(selectedMarkets, func(i, j int) bool {
		if selectedMarkets[i].score != selectedMarkets[j].score {
			return selectedMarkets[i].score > selectedMarkets[j].score
		}
		if selectedMarkets[i].spread != selectedMarkets[j].spread {
			return selectedMarkets[i].spread < selectedMarkets[j].spread
		}
		return builtinPolymarketMarketNewestFirstLess(selectedMarkets[i].market, selectedMarkets[j].market)
	})

	markets := make([]map[string]any, 0, len(selectedMarkets))
	for _, selected := range selectedMarkets {
		markets = append(markets, buildBuiltinPolymarketFindMarketResult(selected.market, selected.endAt, selected.lastNoteAt, selected.noteCount, selected.score, selected.spread, selected.daysToEnd, shareCapacity, aum))
	}

	return map[string]any{
		"source":                  "polymarket",
		"company_id":              resolvedCompanyID,
		"query":                   query,
		"queries_used":            queriesUsed,
		"pages_scanned":           pagesScanned,
		"scan_mode":               scanMode,
		"limit":                   limit,
		"markets":                 markets,
		"items":                   markets,
		"markets_found":           len(markets),
		"candidates_examined":     candidatesExamined,
		"cache_used":              cacheUsed,
		"cache_sync_performed":    cacheSyncPerformed,
		"cache_sync_queued":       cacheSyncQueued,
		"cache_synced_at":         cacheSyncAt,
		"cache_sync_error":        cacheSyncError,
		"cache_markets_loaded":    cacheMarketsLoaded,
		"skipped_sports":          skippedSports,
		"skipped_crypto":          skippedCrypto,
		"skipped_stocks":          skippedStocks,
		"skipped_existing_orders": skippedExistingOrders,
		"skipped_existing_shares": skippedExistingShares,
		"skipped_low_volume":      skippedLowVolume,
		"skipped_recent_notes":    skippedRecentNotes,
		"skipped_far_resolution":  skippedFarResolution,
		"skipped_expired":         skippedExpired,
		"skipped_missing_end":     skippedMissingEnd,
		"skipped_stale":           skippedStale,
		"stale_only":              staleOnly,
		"min_volume":              builtinPolymarketFindMarketsMinVolume,
		"recent_note_days":        builtinPolymarketFindMarketsRecentNoteDays,
		"max_resolution_date":     resolutionCutoff.Format("2006-01-02"),
	}, nil
}

func builtinPolymarketFindMarketsLimit(params map[string]any, usdcBalance, aum float64) int {
	limit, ok := intParam(params, "limit")
	if !ok || limit <= 0 {
		if aum > 0 && usdcBalance > 0 {
			limit = int(usdcBalance / (aum / 40))
		}
		if limit < builtinPolymarketFindMarketsDefaultLimit {
			limit = builtinPolymarketFindMarketsDefaultLimit
		}
	}
	if limit > builtinPolymarketFindMarketsMaxLimit {
		limit = builtinPolymarketFindMarketsMaxLimit
	}
	return limit
}

func builtinPolymarketFindMarketsSearchLimit(limit int) int {
	if limit <= 0 {
		limit = builtinPolymarketFindMarketsDefaultLimit
	}
	searchLimit := limit * 4
	if searchLimit < 30 {
		searchLimit = 30
	}
	if searchLimit > builtinPolymarketFindMarketsMaxSearchWindow {
		searchLimit = builtinPolymarketFindMarketsMaxSearchWindow
	}
	return searchLimit
}

func builtinPolymarketFindMarketQueries(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	return []string{query}
}

func builtinPolymarketFindMarketsScanMode(query string) string {
	if strings.TrimSpace(query) != "" {
		return "query"
	}
	return "all_markets"
}

func sortBuiltinPolymarketMarketsNewestFirst(markets []polymarket.Market) {
	sort.SliceStable(markets, func(i, j int) bool {
		return builtinPolymarketMarketNewestFirstLess(markets[i], markets[j])
	})
}

func builtinPolymarketMarketNewestFirstLess(left, right polymarket.Market) bool {
	leftNewest, leftOK := builtinPolymarketMarketNewestAt(left)
	rightNewest, rightOK := builtinPolymarketMarketNewestAt(right)
	if leftOK != rightOK {
		return leftOK
	}
	if leftOK && !leftNewest.Equal(rightNewest) {
		return leftNewest.After(rightNewest)
	}

	leftID, leftIDOK := builtinPolymarketNumericSortID(left.ID)
	rightID, rightIDOK := builtinPolymarketNumericSortID(right.ID)
	if leftIDOK != rightIDOK {
		return leftIDOK
	}
	if leftIDOK && leftID != rightID {
		return leftID > rightID
	}
	return false
}

func builtinPolymarketMarketNewestAt(market polymarket.Market) (time.Time, bool) {
	for _, raw := range []string{
		strings.TrimSpace(market.CreatedAt),
		strings.TrimSpace(market.CreationDate),
		strings.TrimSpace(market.StartDateISO),
		strings.TrimSpace(market.StartDate),
	} {
		if timestamp, ok := parseBuiltinPolymarketTime(raw); ok {
			return timestamp.UTC(), true
		}
	}
	return time.Time{}, false
}

func builtinPolymarketMarketVolume(market polymarket.Market) float64 {
	return parseBuiltinPolymarketFloatString(market.Volume)
}

func builtinPolymarketNumericSortID(raw string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func buildBuiltinPolymarketFindMarketResult(market polymarket.Market, endAt time.Time, lastNoteAt string, noteCount int, score, spread, daysToEnd, shareCapacity, aum float64) map[string]any {
	tokens, probability := buildBuiltinPolymarketFindMarketTokens(market)
	result := map[string]any{
		"source":           "polymarket",
		"id":               strings.TrimSpace(market.ID),
		"condition_id":     strings.TrimSpace(market.ConditionID),
		"question":         strings.TrimSpace(market.Question),
		"description":      strings.TrimSpace(market.Description),
		"slug":             strings.TrimSpace(market.Slug),
		"probability":      roundBuiltinPolymarketFloat(probability, 4),
		"end_date":         endAt.UTC().Format("2006-01-02"),
		"resolution_date":  endAt.UTC().Format("2006-01-02"),
		"tokens":           tokens,
		"volume":           roundBuiltinPolymarketFloat(parseBuiltinPolymarketFloatString(market.Volume), 2),
		"volume_24hr":      roundBuiltinPolymarketFloat(math.Max(market.Volume24hr, 0), 2),
		"liquidity":        roundBuiltinPolymarketFloat(parseBuiltinPolymarketFloatString(market.Liquidity), 2),
		"best_bid":         roundBuiltinPolymarketFloat(math.Max(market.BestBid, 0), 4),
		"best_ask":         roundBuiltinPolymarketFloat(math.Max(market.BestAsk, 0), 4),
		"spread":           roundBuiltinPolymarketFloat(math.Max(spread, 0), 4),
		"days_to_end":      roundBuiltinPolymarketFloat(math.Max(daysToEnd, 0), 2),
		"selection_score":  roundBuiltinPolymarketFloat(math.Max(score, 0), 4),
		"neg_risk":         market.NegRisk,
		"current_position": 0.0,
		"note_count":       noteCount,
	}
	if strings.TrimSpace(lastNoteAt) != "" {
		result["last_note_at"] = strings.TrimSpace(lastNoteAt)
	}
	if shareCapacity > 0 {
		result["max_allowed"] = roundBuiltinPolymarketFloat(shareCapacity, 2)
		result["remaining_capacity"] = roundBuiltinPolymarketFloat(shareCapacity, 2)
	}
	if aum > 0 {
		result["aum"] = roundBuiltinPolymarketFloat(aum, 2)
	}
	return result
}

func builtinPolymarketFindMarketScore(now time.Time, market polymarket.Market, endAt time.Time, lastNoteAt string) (float64, float64, float64) {
	volume := math.Max(builtinPolymarketMarketVolume(market), 0)
	liquidity := math.Max(parseBuiltinPolymarketFloatString(market.Liquidity), 0)
	volume24hr := math.Max(market.Volume24hr, 0)
	spread := builtinPolymarketMarketSpread(market)
	daysToEnd := math.Max(endAt.Sub(now).Hours()/24, 0)

	volumeScore := builtinPolymarketNormalizedLogScore(volume, 250000)
	liquidityScore := builtinPolymarketNormalizedLogScore(liquidity, 50000)
	activityScore := builtinPolymarketNormalizedLogScore(volume24hr, 50000)
	spreadScore := builtinPolymarketSpreadScore(spread)
	timeScore := builtinPolymarketResolutionWindowScore(daysToEnd)
	noteFreshnessScore := builtinPolymarketNoteFreshnessScore(now, lastNoteAt)
	recencyScore := builtinPolymarketListingRecencyScore(now, market)

	score := (volumeScore * 0.24) +
		(liquidityScore * 0.24) +
		(activityScore * 0.15) +
		(spreadScore * 0.14) +
		(timeScore * 0.13) +
		(noteFreshnessScore * 0.06) +
		(recencyScore * 0.04)
	return roundBuiltinPolymarketFloat(score, 6), spread, daysToEnd
}

func builtinPolymarketMarketSpread(market polymarket.Market) float64 {
	bestAsk := math.Max(market.BestAsk, 0)
	bestBid := math.Max(market.BestBid, 0)
	if bestAsk > 0 && bestBid > 0 && bestAsk >= bestBid {
		return roundBuiltinPolymarketFloat(bestAsk-bestBid, 6)
	}

	prices := decodeBuiltinPolymarketFloatList(strings.TrimSpace(market.OutcomePrices))
	if len(prices) >= 2 {
		yes := clampBuiltinPolymarketProbability(prices[0])
		no := clampBuiltinPolymarketProbability(prices[1])
		derived := math.Abs((yes + no) - 1)
		if derived > 0 {
			return roundBuiltinPolymarketFloat(derived, 6)
		}
	}
	return 0
}

func builtinPolymarketNormalizedLogScore(value, target float64) float64 {
	if value <= 0 || target <= 0 {
		return 0
	}
	score := math.Log1p(value) / math.Log1p(target)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func builtinPolymarketSpreadScore(spread float64) float64 {
	switch {
	case spread <= 0:
		return 0.6
	case spread <= 0.01:
		return 1.0
	case spread <= 0.02:
		return 0.9
	case spread <= 0.04:
		return 0.7
	case spread <= 0.06:
		return 0.45
	default:
		return 0.15
	}
}

func builtinPolymarketResolutionWindowScore(daysToEnd float64) float64 {
	switch {
	case daysToEnd <= 0:
		return 0
	case daysToEnd <= 2:
		return 0.35
	case daysToEnd <= 14:
		return 1.0
	case daysToEnd <= 45:
		return 0.85
	case daysToEnd <= 90:
		return 0.6
	default:
		return 0.25
	}
}

func builtinPolymarketNoteFreshnessScore(now time.Time, lastNoteAt string) float64 {
	lastNoteAt = strings.TrimSpace(lastNoteAt)
	if lastNoteAt == "" {
		return 1.0
	}
	parsed, ok := parseBuiltinPolymarketTime(lastNoteAt)
	if !ok {
		return 0.5
	}
	days := math.Max(now.Sub(parsed).Hours()/24, 0)
	if days >= 21 {
		return 1.0
	}
	return math.Max(days/21, 0.05)
}

func builtinPolymarketListingRecencyScore(now time.Time, market polymarket.Market) float64 {
	createdAt, ok := builtinPolymarketMarketNewestAt(market)
	if !ok {
		return 0.4
	}
	days := math.Max(now.Sub(createdAt).Hours()/24, 0)
	switch {
	case days <= 1:
		return 1.0
	case days <= 7:
		return 0.85
	case days <= 30:
		return 0.6
	default:
		return 0.3
	}
}

func builtinPolymarketMarketLooksSports(market polymarket.Market) bool {
	if builtinPolymarketMarketTagMatches(market, "sports") {
		return true
	}
	if builtinPolymarketMarketTextMatches(market,
		" nba ",
		" nfl ",
		" mlb ",
		" nhl ",
		" ncaa ",
		" soccer ",
		" football ",
		" baseball ",
		" basketball ",
		" tennis ",
		" golf ",
		" cricket ",
		" ufc ",
		" mma ",
		" formula 1 ",
		" f1 ",
		" nascar ",
		" olympics ",
		" fifa ",
		" premier league ",
		" champions league ",
		" super bowl ",
		" world cup ",
		" world series ",
		" stanley cup ",
		" march madness ",
		" serie a ",
		" la liga ",
		" bundesliga ",
		" copa del rey ",
		" europa league ",
		" conference league ",
		" club world cup ",
		" fa cup ",
		" epl ",
		" mls ",
	) {
		return true
	}

	normalized := builtinPolymarketNormalizeSearchText(strings.Join([]string{
		strings.TrimSpace(market.Question),
		strings.TrimSpace(market.Description),
		strings.TrimSpace(market.Slug),
	}, " "))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, " listed club ") {
		return true
	}
	if strings.Contains(normalized, " officially crowned the winner ") {
		return true
	}
	if strings.Contains(normalized, " league winner ") {
		return true
	}
	if strings.Contains(normalized, " will ") && strings.Contains(normalized, " win the ") {
		if strings.Contains(normalized, " league ") || strings.Contains(normalized, " cup ") || strings.Contains(normalized, " finals ") || strings.Contains(normalized, " title ") {
			return true
		}
	}
	return false
}

func builtinPolymarketMarketLooksCrypto(market polymarket.Market) bool {
	if builtinPolymarketMarketTagMatches(market, "crypto", "crypto-prices", "bitcoin", "ethereum", "solana") {
		return true
	}
	return builtinPolymarketMarketTextMatches(market,
		" crypto ",
		" cryptocurrency ",
		" bitcoin ",
		" btc ",
		" ethereum ",
		" eth ",
		" solana ",
		" xrp ",
		" dogecoin ",
		" doge ",
		" cardano ",
		" ada ",
		" memecoin ",
		" token price ",
		" coin price ",
	)
}

func builtinPolymarketMarketLooksStocks(market polymarket.Market) bool {
	if builtinPolymarketMarketTagMatches(market, "stock-prices", "stocks", "equities", "stock-market") {
		return true
	}
	return builtinPolymarketMarketTextMatches(market,
		" stock price ",
		" stock prices ",
		" share price ",
		" share prices ",
		" equities ",
		" equity ",
		" stock market ",
		" close at ",
	)
}

func builtinPolymarketMarketTagMatches(market polymarket.Market, slugs ...string) bool {
	if len(market.Tags) == 0 || len(slugs) == 0 {
		return false
	}
	allowed := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(strings.ToLower(slug))
		if slug != "" {
			allowed[slug] = struct{}{}
		}
	}
	for _, tag := range market.Tags {
		slug := strings.TrimSpace(strings.ToLower(tag.Slug))
		label := strings.TrimSpace(strings.ToLower(tag.Label))
		if _, ok := allowed[slug]; ok {
			return true
		}
		if _, ok := allowed[label]; ok {
			return true
		}
	}
	return false
}

func builtinPolymarketMarketTextMatches(market polymarket.Market, keywords ...string) bool {
	normalized := builtinPolymarketNormalizeSearchText(strings.Join([]string{
		strings.TrimSpace(market.Question),
		strings.TrimSpace(market.Description),
		strings.TrimSpace(market.Slug),
	}, " "))
	if normalized == "" {
		return false
	}
	for _, keyword := range keywords {
		if strings.Contains(normalized, builtinPolymarketNormalizeSearchText(keyword)) {
			return true
		}
	}
	return false
}

func builtinPolymarketNormalizeSearchText(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw) + 2)
	b.WriteByte(' ')
	lastSpace := true
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	if !lastSpace {
		b.WriteByte(' ')
	}
	return b.String()
}

func buildBuiltinPolymarketFindMarketTokens(market polymarket.Market) ([]map[string]any, float64) {
	outcomes := decodeBuiltinPolymarketStringList(market.Outcomes)
	tokenIDs := decodeBuiltinPolymarketStringList(market.ClobTokenIDs)
	prices := decodeBuiltinPolymarketFloatList(market.OutcomePrices)

	count := len(outcomes)
	if len(tokenIDs) > count {
		count = len(tokenIDs)
	}
	if len(prices) > count {
		count = len(prices)
	}

	tokens := make([]map[string]any, 0, count)
	probability := -1.0
	for i := 0; i < count; i++ {
		outcome := ""
		if i < len(outcomes) {
			outcome = strings.TrimSpace(outcomes[i])
		}
		tokenID := ""
		if i < len(tokenIDs) {
			tokenID = strings.TrimSpace(tokenIDs[i])
		}
		token := map[string]any{
			"outcome":  outcome,
			"token_id": tokenID,
		}
		if i < len(prices) {
			price := clampBuiltinPolymarketProbability(prices[i])
			token["price"] = roundBuiltinPolymarketFloat(price, 4)
			if probability < 0 && normalizeBuiltinPolymarketSide(outcome) == "yes" {
				probability = price
			}
		}
		tokens = append(tokens, token)
	}

	if probability < 0 && len(prices) > 0 {
		probability = clampBuiltinPolymarketProbability(prices[0])
	}
	if probability < 0 {
		probability = 0
	}
	return tokens, probability
}

func builtinPolymarketRecentNoteStillApplies(market polymarket.Market, note *data.MarketNote) bool {
	metadata := data.ParseMarketNoteMetadata(note)
	if metadata == nil {
		return true
	}

	if metadata.MarketFingerprint != "" && metadata.MarketFingerprint != builtinPolymarketMarketFingerprint(market) {
		return false
	}

	if metadata.ResolutionDate != "" {
		if notedEndAt, notedOK := parseBuiltinPolymarketTime(metadata.ResolutionDate); notedOK {
			if currentEndAt, currentOK := parseBuiltinPolymarketTime(market.EndDate); currentOK && math.Abs(currentEndAt.Sub(notedEndAt).Hours()) >= 12 {
				return false
			}
		}
	}

	if metadata.MarketProbability != nil {
		currentProbability := builtinPolymarketMarketProbability(market)
		if currentProbability >= 0 && math.Abs(currentProbability-*metadata.MarketProbability) >= 0.05 {
			return false
		}
	}

	if metadata.MarketVolume != nil {
		currentVolume := math.Max(builtinPolymarketMarketVolume(market), 0)
		if currentVolume >= *metadata.MarketVolume*1.35 && currentVolume >= *metadata.MarketVolume+10000 {
			return false
		}
	}

	if metadata.MarketVolume24hr != nil {
		currentVolume24hr := math.Max(market.Volume24hr, 0)
		if currentVolume24hr >= *metadata.MarketVolume24hr*1.5 && currentVolume24hr >= *metadata.MarketVolume24hr+5000 {
			return false
		}
	}

	if metadata.Spread != nil {
		currentSpread := builtinPolymarketMarketSpread(market)
		if math.Abs(currentSpread-*metadata.Spread) >= 0.04 {
			return false
		}
	}

	return true
}

func builtinPolymarketMarketProbability(market polymarket.Market) float64 {
	_, probability := buildBuiltinPolymarketFindMarketTokens(market)
	if probability < 0 {
		return -1
	}
	return roundBuiltinPolymarketFloat(probability, 6)
}

func builtinPolymarketMarketFingerprint(market polymarket.Market) string {
	return polymarketStableHash(
		strings.TrimSpace(market.ConditionID),
		polymarketStableText(market.Question),
		polymarketStableText(market.EndDate),
		polymarketStableText(market.Outcomes),
		polymarketStableText(market.ClobTokenIDs),
	)
}

var builtinPolymarketStaleKeys = []string{
	"last_managed_at",
	"last_policy_check",
	"last_researched_at",
}

func builtinPolymarketMarketRecentlyTouched(props []*data.MarketProperty, cutoff time.Time) bool {
	for _, prop := range props {
		if prop == nil || prop.ValueType != data.MarketPropertyTypeDatetime {
			continue
		}
		key := strings.TrimSpace(prop.Key)
		match := false
		for _, staleKey := range builtinPolymarketStaleKeys {
			if key == staleKey {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(prop.Value))
		if err != nil {
			continue
		}
		if ts.After(cutoff) {
			return true
		}
	}
	return false
}

func builtinPolymarketDaysUntil(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	parsed, ok := parseBuiltinPolymarketTime(raw)
	if !ok {
		return 0
	}
	return math.Max(parsed.Sub(time.Now().UTC()).Hours()/24, 0)
}
