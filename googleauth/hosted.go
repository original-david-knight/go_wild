package googleauth

import (
	"context"

	"github.com/original-david-knight/go_wild/oauth2app"
)

// Hosted is the registry's fixed-route consent ceremony: oauth2app's hosted
// machinery under Google's provider configuration (offline access, forced
// re-consent, this registry's scopes), finishing with the same identity
// resolution Connect does — the consenting email and the granted scopes,
// which is what a consumer records beside the token.
//
// It is what a web OAuth client runs: the service answers Start's consent URL
// out of its own API, Google redirects the browser to the pre-registered
// callback route, and that route's handler calls Finish (or FinishHosted,
// when several registries share the route).
type Hosted struct {
	// UserInfoURL overrides the endpoint used to resolve the account's email.
	// Tests point this at a stub.
	UserInfoURL string

	registry *Registry
	inner    *oauth2app.Hosted
}

// Hosted returns the registry's ceremony against a jar and a pre-registered
// absolute redirect URL. kind tags the states it mints (it may not contain
// "|"): several registries' ceremonies can share one jar behind one callback
// route, each under its own kind, dispatched with FinishHosted.
func (r *Registry) Hosted(states *oauth2app.States, redirectURL, kind string) *Hosted {
	return &Hosted{
		registry: r,
		inner: &oauth2app.Hosted{
			Flow:        r.flow(),
			States:      states,
			RedirectURL: redirectURL,
			Kind:        kind,
		},
	}
}

// Kind is the tag this ceremony's states carry.
func (h *Hosted) Kind() string { return h.inner.Kind }

// Start mints a PKCE verifier and a single-use state bound to the account,
// and returns the consent URL. It does not wait for consent — the callback
// route completes the ceremony, however long the browser takes.
func (h *Hosted) Start(account string) (string, error) {
	return h.inner.Start(account)
}

// Finish completes the ceremony a redirect landed — single-use state, PKCE
// verifier replayed at the registered redirect, a grant with no refresh token
// refused, the token stored — and reports what actually connected.
func (h *Hosted) Finish(ctx context.Context, state, code string) (*ConnectResult, error) {
	account, tok, err := h.inner.Finish(ctx, state, code)
	if err != nil {
		return nil, err
	}
	return h.registry.connectResult(ctx, account, tok, h.UserInfoURL), nil
}

// Pending reports whether a consent attempt for account is outstanding.
func (h *Hosted) Pending(account string) bool { return h.inner.Pending(account) }

// Cancel forgets account's outstanding attempts — the user closing the flow.
func (h *Hosted) Cancel(account string) { h.inner.Cancel(account) }

// FinishHosted routes a redirect to whichever ceremony minted its state,
// matching by kind, and finishes there. The ceremonies must share one States
// jar — the sharing is what lets one public callback route serve several
// registries (lifedash: the Google accounts and Google Health behind one
// /oauth/google/callback).
func FinishHosted(ctx context.Context, ceremonies []*Hosted, state, code string) (kind string, res *ConnectResult, err error) {
	inners := make([]*oauth2app.Hosted, len(ceremonies))
	for i, ceremony := range ceremonies {
		inners[i] = ceremony.inner
	}
	kind, account, tok, err := oauth2app.FinishHosted(ctx, inners, state, code)
	if err != nil {
		return "", nil, err
	}
	for _, ceremony := range ceremonies {
		if ceremony.inner.Kind == kind {
			return kind, ceremony.registry.connectResult(ctx, account, tok, ceremony.UserInfoURL), nil
		}
	}
	// Unreachable: oauth2app.FinishHosted only succeeds for a kind in the list.
	return "", nil, oauth2app.ErrStaleState
}
