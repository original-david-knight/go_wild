package oauth2app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testHosted(t *testing.T) (*Hosted, *MemoryTokenStore, *[]url.Values) {
	t.Helper()
	provider, exchanges := tokenEndpoint(t, map[string]any{
		"access_token": "at-1", "refresh_token": "rt-1", "token_type": "Bearer", "expires_in": 3600,
	})
	flow, store := testFlow(t, provider.URL)
	return &Hosted{
		Flow:        flow,
		States:      NewStates(time.Minute),
		RedirectURL: "https://app.example/oauth/provider/callback",
	}, store, exchanges
}

// TestHostedCeremonyPKCEVerifierRidesTheExchange drives the whole hosted
// shape: Start's consent URL carries an S256 challenge at the registered
// redirect, and Finish replays the verifier that challenge was derived from —
// the thing that makes a code intercepted off the public callback worthless.
func TestHostedCeremonyPKCEVerifierRidesTheExchange(t *testing.T) {
	hosted, store, exchanges := testHosted(t)

	authURL, err := hosted.Start("personal")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	consent, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := consent.Query()
	if got := q.Get("redirect_uri"); got != hosted.RedirectURL {
		t.Errorf("redirect_uri = %q, want the registered route", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	challenge := q.Get("code_challenge")
	if challenge == "" {
		t.Fatal("consent url carries no code_challenge")
	}
	state := q.Get("state")
	if state == "" {
		t.Fatal("consent url carries no state")
	}

	account, tok, err := hosted.Finish(context.Background(), state, "auth-code-1")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if account != "personal" {
		t.Errorf("account = %q, want personal", account)
	}
	if tok == nil || tok.RefreshToken != "rt-1" {
		t.Errorf("token = %+v, want the granted one returned", tok)
	}
	if stored, _ := store.Load("personal"); stored == nil || stored.AccessToken != "at-1" {
		t.Errorf("stored = %v, want the exchanged token under the account", stored)
	}

	if len(*exchanges) != 1 {
		t.Fatalf("exchanges = %d, want 1", len(*exchanges))
	}
	form := (*exchanges)[0]
	if got := form.Get("redirect_uri"); got != hosted.RedirectURL {
		t.Errorf("exchange redirect_uri = %q, want the registered route", got)
	}
	verifier := form.Get("code_verifier")
	if verifier == "" {
		t.Fatal("the exchange carried no code_verifier")
	}
	sum := sha256.Sum256([]byte(verifier))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge {
		t.Errorf("S256(code_verifier) = %q, want the consent url's challenge %q", got, challenge)
	}
}

// TestHostedStateIsSingleUseAndForgeryIsRefused: the second redirect with the
// same state, and a redirect with a state the jar never issued, both stop
// before any exchange.
func TestHostedStateIsSingleUseAndForgeryIsRefused(t *testing.T) {
	hosted, store, exchanges := testHosted(t)

	if _, _, err := hosted.Finish(context.Background(), "forged-state", "auth-code-1"); !errors.Is(err, ErrStaleState) {
		t.Fatalf("Finish(forged) = %v, want ErrStaleState", err)
	}
	if len(*exchanges) != 0 {
		t.Fatalf("a forged state reached the token endpoint")
	}

	authURL, err := hosted.Start("personal")
	if err != nil {
		t.Fatal(err)
	}
	state := stateOf(t, authURL)
	if _, _, err := hosted.Finish(context.Background(), state, "auth-code-1"); err != nil {
		t.Fatalf("first Finish: %v", err)
	}
	if _, _, err := hosted.Finish(context.Background(), state, "auth-code-1"); !errors.Is(err, ErrStaleState) {
		t.Fatalf("second Finish = %v, want ErrStaleState — a state is single-use", err)
	}
	if len(*exchanges) != 1 {
		t.Errorf("exchanges = %d, want exactly the first redirect's", len(*exchanges))
	}
	if stored, _ := store.Load("personal"); stored == nil {
		t.Error("the legitimate finish stored nothing")
	}
}

// TestHostedStateExpires: a consent tab left open past the TTL is a closed
// attempt, not a way in.
func TestHostedStateExpires(t *testing.T) {
	hosted, _, exchanges := testHosted(t)

	authURL, err := hosted.Start("personal")
	if err != nil {
		t.Fatal(err)
	}
	// Move the jar's clock rather than sleeping through a TTL.
	hosted.States.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, _, err := hosted.Finish(context.Background(), stateOf(t, authURL), "auth-code-1"); !errors.Is(err, ErrStaleState) {
		t.Fatalf("Finish after TTL = %v, want ErrStaleState", err)
	}
	if len(*exchanges) != 0 {
		t.Error("an expired state reached the token endpoint")
	}
}

// TestHostedPendingAndCancelArePerAccount: pending tracks the account that
// started, cancel forgets only that account's attempts, and a finished
// ceremony leaves nothing pending behind.
func TestHostedPendingAndCancelArePerAccount(t *testing.T) {
	hosted, _, _ := testHosted(t)

	if hosted.Pending("personal") {
		t.Fatal("pending before any Start")
	}
	if _, err := hosted.Start("personal"); err != nil {
		t.Fatal(err)
	}
	if !hosted.Pending("personal") {
		t.Error("Pending(personal) = false after Start")
	}
	if hosted.Pending("work") {
		t.Error("Pending(work) = true; only personal started")
	}

	authURL, err := hosted.Start("work")
	if err != nil {
		t.Fatal(err)
	}
	hosted.Cancel("personal")
	if hosted.Pending("personal") {
		t.Error("Pending(personal) = true after Cancel")
	}
	if !hosted.Pending("work") {
		t.Error("Cancel(personal) also cancelled work")
	}

	if _, _, err := hosted.Finish(context.Background(), stateOf(t, authURL), "auth-code-1"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if hosted.Pending("work") {
		t.Error("Pending(work) = true after a completed ceremony")
	}
}

// TestHostedRefusesGrantWithoutRefreshToken: the shared refresh-token rule
// stands on the hosted path — nothing stored, the named error back.
func TestHostedRefusesGrantWithoutRefreshToken(t *testing.T) {
	provider, _ := tokenEndpoint(t, map[string]any{
		"access_token": "at-1", "token_type": "Bearer", "expires_in": 3600,
	})
	flow, store := testFlow(t, provider.URL)
	hosted := &Hosted{Flow: flow, States: NewStates(time.Minute), RedirectURL: "https://app.example/oauth/provider/callback"}

	authURL, err := hosted.Start("personal")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hosted.Finish(context.Background(), stateOf(t, authURL), "auth-code-1"); !errors.Is(err, ErrNoRefreshToken) {
		t.Fatalf("Finish = %v, want ErrNoRefreshToken", err)
	}
	if tok, _ := store.Load("personal"); tok != nil {
		t.Errorf("a refused grant was stored: %v", tok)
	}
}

// TestStatesPayloadRoundTrip pins the jar's payload variants: what NewWith
// bound comes back from TakeWith exactly once, a plain New reads as the empty
// payload, and expiry applies unchanged.
func TestStatesPayloadRoundTrip(t *testing.T) {
	states := NewStates(time.Minute)

	state, err := states.NewWith("google|personal")
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := states.TakeWith(state)
	if !ok || payload != "google|personal" {
		t.Fatalf("TakeWith = %q, %v — want the bound payload once", payload, ok)
	}
	if _, ok := states.TakeWith(state); ok {
		t.Fatal("a state was accepted twice — it must be single-use")
	}

	plain, err := states.New()
	if err != nil {
		t.Fatal(err)
	}
	if payload, ok := states.TakeWith(plain); !ok || payload != "" {
		t.Errorf("TakeWith(plain New) = %q, %v — want the empty payload", payload, ok)
	}

	expiring, err := states.NewWith("kind|acct")
	if err != nil {
		t.Fatal(err)
	}
	states.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, ok := states.TakeWith(expiring); ok {
		t.Error("an expired payload-carrying state was accepted")
	}
}

// TestFinishHostedRoutesByKind: two ceremonies sharing one jar behind one
// callback route — the redirect finishes at the ceremony that minted its
// state, and only that ceremony's store receives the token. A direct Finish
// on the wrong ceremony refuses the state.
func TestFinishHostedRoutesByKind(t *testing.T) {
	provider, exchanges := tokenEndpoint(t, map[string]any{
		"access_token": "at-1", "refresh_token": "rt-1", "token_type": "Bearer", "expires_in": 3600,
	})
	jar := NewStates(time.Minute)

	flowA, storeA := testFlow(t, provider.URL)
	flowB, storeB := testFlow(t, provider.URL)
	a := &Hosted{Flow: flowA, States: jar, RedirectURL: "https://app.example/oauth/callback", Kind: "google"}
	b := &Hosted{Flow: flowB, States: jar, RedirectURL: "https://app.example/oauth/callback", Kind: "googlehealth"}

	authURL, err := b.Start("personal")
	if err != nil {
		t.Fatal(err)
	}
	kind, account, tok, err := FinishHosted(context.Background(), []*Hosted{a, b}, stateOf(t, authURL), "auth-code-1")
	if err != nil {
		t.Fatalf("FinishHosted: %v", err)
	}
	if kind != "googlehealth" || account != "personal" {
		t.Errorf("FinishHosted = %q, %q — want googlehealth, personal", kind, account)
	}
	if tok == nil || tok.RefreshToken != "rt-1" {
		t.Errorf("token = %+v, want the granted one", tok)
	}
	if stored, _ := storeB.Load("personal"); stored == nil {
		t.Error("the minting ceremony's store holds no token")
	}
	if stray, _ := storeA.Load("personal"); stray != nil {
		t.Errorf("the other ceremony's store received a token: %v", stray)
	}
	if len(*exchanges) != 1 {
		t.Errorf("exchanges = %d, want 1", len(*exchanges))
	}

	// A ceremony asked directly to finish another's state refuses it.
	authURL, err = a.Start("personal")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Finish(context.Background(), stateOf(t, authURL), "auth-code-2"); !errors.Is(err, ErrStaleState) {
		t.Fatalf("Finish across kinds = %v, want ErrStaleState", err)
	}
}

// TestWriteResultPageEscapes: a failure line can echo the query string, so
// everything on the page arrives escaped.
func TestWriteResultPageEscapes(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResultPage(rec, `Connected <b>`, `return "to" the <script>app</script>`)

	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "<b>") {
		t.Errorf("page carries unescaped input: %q", body)
	}
	for _, want := range []string{"Connected &lt;b&gt;", "&lt;script&gt;app&lt;/script&gt;", "&#34;to&#34;"} {
		if !strings.Contains(body, want) {
			t.Errorf("page %q does not contain %q", body, want)
		}
	}
}

func stateOf(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("consent url carries no state")
	}
	return state
}
