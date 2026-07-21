package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

const (
	polymarketMarketCacheSyncInterval     = 30 * time.Minute
	polymarketMarketCacheEventsPageSize   = 100
	polymarketMarketCacheLastSyncSetting  = "polymarket_market_cache_last_sync_at"
	polymarketMarketCacheLastErrorSetting = "polymarket_market_cache_last_sync_error"
)

var polymarketMarketCacheSyncMu sync.Mutex
var polymarketMarketCacheRefreshMu sync.Mutex
var polymarketMarketCacheRefreshInFlight bool

type polymarketCachedMarket struct {
	ID              string    `json:"id"`
	MarketID        string    `json:"market_id"`
	EventID         string    `json:"event_id"`
	EventTitle      string    `json:"event_title"`
	EventSlug       string    `json:"event_slug"`
	Question        string    `json:"question"`
	Description     string    `json:"description"`
	CreatedAt       string    `json:"created_at"`
	CreationDate    string    `json:"creation_date"`
	StartDate       string    `json:"start_date"`
	StartDateISO    string    `json:"start_date_iso"`
	Slug            string    `json:"slug"`
	EndDate         string    `json:"end_date"`
	Volume          string    `json:"volume"`
	Liquidity       string    `json:"liquidity"`
	OutcomePrices   string    `json:"outcome_prices"`
	Outcomes        string    `json:"outcomes"`
	ClobTokenIDs    string    `json:"clob_token_ids"`
	Active          bool      `json:"active"`
	Closed          bool      `json:"closed"`
	AcceptingOrders bool      `json:"accepting_orders"`
	NegRisk         bool      `json:"neg_risk"`
	BestBid         float64   `json:"best_bid"`
	BestAsk         float64   `json:"best_ask"`
	Volume24hr      float64   `json:"volume_24hr"`
	TagsJSON        string    `json:"tags_json"`
	TagSlugs        string    `json:"tag_slugs"`
	SearchText      string    `json:"search_text"`
	IsSports        bool      `json:"is_sports"`
	IsCrypto        bool      `json:"is_crypto"`
	Image           string    `json:"image"`
	Icon            string    `json:"icon"`
	IsStocks        bool      `json:"is_stocks"`
	SyncedAt        time.Time `json:"synced_at"`
}

func (polymarketCachedMarket) TableName() string { return "polymarket_market_cache" }

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		return db.AddTable(polymarketCachedMarket{})
	})
}

type polymarketMarketCacheStatus struct {
	Used          bool
	SyncPerformed bool
	SyncQueued    bool
	LastSyncAt    string
	SyncError     string
	MarketsLoaded int
}

func loadPolymarketMarketCacheCandidates(ctx context.Context, db gowild_data.Database, client builtinPolymarketClient, query string) ([]polymarket.Market, polymarketMarketCacheStatus, error) {
	var status polymarketMarketCacheStatus
	if db == nil || client == nil {
		return nil, status, nil
	}
	fresh, freshErr := polymarketMarketCacheIsFresh(ctx, db)
	if freshErr != nil {
		return nil, status, freshErr
	}
	if lastSyncAt, err := GetSetting(ctx, db, polymarketMarketCacheLastSyncSetting); err == nil {
		status.LastSyncAt = strings.TrimSpace(lastSyncAt)
	}
	if syncErrRaw, err := GetSetting(ctx, db, polymarketMarketCacheLastErrorSetting); err == nil {
		status.SyncError = strings.TrimSpace(syncErrRaw)
	}

	loadRows := func() ([]any, error) {
		return db.Table(polymarketCachedMarket{}).Query(ctx, gowild_data.QueryOpts{
			Where: map[string]any{
				"active":           true,
				"closed":           false,
				"accepting_orders": true,
			},
		})
	}

	rows, err := loadRows()
	if err != nil {
		return nil, status, err
	}
	if !fresh {
		if len(rows) == 0 {
			syncPerformed, syncErr := maybeSyncPolymarketMarketCache(ctx, db, client)
			status.SyncPerformed = syncPerformed
			if syncErr != nil {
				status.SyncError = strings.TrimSpace(syncErr.Error())
			}
			if lastSyncAt, err := GetSetting(ctx, db, polymarketMarketCacheLastSyncSetting); err == nil {
				status.LastSyncAt = strings.TrimSpace(lastSyncAt)
			}
			rows, err = loadRows()
			if err != nil {
				return nil, status, err
			}
			if len(rows) == 0 && syncErr != nil {
				return nil, status, syncErr
			}
		} else {
			status.SyncQueued = queuePolymarketMarketCacheRefresh(db, client)
		}
	}

	if len(rows) == 0 {
		return nil, status, nil
	}

	status.Used = true
	cached := make([]*polymarketCachedMarket, 0, len(rows))
	for _, row := range rows {
		if item, ok := row.(*polymarketCachedMarket); ok {
			cached = append(cached, item)
		}
	}
	sortPolymarketCachedMarkets(cached)

	normalizedQuery := builtinPolymarketNormalizeSearchText(query)
	terms := strings.Fields(strings.TrimSpace(normalizedQuery))
	markets := make([]polymarket.Market, 0, len(cached))
	for _, item := range cached {
		if !polymarketCachedMarketMatchesQuery(item, normalizedQuery, terms) {
			continue
		}
		markets = append(markets, cachedPolymarketMarketToLive(item))
	}
	status.MarketsLoaded = len(markets)
	return markets, status, nil
}

