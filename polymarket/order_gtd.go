package gowild_polymarket

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ExpirationNotFutureError indicates a GTD expiration timestamp at or before the
// current time. Callers should skip placement rather than submit an order that is
// already expired.
type ExpirationNotFutureError struct {
	Expiration int64
	Now        int64
}

func (e *ExpirationNotFutureError) Error() string {
	return fmt.Sprintf("GTD expiration %d is not in the future (now %d)", e.Expiration, e.Now)
}

// buildSignedGTDOrder builds and signs a GTD order that expires at an arbitrary
// absolute Unix timestamp, honoring the client's funder and signature type. The
// expiration is part of the signed EIP-712 payload, so the signature commits to
// the exact expiration rather than a fixed TTL relative to "now". Non-future,
// zero, or negative expirations are rejected with ExpirationNotFutureError and no
// signed order is produced.
func (c *Client) buildSignedGTDOrder(tokenID string, price, size float64, side string, negRisk bool, tickSize float64, expirationUnix int64) (*signedOrder, error) {
	now := time.Now().UTC().Unix()
	if expirationUnix <= now {
		return nil, &ExpirationNotFutureError{Expiration: expirationUnix, Now: now}
	}

	order, err := buildOrder(c.privateKey, tokenID, price, size, side, c.funder, c.signatureType, c.chainID, negRisk, tickSize)
	if err != nil {
		return nil, err
	}
	order.Expiration = strconv.FormatInt(expirationUnix, 10)

	sig, err := signOrder(c.privateKey, order, c.chainID, negRisk)
	if err != nil {
		return nil, fmt.Errorf("failed to sign GTD order: %w", err)
	}
	order.Signature = sig
	return order, nil
}

// buildGTDPlaceRequest builds (without submitting) a GTD place request with a
// custom absolute expiration. negRisk and tickSize are supplied explicitly so the
// builder can run without network access; PlaceOrderWithExpiration resolves them
// from the CLOB API before calling this.
func (c *Client) buildGTDPlaceRequest(tokenID string, price, size float64, side string, negRisk bool, tickSize float64, expirationUnix int64) (*placeOrderRequest, error) {
	order, err := c.buildSignedGTDOrder(tokenID, price, size, side, negRisk, tickSize, expirationUnix)
	if err != nil {
		return nil, err
	}
	owner := ""
	if c.creds != nil {
		owner = c.creds.APIKey
	}
	return &placeOrderRequest{
		Order:     order,
		OrderType: GTD,
		Owner:     owner,
	}, nil
}

// PlaceOrderWithExpiration places a GTD limit order that expires exactly at
// expirationUnix. It queries the CLOB API for the authoritative negRisk and tick
// size (matching PlaceOrder), rejects non-future expirations, and re-signs so the
// signature commits to the custom expiration. It performs the same automatic
// allowance setup retry as PlaceOrder.
func (c *Client) PlaceOrderWithExpiration(ctx context.Context, tokenID string, price, size float64, side string, expirationUnix int64) (*PlaceOrderResponse, error) {
	if c.creds == nil {
		return nil, c.authUnavailableError("PlaceOrderWithExpiration")
	}
	if now := time.Now().UTC().Unix(); expirationUnix <= now {
		return nil, &ExpirationNotFutureError{Expiration: expirationUnix, Now: now}
	}

	negRisk, err := c.getNegRisk(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to check neg-risk: %w", err)
	}
	tickSize, err := c.getTickSize(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tick size: %w", err)
	}

	req, err := c.buildGTDPlaceRequest(tokenID, price, size, side, negRisk, tickSize, expirationUnix)
	if err != nil {
		return nil, fmt.Errorf("failed to build GTD order: %w", err)
	}

	return c.submitOrderWithAllowanceSetup(ctx, *req, side, tokenID)
}

// submitOrderWithAllowanceSetup submits an order and, if it is rejected for a
// missing allowance, performs the automatic allowance setup retry (mirroring
// PlaceOrder) before resubmitting once.
func (c *Client) submitOrderWithAllowanceSetup(ctx context.Context, req placeOrderRequest, side, tokenID string) (*PlaceOrderResponse, error) {
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
	if refreshErr != nil && resp != nil && strings.TrimSpace(resp.ErrorMsg) != "" {
		resp.ErrorMsg = fmt.Sprintf("%s (balance cache refresh failed: %v)", strings.TrimSpace(resp.ErrorMsg), refreshErr)
	}
	return resp, nil
}
