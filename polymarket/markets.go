package gowild_polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Event represents a Polymarket event from the Gamma API (contains nested markets).
type Event struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Slug    string   `json:"slug"`
	Tags    []Tag    `json:"tags"`
	Markets []Market `json:"markets"`
}

// searchResponse is the response from the Gamma /public-search endpoint.
type searchResponse struct {
	Events []Event `json:"events"`
}

// MarketPage is one cursor-paginated page of Gamma markets.
type MarketPage struct {
	Markets    []Market
	NextCursor string
}

// ListEvents returns a page of active Polymarket events with nested markets and tags.
func (c *Client) ListEvents(ctx context.Context, limit, offset int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("active", "true")
	params.Set("closed", "false")
	params.Set("order", "id")
	params.Set("ascending", "false")

	data, err := c.getPublic(ctx, gammaBaseURL, "/events", params)
	if err != nil {
		return nil, fmt.Errorf("list events failed: %w", err)
	}

	var payloads []eventPayload
	if err := json.Unmarshal(data, &payloads); err != nil {
		return nil, fmt.Errorf("failed to decode events response: %w", err)
	}

	events := make([]Event, 0, len(payloads))
	for _, payload := range payloads {
		event := Event{
			ID:    payload.ID,
			Title: payload.Title,
			Slug:  payload.Slug,
			Tags:  append([]Tag(nil), payload.Tags...),
		}
		event.Markets = make([]Market, 0, len(payload.Markets))
		for _, market := range payload.Markets {
			event.Markets = append(event.Markets, market.toMarket())
		}
		events = append(events, event)
	}
	return events, nil
}

// SearchMarkets searches for markets by query string via the Gamma public-search API (public, no auth).
func (c *Client) SearchMarkets(ctx context.Context, query string, limit int) ([]Market, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("limit_per_type", fmt.Sprintf("%d", limit))

	data, err := c.getPublic(ctx, gammaBaseURL, "/public-search", params)
	if err != nil {
		return nil, fmt.Errorf("search markets failed: %w", err)
	}

	var resp searchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	// Flatten: extract markets from events
	var markets []Market
	for _, e := range resp.Events {
		for _, m := range e.Markets {
			if m.Active && !m.Closed && m.AcceptingOrders {
				if len(m.Tags) == 0 && len(e.Tags) > 0 {
					m.Tags = append([]Tag(nil), e.Tags...)
				}
				markets = append(markets, m)
			}
		}
		if len(markets) >= limit {
			markets = markets[:limit]
			break
		}
	}
	return markets, nil
}

type eventPayload struct {
	ID      string               `json:"id"`
	Title   string               `json:"title"`
	Slug    string               `json:"slug"`
	Tags    []Tag                `json:"tags"`
	Markets []eventMarketPayload `json:"markets"`
}

type eventMarketPayload struct {
	ID               string          `json:"id"`
	Question         string          `json:"question"`
	Description      string          `json:"description"`
	ConditionID      string          `json:"conditionId"`
	CreatedAt        string          `json:"createdAt"`
	CreationDate     string          `json:"creationDate"`
	StartDate        string          `json:"startDate"`
	StartDateISO     string          `json:"startDateIso"`
	Slug             string          `json:"slug"`
	Image            string          `json:"image"`
	Icon             string          `json:"icon"`
	Active           bool            `json:"active"`
	Closed           bool            `json:"closed"`
	EndDate          string          `json:"endDate"`
	EndDateISO       string          `json:"endDateIso"`
	Volume           json.RawMessage `json:"volume"`
	Liquidity        json.RawMessage `json:"liquidity"`
	OutcomePrices    string          `json:"outcomePrices"`
	Outcomes         string          `json:"outcomes"`
	ClobTokenIDs     string          `json:"clobTokenIds"`
	AcceptingOrders  bool            `json:"acceptingOrders"`
	NegRisk          bool            `json:"negRisk"`
	BestBid          json.RawMessage `json:"bestBid"`
	BestAsk          json.RawMessage `json:"bestAsk"`
	Volume24hr       json.RawMessage `json:"volume24hr"`
	Tags             []Tag           `json:"tags"`
	NegRiskMarketID  string          `json:"negRiskMarketID"`
	NegRiskRequestID string          `json:"negRiskRequestID"`
}

