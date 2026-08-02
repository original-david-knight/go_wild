package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/tools/supplier/providers/cjdropshipping"
)

const cjdropshippingTokenRefreshSkew = 2 * time.Minute

func cjdropshippingAPIBaseURL() string {
	if raw := strings.TrimSpace(os.Getenv("CJDROPSHIPPING_BASE_URL")); raw != "" {
		return strings.TrimRight(raw, "/")
	}
	return cjdropshipping.DefaultBaseURL
}

func newCJDropshippingAPIClient(accessToken, platformToken string) *cjdropshipping.Client {
	opts := []cjdropshipping.Option{
		cjdropshipping.WithBaseURL(cjdropshippingAPIBaseURL()),
	}
	if strings.TrimSpace(platformToken) != "" {
		opts = append(opts, cjdropshipping.WithPlatformToken(strings.TrimSpace(platformToken)))
	}
	return cjdropshipping.NewClient(strings.TrimSpace(accessToken), opts...)
}

func hasUsableCachedCJDropshippingToken(conn *data.CompanyCJDropshippingConnection, now time.Time) bool {
	if conn == nil {
		return false
	}
	if strings.TrimSpace(conn.AccessTokenEnc) == "" {
		return false
	}
	if conn.AccessTokenExpAt.IsZero() {
		// Manual token entry can omit expiry; treat as usable until API rejects it.
		return true
	}
	return now.Add(cjdropshippingTokenRefreshSkew).Before(conn.AccessTokenExpAt)
}

func canRefreshCJDropshippingToken(conn *data.CompanyCJDropshippingConnection, now time.Time) bool {
	if conn == nil {
		return false
	}
	if strings.TrimSpace(conn.RefreshTokenEnc) == "" {
		return false
	}
	if conn.RefreshTokenExpAt.IsZero() {
		return true
	}
	return now.Add(cjdropshippingTokenRefreshSkew).Before(conn.RefreshTokenExpAt)
}

func resolveCompanyCJDropshippingAccessToken(ctx context.Context, db gowild_data.Database, conn *data.CompanyCJDropshippingConnection) (string, error) {
	if conn == nil {
		return "", fmt.Errorf("company cjdropshipping connection is missing")
	}

	now := time.Now()
	if hasUsableCachedCJDropshippingToken(conn, now) {
		return strings.TrimSpace(conn.AccessTokenEnc), nil
	}

	client := newCJDropshippingAPIClient("", conn.PlatformTokenEnc)

	if canRefreshCJDropshippingToken(conn, now) {
		refreshed, err := client.RefreshAccessToken(ctx, strings.TrimSpace(conn.RefreshTokenEnc))
		if err == nil {
			return persistCompanyCJDropshippingToken(ctx, db, conn, refreshed)
		}
		if strings.TrimSpace(conn.APIKeyEnc) == "" {
			return "", fmt.Errorf("failed to refresh cjdropshipping access token: %w", err)
		}
	}

	apiKey := strings.TrimSpace(conn.APIKeyEnc)
	if apiKey == "" {
		return "", fmt.Errorf("cjdropshipping credentials are incomplete")
	}

	tokenResp, err := client.GetAccessToken(ctx, apiKey)
	if err != nil {
		return "", fmt.Errorf("failed to fetch cjdropshipping access token: %w", err)
	}
	return persistCompanyCJDropshippingToken(ctx, db, conn, tokenResp)
}

func persistCompanyCJDropshippingToken(ctx context.Context, db gowild_data.Database, conn *data.CompanyCJDropshippingConnection, tokenResp *cjdropshipping.TokenResponse) (string, error) {
	if conn == nil {
		return "", fmt.Errorf("company cjdropshipping connection is missing")
	}
	if tokenResp == nil {
		return "", fmt.Errorf("empty cjdropshipping token response")
	}
	accessToken := strings.TrimSpace(tokenResp.AccessToken)
	if accessToken == "" {
		return "", fmt.Errorf("cjdropshipping token response did not include access_token")
	}

	conn.AccessTokenEnc = accessToken
	if exp := tokenResp.AccessTokenExpiresAt(); !exp.IsZero() {
		conn.AccessTokenExpAt = exp
	}

	if refresh := strings.TrimSpace(tokenResp.RefreshToken); refresh != "" {
		conn.RefreshTokenEnc = refresh
	}
	if exp := tokenResp.RefreshTokenExpiresAt(); !exp.IsZero() {
		conn.RefreshTokenExpAt = exp
	}

	if err := data.UpsertCompanyCJDropshippingConnection(ctx, db, conn); err != nil {
		return "", fmt.Errorf("failed to persist cjdropshipping token: %w", err)
	}

	return accessToken, nil
}
