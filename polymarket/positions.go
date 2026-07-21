package gowild_polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	positionsPageLimit = 500
	positionsMaxOffset = 10000
)

// GetPositions returns the account's open positions via the Data API.
func (c *Client) GetPositions(ctx context.Context) ([]Position, error) {
	user := strings.TrimSpace(c.accountAddress())
	if user == "" {
		return nil, fmt.Errorf("position user address is empty")
	}

	var all []Position
	for offset := 0; offset <= positionsMaxOffset; offset += positionsPageLimit {
		params := url.Values{}
		params.Set("user", user)
		params.Set("limit", strconv.Itoa(positionsPageLimit))
		params.Set("offset", strconv.Itoa(offset))

		data, err := c.getPublic(ctx, dataBaseURL, "/positions", params)
		if err != nil {
			return nil, fmt.Errorf("get positions failed: %w", err)
		}

		var batch []Position
		if err := json.Unmarshal(data, &batch); err != nil {
			return nil, fmt.Errorf("failed to decode positions: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < positionsPageLimit {
			return all, nil
		}
	}

	return nil, fmt.Errorf("positions pagination exceeded max offset %d", positionsMaxOffset)
}

// GetPositionsValue returns Polymarket's aggregate marked value for the account's
// open positions via the Data API.
func (c *Client) GetPositionsValue(ctx context.Context) (float64, error) {
	user := strings.TrimSpace(c.accountAddress())
	if user == "" {
		return 0, fmt.Errorf("position value user address is empty")
	}

	params := url.Values{}
	params.Set("user", user)

	data, err := c.getPublic(ctx, dataBaseURL, "/value", params)
	if err != nil {
		return 0, fmt.Errorf("get positions value failed: %w", err)
	}

	var values []struct {
		User  string          `json:"user"`
		Value float64OrString `json:"value"`
	}
	if err := json.Unmarshal(data, &values); err != nil {
		return 0, fmt.Errorf("failed to decode positions value: %w", err)
	}
	if len(values) == 0 {
		return 0, nil
	}

	for _, entry := range values {
		if strings.EqualFold(strings.TrimSpace(entry.User), user) {
			return float64(entry.Value), nil
		}
	}
	return float64(values[0].Value), nil
}

// GetTrades returns the account's trade history via the Data API.
func (c *Client) GetTrades(ctx context.Context, limit int) ([]Trade, error) {
	params := url.Values{}
	params.Set("user", c.accountAddress())
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}

	data, err := c.getPublic(ctx, dataBaseURL, "/trades", params)
	if err != nil {
		return nil, fmt.Errorf("get trades failed: %w", err)
	}

	var trades []Trade
	if err := json.Unmarshal(data, &trades); err != nil {
		return nil, fmt.Errorf("failed to decode trades: %w", err)
	}
	return trades, nil
}
