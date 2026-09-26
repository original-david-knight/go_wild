package oauth2app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestFileTokenStoreRoundTripsAtMode0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tokens")
	store := NewFileTokenStore(dir)

	if tok, err := store.Load("work"); tok != nil || err != nil {
		t.Fatalf("Load before any Save = %v, %v; want nil, nil", tok, err)
	}
	if got, err := store.Accounts(); got != nil || err != nil {
		t.Fatalf("Accounts on a missing dir = %v, %v; want nil, nil", got, err)
	}

	in := &oauth2.Token{AccessToken: "at", RefreshToken: "rt", TokenType: "Bearer"}
	if err := store.Save("Work", in); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "token_work.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Save should write %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("token dir mode = %v, want 0700", dirInfo.Mode().Perm())
	}

	got, err := store.Load("work")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" || got.TokenType != "Bearer" {
		t.Fatalf("Load = %+v", got)
	}

	// Overwrite replaces the token and leaves no temp files behind.
	if err := store.Save("work", &oauth2.Token{AccessToken: "at2", RefreshToken: "rt2"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Load("work"); got.AccessToken != "at2" {
		t.Fatalf("after overwrite Load = %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "token_work.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir holds %v, want only token_work.json", names)
	}

	if err := store.Delete("work"); err != nil {
		t.Fatal(err)
	}
	if tok, err := store.Load("work"); tok != nil || err != nil {
		t.Fatalf("Load after Delete = %v, %v", tok, err)
	}
	if err := store.Delete("work"); err != nil {
		t.Fatalf("deleting an absent token should not fail, got %v", err)
	}
}

func TestFileTokenStoreAccountsFiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	store := &FileTokenStore{Dir: dir, Prefix: "google_", Suffix: ".tok"}
	for _, a := range []string{"zed", "amy", "mid"} {
		if err := store.Save(a, &oauth2.Token{AccessToken: a}); err != nil {
			t.Fatal(err)
		}
	}
	// Files and dirs that do not match the naming are ignored.
	os.WriteFile(filepath.Join(dir, "google_x.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(dir, "other_y.tok"), []byte("{}"), 0o600)
	os.Mkdir(filepath.Join(dir, "google_dir.tok"), 0o700)

	got, err := store.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"amy", "mid", "zed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Accounts = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "google_amy.tok")); err != nil {
		t.Fatalf("custom prefix and suffix should name the file: %v", err)
	}

	// Zero Prefix and Suffix fall back to token_<account>.json.
	plain := &FileTokenStore{Dir: dir}
	if err := plain.Save("bob", &oauth2.Token{AccessToken: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "token_bob.json")); err != nil {
		t.Fatalf("zero-value naming: %v", err)
	}
}

func TestFileTokenStoreErrors(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTokenStore(dir)

	if err := store.Save("a", nil); err == nil || err.Error() != "refusing to store a nil token for a" {
		t.Fatalf("Save(nil) = %v", err)
	}
	for _, bad := range []string{"", "../etc", "a b"} {
		if _, err := store.Load(bad); err == nil {
			t.Errorf("Load(%q) should refuse the account name", bad)
		}
		if err := store.Save(bad, &oauth2.Token{}); err == nil {
			t.Errorf("Save(%q) should refuse the account name", bad)
		}
		if err := store.Delete(bad); err == nil {
			t.Errorf("Delete(%q) should refuse the account name", bad)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "token_broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("broken"); err == nil || !strings.HasPrefix(err.Error(), "parse token for broken:") {
		t.Fatalf("Load of a corrupt file = %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "token_isdir.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("isdir"); err == nil || !strings.HasPrefix(err.Error(), "read token for isdir:") {
		t.Fatalf("Load of a directory = %v", err)
	}
	if err := store.Save("isdir", &oauth2.Token{AccessToken: "x"}); err == nil || !strings.HasPrefix(err.Error(), "write token for isdir:") {
		t.Fatalf("Save over a directory = %v", err)
	}
	os.WriteFile(filepath.Join(dir, "token_isdir.json", "keep"), nil, 0o600)
	if err := store.Delete("isdir"); err == nil {
		t.Fatal("Delete of a non-empty directory should fail")
	}

	// A Dir that is a file cannot hold tokens.
	file := filepath.Join(dir, "plainfile")
	os.WriteFile(file, nil, 0o600)
	onFile := NewFileTokenStore(file)
	if err := onFile.Save("a", &oauth2.Token{}); err == nil || !strings.HasPrefix(err.Error(), "create token dir:") {
		t.Fatalf("Save into a file-as-dir = %v", err)
	}
	if _, err := onFile.Accounts(); err == nil {
		t.Fatal("Accounts on a file-as-dir should fail")
	}
}

func TestMemoryTokenStoreCopiesAndLists(t *testing.T) {
	var store MemoryTokenStore // zero value must work
	tok := &oauth2.Token{AccessToken: "a"}
	if err := store.Save("b", tok); err != nil {
		t.Fatal(err)
	}
	store.Save("a", &oauth2.Token{AccessToken: "x"})
	tok.AccessToken = "mutated"
	if got, _ := store.Load("b"); got.AccessToken != "a" {
		t.Fatalf("store kept the caller's pointer: %q", got.AccessToken)
	}
	if got, _ := store.Accounts(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Accounts = %v", got)
	}
	store.Delete("a")
	if got, _ := store.Accounts(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("Accounts after Delete = %v", got)
	}
}

func TestTokenSourceRefreshesAndPersists(t *testing.T) {
	provider, exchanges := tokenEndpoint(t, map[string]any{
		"access_token": "fresh", "token_type": "Bearer", "expires_in": 3600,
	})
	flow, store := testFlow(t, provider.URL)
	ctx := context.Background()

	if _, err := flow.TokenSource(ctx, "nobody"); err == nil || err.Error() != "nobody: no stored token" {
		t.Fatalf("TokenSource for an unknown account = %v", err)
	}

	store.Save("me", &oauth2.Token{AccessToken: "stale", RefreshToken: "rt-keep", Expiry: time.Unix(1, 0)})
	src, err := flow.TokenSource(ctx, "me")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := src.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "fresh" || tok.RefreshToken != "rt-keep" {
		t.Fatalf("refreshed token = %+v, want fresh access and the carried refresh token", tok)
	}
	if len(*exchanges) != 1 || (*exchanges)[0].Get("grant_type") != "refresh_token" || (*exchanges)[0].Get("refresh_token") != "rt-keep" {
		t.Fatalf("token endpoint saw %v", *exchanges)
	}
	saved, _ := store.Load("me")
	if saved.AccessToken != "fresh" || saved.RefreshToken != "rt-keep" {
		t.Fatalf("persisted token = %+v", saved)
	}
}

type loadFailStore struct{ MemoryTokenStore }

func (*loadFailStore) Load(string) (*oauth2.Token, error) { return nil, os.ErrPermission }

func TestTokenSourceSurfacesStoreErrors(t *testing.T) {
	flow := &Flow{Config: &oauth2.Config{}, Store: &loadFailStore{}}
	if _, err := flow.TokenSource(context.Background(), "me"); err != os.ErrPermission {
		t.Fatalf("TokenSource = %v, want the store's error", err)
	}
}

func TestPersistingSourceSurfacesUpstreamError(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer provider.Close()
	flow, store := testFlow(t, provider.URL)
	store.Save("me", &oauth2.Token{AccessToken: "stale", RefreshToken: "revoked", Expiry: time.Unix(1, 0)})
	src, err := flow.TokenSource(context.Background(), "me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Token(); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("Token = %v, want the provider's invalid_grant", err)
	}
	if saved, _ := store.Load("me"); saved.AccessToken != "stale" {
		t.Fatalf("a failed refresh must not touch the stored token, got %+v", saved)
	}
}

func TestExchangeErrors(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer provider.Close()
	flow, store := testFlow(t, provider.URL)
	ctx := context.Background()

	if _, err := flow.Exchange(ctx, "bad name", "http://x/cb", "code"); err == nil || !strings.Contains(err.Error(), "may hold only letters") {
		t.Fatalf("Exchange with a bad account = %v", err)
	}
	if _, err := flow.Exchange(ctx, "me", "http://x/cb", "code"); err == nil || !strings.HasPrefix(err.Error(), "exchange authorization code:") {
		t.Fatalf("Exchange against a failing provider = %v", err)
	}
	if got, _ := store.Accounts(); len(got) != 0 {
		t.Fatalf("a failed exchange stored %v", got)
	}
}

func TestExchangeSurfacesStoreFailure(t *testing.T) {
	provider, _ := tokenEndpoint(t, map[string]any{"access_token": "a", "refresh_token": "r", "token_type": "Bearer"})
	flow, _ := testFlow(t, provider.URL)
	flow.Store = &failingStore{}
	if _, err := flow.Exchange(context.Background(), "me", "http://x/cb", "code"); err == nil || err.Error() != "disk full" {
		t.Fatalf("Exchange = %v, want the store's disk full error", err)
	}
}

func TestStatesCancelForgetsEverything(t *testing.T) {
	s := NewStates(0) // zero TTL falls back to the default
	a, _ := s.New()
	b, _ := s.NewWith("payload")
	if !s.Pending() {
		t.Fatal("two states minted, Pending should be true")
	}
	s.Cancel()
	if s.Pending() || s.Take(a) || s.Take(b) {
		t.Fatal("Cancel should forget every outstanding state")
	}
}
