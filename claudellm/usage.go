package claudellm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The account's rate-limit usage, read the way Claude Code's own /usage
// does: the OAuth token Claude Code keeps in ~/.claude/.credentials.json
// against the usage endpoint. Claude Code refreshes that token whenever it
// runs, and the runner runs it for every job, so the token is fresh in
// practice; an expired one is ErrUsageUnauthorized and the caller keeps
// its last reading. The endpoint is not a documented API — its shape is
// pinned by a test on a captured response, and only the fields the
// tracker's quota gates need are read.

// UsageURL is the endpoint the OAuth token is presented to.
const UsageURL = "https://api.anthropic.com/api/oauth/usage"

// ErrUsageUnauthorized is a token the endpoint refused: expired, or not an
// OAuth token at all.
var ErrUsageUnauthorized = errors.New("claude usage: the OAuth token was refused")

// UsageWindow is one rate-limit window: percent used and when it resets.
// A window the account does not have has a zero ResetsAt.
type UsageWindow struct {
	Used     float64
	ResetsAt time.Time
}

// ScopedUsage is a weekly window the account scopes to one model, by the
// display name the account uses ("Fable").
type ScopedUsage struct {
	Model string
	UsageWindow
}

// Usage is what the account reports: the five-hour window, the weekly
// window across models, and every model-scoped weekly window.
type Usage struct {
	Session UsageWindow
	Weekly  UsageWindow
	Scoped  []ScopedUsage
}

// UsageReader reads the account's usage. The zero value reads the
// credentials Claude Code wrote and calls the real endpoint.
type UsageReader struct {
	// CredentialsPath is the credentials file; "" is ~/.claude/.credentials.json.
	CredentialsPath string
	// URL is the endpoint; "" is UsageURL.
	URL string
	// Client is the HTTP client; nil is a client with a 20-second timeout.
	Client *http.Client
}

// Read reads the account's usage.
func (r UsageReader) Read(ctx context.Context) (*Usage, error) {
	token, err := r.token()
	if err != nil {
		return nil, err
	}
	url := r.URL
	if url == "" {
		url = UsageURL
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude usage: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("claude usage: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w (HTTP %d)", ErrUsageUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("claude usage: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return ParseUsage(body)
}

// token reads the OAuth access token out of the credentials file.
func (r UsageReader) token() (string, error) {
	path := r.CredentialsPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".claude", ".credentials.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("claude usage: %w", err)
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return "", fmt.Errorf("claude usage: %s: %w", path, err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("claude usage: %s carries no claudeAiOauth.accessToken; log in with claude", path)
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}

// ParseUsage reads the endpoint's JSON. The five-hour and weekly windows
// come from five_hour and seven_day; the scoped weekly windows from the
// limits list, where kind weekly_scoped carries the model's display name.
func ParseUsage(body []byte) (*Usage, error) {
	var raw struct {
		FiveHour *rawWindow `json:"five_hour"`
		SevenDay *rawWindow `json:"seven_day"`
		Limits   []struct {
			Kind     string    `json:"kind"`
			Percent  float64   `json:"percent"`
			ResetsAt rawTime   `json:"resets_at"`
			Scope    *rawScope `json:"scope"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("claude usage: %w", err)
	}
	if raw.FiveHour == nil || raw.SevenDay == nil {
		return nil, errors.New("claude usage: the response carries no five_hour or seven_day window")
	}
	u := &Usage{Session: raw.FiveHour.window(), Weekly: raw.SevenDay.window()}
	for _, l := range raw.Limits {
		if l.Kind != "weekly_scoped" || l.Scope == nil || l.Scope.Model == nil {
			continue
		}
		name := strings.TrimSpace(l.Scope.Model.DisplayName)
		if name == "" {
			continue
		}
		u.Scoped = append(u.Scoped, ScopedUsage{Model: name, UsageWindow: UsageWindow{Used: l.Percent, ResetsAt: time.Time(l.ResetsAt)}})
	}
	return u, nil
}

type rawWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    rawTime `json:"resets_at"`
}

func (w *rawWindow) window() UsageWindow {
	return UsageWindow{Used: w.Utilization, ResetsAt: time.Time(w.ResetsAt)}
}

type rawScope struct {
	Model *struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
}

// rawTime reads the endpoint's timestamps, which are RFC 3339 with
// fractional seconds, and null.
type rawTime time.Time

func (t *rawTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*t = rawTime(time.Time{})
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	*t = rawTime(parsed.UTC())
	return nil
}
