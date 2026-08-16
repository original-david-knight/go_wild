package oauth2app

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// ErrStaleState reports a redirect whose state this ceremony did not issue,
// already took, or let expire. All three collapse into one answer on purpose:
// telling a caller which of them happened would tell a forger the same thing.
var ErrStaleState = errors.New("the consent link had expired or was already used; start the connect again")

// Hosted is the fixed-route half of the authorization-code flow, PKCE-guarded:
// the shape a service with a public callback route runs (lifedash's Withings
// handler hand-rolled exactly this stack — state jar, exchange at the
// registered redirect, result page). Start hands back a consent URL for the
// service to answer with; the provider later redirects the browser to the
// registered route, whose handler calls Finish.
//
// PKCE (S256) rides every ceremony: on a public callback the authorization
// code crosses the network in a query string, and the verifier is what makes
// an intercepted code worthless without the process that minted it.
type Hosted struct {
	// Flow supplies the provider's credentials, endpoints, consent options and
	// the token store.
	Flow *Flow
	// States is the jar the ceremony mints against. Several ceremonies may
	// share one jar behind one callback route — give each a distinct Kind and
	// route redirects with FinishHosted.
	States *States
	// RedirectURL is the absolute callback URL, exactly as pre-registered with
	// the provider.
	RedirectURL string
	// Kind tags the states this ceremony mints, so a callback serving several
	// ceremonies out of one jar can tell whose a redirect is. Empty is fine
	// for a ceremony with a jar of its own. It may not contain "|", which
	// separates the fields the jar carries.
	Kind string
}

// Start mints a PKCE verifier and a single-use state bound to
// {account, verifier}, and returns the consent URL for the service to answer
// with. It does not wait for consent — the callback route is what completes
// the ceremony, however long the browser takes.
func (h *Hosted) Start(account string) (string, error) {
	name, err := NormalizeAccount(account)
	if err != nil {
		return "", err
	}
	if strings.Contains(h.Kind, "|") {
		return "", fmt.Errorf("ceremony kind %q may not contain %q", h.Kind, "|")
	}
	verifier := oauth2.GenerateVerifier()
	state, err := h.States.NewWith(hostedPayload(h.Kind, name, verifier))
	if err != nil {
		return "", err
	}
	return h.Flow.AuthURL(h.RedirectURL, state, oauth2.S256ChallengeOption(verifier)), nil
}

// Finish completes the ceremony a redirect landed: it takes the state
// (single-use), replays the PKCE verifier in the exchange at the same
// registered redirect, refuses a grant with no refresh token
// (ErrNoRefreshToken), and stores the token under the account Start was given.
// The returned token is the exchange's own — the only copy that still carries
// the provider's extra fields (Google reports the granted scopes there), which
// do not survive the store's round-trip.
//
// A ceremony sharing its jar refuses another ceremony's state — but has
// consumed it. A callback route serving several ceremonies must dispatch with
// FinishHosted instead of trying each Finish in turn.
func (h *Hosted) Finish(ctx context.Context, state, code string) (string, *oauth2.Token, error) {
	payload, ok := h.States.TakeWith(state)
	if !ok {
		return "", nil, ErrStaleState
	}
	kind, account, verifier, err := parseHostedPayload(payload)
	if err != nil || kind != h.Kind {
		return "", nil, ErrStaleState
	}
	return h.finishTaken(ctx, account, verifier, code)
}

// finishTaken is Finish after the state has been taken and its payload read.
func (h *Hosted) finishTaken(ctx context.Context, account, verifier, code string) (string, *oauth2.Token, error) {
	tok, err := h.Flow.Exchange(ctx, account, h.RedirectURL, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", nil, err
	}
	// The grant landed; any older attempt for the same account is a tab that
	// no longer matters.
	h.Cancel(account)
	return account, tok, nil
}

// Pending reports whether a consent attempt for account is outstanding —
// started, not yet finished, cancelled or expired.
func (h *Hosted) Pending(account string) bool {
	prefix, err := h.accountPrefix(account)
	if err != nil {
		return false
	}
	return h.States.pendingMatch(func(payload string) bool {
		return strings.HasPrefix(payload, prefix)
	})
}

// Cancel forgets account's outstanding attempts — the user closing the flow.
// Other accounts' attempts, and other ceremonies' in a shared jar, stay.
func (h *Hosted) Cancel(account string) {
	prefix, err := h.accountPrefix(account)
	if err != nil {
		return
	}
	h.States.cancelMatch(func(payload string) bool {
		return strings.HasPrefix(payload, prefix)
	})
}

func (h *Hosted) accountPrefix(account string) (string, error) {
	name, err := NormalizeAccount(account)
	if err != nil {
		return "", err
	}
	return h.Kind + "|" + name + "|", nil
}

// FinishHosted routes a redirect to whichever of several ceremonies minted
// its state, matching by Kind, and finishes there. The ceremonies must share
// one States jar — it is the first one's that is consulted — which is what
// lets one public callback route serve several providers or registries.
func FinishHosted(ctx context.Context, ceremonies []*Hosted, state, code string) (kind, account string, tok *oauth2.Token, err error) {
	if len(ceremonies) == 0 {
		return "", "", nil, ErrStaleState
	}
	payload, ok := ceremonies[0].States.TakeWith(state)
	if !ok {
		return "", "", nil, ErrStaleState
	}
	kind, account, verifier, err := parseHostedPayload(payload)
	if err != nil {
		return "", "", nil, ErrStaleState
	}
	for _, ceremony := range ceremonies {
		if ceremony.Kind == kind {
			account, tok, err = ceremony.finishTaken(ctx, account, verifier, code)
			return kind, account, tok, err
		}
	}
	return "", "", nil, ErrStaleState
}

// hostedPayload binds what a state stands for. kind and account cannot carry
// "|" (Start validates the kind, NormalizeAccount the account) and the
// verifier is base64url, so the separators are unambiguous.
func hostedPayload(kind, account, verifier string) string {
	return kind + "|" + account + "|" + verifier
}

func parseHostedPayload(payload string) (kind, account, verifier string, err error) {
	parts := strings.SplitN(payload, "|", 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("state payload is not a hosted ceremony's")
	}
	return parts[0], parts[1], parts[2], nil
}

// WriteResultPage answers the browser at the end of a ceremony — the
// "Connected. Return to the app." page, or the failure line. It is the last
// thing a flow shows, so it says what happened and nothing else; both strings
// are escaped, because parts of a failure line can echo the query string.
func WriteResultPage(w http.ResponseWriter, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>
<body style="font-family:system-ui;background:#161826;color:#e9e9ed;padding:3rem">
<h1 style="font-weight:500;font-size:1.5rem">%s</h1><p style="color:#9397ab">%s</p>`,
		html.EscapeString(title), html.EscapeString(title), html.EscapeString(detail))
}
