package googleauth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

var (
	_ TokenStore = (*FileTokenStore)(nil)
	_ TokenStore = (*MemoryTokenStore)(nil)
)

func testToken(access, refresh string) *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).Round(time.Second),
	}
}

// mode returns a path's permission bits.
func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s failed: %v", path, err)
	}
	return info.Mode().Perm()
}

func TestFileTokenStoreRoundTrip(t *testing.T) {
	store := NewFileTokenStore(t.TempDir())
	want := testToken("access-1", "refresh-1")
	if err := store.Save("personal", want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := store.Load("personal")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("Load returned %+v, want %+v", got, want)
	}
	if !got.Expiry.Equal(want.Expiry) {
		t.Errorf("Expiry = %s, want %s", got.Expiry, want.Expiry)
	}

	// Account names are normalised on the way to the filename, so a differently
	// cased name is the same account.
	if got, err := store.Load("PERSONAL"); err != nil || got == nil || got.AccessToken != "access-1" {
		t.Errorf("Load(PERSONAL) = %+v, %v", got, err)
	}
}

func TestFileTokenStoreLoadMissingIsNotAnError(t *testing.T) {
	store := NewFileTokenStore(filepath.Join(t.TempDir(), "never-created"))
	tok, err := store.Load("personal")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if tok != nil {
		t.Fatalf("Load returned %+v, want nil", tok)
	}
	// An unconnected account is a normal state, so listing must work too.
	accounts, err := store.Accounts()
	if err != nil || len(accounts) != 0 {
		t.Fatalf("Accounts() = %v, %v", accounts, err)
	}
}

func TestFileTokenStoreLoadCorruptNamesTheAccount(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTokenStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "google_token_work.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, err := store.Load("work")
	if err == nil {
		t.Fatal("Load succeeded on a corrupt file")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("error %q does not name the account", err)
	}
}

func TestFileTokenStoreRefusesNilToken(t *testing.T) {
	store := NewFileTokenStore(t.TempDir())
	if err := store.Save("personal", nil); err == nil {
		t.Fatal("Save(nil) succeeded")
	}
}

func TestFileTokenStorePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lifedash")
	store := NewFileTokenStore(dir)
	if err := store.Save("personal", testToken("access-1", "refresh-1")); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if got := mode(t, dir); got != 0o700 {
		t.Errorf("token dir mode = %04o, want 0700", got)
	}
	path := filepath.Join(dir, "google_token_personal.json")
	if got := mode(t, path); got != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", got)
	}

	// A token file that pre-exists at a looser mode must not stay loose: the
	// rotated token is as sensitive as the first one.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	if err := store.Save("personal", testToken("access-2", "refresh-2")); err != nil {
		t.Fatalf("re-Save failed: %v", err)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("token file mode after re-save = %04o, want 0600", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "google_token_personal.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only the token file (a temp file was left behind)", names)
	}
}

func TestFileTokenStoreDefaultsPrefixAndSuffix(t *testing.T) {
	dir := t.TempDir()
	store := &FileTokenStore{Dir: dir}
	if err := store.Save("work", testToken("access-1", "refresh-1")); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "google_token_work.json")); err != nil {
		t.Fatalf("stat expected filename failed: %v", err)
	}
}

func TestFileTokenStoreDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTokenStore(dir)
	if err := store.Save("personal", testToken("access-1", "refresh-1")); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Delete("personal"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "google_token_personal.json")); !os.IsNotExist(err) {
		t.Fatalf("token file survived Delete: %v", err)
	}
	// Deleting an account that was never connected is not a fault.
	if err := store.Delete("personal"); err != nil {
		t.Fatalf("second Delete failed: %v", err)
	}
}

func TestFileTokenStoreAccountsListing(t *testing.T) {
	dir := t.TempDir()
	store := NewFileTokenStore(dir)
	for _, account := range []string{"work", "personal"} {
		if err := store.Save(account, testToken("access-"+account, "refresh-"+account)); err != nil {
			t.Fatalf("Save(%s) failed: %v", account, err)
		}
	}

	// Files the store did not write — including a leftover temp file of the shape
	// Save uses — must never be reported as accounts.
	strays := []string{".token-123456", "google_client.json", "google_token_work.json.bak", "lifedash.env", "notes"}
	for _, name := range strays {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write stray %s failed: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "google_token_dir.json"), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	accounts, err := store.Accounts()
	if err != nil {
		t.Fatalf("Accounts() failed: %v", err)
	}
	if len(accounts) != 2 || accounts[0] != "personal" || accounts[1] != "work" {
		t.Fatalf("Accounts() = %v, want [personal work]", accounts)
	}
}

// A second store instance stands in for a second process: it shares nothing with
// the writer but the directory, so what it observes is what the filesystem
// actually exposes mid-write.
func TestFileTokenStoreSaveIsAtomicForConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	writer := NewFileTokenStore(dir)
	reader := NewFileTokenStore(dir)

	if err := writer.Save("personal", testToken("access-0", "refresh-0")); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= rounds; i++ {
			// A long, varying payload gives a torn write somewhere to show up.
			tok := testToken("access-"+strings.Repeat("x", i), "refresh-"+strings.Repeat("y", i))
			if err := writer.Save("personal", tok); err != nil {
				t.Errorf("Save failed: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			tok, err := reader.Load("personal")
			if err != nil {
				t.Errorf("Load saw a partial file: %v", err)
				return
			}
			if tok == nil {
				t.Error("Load saw no file while one was being replaced")
				return
			}
			if !strings.HasPrefix(tok.AccessToken, "access-") || !strings.HasPrefix(tok.RefreshToken, "refresh-") {
				t.Errorf("Load returned a half-written token: %+v", tok)
				return
			}
		}
	}()
	wg.Wait()
}

func TestMemoryTokenStore(t *testing.T) {
	store := NewMemoryTokenStore()

	tok, err := store.Load("personal")
	if err != nil || tok != nil {
		t.Fatalf("Load of an unknown account = %+v, %v", tok, err)
	}

	saved := testToken("access-1", "refresh-1")
	if err := store.Save("personal", saved); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Save("work", testToken("access-2", "refresh-2")); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// The store keeps a copy: a caller that reuses its token struct after Save
	// must not be able to rewrite what was stored.
	saved.AccessToken = "mutated"
	saved.RefreshToken = "mutated"
	got, err := store.Load("personal")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got.AccessToken != "access-1" || got.RefreshToken != "refresh-1" {
		t.Errorf("Load returned %+v; the store aliased the caller's token", got)
	}

	accounts, err := store.Accounts()
	if err != nil {
		t.Fatalf("Accounts() failed: %v", err)
	}
	if len(accounts) != 2 || accounts[0] != "personal" || accounts[1] != "work" {
		t.Fatalf("Accounts() = %v, want [personal work]", accounts)
	}

	if err := store.Delete("personal"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if tok, err := store.Load("personal"); err != nil || tok != nil {
		t.Fatalf("Load after Delete = %+v, %v", tok, err)
	}
	if err := store.Delete("personal"); err != nil {
		t.Fatalf("second Delete failed: %v", err)
	}
}
