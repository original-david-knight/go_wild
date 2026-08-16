// Package oauth2app is the provider-agnostic core of an installed app's
// OAuth2 machinery: the authorization-code flow with either an ephemeral
// loopback redirect or a fixed pre-registered route, single-use expiring
// state, a consumer-owned token store, and a token source that persists every
// rotation.
//
// It exists because the second OAuth provider in an application otherwise
// copies the first one's stack (lifedash R4: the Withings flow re-implemented
// the Google one's state handling and both token rules). A new provider
// supplies only its credentials, endpoints and scopes; the two rules every
// provider needs are enforced here, once:
//
//   - a grant that carries no refresh token is refused, because the account
//     would stop working at the next restart (ErrNoRefreshToken);
//   - a rotated token that cannot be persisted fails the call, because a
//     rotation held only in memory is a token the next process has lost.
package oauth2app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
)

// Opener is handed the consent URL. A CLI opens a browser; a test asserts.
type Opener func(url string) error

// ErrNoRefreshToken reports a grant that arrived without a refresh token —
// refused rather than stored, because it would strand at the next restart.
var ErrNoRefreshToken = errors.New("the grant carried no refresh token; the account would stop working at the next restart")

// Flow is one provider's installed-app OAuth machinery. Config carries the
// provider's credentials, endpoints and scopes — everything a new provider
// has to supply, and nothing more.
type Flow struct {
	Config *oauth2.Config
	// Store receives the tokens the flow produces. The package never chooses
	// where a token lands: that is the consumer's privacy decision.
	Store TokenStore
	// AuthCodeOptions add provider parameters to every consent URL (Google:
	// access_type=offline and approval_prompt=force, so a re-consent still
	// reissues the refresh token).
	AuthCodeOptions []oauth2.AuthCodeOption
}

// AuthURL is the consent URL for a fixed, pre-registered redirect — the
// route the consuming service serves itself. state should come from a States
// jar, so the callback can refuse what it did not issue. extra options ride
// after the Flow's own (the hosted ceremony adds its PKCE challenge here).
func (f *Flow) AuthURL(redirectURL, state string, extra ...oauth2.AuthCodeOption) string {
	opts := append(append([]oauth2.AuthCodeOption(nil), f.AuthCodeOptions...), extra...)
	return f.configFor(redirectURL).AuthCodeURL(state, opts...)
}

// Exchange trades an authorization code for a token at the same redirect the
// provider saw, refuses a grant with no refresh token, and stores the rest
// under account. opts add per-exchange parameters (the hosted ceremony
// replays its PKCE verifier here).
func (f *Flow) Exchange(ctx context.Context, account, redirectURL, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	name, err := NormalizeAccount(account)
	if err != nil {
		return nil, err
	}
	tok, err := f.configFor(redirectURL).Exchange(ctx, code, opts...)
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}
	if tok.RefreshToken == "" {
		return nil, ErrNoRefreshToken
	}
	if err := f.Store.Save(name, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// TokenSource returns an auto-refreshing source for an account, with every
// rotation persisted through the store. A refresh response that omits the
// refresh token carries the old one forward, and a rotation that cannot be
// written back fails the call.
func (f *Flow) TokenSource(ctx context.Context, account string) (oauth2.TokenSource, error) {
	tok, err := f.Store.Load(account)
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, fmt.Errorf("%s: no stored token", account)
	}
	// The redirect URL is only used during the code exchange; refreshing
	// needs the client credentials and the token endpoint alone.
	return NewPersistingSource(account, f.Store, f.configFor("").TokenSource(ctx, tok), tok), nil
}

func (f *Flow) configFor(redirectURL string) *oauth2.Config {
	conf := *f.Config
	conf.RedirectURL = redirectURL
	return &conf
}

// NewPersistingSource wraps a TokenSource so a rotated token is written back
// to the store before it is handed out. A refresh response that omits the
// refresh token carries the previous one forward — a rotation can never blank
// the only credential that survives a restart — and a rotation that cannot
// persist fails the call.
func NewPersistingSource(account string, store TokenStore, source oauth2.TokenSource, last *oauth2.Token) oauth2.TokenSource {
	return &persistingSource{account: account, store: store, source: source, last: last}
}

type persistingSource struct {
	account string
	store   TokenStore
	source  oauth2.TokenSource
	last    *oauth2.Token
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	tok, err := p.source.Token()
	if err != nil {
		return nil, err
	}
	if p.last == nil || tok.AccessToken != p.last.AccessToken {
		if tok.RefreshToken == "" && p.last != nil {
			tok.RefreshToken = p.last.RefreshToken
		}
		if err := p.store.Save(p.account, tok); err != nil {
			return nil, fmt.Errorf("persist refreshed token for %s: %w", p.account, err)
		}
		p.last = tok
	}
	return tok, nil
}

// NormalizeAccount keeps account names to the shape that is safe in a
// filename, since the file store derives paths from them.
func NormalizeAccount(account string) (string, error) {
	name := strings.TrimSpace(strings.ToLower(account))
	if name == "" {
		return "", fmt.Errorf("account name is empty")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", fmt.Errorf("account name %q may hold only letters, digits, - and _", account)
		}
	}
	return name, nil
}
