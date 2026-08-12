package gowild_ytmusic

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	apiBase       = "https://music.youtube.com/youtubei/v1"
	origin        = "https://music.youtube.com"
	clientVersion = "1.20260801.01.00"
	userAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// maxResponse bounds one browse response read. Library pages run to a few MB
// of renderer JSON; a response beyond this is not a browse response.
const maxResponse = 32 << 20

// ErrAuthExpired marks a browse rejected as unauthenticated (HTTP 401/403 or
// the equivalent error payload). The stored cookie must be re-captured from a
// logged-in browser; retrying with the same credentials cannot succeed.
var ErrAuthExpired = errors.New("ytmusic: authentication expired")

// Credentials is what a caller stores to act as a YouTube Music account: the
// raw Cookie header copied from an authenticated music.youtube.com request,
// and the X-Goog-AuthUser index for multi-login browsers (empty means "0").
type Credentials struct {
	Cookie   string `json:"cookie"`
	AuthUser string `json:"x_goog_authuser"`
}

// extractSAPISID pulls the cookie value SAPISIDHASH signs with, preferring
// the __Secure-3PAPISID name modern Google sessions set over the legacy
// SAPISID fallback.
func extractSAPISID(cookie string) (string, error) {
	fallback := ""
	for _, part := range strings.Split(cookie, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || value == "" {
			continue
		}
		switch name {
		case "__Secure-3PAPISID":
			return value, nil
		case "SAPISID":
			fallback = value
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("ytmusic: cookie contains neither __Secure-3PAPISID nor SAPISID; copy the full Cookie header from a logged-in music.youtube.com request")
}

// sapisidHash builds the full Authorization header value Google's frontends
// require: "SAPISIDHASH <unix_ts>_<hex(sha1("<unix_ts> <SAPISID> <origin>"))>".
func sapisidHash(sapisid string, t time.Time) string {
	ts := t.Unix()
	sum := sha1.Sum(fmt.Appendf(nil, "%d %s %s", ts, sapisid, origin))
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, sum)
}

// Client speaks InnerTube browse as the WEB_REMIX (music web player) client.
type Client struct {
	creds   *Credentials
	sapisid string
	httpc   *http.Client
}

// Option configures a Client at construction.
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client (30s timeout) — how tests
// inject a stub transport.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpc = h }
}

// NewClient validates the credentials up front — nil creds or a cookie
// without a SAPISID is a construction error, not a per-request surprise.
func NewClient(creds *Credentials, opts ...Option) (*Client, error) {
	if creds == nil {
		return nil, fmt.Errorf("ytmusic: nil credentials")
	}
	sapisid, err := extractSAPISID(creds.Cookie)
	if err != nil {
		return nil, err
	}
	c := &Client{
		creds:   creds,
		sapisid: sapisid,
		httpc:   &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Client) authUser() string {
	if c.creds.AuthUser != "" {
		return c.creds.AuthUser
	}
	return "0"
}

// browse POSTs one InnerTube browse request — body carries the
// request-specific keys (browseId, continuation, ...) and the WEB_REMIX
// context is merged in here — and returns the decoded response.
// Authentication rejections, whether an HTTP 401/403 status or a 200 carrying
// an {"error": {"code": 401|403}} payload, come back wrapping ErrAuthExpired.
func (c *Client) browse(ctx context.Context, body map[string]any) (map[string]any, error) {
	payload := make(map[string]any, len(body)+1)
	for k, v := range body {
		payload[k] = v
	}
	payload["context"] = map[string]any{
		"client": map[string]any{
			"clientName":    "WEB_REMIX",
			"clientVersion": clientVersion,
			"hl":            "en",
		},
		"user": map[string]any{},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ytmusic: browse: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/browse?alt=json", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: browse: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", c.creds.Cookie)
	// The hash is bound to its timestamp, so it is recomputed per request.
	req.Header.Set("Authorization", sapisidHash(c.sapisid, time.Now()))
	req.Header.Set("Origin", origin)
	req.Header.Set("X-Origin", origin)
	req.Header.Set("X-Goog-AuthUser", c.authUser())
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ytmusic: browse: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: browse: read response (HTTP %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("ytmusic: browse: HTTP %d: %w", resp.StatusCode, ErrAuthExpired)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ytmusic: browse: HTTP %d: %s", resp.StatusCode, bodySnippet(raw))
		}
		return nil, fmt.Errorf("ytmusic: browse: decode response: %w", err)
	}

	// InnerTube reports auth failures as HTTP 200 with a top-level error
	// object; the payload code is as authoritative as the status line.
	if errObj, ok := navMap(decoded, "error"); ok {
		code, _ := navInt(errObj, "code")
		message, _ := navString(errObj, "message")
		if code == http.StatusUnauthorized || code == http.StatusForbidden {
			return nil, fmt.Errorf("ytmusic: browse: error %d %q: %w", code, message, ErrAuthExpired)
		}
		return nil, fmt.Errorf("ytmusic: browse: error %d (HTTP %d): %s", code, resp.StatusCode, message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ytmusic: browse: HTTP %d: %s", resp.StatusCode, bodySnippet(raw))
	}
	return decoded, nil
}

// bodySnippet trims a response body to something an error message can carry.
func bodySnippet(raw []byte) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > 300 {
		return s[:300] + "..."
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}
