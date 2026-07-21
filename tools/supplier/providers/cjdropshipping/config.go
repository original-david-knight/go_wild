package cjdropshipping

import (
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the default API base for CJ API 2.0.
	DefaultBaseURL = "https://developers.cjdropshipping.com/api2.0/v1"

	defaultHTTPTimeout = 30 * time.Second
)

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the CJ API base URL.
func WithBaseURL(raw string) Option {
	return func(c *Client) {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			c.baseURL = strings.TrimRight(trimmed, "/")
		}
	}
}

// WithPlatformToken sets the optional platform token header used on order creation.
func WithPlatformToken(token string) Option {
	return func(c *Client) {
		c.platformToken = strings.TrimSpace(token)
	}
}