func queuePolymarketMarketCacheRefresh(db gowild_data.Database, client builtinPolymarketClient) bool {
	if db == nil || client == nil {
		return false
	}
	polymarketMarketCacheRefreshMu.Lock()
	if polymarketMarketCacheRefreshInFlight {
		polymarketMarketCacheRefreshMu.Unlock()
		return false
	}
	polymarketMarketCacheRefreshInFlight = true
	polymarketMarketCacheRefreshMu.Unlock()

	go func() {
		defer func() {
			polymarketMarketCacheRefreshMu.Lock()
			polymarketMarketCacheRefreshInFlight = false
			polymarketMarketCacheRefreshMu.Unlock()
		}()
		refreshCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = maybeSyncPolymarketMarketCache(refreshCtx, db, client)
	}()
	return true
}

func loadPolymarketCachedMarketsByConditionIDs(ctx context.Context, db gowild_data.Database, conditionIDs []string) (map[string]polymarket.Market, error) {
	if db == nil {
		return map[string]polymarket.Market{}, nil
	}
	normalized := make([]string, 0, len(conditionIDs))
	seen := make(map[string]struct{}, len(conditionIDs))
	for _, conditionID := range conditionIDs {
		conditionID = strings.TrimSpace(conditionID)
		if conditionID == "" {
			continue
		}
		if _, ok := seen[conditionID]; ok {
			continue
		}
		seen[conditionID] = struct{}{}
		normalized = append(normalized, conditionID)
	}
	if len(normalized) == 0 {
		return map[string]polymarket.Market{}, nil
	}

	values := make([]any, 0, len(normalized))
	for _, conditionID := range normalized {
		values = append(values, conditionID)
	}

	rows, err := db.Table(polymarketCachedMarket{}).Query(ctx, gowild_data.QueryOpts{
		WhereIn: map[string][]any{"id": values},
	})
	if err != nil {
		return nil, err
	}

	markets := make(map[string]polymarket.Market, len(rows))
	for _, row := range rows {
		item, ok := row.(*polymarketCachedMarket)
		if !ok || item == nil {
			continue
		}
		markets[strings.TrimSpace(item.ID)] = cachedPolymarketMarketToLive(item)
	}
	return markets, nil
}

func maybeSyncPolymarketMarketCache(ctx context.Context, db gowild_data.Database, client builtinPolymarketClient) (bool, error) {
	if db == nil || client == nil {
		return false, nil
	}
	if fresh, err := polymarketMarketCacheIsFresh(ctx, db); err == nil && fresh {
		return false, nil
	}

	polymarketMarketCacheSyncMu.Lock()
	defer polymarketMarketCacheSyncMu.Unlock()

	if fresh, err := polymarketMarketCacheIsFresh(ctx, db); err == nil && fresh {
		return false, nil
	}

	now := time.Now().UTC()
	err := db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		table := tx.Table(polymarketCachedMarket{})
		if table == nil {
			return fmt.Errorf("polymarket market cache table not available")
		}

		existingRows, err := table.GetAll(ctx)
		if err != nil {
			return err
		}
		existing := make(map[string]*polymarketCachedMarket, len(existingRows))
		for _, row := range existingRows {
			if item, ok := row.(*polymarketCachedMarket); ok {
				existing[item.ID] = item
			}
		}

		seen := make(map[string]struct{}, len(existing))
		offset := 0
		for {
			events, err := client.ListEvents(ctx, polymarketMarketCacheEventsPageSize, offset)
			if err != nil {
				return err
			}
			if len(events) == 0 {
				break
			}

			for _, event := range events {
				for _, market := range event.Markets {
					item := buildPolymarketCachedMarket(event, market, now)
					if item == nil {
						continue
					}
					seen[item.ID] = struct{}{}
					if _, ok := existing[item.ID]; ok {
						if err := table.Update(ctx, item); err != nil {
							return err
						}
					} else {
						if err := table.Insert(ctx, item); err != nil {
							return err
						}
					}
					existing[item.ID] = item
				}
			}

			if len(events) < polymarketMarketCacheEventsPageSize {
				break
			}
			offset += len(events)
		}

		for id := range existing {
			if _, ok := seen[id]; ok {
				continue
			}
			if err := table.Delete(ctx, id); err != nil {
				return err
			}
		}

		if err := SetSetting(ctx, tx, polymarketMarketCacheLastSyncSetting, now.Format(time.RFC3339)); err != nil {
			return err
		}
		if err := SetSetting(ctx, tx, polymarketMarketCacheLastErrorSetting, ""); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = SetSetting(ctx, db, polymarketMarketCacheLastErrorSetting, strings.TrimSpace(err.Error()))
		return true, err
	}
	return true, nil
}