func (m eventMarketPayload) toMarket() Market {
	endDate := strings.TrimSpace(m.EndDateISO)
	if endDate == "" {
		endDate = strings.TrimSpace(m.EndDate)
	}
	return Market{
		ID:               strings.TrimSpace(m.ID),
		Question:         strings.TrimSpace(m.Question),
		Description:      strings.TrimSpace(m.Description),
		ConditionID:      strings.TrimSpace(m.ConditionID),
		CreatedAt:        strings.TrimSpace(m.CreatedAt),
		CreationDate:     strings.TrimSpace(m.CreationDate),
		StartDate:        strings.TrimSpace(m.StartDate),
		StartDateISO:     strings.TrimSpace(m.StartDateISO),
		Slug:             strings.TrimSpace(m.Slug),
		Image:            strings.TrimSpace(m.Image),
		Icon:             strings.TrimSpace(m.Icon),
		Active:           m.Active,
		Closed:           m.Closed,
		EndDate:          endDate,
		Volume:           decodeScalarString(m.Volume),
		Liquidity:        decodeScalarString(m.Liquidity),
		OutcomePrices:    strings.TrimSpace(m.OutcomePrices),
		Outcomes:         strings.TrimSpace(m.Outcomes),
		ClobTokenIDs:     strings.TrimSpace(m.ClobTokenIDs),
		AcceptingOrders:  m.AcceptingOrders,
		NegRisk:          m.NegRisk,
		NegRiskMarketID:  strings.TrimSpace(m.NegRiskMarketID),
		NegRiskRequestID: strings.TrimSpace(m.NegRiskRequestID),
		BestBid:          decodeScalarFloat(m.BestBid),
		BestAsk:          decodeScalarFloat(m.BestAsk),
		Volume24hr:       decodeScalarFloat(m.Volume24hr),
		Tags:             append([]Tag(nil), m.Tags...),
	}
}

func marketPayloadsToMarkets(payloads []eventMarketPayload) []Market {
	markets := make([]Market, 0, len(payloads))
	for _, payload := range payloads {
		markets = append(markets, payload.toMarket())
	}
	return markets
}

func decodeScalarString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return strings.TrimSpace(asNumber.String())
	}
	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return strings.TrimSpace(fmt.Sprintf("%v", asFloat))
	}
	return ""
}

func decodeScalarFloat(raw json.RawMessage) float64 {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return asFloat
	}
	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		if parsed, parseErr := asNumber.Float64(); parseErr == nil {
			return parsed
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(asString), 64); parseErr == nil {
			return parsed
		}
	}
	return 0
}

// ListMarketsClosingBetween returns a page of active, unresolved markets whose
// close date falls within [minClose, maxClose], filtered server-side by close
// date and minimum liquidity and ordered by close date ascending (soonest-closing
// first). This lets a caller scan exactly the markets in its close-time window
// instead of paging the entire active set. minLiquidity <= 0 omits the liquidity
// filter. Zero-value times omit the corresponding bound.
func (c *Client) ListMarketsClosingBetween(ctx context.Context, minClose, maxClose time.Time, minLiquidity float64, limit, offset int) ([]Market, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("active", "true")
	params.Set("closed", "false")
	params.Set("order", "endDate")
	params.Set("ascending", "true")
	if !minClose.IsZero() {
		params.Set("end_date_min", minClose.UTC().Format(time.RFC3339))
	}
	if !maxClose.IsZero() {
		params.Set("end_date_max", maxClose.UTC().Format(time.RFC3339))
	}
	if minLiquidity > 0 {
		params.Set("liquidity_num_min", strconv.FormatFloat(minLiquidity, 'f', -1, 64))
	}

	data, err := c.getPublic(ctx, gammaBaseURL, "/markets", params)
	if err != nil {
		return nil, fmt.Errorf("list markets (close window) failed: %w", err)
	}

	var markets []Market
	if err := json.Unmarshal(data, &markets); err != nil {
		return nil, fmt.Errorf("failed to decode markets response: %w", err)
	}
	return markets, nil
}

