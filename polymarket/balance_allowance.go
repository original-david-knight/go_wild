package gowild_polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	balanceAllowanceAssetCollateral  = "COLLATERAL"
	balanceAllowanceAssetConditional = "CONDITIONAL"
)

func (c *Client) updateBalanceAllowance(ctx context.Context, assetType, tokenID string) error {
	if c.updateBalanceAllowanceFn != nil {
		return c.updateBalanceAllowanceFn(ctx, assetType, tokenID)
	}

	assetType = strings.ToUpper(strings.TrimSpace(assetType))
	if assetType == "" {
		return fmt.Errorf("asset_type is required")
	}

	params := url.Values{}
	params.Set("asset_type", assetType)
	params.Set("signature_type", strconv.Itoa(c.signatureType))
	if strings.TrimSpace(tokenID) != "" {
		params.Set("token_id", strings.TrimSpace(tokenID))
	}

	_, err := c.getAuthenticated(ctx, "/balance-allowance/update", params)
	if err != nil {
		return fmt.Errorf("update balance allowance failed: %w", err)
	}
	return nil
}

// refreshOrderBalanceAllowance asks CLOB to refresh cached balance/allowance data
// after on-chain approvals so a retry can see current values.
func (c *Client) refreshOrderBalanceAllowance(ctx context.Context, side, tokenID string) error {
	if err := c.updateBalanceAllowance(ctx, balanceAllowanceAssetCollateral, ""); err != nil {
		return err
	}

	normalizedSide := strings.ToUpper(strings.TrimSpace(side))
	if normalizedSide == Sell && strings.TrimSpace(tokenID) != "" {
		if err := c.updateBalanceAllowance(ctx, balanceAllowanceAssetConditional, tokenID); err != nil {
			return err
		}
	}

	return nil
}
