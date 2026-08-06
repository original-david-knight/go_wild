package oauth2app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// tokenEndpoint is a stub provider token endpoint. Each POST answers with the
// scripted token body.
func tokenEndpoint(t *testing.T, body map[string]any) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var exchanges []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token request: %v", err)
		}
		exchanges = append(exchanges, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &exchanges
}

func testFlow(t *testing.T, tokenURL string) (*Flow, *MemoryTokenStore) {
	t.Helper()
	store := NewMemoryTokenStore()
	return &Flow{
		Config: &oauth2.Config{
			ClientID:     "id",
			ClientSecret: "secret",
			Scopes:       []string{"scope-a", "scope-b"},
			Endpoint:     oauth2.Endpoint{AuthURL: "https://provider.example/auth", TokenURL: tokenURL},
		},
		Store: store,
	}, store
}

// TestConnectFlowEphemeralLoopbackRedirect drives the whole loopback flow the
// way a provider would: the opener follows the consent URL's redirect_uri
// back with the state and a code, the code is exchanged, and the token lands
// in the store under the account.
func TestConnectFlowEphemeralLoopbackRedirect(t *testing.T) {
	provider, exchanges := tokenEndpoint(t, map[string]any{
		"access_token": "at-1", "refresh_token": "rt-1", "token_type": "Bearer", "expires_in": 3600,
	})
	flow, store := testFlow(t, provider.URL)

	opener := func(consentURL string) error {
		u, err := url.Parse(consentURL)
		if err != nil {
			return err
		}
		q := u.Query()
		redirect := q.Get("redirect_uri")
		if !strings.HasPrefix(redirect, "http://127.0.0.1:") {
			return fmt.Errorf("redirect_uri = %q, want an ephemeral loopback", redirect)
		}
		go func() {
			resp, err := http.Get(redirect + "?state=" + url.QueryEscape(q.Get("state")) + "&code=auth-code-1")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	tok, err := flow.ConnectLoopback(context.Background(), "personal", opener, LoopbackOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("ConnectLoopback: %v", err)
	}
	if tok.RefreshToken != "rt-1" {
		t.Errorf("refresh token = %q, want rt-1", tok.RefreshToken)
	}
	stored, err := store.Load("personal")
	if err != nil || stored == nil || stored.AccessToken != "at-1" {
		t.Errorf("stored token = %v, %v — want the exchanged token under the account", stored, err)
	}
	if len(*exchanges) != 1 || (*exchanges)[0].Get("code") != "auth-code-1" {
		t.Errorf("exchanges = %v, want exactly the redirect's code", *exchanges)
	}
}

// TestConnectFlowFixedRouteRedirect is the pre-registered-route half — the
// Withings shape, where the callback is a fixed path the consuming service
// serves itself: the flow only issues the URL, the jar validates the state,
// and Exchange lands the token at the same registered redirect.
func TestConnectFlowFixedRouteRedirect(t *testing.T) {
	provider, exchanges := tokenEndpoint(t, map[string]any{
		"access_token": "at-9", "refresh_token": "rt-9", "token_type": "Bearer", "expires_in": 3600,
	})
	flow, store := testFlow(t, provider.URL)
	const registered = "http://localhost:8080/oauth/provider/callback"

	states := NewStates(time.Minute)
	state, err := states.New()
	if err != nil {
		t.Fatal(err)
	}
	consent, err := url.Parse(flow.AuthURL(registered, state))
	if err != nil {
		t.Fatal(err)
	}
	if got := consent.Query().Get("redirect_uri"); got != registered {
		t.Fatalf("redirect_uri = %q, want the fixed registered route", got)
	}
	if got := consent.Query().Get("state"); got != state {
		t.Fatalf("state = %q, want the jar's", got)
	}

	// The redirect arrives at the fixed route: the handler takes the state
	// once, then exchanges.
	if !states.Take(state) {
		t.Fatal("the jar refused its own state")
	}
	if states.Take(state) {
		t.Fatal("a state was accepted twice — it must be single-use")
	}
	tok, err := flow.Exchange(context.Background(), "scale", registered, "auth-code-9")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "at-9" {
		t.Errorf("access token = %q, want at-9", tok.AccessToken)
	}
	if stored, _ := store.Load("scale"); stored == nil || stored.RefreshToken != "rt-9" {
		t.Errorf("stored = %v, want the granted token under the account", stored)
	}
	if got := (*exchanges)[0].Get("redirect_uri"); got != registered {
		t.Errorf("exchange redirect_uri = %q, want the registered route", got)
	}
}

// TestStateRejectedAfterTTL pins the jar's expiry: a consent tab left open
// past the TTL is a closed attempt, not a way in.
func TestStateRejectedAfterTTL(t *testing.T) {
	states := NewStates(time.Minute)
	state, err := states.New()
	if err != nil {
		t.Fatal(err)
	}
	if !states.Pending() {
		t.Fatal("a fresh state does not read as pending")
	}
	// Move the clock rather than sleeping through a TTL.
	states.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if states.Take(state) {
		t.Error("an expired state was accepted")
	}
	if states.Pending() {
		t.Error("an expired state still reads as pending")
	}
}

// TestRefusesGrantWithoutRefreshToken is the first shared rule: a grant with
// no refresh token would strand at the next restart, so nothing is stored and
// the named error comes back.
func TestRefusesGrantWithoutRefreshToken(t *testing.T) {
	provider, _ := tokenEndpoint(t, map[string]any{
		"access_token": "at-1", "token_type": "Bearer", "expires_in": 3600,
	})
	flow, store := testFlow(t, provider.URL)

	_, err := flow.Exchange(context.Background(), "personal", "http://localhost/cb", "code-1")
	if !errors.Is(err, ErrNoRefreshToken) {
		t.Fatalf("Exchange = %v, want ErrNoRefreshToken", err)
	}
	if tok, _ := store.Load("personal"); tok != nil {
		t.Errorf("a refused grant was stored: %v", tok)
	}
}

// failingStore refuses every Save — the disk-full case the second shared
// rule exists for.
type failingStore struct{ MemoryTokenStore }

func (f *failingStore) Save(string, *oauth2.Token) error {
	return errors.New("disk full")
}

// TestRotatedTokenPersistFailureFailsTheCall is the second shared rule: a
// rotated token only in memory is a token the next process has lost, so the
// call fails rather than continuing on a credential that will vanish.
func TestRotatedTokenPersistFailureFailsTheCall(t *testing.T) {
	store := &failingStore{}
	rotating := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "at-2", RefreshToken: "rt-2"})
	source := NewPersistingSource("personal", store,
		rotating, &oauth2.Token{AccessToken: "at-1", RefreshToken: "rt-1"})

	if _, err := source.Token(); err == nil || !strings.Contains(err.Error(), "persist refreshed token") {
		t.Fatalf("Token = %v, want the persist failure to fail the call", err)
	}
}

// TestPersistingSourceCarriesTheRefreshTokenForward: a refresh response that
// omits the refresh token must not blank the only credential that survives a
// restart.
func TestPersistingSourceCarriesTheRefreshTokenForward(t *testing.T) {
	store := NewMemoryTokenStore()
	rotated := &oauth2.Token{AccessToken: "at-2"} // no refresh token, as providers answer
	source := NewPersistingSource("personal", store,
		oauth2.StaticTokenSource(rotated), &oauth2.Token{AccessToken: "at-1", RefreshToken: "rt-1"})

	tok, err := source.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.RefreshToken != "rt-1" {
		t.Errorf("refresh token = %q, want the previous one carried forward", tok.RefreshToken)
	}
	if stored, _ := store.Load("personal"); stored == nil || stored.RefreshToken != "rt-1" {
		t.Errorf("stored = %v, want the carried-forward token persisted", stored)
	}
}