func polymarketMarketCacheIsFresh(ctx context.Context, db gowild_data.Database) (bool, error) {
	if db == nil {
		return false, nil
	}
	lastSyncRaw, err := GetSetting(ctx, db, polymarketMarketCacheLastSyncSetting)
	if err != nil || strings.TrimSpace(lastSyncRaw) == "" {
		return false, nil
	}
	lastSyncAt, err := time.Parse(time.RFC3339, strings.TrimSpace(lastSyncRaw))
	if err != nil {
		return false, nil
	}
	if time.Since(lastSyncAt) >= polymarketMarketCacheSyncInterval {
		return false, nil
	}
	rows, err := db.Table(polymarketCachedMarket{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"active":           true,
			"closed":           false,
			"accepting_orders": true,
		},
		Limit: 1,
	})
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func buildPolymarketCachedMarket(event polymarket.Event, market polymarket.Market, syncedAt time.Time) *polymarketCachedMarket {
	conditionID := strings.TrimSpace(market.ConditionID)
	if conditionID == "" {
		return nil
	}
	mergedTags := mergePolymarketTags(event.Tags, market.Tags)
	tagSlugs := polymarketTagSlugs(mergedTags)
	tagsJSON := mustJSON(mergedTags)
	tagText := make([]string, 0, len(mergedTags)*2)
	for _, tag := range mergedTags {
		if label := strings.TrimSpace(tag.Label); label != "" {
			tagText = append(tagText, label)
		}
		if slug := strings.TrimSpace(tag.Slug); slug != "" {
			tagText = append(tagText, slug)
		}
	}
	enriched := market
	enriched.Tags = mergedTags
	searchText := builtinPolymarketNormalizeSearchText(strings.Join([]string{
		strings.TrimSpace(event.Title),
		strings.TrimSpace(event.Slug),
		strings.TrimSpace(market.Question),
		strings.TrimSpace(market.Description),
		strings.TrimSpace(market.Slug),
		strings.Join(tagText, " "),
	}, " "))
	return &polymarketCachedMarket{
		ID:              conditionID,
		MarketID:        strings.TrimSpace(market.ID),
		EventID:         strings.TrimSpace(event.ID),
		EventTitle:      strings.TrimSpace(event.Title),
		EventSlug:       strings.TrimSpace(event.Slug),
		Question:        strings.TrimSpace(market.Question),
		Description:     strings.TrimSpace(market.Description),
		CreatedAt:       strings.TrimSpace(market.CreatedAt),
		CreationDate:    strings.TrimSpace(market.CreationDate),
		StartDate:       strings.TrimSpace(market.StartDate),
		StartDateISO:    strings.TrimSpace(market.StartDateISO),
		Slug:            strings.TrimSpace(market.Slug),
		EndDate:         strings.TrimSpace(market.EndDate),
		Volume:          strings.TrimSpace(market.Volume),
		Liquidity:       strings.TrimSpace(market.Liquidity),
		OutcomePrices:   strings.TrimSpace(market.OutcomePrices),
		Outcomes:        strings.TrimSpace(market.Outcomes),
		ClobTokenIDs:    strings.TrimSpace(market.ClobTokenIDs),
		Active:          market.Active,
		Closed:          market.Closed,
		AcceptingOrders: market.AcceptingOrders,
		NegRisk:         market.NegRisk,
		BestBid:         market.BestBid,
		BestAsk:         market.BestAsk,
		Volume24hr:      market.Volume24hr,
		TagsJSON:        tagsJSON,
		TagSlugs:        strings.Join(tagSlugs, " "),
		SearchText:      searchText,
		Image:           strings.TrimSpace(market.Image),
		Icon:            strings.TrimSpace(market.Icon),
		IsSports:        builtinPolymarketMarketLooksSports(enriched),
		IsCrypto:        builtinPolymarketMarketLooksCrypto(enriched),
		IsStocks:        builtinPolymarketMarketLooksStocks(enriched),
		SyncedAt:        syncedAt,
	}
}

