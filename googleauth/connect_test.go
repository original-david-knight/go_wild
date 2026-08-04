package googleauth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// The suite must run with the network unplugged. offlineContext installs an HTTP
// client that refuses anything off this machine, so a test can never pass by
// quietly reaching the real Google endpoints.
func offlineContext(t *testing.T) context.Context {
	t.Helper()
	return context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Transport: loopbackOnlyTransport{t: t},
	})
}

type loopbackOnlyTransport struct{ t *testing.T }

func (l loopbackOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isLoopbackHost(req.URL.Hostname()) {
		l.t.Errorf("test tried to reach %s; the suite must run offline", req.URL)
		return nil, fmt.Errorf("blocked non-loopback request to %s", req.URL.Host)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// fakeGoogle stands in for accounts.google.com: a token endpoint and a userinfo
// endpoint, recording what the flow actually sent.
type fakeGoogle struct {
	*httptest.Server

	mu        sync.Mutex
	exchanges []url.Values
	refreshes []url.Values

	// token answers the token endpoint; swap it to drive a failure case.
	token func(form url.Values) (int, string)
	email string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{
		email: "zenbulogy@gmail.com",
		token: func(url.Values) (int, string) {
			return http.StatusOK, tokenBody("access-1", "refresh-1", grantedScopeOrder)
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", g.handleToken)
	mux.HandleFunc("GET /userinfo", g.handleUserInfo)
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGoogle) authURL() string     { return g.URL + "/auth" }
func (g *fakeGoogle) tokenURL() string    { return g.URL + "/token" }
func (g *fakeGoogle) userInfoURL() string { return g.URL + "/userinfo" }

func (g *fakeGoogle) handleToken(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	form := req.PostForm
	// oauth2 probes with the credentials in a Basic header first and falls back
	// to form fields, so accept both and record one shape.
	if id, secret, ok := req.BasicAuth(); ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
		form.Set("client_id", id)
		form.Set("client_secret", secret)
	}

	g.mu.Lock()
	switch form.Get("grant_type") {
	case "authorization_code":
		g.exchanges = append(g.exchanges, form)
	case "refresh_token":
		g.refreshes = append(g.refreshes, form)
	}
	answer := g.token
	g.mu.Unlock()

	status, body := answer(form)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func (g *fakeGoogle) handleUserInfo(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, "no bearer token", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"email":%q}`, g.email)
}

func (g *fakeGoogle) setToken(fn func(form url.Values) (int, string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.token = fn
}

func (g *fakeGoogle) counts() (exchanges, refreshes int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.exchanges), len(g.refreshes)
}

func (g *fakeGoogle) lastExchange(t *testing.T) url.Values {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.exchanges) == 0 {
		t.Fatal("the token endpoint was never asked to exchange a code")
	}
	return g.exchanges[len(g.exchanges)-1]
}

func (g *fakeGoogle) lastRefresh(t *testing.T) url.Values {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.refreshes) == 0 {
		t.Fatal("the token endpoint was never asked to refresh")
	}
	return g.refreshes[len(g.refreshes)-1]
}

// grantedScopeOrder deliberately differs from the order the registry requests,
// so an assertion on ConnectResult.Scopes proves the scopes were read off the
// grant rather than echoed back from the request.
var grantedScopeOrder = []string{ScopeUserinfoEmail, ScopeTasks, ScopeCalendarReadonly, ScopeGmailModify}

// tokenBody renders a token-endpoint response. An empty refresh is omitted,
// which is what Google does on a refresh.
func tokenBody(access, refresh string, scopes []string) string {
	body := map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if refresh != "" {
		body["refresh_token"] = refresh
	}
	if len(scopes) > 0 {
		body["scope"] = strings.Join(scopes, " ")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func testRegistry(t *testing.T, g *fakeGoogle) (*Registry, *FileTokenStore, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "lifedash")
	store := NewFileTokenStore(dir)
	cfg := &ClientConfig{
		ClientID:     "1234.apps.googleusercontent.com",
		ClientSecret: "GOCSPX-secret",
		AuthURI:      g.authURL(),
		TokenURI:     g.tokenURL(),
		ProjectID:    "super-cosmic-genius-4aa3b",
	}
	return NewRegistry(cfg, store), store, dir
}

// browser plays the part of the user's browser: it reads the consent URL,
// records it, and drives the redirect back to the loopback listener itself.
// callback decides what query the redirect carries.
func browser(t *testing.T, consent *url.Values, callback func(q url.Values) url.Values) Opener {
	t.Helper()
	return func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("parse consent url: %w", err)
		}
		q := u.Query()
		if consent != nil {
			*consent = q
		}
		target, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			return fmt.Errorf("parse redirect_uri: %w", err)
		}
		target.RawQuery = callback(q).Encode()
		resp, err := http.Get(target.String())
		if err != nil {
			return fmt.Errorf("follow redirect: %w", err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		// A rejected callback is still a delivered redirect: returning an error
		// here would mask the flow's own diagnosis with "open consent page".
		return nil
	}
}

func withCode(code string) func(url.Values) url.Values {
	return func(q url.Values) url.Values {
		return url.Values{"state": {q.Get("state")}, "code": {code}}
	}
}

func TestConnectExchangesCodeAndStoresToken(t *testing.T) {
	google := newFakeGoogle(t)
	reg, store, dir := testRegistry(t, google)

	var consent url.Values
	res, err := reg.Connect(offlineContext(t), "Personal", browser(t, &consent, withCode("auth-code-1")), ConnectOptions{
		Timeout:     10 * time.Second,
		UserInfoURL: google.userInfoURL(),
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if res.Account != "personal" {
		t.Errorf("Account = %q, want %q", res.Account, "personal")
	}
	if res.Email != google.email {
		t.Errorf("Email = %q, want %q", res.Email, google.email)
	}
	if strings.Join(res.Scopes, " ") != strings.Join(grantedScopeOrder, " ") {
		t.Errorf("Scopes = %v, want %v", res.Scopes, grantedScopeOrder)
	}

	// The consent request is the only chance to get a durable grant: without
	// access_type=offline there is no refresh token at all, and without a forced
	// prompt a re-consent of an already-granted account returns none either.
	if got := consent.Get("access_type"); got != "offline" {
		t.Errorf("access_type = %q, want offline", got)
	}
	if consent.Get("prompt") != "consent" && consent.Get("approval_prompt") != "force" {
		t.Errorf("consent url forces no re-consent: prompt=%q approval_prompt=%q",
			consent.Get("prompt"), consent.Get("approval_prompt"))
	}
	if got := consent.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
	if got := consent.Get("client_id"); got != "1234.apps.googleusercontent.com" {
		t.Errorf("client_id = %q", got)
	}
	if consent.Get("state") == "" {
		t.Error("consent url carries no state")
	}
	for _, scope := range reg.Scopes() {
		if !strings.Contains(consent.Get("scope"), scope) {
			t.Errorf("consent url does not request %s", scope)
		}
	}

	// The redirect must stay on the loopback interface: the authorization code
	// travels in its query string.
	redirect, err := url.Parse(consent.Get("redirect_uri"))
	if err != nil {
		t.Fatalf("parse redirect_uri failed: %v", err)
	}
	if redirect.Hostname() != "127.0.0.1" {
		t.Errorf("redirect_uri host = %q, want 127.0.0.1", redirect.Hostname())
	}
	if redirect.Path != "/oauth/callback" {
		t.Errorf("redirect_uri path = %q", redirect.Path)
	}

	exchange := google.lastExchange(t)
	if got := exchange.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q", got)
	}
	if got := exchange.Get("code"); got != "auth-code-1" {
		t.Errorf("exchanged code = %q, want auth-code-1", got)
	}
	if got := exchange.Get("redirect_uri"); got != consent.Get("redirect_uri") {
		t.Errorf("exchange redirect_uri = %q, want %q", got, consent.Get("redirect_uri"))
	}
	if got := exchange.Get("client_secret"); got != "GOCSPX-secret" {
		t.Errorf("exchange client_secret = %q", got)
	}

	stored, err := store.Load("personal")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if stored == nil {
		t.Fatal("Connect stored no token")
	}
	if stored.AccessToken != "access-1" || stored.RefreshToken != "refresh-1" {
		t.Errorf("stored token = %+v", stored)
	}
	if got := mode(t, filepath.Join(dir, "google_token_personal.json")); got != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", got)
	}
	if !reg.Connected("personal") {
		t.Error("Connected reported false after a successful Connect")
	}
	if accounts, err := reg.Accounts(); err != nil || len(accounts) != 1 || accounts[0] != "personal" {
		t.Errorf("Accounts() = %v, %v", accounts, err)
	}
}

func TestConnectRejectsBadCallbacks(t *testing.T) {
	cases := []struct {
		name     string
		callback func(q url.Values) url.Values
		want     string
	}{
		{
			name: "state mismatch",
			callback: func(q url.Values) url.Values {
				return url.Values{"state": {"forged-" + q.Get("state")}, "code": {"auth-code-1"}}
			},
			want: "state mismatch",
		},
		{
			name: "consent declined",
			callback: func(q url.Values) url.Values {
				return url.Values{"state": {q.Get("state")}, "error": {"access_denied"}}
			},
			want: "consent declined",
		},
		{
			name: "no code",
			callback: func(q url.Values) url.Values {
				return url.Values{"state": {q.Get("state")}}
			},
			want: "no authorization code",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			google := newFakeGoogle(t)
			reg, store, dir := testRegistry(t, google)

			_, err := reg.Connect(offlineContext(t), "personal", browser(t, nil, tc.callback), ConnectOptions{
				Timeout:     10 * time.Second,
				UserInfoURL: google.userInfoURL(),
			})
			if err == nil {
				t.Fatal("Connect succeeded on a bad callback")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}

			// A callback the flow did not ask for must never be turned into a
			// token: no exchange, nothing stored.
			if exchanges, _ := google.counts(); exchanges != 0 {
				t.Errorf("the token endpoint saw %d exchanges, want 0", exchanges)
			}
			if accounts, err := store.Accounts(); err != nil || len(accounts) != 0 {
				t.Errorf("Accounts() = %v, %v, want none", accounts, err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("token directory was created: %v", err)
			}
		})
	}
}

func TestConnectRequiresARefreshToken(t *testing.T) {
	google := newFakeGoogle(t)
	google.setToken(func(url.Values) (int, string) {
		return http.StatusOK, tokenBody("access-1", "", grantedScopeOrder)
	})
	reg, store, _ := testRegistry(t, google)

	_, err := reg.Connect(offlineContext(t), "personal", browser(t, nil, withCode("auth-code-1")), ConnectOptions{
		Timeout:     10 * time.Second,
		UserInfoURL: google.userInfoURL(),
	})
	if err == nil {
		t.Fatal("Connect accepted a grant with no refresh token")
	}
	if !strings.Contains(err.Error(), "refresh token") {
		t.Errorf("error %q does not name the missing refresh token", err)
	}
	// Storing an access-token-only grant would look connected and then fail at
	// the next restart, which is worse than failing here.
	if accounts, err := store.Accounts(); err != nil || len(accounts) != 0 {
		t.Errorf("Accounts() = %v, %v, want none", accounts, err)
	}
}

func TestConnectRejectsAnUnsafeAccountNameBeforeOpeningAnything(t *testing.T) {
	google := newFakeGoogle(t)
	reg, _, _ := testRegistry(t, google)

	opened := false
	_, err := reg.Connect(offlineContext(t), "../personal", func(string) error {
		opened = true
		return nil
	}, ConnectOptions{Timeout: 10 * time.Second, UserInfoURL: google.userInfoURL()})
	if err == nil {
		t.Fatal("Connect accepted an unsafe account name")
	}
	if opened {
		t.Error("Connect opened a consent page for an unsafe account name")
	}
	if exchanges, _ := google.counts(); exchanges != 0 {
		t.Errorf("the token endpoint saw %d exchanges, want 0", exchanges)
	}
}

func TestTokenSourceRefreshesAndPersists(t *testing.T) {
	google := newFakeGoogle(t)
	google.setToken(func(url.Values) (int, string) {
		return http.StatusOK, tokenBody("access-2", "refresh-2", nil)
	})
	reg, store, dir := testRegistry(t, google)

	seed := &oauth2.Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if err := store.Save("personal", seed); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	source, err := reg.TokenSource(offlineContext(t), "personal")
	if err != nil {
		t.Fatalf("TokenSource failed: %v", err)
	}
	tok, err := source.Token()
	if err != nil {
		t.Fatalf("Token failed: %v", err)
	}

	refresh := google.lastRefresh(t)
	if got := refresh.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", got)
	}
	if got := refresh.Get("refresh_token"); got != "refresh-1" {
		t.Errorf("refresh_token = %q, want refresh-1", got)
	}
	if tok.AccessToken != "access-2" {
		t.Errorf("Token().AccessToken = %q, want access-2", tok.AccessToken)
	}

	// A rotated token that is not written back is lost at the next restart.
	stored, err := store.Load("personal")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if stored.AccessToken != "access-2" {
		t.Errorf("stored AccessToken = %q, want access-2", stored.AccessToken)
	}
	if stored.RefreshToken != "refresh-2" {
		t.Errorf("stored RefreshToken = %q, want refresh-2", stored.RefreshToken)
	}
	if got := mode(t, filepath.Join(dir, "google_token_personal.json")); got != 0o600 {
		t.Errorf("token file mode after refresh = %04o, want 0600", got)
	}

	// The fresh token is good for an hour; asking again must not spend another
	// round trip.
	if _, err := source.Token(); err != nil {
		t.Fatalf("second Token failed: %v", err)
	}
	if _, refreshes := google.counts(); refreshes != 1 {
		t.Errorf("the token endpoint saw %d refreshes, want 1", refreshes)
	}
}

func TestTokenSourceKeepsRefreshTokenWhenTheResponseOmitsIt(t *testing.T) {
	google := newFakeGoogle(t)
	// Google normally omits refresh_token on a refresh. Persisting the response
	// verbatim would blank the only credential that survives a restart.
	google.setToken(func(url.Values) (int, string) {
		return http.StatusOK, tokenBody("access-2", "", nil)
	})
	reg, store, _ := testRegistry(t, google)

	if err := store.Save("personal", &oauth2.Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	source, err := reg.TokenSource(offlineContext(t), "personal")
	if err != nil {
		t.Fatalf("TokenSource failed: %v", err)
	}
	tok, err := source.Token()
	if err != nil {
		t.Fatalf("Token failed: %v", err)
	}
	if tok.RefreshToken != "refresh-1" {
		t.Errorf("Token().RefreshToken = %q, want refresh-1", tok.RefreshToken)
	}

	stored, err := store.Load("personal")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if stored.AccessToken != "access-2" {
		t.Errorf("stored AccessToken = %q, want access-2", stored.AccessToken)
	}
	if stored.RefreshToken != "refresh-1" {
		t.Errorf("stored RefreshToken = %q, want refresh-1; the rotation blanked the account", stored.RefreshToken)
	}
}

func TestRefreshingOneAccountLeavesTheOtherAlone(t *testing.T) {
	google := newFakeGoogle(t)
	google.setToken(func(url.Values) (int, string) {
		return http.StatusOK, tokenBody("access-personal-2", "refresh-personal-2", nil)
	})
	reg, store, dir := testRegistry(t, google)

	if err := store.Save("personal", &oauth2.Token{
		AccessToken:  "access-personal-1",
		RefreshToken: "refresh-personal-1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("Save(personal) failed: %v", err)
	}
	if err := store.Save("work", &oauth2.Token{
		AccessToken:  "access-work-1",
		RefreshToken: "refresh-work-1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("Save(work) failed: %v", err)
	}

	workPath := filepath.Join(dir, "google_token_work.json")
	before := hashFile(t, workPath)

	source, err := reg.TokenSource(offlineContext(t), "personal")
	if err != nil {
		t.Fatalf("TokenSource failed: %v", err)
	}
	if _, err := source.Token(); err != nil {
		t.Fatalf("Token failed: %v", err)
	}

	if after := hashFile(t, workPath); after != before {
		t.Error("refreshing personal rewrote work's token file")
	}
	if got := mode(t, workPath); got != 0o600 {
		t.Errorf("work token file mode = %04o, want 0600", got)
	}
	work, err := store.Load("work")
	if err != nil {
		t.Fatalf("Load(work) failed: %v", err)
	}
	if work.AccessToken != "access-work-1" || work.RefreshToken != "refresh-work-1" {
		t.Errorf("work token = %+v, want it untouched", work)
	}
	if _, refreshes := google.counts(); refreshes != 1 {
		t.Errorf("the token endpoint saw %d refreshes, want 1", refreshes)
	}

	accounts, err := reg.Accounts()
	if err != nil {
		t.Fatalf("Accounts() failed: %v", err)
	}
	if len(accounts) != 2 || accounts[0] != "personal" || accounts[1] != "work" {
		t.Fatalf("Accounts() = %v, want [personal work]", accounts)
	}

	if err := reg.Disconnect("personal"); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}
	if reg.Connected("personal") {
		t.Error("Connected reported true after Disconnect")
	}
	if !reg.Connected("work") {
		t.Error("Disconnect(personal) also disconnected work")
	}
}

func hashFile(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s failed: %v", path, err)
	}
	return sha256.Sum256(raw)
}

// A guard on the guard: if the offline policy let anything through, every
// assertion above could be passing against the real Google.
func TestOfflinePolicyBlocksTheInternet(t *testing.T) {
	blocked := []string{
		hostOf(t, defaultUserInfoURL),
		hostOf(t, "https://oauth2.googleapis.com/token"),
		hostOf(t, "https://accounts.google.com/o/oauth2/auth"),
		"127.0.0.1.example.com",
	}
	for _, host := range blocked {
		if isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = true; the offline guard would let it through", host)
		}
	}
	for _, host := range []string{"127.0.0.1", "localhost", "::1"} {
		if !isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = false; the stubs are unreachable", host)
		}
	}
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s failed: %v", raw, err)
	}
	return u.Hostname()
}
