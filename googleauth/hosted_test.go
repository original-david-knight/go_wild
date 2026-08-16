package googleauth

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/oauth2app"
)

// TestConnectRefusesAWebClient pins the refusal to the loopback path: a web
// client loads fine, but cannot consent at a runtime-chosen loopback port, so
// Connect stops before opening anything.
func TestConnectRefusesAWebClient(t *testing.T) {
	google := newFakeGoogle(t)
	reg, store, _ := testRegistry(t, google)
	reg.cfg.Web = true

	opened := false
	_, err := reg.Connect(offlineContext(t), "personal", func(string) error {
		opened = true
		return nil
	}, ConnectOptions{Timeout: 10 * time.Second, UserInfoURL: google.userInfoURL()})
	if !errors.Is(err, ErrWebClient) {
		t.Fatalf("Connect = %v, want ErrWebClient", err)
	}
	if opened {
		t.Error("Connect opened a consent page for a web client")
	}
	if exchanges, _ := google.counts(); exchanges != 0 {
		t.Errorf("the token endpoint saw %d exchanges, want 0", exchanges)
	}
	if accounts, err := store.Accounts(); err != nil || len(accounts) != 0 {
		t.Errorf("Accounts() = %v, %v, want none", accounts, err)
	}
}

// TestHostedCeremonyConnects drives the registry's hosted shape end to end:
// Start's consent URL carries Google's durable-grant parameters, the fixed
// registered redirect and an S256 challenge; Finish replays the verifier,
// stores the token at 0600 and reports who actually consented.
func TestHostedCeremonyConnects(t *testing.T) {
	google := newFakeGoogle(t)
	reg, store, dir := testRegistry(t, google)

	const redirect = "https://app.example/oauth/google/callback"
	jar := oauth2app.NewStates(time.Minute)
	hosted := reg.Hosted(jar, redirect, "google")
	hosted.UserInfoURL = google.userInfoURL()
	if hosted.Kind() != "google" {
		t.Errorf("Kind() = %q, want google", hosted.Kind())
	}

	authURL, err := hosted.Start("Personal")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !hosted.Pending("personal") {
		t.Error("Pending(personal) = false after Start")
	}

	consent, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := consent.Query()
	// The consent request is the only chance to get a durable grant — same
	// rules as the loopback flow, at the pre-registered redirect instead.
	if got := q.Get("access_type"); got != "offline" {
		t.Errorf("access_type = %q, want offline", got)
	}
	if q.Get("prompt") != "consent" && q.Get("approval_prompt") != "force" {
		t.Errorf("consent url forces no re-consent: prompt=%q approval_prompt=%q",
			q.Get("prompt"), q.Get("approval_prompt"))
	}
	if got := q.Get("redirect_uri"); got != redirect {
		t.Errorf("redirect_uri = %q, want the registered route", got)
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if q.Get("code_challenge") == "" {
		t.Error("consent url carries no code_challenge")
	}
	for _, scope := range reg.Scopes() {
		if !strings.Contains(q.Get("scope"), scope) {
			t.Errorf("consent url does not request %s", scope)
		}
	}

	res, err := hosted.Finish(offlineContext(t), q.Get("state"), "auth-code-1")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if res.Account != "personal" {
		t.Errorf("Account = %q, want personal", res.Account)
	}
	if res.Email != google.email {
		t.Errorf("Email = %q, want %q", res.Email, google.email)
	}
	if strings.Join(res.Scopes, " ") != strings.Join(grantedScopeOrder, " ") {
		t.Errorf("Scopes = %v, want %v", res.Scopes, grantedScopeOrder)
	}

	exchange := google.lastExchange(t)
	if got := exchange.Get("redirect_uri"); got != redirect {
		t.Errorf("exchange redirect_uri = %q, want the registered route", got)
	}
	if exchange.Get("code_verifier") == "" {
		t.Error("the exchange carried no code_verifier")
	}

	stored, err := store.Load("personal")
	if err != nil || stored == nil || stored.RefreshToken != "refresh-1" {
		t.Errorf("stored = %+v, %v — want the granted token under the account", stored, err)
	}
	if got := mode(t, filepath.Join(dir, "google_token_personal.json")); got != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", got)
	}
	if hosted.Pending("personal") {
		t.Error("Pending(personal) = true after a completed ceremony")
	}
}

// TestFinishHostedRoutesToTheRegistryThatMinted: two registries — different
// stores, different scopes — behind one callback route and one jar. The
// redirect finishes at the registry whose ceremony minted the state, and only
// that registry's store receives the token.
func TestFinishHostedRoutesToTheRegistryThatMinted(t *testing.T) {
	google := newFakeGoogle(t)
	gmailReg, gmailStore, _ := testRegistry(t, google)
	healthReg, healthStore, _ := testRegistry(t, google)

	jar := oauth2app.NewStates(time.Minute)
	const redirect = "https://app.example/oauth/google/callback"
	gmail := gmailReg.Hosted(jar, redirect, "google")
	health := healthReg.Hosted(jar, redirect, "googlehealth")
	gmail.UserInfoURL = google.userInfoURL()
	health.UserInfoURL = google.userInfoURL()

	authURL, err := health.Start("personal")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	consent, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}

	kind, res, err := FinishHosted(offlineContext(t), []*Hosted{gmail, health},
		consent.Query().Get("state"), "auth-code-1")
	if err != nil {
		t.Fatalf("FinishHosted: %v", err)
	}
	if kind != "googlehealth" {
		t.Errorf("kind = %q, want googlehealth", kind)
	}
	if res.Account != "personal" || res.Email != google.email {
		t.Errorf("result = %+v, want personal / %s", res, google.email)
	}
	if stored, _ := healthStore.Load("personal"); stored == nil {
		t.Error("the minting registry's store holds no token")
	}
	if stray, _ := gmailStore.Load("personal"); stray != nil {
		t.Errorf("the other registry's store received a token: %+v", stray)
	}

	// The state went with the ceremony: a replay of the same redirect is stale.
	if _, _, err := FinishHosted(offlineContext(t), []*Hosted{gmail, health},
		consent.Query().Get("state"), "auth-code-1"); !errors.Is(err, oauth2app.ErrStaleState) {
		t.Fatalf("replayed FinishHosted = %v, want ErrStaleState", err)
	}
}

func TestHostedFinishRefusesAForgedState(t *testing.T) {
	google := newFakeGoogle(t)
	reg, store, _ := testRegistry(t, google)
	hosted := reg.Hosted(oauth2app.NewStates(time.Minute), "https://app.example/oauth/google/callback", "google")
	hosted.UserInfoURL = google.userInfoURL()

	if _, err := hosted.Finish(offlineContext(t), "forged-state", "auth-code-1"); !errors.Is(err, oauth2app.ErrStaleState) {
		t.Fatalf("Finish(forged) = %v, want ErrStaleState", err)
	}
	if exchanges, _ := google.counts(); exchanges != 0 {
		t.Errorf("a forged state reached the token endpoint")
	}
	if accounts, err := store.Accounts(); err != nil || len(accounts) != 0 {
		t.Errorf("Accounts() = %v, %v, want none", accounts, err)
	}
}
