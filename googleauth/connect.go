package googleauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"

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
type Opener func(url string) error

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

// Connect runs the installed-app authorization-code flow: it starts a loopback
// listener, hands the consent URL to opener, waits for Google to redirect back
// with a code, exchanges it, and stores the token under `account`.
//
// The listener binds 127.0.0.1 — the redirect must never be reachable off the
// machine, because the authorization code arrives in its query string.
func (r *Registry) Connect(ctx context.Context, account string, opener Opener, opts ConnectOptions) (*ConnectResult, error) {
	name, err := normalizeAccount(account)
	if err != nil {
		return nil, err
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()

	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", listener.Addr().(*net.TCPAddr).Port)
	conf := r.oauthConfig(redirectURL)

	// The state parameter is what stops another page on this machine from
	// feeding a code of its own into the listener.
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	type callback struct {
		code string
		err  error
	}
	results := make(chan callback, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if got := q.Get("state"); got != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			results <- callback{err: fmt.Errorf("state mismatch: the redirect did not come from this request")}
			return
		}
		if errMsg := q.Get("error"); errMsg != "" {
			writeClosePage(w, "Consent was declined.", html.EscapeString(errMsg))
			results <- callback{err: fmt.Errorf("consent declined: %s", errMsg)}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "no code", http.StatusBadRequest)
			results <- callback{err: fmt.Errorf("redirect carried no authorization code")}
			return
		}
		writeClosePage(w, "Connected.", "You can close this tab and return to the terminal.")
		results <- callback{code: code}
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	// AccessTypeOffline is what produces a refresh token at all, and
	// ApprovalForce makes Google reissue one even when the user has consented
	// before — without it, a second connect of an already-granted account
	// returns no refresh token and the account dies at the next restart.
	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)
	if err := opener(authURL); err != nil {
		return nil, fmt.Errorf("open consent page: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var result callback
	select {
	case result = <-results:
	case <-waitCtx.Done():
		return nil, fmt.Errorf("timed out waiting for consent after %s", opts.Timeout)
	}
	if result.err != nil {
		return nil, result.err
	}

	tok, err := conf.Exchange(ctx, result.code)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("the grant carried no refresh token; the account would stop working at the next restart")
	}
	if err := r.store.Save(name, tok); err != nil {
		return nil, err
	}

	out := &ConnectResult{Account: name, Scopes: grantedScopes(tok)}
	// Resolving the email is a convenience, not the point of the flow: a
	// failure here must not discard a token that was granted and stored.
	if email, err := r.accountEmail(ctx, tok, opts.UserInfoURL); err == nil {
		out.Email = email
	}
	return out, nil
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

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// writeClosePage answers the browser with a plain page. It is the last thing
// the flow shows, so it says what happened and nothing else.
func writeClosePage(w http.ResponseWriter, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>
<body style="font-family:system-ui;background:#161826;color:#e9e9ed;padding:3rem">
<h1 style="font-weight:500;font-size:1.5rem">%s</h1><p style="color:#9397ab">%s</p>`,
		html.EscapeString(title), html.EscapeString(title), detail)
}
