package gowild_polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const defaultGTDOrderTTL = 3 * time.Hour

func (c *Client) buildSignedOrderForType(tokenID string, price, size float64, side, orderType string, negRisk bool, tickSize float64) (*signedOrder, error) {
	switch strings.ToUpper(strings.TrimSpace(orderType)) {
	case GTD:
		expirationUnix := time.Now().UTC().Add(defaultGTDOrderTTL).Unix()
		return buildOrderWithExpiration(c.privateKey, tokenID, price, size, side, c.chainID, negRisk, expirationUnix, tickSize)
	default:
		return buildOrder(c.privateKey, tokenID, price, size, side, c.funder, c.signatureType, c.chainID, negRisk, tickSize)
	}
}

// buildAndPreviewOrder builds an order request without sending it, for debugging.
// Like PlaceOrder, it queries the CLOB API for the authoritative negRisk and tick size values.
func (c *Client) buildAndPreviewOrder(ctx context.Context, tokenID string, price, size float64, side string, orderType string, _ bool) (*placeOrderRequest, error) {
	if c.creds == nil {
		return nil, c.authUnavailableError("buildAndPreviewOrder")
	}
	if orderType == "" {
		orderType = GTC
	}

	negRisk, err := c.getNegRisk(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to check neg-risk: %w", err)
	}

	tickSize, err := c.getTickSize(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tick size: %w", err)
	}

	order, err := c.buildSignedOrderForType(tokenID, price, size, side, orderType, negRisk, tickSize)
	if err != nil {
		return nil, err
	}
	return &placeOrderRequest{
		Order:     order,
		OrderType: orderType,
		Owner:     c.creds.APIKey,
	}, nil
}

// PlaceOrder places a signed order on the CLOB (L2 auth + EIP-712 signing).
// The negRisk parameter from the Gamma API is unreliable — this method queries the
// CLOB API's /neg-risk endpoint to determine the correct exchange contract for signing.
func (c *Client) PlaceOrder(ctx context.Context, tokenID string, price, size float64, side string, orderType string, _ bool) (*PlaceOrderResponse, error) {
	if c.creds == nil {
		return nil, c.authUnavailableError("PlaceOrder")
	}
	if orderType == "" {
		orderType = GTC
	}

	// Always query the CLOB API for the authoritative negRisk and tick size values
	negRisk, err := c.getNegRisk(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to check neg-risk: %w", err)
	}

	tickSize, err := c.getTickSize(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tick size: %w", err)
	}

	order, err := c.buildSignedOrderForType(tokenID, price, size, side, orderType, negRisk, tickSize)
	if err != nil {
		return nil, fmt.Errorf("failed to build order: %w", err)
	}

	req := placeOrderRequest{
		Order:     order,
		OrderType: orderType,
		Owner:     c.creds.APIKey,
	}

	resp, err := c.submitOrder(ctx, req)
	if !shouldAutoSetupAllowances(resp, err) || !c.supportsAutomaticAllowanceSetup() {
		return resp, err
	}

	if ensureErr := c.ensureTradingAllowances(ctx); ensureErr != nil {
		if err != nil {
			return nil, fmt.Errorf("%w (automatic allowance setup failed: %v)", err, ensureErr)
		}
		if resp != nil {
			msg := strings.TrimSpace(resp.ErrorMsg)
			if msg == "" {
				msg = "order rejected"
			}
			resp.ErrorMsg = fmt.Sprintf("%s (automatic allowance setup failed: %v)", msg, ensureErr)
			return resp, nil
		}
		return nil, fmt.Errorf("automatic allowance setup failed: %w", ensureErr)
	}

	refreshErr := c.refreshOrderBalanceAllowance(ctx, side, tokenID)
	resp, err = c.submitOrder(ctx, req)
	if err != nil {
		if refreshErr != nil {
			return nil, fmt.Errorf("place order failed after automatic allowance setup retry (balance cache refresh failed: %v): %w", refreshErr, err)
		}
		return nil, fmt.Errorf("place order failed after automatic allowance setup retry: %w", err)
	}
	if refreshErr != nil {
		if resp == nil {
			return nil, fmt.Errorf("order retry succeeded but balance cache refresh failed: %v", refreshErr)
		}
		if strings.TrimSpace(resp.ErrorMsg) != "" {
			resp.ErrorMsg = fmt.Sprintf("%s (balance cache refresh failed: %v)", strings.TrimSpace(resp.ErrorMsg), refreshErr)
		}
	}
	return resp, nil
}

