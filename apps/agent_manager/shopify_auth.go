package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

const (
	defaultShopifyAPIVersion = "2025-01"
	shopifyTokenRefreshSkew  = 2 * time.Minute
)

var shopifyTokenHTTPClient = &http.Client{Timeout: 30 * time.Second}

func normalizeShopifyShopURL(raw string) string {
	shopURL := strings.TrimSpace(raw)
	shopURL = strings.TrimPrefix(shopURL, "https://")
	shopURL = strings.TrimPrefix(shopURL, "http://")
	shopURL = strings.TrimSuffix(shopURL, "/")
	if slash := strings.Index(shopURL, "/"); slash >= 0 {
		shopURL = shopURL[:slash]
	}
	return strings.ToLower(strings.TrimSpace(shopURL))
}

func normalizeShopifyAPIVersion(raw string) string {
	apiVersion := strings.TrimSpace(raw)
	if apiVersion == "" {
		return defaultShopifyAPIVersion
	}
	return apiVersion
}

func hasCompanyShopifyClientCredentials(conn *data.CompanyShopifyConnection) bool {
	if conn == nil {
		return false
	}
	return strings.TrimSpace(conn.ClientID) != "" && strings.TrimSpace(conn.ClientSecretEnc) != ""
}

func hasUsableCachedShopifyToken(conn *data.CompanyShopifyConnection, now time.Time) bool {
	if conn == nil || strings.TrimSpace(conn.AccessTokenEnc) == "" {
		return false
	}
	if conn.AccessTokenExpAt.IsZero() {
		return false
	}
	return now.Add(shopifyTokenRefreshSkew).Before(conn.AccessTokenExpAt)
}

func resolveCompanyShopifyAccessToken(ctx context.Context, db gowild_data.Database, conn *data.CompanyShopifyConnection) (string, error) {
	if conn == nil {
		return "", fmt.Errorf("company shopify connection is missing")
	}
	now := time.Now()
	if hasUsableCachedShopifyToken(conn, now) {
		return strings.TrimSpace(conn.AccessTokenEnc), nil
	}
	if !hasCompanyShopifyClientCredentials(conn) {
		return "", fmt.Errorf("shopify client credentials are incomplete")
	}

	token, expAt, err := exchangeShopifyClientCredentialsToken(ctx, conn.ShopURL, conn.ClientID, conn.ClientSecretEnc)
	if err != nil {
		return "", err
	}

	conn.AccessTokenEnc = token
	conn.AccessTokenExpAt = expAt
	if err := data.UpsertCompanyShopifyConnection(ctx, db, conn); err != nil {
		return "", fmt.Errorf("failed to persist refreshed shopify token: %w", err)
	}

	return token, nil
}

func exchangeShopifyClientCredentialsToken(ctx context.Context, shopURL, clientID, clientSecret string) (string, time.Time, error) {
	host := normalizeShopifyShopURL(shopURL)
	if host == "" {
		return "", time.Time{}, fmt.Errorf("shop_url is required")
	}
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	if clientID == "" || clientSecret == "" {
		return "", time.Time{}, fmt.Errorf("client credentials are required")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/admin/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to build shopify token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := shopifyTokenHTTPClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("shopify token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to read shopify token response: %w", err)
	}

	type tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	var parsed tokenResponse
	_ = json.Unmarshal(body, &parsed)

	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(parsed.Error)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if msg == "" {
			msg = "request failed"
		}
		return "", time.Time{}, fmt.Errorf("shopify token exchange failed (%d): %s", resp.StatusCode, msg)
	}

	token := strings.TrimSpace(parsed.AccessToken)
	if token == "" {
		return "", time.Time{}, fmt.Errorf("shopify token exchange did not return access_token")
	}

	expAt := time.Time{}
	if parsed.ExpiresIn > 0 {
		expAt = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return token, expAt, nil
}
