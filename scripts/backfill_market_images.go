// backfill_market_images populates image/icon columns in polymarket_market_cache
// by fetching from the Gamma API for rows that are missing them.
//
// Usage: go run scripts/backfill_market_images.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

const gammaBaseURL = "https://gamma-api.polymarket.com"

type market struct {
	ConditionID string `json:"condition_id"`
	Image       string `json:"image"`
	Icon        string `json:"icon"`
}

func main() {
	_ = godotenv.Load()
	_ = godotenv.Load("apps/agent_manager/.env")

	dbURL := os.Getenv("GOWILD_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gowild_agent:gowild_agent@localhost:5432/gowild_agent"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	// Ensure columns exist
	for _, col := range []string{"image", "icon"} {
		_, _ = pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE polymarket_market_cache ADD COLUMN IF NOT EXISTS %s TEXT DEFAULT ''`, col))
	}

	rows, err := pool.Query(ctx, `SELECT id FROM polymarket_market_cache WHERE (image IS NULL OR image = '') AND (icon IS NULL OR icon = '')`)
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Fatalf("scan failed: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	fmt.Printf("Found %d markets missing images\n", len(ids))

	client := &http.Client{Timeout: 10 * time.Second}
	updated := 0

	for i, conditionID := range ids {
		image, icon, err := fetchMarketImage(ctx, client, conditionID)
		if err != nil {
			fmt.Printf("  [%d/%d] %s — error: %v\n", i+1, len(ids), conditionID[:min(16, len(conditionID))], err)
			continue
		}
		if image == "" && icon == "" {
			fmt.Printf("  [%d/%d] %s — no image available\n", i+1, len(ids), conditionID[:min(16, len(conditionID))])
			continue
		}

		_, err = pool.Exec(ctx, `UPDATE polymarket_market_cache SET image = $1, icon = $2 WHERE id = $3`, image, icon, conditionID)
		if err != nil {
			fmt.Printf("  [%d/%d] %s — update failed: %v\n", i+1, len(ids), conditionID[:min(16, len(conditionID))], err)
			continue
		}
		updated++
		fmt.Printf("  [%d/%d] %s — updated\n", i+1, len(ids), conditionID[:min(16, len(conditionID))])

		// Rate limit
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("Done. Updated %d/%d markets.\n", updated, len(ids))
}

func fetchMarketImage(ctx context.Context, client *http.Client, conditionID string) (image, icon string, err error) {
	params := url.Values{}
	params.Set("condition_ids", conditionID)
	u := gammaBaseURL + "/markets?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var markets []market
	if err := json.Unmarshal(body, &markets); err != nil {
		return "", "", err
	}
	if len(markets) == 0 {
		return "", "", nil
	}
	return strings.TrimSpace(markets[0].Image), strings.TrimSpace(markets[0].Icon), nil
}
