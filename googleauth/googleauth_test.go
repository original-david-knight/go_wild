package googleauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2/google"
)

// writeClientSecret drops a client-secret JSON in a temp dir and returns its path.
func writeClientSecret(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client_secret.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write client secret failed: %v", err)
	}
	return path
}

const installedClientJSON = `{
  "installed": {
    "client_id": "1234.apps.googleusercontent.com",
    "client_secret": "GOCSPX-secret",
    "project_id": "super-cosmic-genius-4aa3b",
    "auth_uri": "https://accounts.example/auth",
    "token_uri": "https://accounts.example/token",
    "redirect_uris": ["http://localhost"]
  }
}`

func TestLoadClientConfigInstalled(t *testing.T) {
	cfg, err := LoadClientConfig(writeClientSecret(t, installedClientJSON))
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if cfg.ClientID != "1234.apps.googleusercontent.com" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "GOCSPX-secret" {
		t.Errorf("ClientSecret = %q", cfg.ClientSecret)
	}
	if cfg.ProjectID != "super-cosmic-genius-4aa3b" {
		t.Errorf("ProjectID = %q", cfg.ProjectID)
	}
	if cfg.AuthURI != "https://accounts.example/auth" {
		t.Errorf("AuthURI = %q", cfg.AuthURI)
	}
	if cfg.TokenURI != "https://accounts.example/token" {
		t.Errorf("TokenURI = %q", cfg.TokenURI)
	}
}

const webClientJSON = `{
  "web": {
    "client_id": "5678.apps.googleusercontent.com",
    "client_secret": "GOCSPX-web-secret",
    "project_id": "super-cosmic-genius-4aa3b",
    "auth_uri": "https://accounts.example/auth",
    "token_uri": "https://accounts.example/token",
    "redirect_uris": ["https://app.example/oauth/google/callback"]
  }
}`

// A Web-application client loads like any other — it runs the hosted ceremony
// rather than the loopback flow — and the config says which kind it is, so a
// consumer can pick the ceremony.
func TestLoadClientConfigWebClient(t *testing.T) {
	cfg, err := LoadClientConfig(writeClientSecret(t, webClientJSON))
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if !cfg.Web {
		t.Error("Web = false for a web client")
	}
	if cfg.ClientID != "5678.apps.googleusercontent.com" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "GOCSPX-web-secret" {
		t.Errorf("ClientSecret = %q", cfg.ClientSecret)
	}

	installed, err := LoadClientConfig(writeClientSecret(t, installedClientJSON))
	if err != nil {
		t.Fatalf("LoadClientConfig(installed) failed: %v", err)
	}
	if installed.Web {
		t.Error("Web = true for an installed client")
	}
}

func TestLoadClientConfigErrorsNameTheProblem(t *testing.T) {
	cases := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{
			name: "missing file",
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.json") },
			want: "read client secret",
		},
		{
			name: "malformed json",
			path: func(t *testing.T) string { return writeClientSecret(t, `{"installed": {`) },
			want: "parse client secret",
		},
		{
			name: "no client id",
			path: func(t *testing.T) string {
				return writeClientSecret(t, `{"installed":{"client_secret":"s","project_id":"p"}}`)
			},
			want: "client id or secret missing",
		},
		{
			name: "neither installed nor web",
			path: func(t *testing.T) string { return writeClientSecret(t, `{"other":{"client_id":"1234"}}`) },
			want: "no installed or web client",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path(t)
			cfg, err := LoadClientConfig(path)
			if err == nil {
				t.Fatalf("LoadClientConfig succeeded, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadClientConfigDefaultsEndpoints(t *testing.T) {
	path := writeClientSecret(t, `{"installed":{"client_id":"1234","client_secret":"s"}}`)
	cfg, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if cfg.AuthURI != google.Endpoint.AuthURL {
		t.Errorf("AuthURI = %q, want %q", cfg.AuthURI, google.Endpoint.AuthURL)
	}
	if cfg.TokenURI != google.Endpoint.TokenURL {
		t.Errorf("TokenURI = %q, want %q", cfg.TokenURI, google.Endpoint.TokenURL)
	}
}

func TestNewRegistryDefaultsToTheFullScopeSet(t *testing.T) {
	r := NewRegistry(&ClientConfig{}, NewMemoryTokenStore())
	want := []string{ScopeGmailModify, ScopeCalendarReadonly, ScopeTasks, ScopeUserinfoEmail}
	got := r.Scopes()
	if len(got) != len(want) {
		t.Fatalf("Scopes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Scopes() = %v, want %v", got, want)
		}
	}

	// Scopes() must hand out a copy: a caller that appends to it would otherwise
	// widen every subsequent consent request.
	got[0] = "mutated"
	if r.Scopes()[0] != ScopeGmailModify {
		t.Error("Scopes() aliases the registry's slice")
	}
}

func TestNewRegistryHonoursExplicitScopes(t *testing.T) {
	r := NewRegistry(&ClientConfig{}, NewMemoryTokenStore(), ScopeTasks)
	if got := r.Scopes(); len(got) != 1 || got[0] != ScopeTasks {
		t.Fatalf("Scopes() = %v, want [%s]", got, ScopeTasks)
	}
}

func TestTokenSourceUnconnectedAccount(t *testing.T) {
	r := NewRegistry(&ClientConfig{}, NewFileTokenStore(t.TempDir()))
	if r.Connected("personal") {
		t.Error("Connected reported true with no token on disk")
	}
	_, err := r.TokenSource(context.Background(), "personal")
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
	if !strings.Contains(err.Error(), "personal") {
		t.Errorf("error %q does not name the account", err)
	}
}

func TestNormalizeAccountFoldsCase(t *testing.T) {
	for _, in := range []string{"Personal", "PERSONAL", "  personal  ", "personal"} {
		got, err := normalizeAccount(in)
		if err != nil {
			t.Fatalf("normalizeAccount(%q) failed: %v", in, err)
		}
		if got != "personal" {
			t.Errorf("normalizeAccount(%q) = %q, want %q", in, got, "personal")
		}
	}
	if got, err := normalizeAccount("work_2-b"); err != nil || got != "work_2-b" {
		t.Errorf("normalizeAccount(%q) = %q, %v", "work_2-b", got, err)
	}
}

// Account names become filenames, so anything that could steer a path — or that
// merely round-trips badly — is refused rather than sanitised.
var unsafeAccountNames = []string{
	"",
	"   ",
	"../etc/passwd",
	"..",
	"personal/work",
	`personal\work`,
	"my account",
	"café",
	"персонал",
	"personal.json",
	"personal\x00",
}

func TestNormalizeAccountRefusesUnsafeNames(t *testing.T) {
	for _, in := range unsafeAccountNames {
		if got, err := normalizeAccount(in); err == nil {
			t.Errorf("normalizeAccount(%q) = %q, want an error", in, got)
		}
	}
}

func TestFileTokenStoreRefusesUnsafeNamesBeforeTouchingDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tokens")
	store := NewFileTokenStore(dir)

	for _, in := range unsafeAccountNames {
		if err := store.Save(in, testToken("a", "r")); err == nil {
			t.Errorf("Save(%q) succeeded, want an error", in)
		}
		if _, err := store.Load(in); err == nil {
			t.Errorf("Load(%q) succeeded, want an error", in)
		}
		if err := store.Delete(in); err == nil {
			t.Errorf("Delete(%q) succeeded, want an error", in)
		}
	}

	// The rejection happens before any I/O: the store directory was never even
	// created, so no traversal could have landed a file outside it.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("store directory exists after rejected names: %v", err)
	}
}
