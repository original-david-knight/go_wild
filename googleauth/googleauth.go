// Package googleauth is a multi-account OAuth2 client for Google installed
// apps: the authorization-code flow with a loopback redirect, a token store
// the consumer owns, and auto-refreshing clients per account.
//
// It exists because a personal tool routinely has more than one Google
// account — a personal one and a work one — and the official helpers assume a
// single set of credentials per process. Accounts here are keyed by a stable
// local name ("personal", "work"), not by email: the email is what the account
// turns out to hold, and it must be able to change without orphaning a token.
//
// Nothing in this package decides where tokens live. The consumer supplies a
// TokenStore, so an application can keep them wherever its own privacy rules
// say — for lifedash, ~/.config/lifedash at mode 0600.
package googleauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/original-david-knight/go_wild/oauth2app"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scopes an installed app can request. Ask for everything the application will
// ever need at first consent: a later scope addition forces the user through
// the whole grant again, and a background job cannot ask.
const (
	// ScopeGmailModify covers read, label, archive and draft — but not send.
	// There is no narrower scope that permits drafting.
	ScopeGmailModify = "https://www.googleapis.com/auth/gmail.modify"
	// ScopeCalendarReadonly is deliberately read-only.
	ScopeCalendarReadonly = "https://www.googleapis.com/auth/calendar.readonly"
	// ScopeTasks is read-write: two-way task sync needs it.
	ScopeTasks = "https://www.googleapis.com/auth/tasks"
	// ScopeUserinfoEmail identifies which account consented, so a connect can
	// report what it actually connected rather than what it was asked for.
	ScopeUserinfoEmail = "https://www.googleapis.com/auth/userinfo.email"
)

// ClientConfig is the installed-app client, as downloaded from the Google
// Cloud console.
type ClientConfig struct {
	ClientID     string
	ClientSecret string
	AuthURI      string
	TokenURI     string
	ProjectID    string
}

// clientSecretFile mirrors the console's download shape. A desktop client
// lands under "installed"; "web" is accepted so a misconfigured download
// fails with a clear message rather than a nil dereference.
type clientSecretFile struct {
	Installed *clientSecretBody `json:"installed"`
	Web       *clientSecretBody `json:"web"`
}

type clientSecretBody struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
	ProjectID    string   `json:"project_id"`
	RedirectURIs []string `json:"redirect_uris"`
}

// ErrWebClient is returned for a web client, which cannot do the loopback
// flow: web clients require an exact pre-registered redirect URI, and an
// installed app's redirect is a loopback port chosen at runtime.
var ErrWebClient = fmt.Errorf("this is a web OAuth client; an installed (Desktop app) client is required")

// LoadClientConfig reads a client-secret JSON from disk.
func LoadClientConfig(path string) (*ClientConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read client secret: %w", err)
	}
	var file clientSecretFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse client secret %s: %w", path, err)
	}
	body := file.Installed
	if body == nil {
		if file.Web != nil {
			return nil, fmt.Errorf("%s: %w", path, ErrWebClient)
		}
		return nil, fmt.Errorf("%s: no installed or web client in the file", path)
	}
	if body.ClientID == "" || body.ClientSecret == "" {
		return nil, fmt.Errorf("%s: client id or secret missing", path)
	}
	cfg := &ClientConfig{
		ClientID:     body.ClientID,
		ClientSecret: body.ClientSecret,
		AuthURI:      body.AuthURI,
		TokenURI:     body.TokenURI,
		ProjectID:    body.ProjectID,
	}
	if cfg.AuthURI == "" {
		cfg.AuthURI = google.Endpoint.AuthURL
	}
	if cfg.TokenURI == "" {
		cfg.TokenURI = google.Endpoint.TokenURL
	}
	return cfg, nil
}

// Registry holds the client configuration and the accounts' tokens.
type Registry struct {
	cfg    *ClientConfig
	store  TokenStore
	scopes []string
}

// NewRegistry builds a registry. Passing no scopes requests the full set the
// package defines, minus none: partial consent is the thing this package
// exists to avoid.
func NewRegistry(cfg *ClientConfig, store TokenStore, scopes ...string) *Registry {
	if len(scopes) == 0 {
		scopes = []string{
			ScopeGmailModify,
			ScopeCalendarReadonly,
			ScopeTasks,
			ScopeUserinfoEmail,
		}
	}
	return &Registry{cfg: cfg, store: store, scopes: scopes}
}

// Scopes returns the scopes this registry requests.
func (r *Registry) Scopes() []string { return append([]string(nil), r.scopes...) }

// oauthConfig builds the oauth2 config for a redirect URL.
func (r *Registry) oauthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     r.cfg.ClientID,
		ClientSecret: r.cfg.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       r.scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  r.cfg.AuthURI,
			TokenURL: r.cfg.TokenURI,
		},
	}
}

// ErrNotConnected is returned for an account with no stored token.
var ErrNotConnected = fmt.Errorf("account is not connected")

// Connected reports whether an account has a stored token.
func (r *Registry) Connected(account string) bool {
	tok, err := r.store.Load(account)
	return err == nil && tok != nil && (tok.RefreshToken != "" || tok.AccessToken != "")
}

// Accounts lists the accounts holding a token.
func (r *Registry) Accounts() ([]string, error) { return r.store.Accounts() }

// Disconnect removes an account's stored token.
func (r *Registry) Disconnect(account string) error { return r.store.Delete(account) }

// TokenSource returns an auto-refreshing source for an account. A refresh
// rotates the token, and a rotated token that is not written back is lost on
// the next restart — so every refresh persists through oauth2app's
// persisting source, which also carries a refresh token the response omitted
// forward so a rotation can never blank the only credential that survives a
// restart.
func (r *Registry) TokenSource(ctx context.Context, account string) (oauth2.TokenSource, error) {
	tok, err := r.store.Load(account)
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, fmt.Errorf("%s: %w", account, ErrNotConnected)
	}
	// The redirect URL is only used during the code exchange; refreshing needs
	// the client credentials and the token endpoint alone.
	base := r.oauthConfig("").TokenSource(ctx, tok)
	return oauth2app.NewPersistingSource(account, r.store, base, tok), nil
}

// normalizeAccount keeps account names to the shape that is safe in a
// filename, since the file store derives paths from them.
func normalizeAccount(account string) (string, error) {
	return oauth2app.NormalizeAccount(account)
}
