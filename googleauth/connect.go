package googleauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/original-david-knight/go_wild/oauth2app"
	"golang.org/x/oauth2"
)

// ConnectResult is what a completed consent produced.
type ConnectResult struct {
	// Account is the local name the token was stored under.
	Account string
	// Email is the Google account that actually consented. Worth reporting
	// back: asking for "work" and signing in as the personal account is an
	// easy mistake, and the only way to catch it is to say what landed.
	Email string
	// Scopes are what the grant actually carries, which is not necessarily
	// what was requested — a user can decline individual scopes.
	Scopes []string
}

// Opener is handed the consent URL. A CLI opens a browser; a test asserts.
type Opener = oauth2app.Opener

// ConnectOptions tune the flow. The zero value is the normal case.
type ConnectOptions struct {
	// Timeout bounds the wait for the user to finish consenting.
	Timeout time.Duration
	// Port pins the loopback port. Zero picks a free one, which is what an
	// installed app should do.
	Port int
	// UserInfoURL overrides the endpoint used to resolve the account's email.
	// Tests point this at a stub.
	UserInfoURL string
}

const defaultUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// Connect runs the installed-app authorization-code flow — oauth2app's
// loopback machinery under Google's provider configuration: it starts a
// loopback listener, hands the consent URL to opener, waits for Google to
// redirect back with a code, exchanges it, and stores the token under
// `account`. The listener binds 127.0.0.1 — the redirect must never be
// reachable off the machine, because the authorization code arrives in its
// query string.
func (r *Registry) Connect(ctx context.Context, account string, opener Opener, opts ConnectOptions) (*ConnectResult, error) {
	name, err := normalizeAccount(account)
	if err != nil {
		return nil, err
	}
	// A web client cannot consent at a runtime-chosen loopback port; only the
	// hosted ceremony (Registry.Hosted) fits it.
	if r.cfg.Web {
		return nil, ErrWebClient
	}
	tok, err := r.flow().ConnectLoopback(ctx, name, opener, oauth2app.LoopbackOptions{
		Timeout: opts.Timeout, Port: opts.Port,
	})
	if err != nil {
		return nil, err
	}
	return r.connectResult(ctx, name, tok, opts.UserInfoURL), nil
}

// flow is oauth2app's machinery under Google's provider configuration.
func (r *Registry) flow() *oauth2app.Flow {
	return &oauth2app.Flow{
		Config: r.oauthConfig(""),
		Store:  r.store,
		// AccessTypeOffline is what produces a refresh token at all, and
		// ApprovalForce makes Google reissue one even when the user has
		// consented before — without it, a second connect of an
		// already-granted account returns no refresh token and the account
		// dies at the next restart.
		AuthCodeOptions: []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.ApprovalForce},
	}
}

// connectResult reports what a completed consent actually connected, whichever
// ceremony ran it. tok must be the exchange's own token: the granted scopes
// live in its extra fields, which a store round-trip does not keep.
func (r *Registry) connectResult(ctx context.Context, account string, tok *oauth2.Token, userInfoURL string) *ConnectResult {
	out := &ConnectResult{Account: account, Scopes: grantedScopes(tok)}
	// Resolving the email is a convenience, not the point of the flow: a
	// failure here must not discard a token that was granted and stored.
	if email, err := r.accountEmail(ctx, tok, userInfoURL); err == nil {
		out.Email = email
	}
	return out
}

// grantedScopes reads the scopes the token actually carries. A user can
// decline individual boxes, and a partial grant fails later in a way that is
// hard to trace back here.
func grantedScopes(tok *oauth2.Token) []string {
	raw, _ := tok.Extra("scope").(string)
	if raw == "" {
		return nil
	}
	return splitScopes(raw)
}

func splitScopes(raw string) []string {
	var out []string
	current := ""
	for _, r := range raw {
		if r == ' ' || r == ',' {
			if current != "" {
				out = append(out, current)
				current = ""
			}
			continue
		}
		current += string(r)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func (r *Registry) accountEmail(ctx context.Context, tok *oauth2.Token, endpoint string) (string, error) {
	if endpoint == "" {
		endpoint = defaultUserInfoURL
	}
	client := r.oauthConfig("").Client(ctx, tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned %s", resp.Status)
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Email, nil
}
