package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// getMe fetches bot information.
func (t *TelegramTools) getMe(ctx context.Context) (*botInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/getMe", nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error: %s", result.Description)
	}

	return &botInfo{
		ID:        result.Result.ID,
		FirstName: result.Result.FirstName,
		Username:  result.Result.Username,
	}, nil
}