// ListMarketsClosingBetweenKeyset returns a cursor-paginated page of active,
// unresolved markets whose close date falls within [minClose, maxClose]. Gamma
// rejects offset pagination for deep scans; callers should pass the previous
// response's NextCursor as afterCursor.
func (c *Client) ListMarketsClosingBetweenKeyset(ctx context.Context, minClose, maxClose time.Time, minLiquidity float64, limit int, afterCursor string) (MarketPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("active", "true")
	params.Set("closed", "false")
	params.Set("order", "endDate")
	params.Set("ascending", "true")
	if cursor := strings.TrimSpace(afterCursor); cursor != "" {
		params.Set("after_cursor", cursor)
	}
	if !minClose.IsZero() {
		params.Set("end_date_min", minClose.UTC().Format(time.RFC3339))
	}
	if !maxClose.IsZero() {
		params.Set("end_date_max", maxClose.UTC().Format(time.RFC3339))
	}
	if minLiquidity > 0 {
		params.Set("liquidity_num_min", strconv.FormatFloat(minLiquidity, 'f', -1, 64))
	}

	data, err := c.getPublic(ctx, gammaBaseURL, "/markets/keyset", params)
	if err != nil {
		return MarketPage{}, fmt.Errorf("list markets keyset (close window) failed: %w", err)
	}

	var resp struct {
		Markets    []eventMarketPayload `json:"markets"`
		NextCursor string               `json:"next_cursor"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return MarketPage{}, fmt.Errorf("failed to decode markets keyset response: %w", err)
	}
	return MarketPage{
		Markets:    marketPayloadsToMarkets(resp.Markets),
		NextCursor: strings.TrimSpace(resp.NextCursor),
	}, nil
}

// ListMarkets returns a page of active Polymarket markets via the Gamma API (public, no auth).
func (c *Client) ListMarkets(ctx context.Context, limit, offset int) ([]Market, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("active", "true")
	params.Set("closed", "false")
	params.Set("order", "id")
	params.Set("ascending", "false")

	data, err := c.getPublic(ctx, gammaBaseURL, "/markets", params)
	if err != nil {
		return nil, fmt.Errorf("list markets failed: %w", err)
	}

	var markets []Market
	if err := json.Unmarshal(data, &markets); err != nil {
		return nil, fmt.Errorf("failed to decode markets response: %w", err)
	}
	return markets, nil
}

// GetMarket returns a single market by condition ID via the Gamma API (public, no auth).
func (c *Client) GetMarket(ctx context.Context, conditionID string) (*Market, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, fmt.Errorf("condition ID is required")
	}

	params := url.Values{}
	params.Set("condition_ids", conditionID)

	data, err := c.getPublic(ctx, gammaBaseURL, "/markets", params)
	if err != nil {
		return nil, fmt.Errorf("get market failed: %w", err)
	}

	// Use eventMarketPayload for decoding so we capture both endDate and
	// endDateIso — the Gamma API may return either or both depending on the market.
	var payloads []eventMarketPayload
	if err := json.Unmarshal(data, &payloads); err != nil {
		return nil, fmt.Errorf("failed to decode market: %w", err)
	}
	for _, p := range payloads {
		if strings.EqualFold(strings.TrimSpace(p.ConditionID), conditionID) {
			m := p.toMarket()
			return &m, nil
		}
	}
	return nil, fmt.Errorf("market not found: %s", conditionID)
}