func mergePolymarketTags(slices ...[]polymarket.Tag) []polymarket.Tag {
	seen := make(map[string]struct{})
	merged := make([]polymarket.Tag, 0)
	for _, tags := range slices {
		for _, tag := range tags {
			slug := strings.TrimSpace(strings.ToLower(tag.Slug))
			label := strings.TrimSpace(strings.ToLower(tag.Label))
			key := slug
			if key == "" {
				key = label
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, polymarket.Tag{
				ID:    strings.TrimSpace(tag.ID),
				Label: strings.TrimSpace(tag.Label),
				Slug:  strings.TrimSpace(tag.Slug),
			})
		}
	}
	return merged
}

func polymarketTagSlugs(tags []polymarket.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		value := strings.TrimSpace(strings.ToLower(tag.Slug))
		if value == "" {
			value = strings.TrimSpace(strings.ToLower(tag.Label))
		}
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func sortPolymarketCachedMarkets(items []*polymarketCachedMarket) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		leftNewest, leftOK := builtinPolymarketCachedMarketNewestAt(left)
		rightNewest, rightOK := builtinPolymarketCachedMarketNewestAt(right)
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && !leftNewest.Equal(rightNewest) {
			return leftNewest.After(rightNewest)
		}

		leftLiquidity := parseBuiltinPolymarketFloatString(left.Liquidity)
		rightLiquidity := parseBuiltinPolymarketFloatString(right.Liquidity)
		if leftLiquidity != rightLiquidity {
			return leftLiquidity > rightLiquidity
		}
		if left.Volume24hr != right.Volume24hr {
			return left.Volume24hr > right.Volume24hr
		}
		leftVolume := parseBuiltinPolymarketFloatString(left.Volume)
		rightVolume := parseBuiltinPolymarketFloatString(right.Volume)
		if leftVolume != rightVolume {
			return leftVolume > rightVolume
		}
		return left.Question < right.Question
	})
}

func builtinPolymarketCachedMarketNewestAt(item *polymarketCachedMarket) (time.Time, bool) {
	if item == nil {
		return time.Time{}, false
	}
	for _, raw := range []string{
		strings.TrimSpace(item.CreatedAt),
		strings.TrimSpace(item.CreationDate),
		strings.TrimSpace(item.StartDateISO),
		strings.TrimSpace(item.StartDate),
	} {
		if timestamp, ok := parseBuiltinPolymarketTime(raw); ok {
			return timestamp.UTC(), true
		}
	}
	return time.Time{}, false
}

func polymarketCachedMarketMatchesQuery(item *polymarketCachedMarket, normalizedQuery string, terms []string) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(normalizedQuery) == "" || len(terms) == 0 {
		return true
	}
	searchText := strings.TrimSpace(item.SearchText)
	if searchText == "" {
		return false
	}
	for _, term := range terms {
		if term == "" {
			continue
		}
		if !strings.Contains(searchText, term) {
			return false
		}
	}
	return true
}

func cachedPolymarketMarketToLive(item *polymarketCachedMarket) polymarket.Market {
	market := polymarket.Market{
		ID:              strings.TrimSpace(item.MarketID),
		Question:        strings.TrimSpace(item.Question),
		Description:     strings.TrimSpace(item.Description),
		ConditionID:     strings.TrimSpace(item.ID),
		CreatedAt:       strings.TrimSpace(item.CreatedAt),
		CreationDate:    strings.TrimSpace(item.CreationDate),
		StartDate:       strings.TrimSpace(item.StartDate),
		StartDateISO:    strings.TrimSpace(item.StartDateISO),
		Slug:            strings.TrimSpace(item.Slug),
		Active:          item.Active,
		Closed:          item.Closed,
		EndDate:         strings.TrimSpace(item.EndDate),
		Volume:          strings.TrimSpace(item.Volume),
		Liquidity:       strings.TrimSpace(item.Liquidity),
		OutcomePrices:   strings.TrimSpace(item.OutcomePrices),
		Outcomes:        strings.TrimSpace(item.Outcomes),
		ClobTokenIDs:    strings.TrimSpace(item.ClobTokenIDs),
		AcceptingOrders: item.AcceptingOrders,
		NegRisk:         item.NegRisk,
		BestBid:         item.BestBid,
		BestAsk:         item.BestAsk,
		Image:           strings.TrimSpace(item.Image),
		Icon:            strings.TrimSpace(item.Icon),
		Volume24hr:      item.Volume24hr,
	}
	if strings.TrimSpace(item.TagsJSON) == "" {
		return market
	}
	var tags []polymarket.Tag
	if err := json.Unmarshal([]byte(item.TagsJSON), &tags); err == nil {
		market.Tags = tags
	}
	return market
}