func (c *Client) submitOrder(ctx context.Context, req placeOrderRequest) (*PlaceOrderResponse, error) {
	if c.submitOrderFn != nil {
		return c.submitOrderFn(ctx, req)
	}

	data, err := c.postAuthenticated(ctx, "/order", req)
	if err != nil {
		return nil, fmt.Errorf("place order failed: %w", err)
	}

	var resp PlaceOrderResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode order response: %w", err)
	}
	return &resp, nil
}

func shouldAutoSetupAllowances(resp *PlaceOrderResponse, err error) bool {
	if err != nil {
		return isAllowanceErrorMessage(err.Error())
	}
	if resp == nil || resp.Success {
		return false
	}
	return isAllowanceErrorMessage(resp.ErrorMsg)
}

func isAllowanceErrorMessage(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "allowance")
}

// CancelOrder cancels an existing order by ID (L2 auth).
func (c *Client) CancelOrder(ctx context.Context, orderID string) error {
	body := map[string]string{"orderID": orderID}
	data, err := c.deleteAuthenticated(ctx, "/order", body)
	if err != nil {
		return fmt.Errorf("cancel order failed: %w", err)
	}

	// Polymarket's CLOB cancel endpoint returns a payload that includes canceled IDs and
	// a not_canceled map with rejection reasons. Validate that our requested ID was
	// actually canceled so we don't report false success.
	var resp struct {
		Canceled    []string          `json:"canceled"`
		NotCanceled map[string]string `json:"not_canceled"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("cancel order failed: unexpected response: %s", string(data))
	}
	for _, id := range resp.Canceled {
		if id == orderID {
			return nil
		}
	}
	if reason, ok := resp.NotCanceled[orderID]; ok && reason != "" {
		return fmt.Errorf("cancel order rejected: %s", reason)
	}
	return fmt.Errorf("cancel order failed: order %s was not canceled: %s", orderID, string(data))
}

// endCursor is the Polymarket API sentinel for "no more pages" (base64 of "-1").
// Matches py-clob-client's END_CURSOR constant.
const endCursor = "LTE="

// GetOrders returns all orders for the authenticated user (L2 auth).
func (c *Client) GetOrders(ctx context.Context, market string) ([]Order, error) {
	var allOrders []Order
	seenCursors := map[string]struct{}{}
	const maxPages = 50

	// Build the base query path. The Polymarket API requires next_cursor as a raw
	// (non-URL-encoded) query parameter — matching the Python client which uses string
	// concatenation rather than URL encoding. We build the query string manually.
	nextCursor := "MA==" // initial cursor = base64("0"), matching py-clob-client default
	for page := 0; page < maxPages; page++ {
		path := "/data/orders?"
		if market != "" {
			path += "market=" + url.QueryEscape(market) + "&"
		}
		path += "next_cursor=" + nextCursor

		data, err := c.getAuthenticated(ctx, path, nil)
		if err != nil {
			return nil, fmt.Errorf("get orders failed: %w", err)
		}

		// Try paginated wrapper first: {"data": [...], "next_cursor": "...", "count": N}
		var wrapper struct {
			Data       []Order `json:"data"`
			NextCursor string  `json:"next_cursor"`
		}
		if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Data != nil {
			allOrders = append(allOrders, wrapper.Data...)
			nextCursor = strings.TrimSpace(wrapper.NextCursor)
			if nextCursor == "" || nextCursor == endCursor {
				return allOrders, nil
			}
			if _, seen := seenCursors[nextCursor]; seen {
				return allOrders, nil
			}
			seenCursors[nextCursor] = struct{}{}
			continue
		}

		// Fall back to bare array.
		var orders []Order
		if err := json.Unmarshal(data, &orders); err != nil {
			return nil, fmt.Errorf("failed to decode orders response: %s", string(data))
		}
		allOrders = append(allOrders, orders...)
		return allOrders, nil
	}
	return allOrders, fmt.Errorf("orders pagination exceeded %d pages", maxPages)
}

// GetOrder returns a single order by ID (L2 auth).
func (c *Client) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	data, err := c.getAuthenticated(ctx, "/data/order/"+orderID, nil)
	if err != nil {
		return nil, fmt.Errorf("get order failed: %w", err)
	}

	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("failed to decode order: %w", err)
	}
	return &order, nil
}
