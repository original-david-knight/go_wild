package googleauth

import (
	"sync"

	"github.com/original-david-knight/go_wild/oauth2app"
	"golang.org/x/oauth2"
)

// TokenStore is where an application keeps its refresh tokens — oauth2app's
// interface, re-exported so consumers of this package need not import both.
// The package never chooses where tokens land: that is the consumer's
// privacy decision.
type TokenStore = oauth2app.TokenStore

// MemoryTokenStore keeps tokens in memory. For tests, and for a caller that
// wants a flow without persistence.
type MemoryTokenStore = oauth2app.MemoryTokenStore

// NewMemoryTokenStore returns an empty in-memory store.
func NewMemoryTokenStore() *MemoryTokenStore { return oauth2app.NewMemoryTokenStore() }

// FileTokenStore keeps one JSON file per account in a directory, at mode
// 0600 — oauth2app's file store, carrying this package's historical
// zero-value naming: an unset Prefix/Suffix still reads and writes
// google_token_<account>.json.
type FileTokenStore struct {
	// Dir holds the token files. Created at 0700 on first write.
	Dir string
	// Prefix and Suffix bracket the account name in the filename. Zero values
	// give "google_token_<account>.json".
	Prefix string
	Suffix string

	mu sync.Mutex
}

// NewFileTokenStore returns a store writing into dir.
func NewFileTokenStore(dir string) *FileTokenStore {
	return &FileTokenStore{Dir: dir, Prefix: "google_token_", Suffix: ".json"}
}

// inner is the generic store this one delegates to, built per call so the
// public fields stay live configuration exactly as they always were.
func (s *FileTokenStore) inner() *oauth2app.FileTokenStore {
	prefix, suffix := s.Prefix, s.Suffix
	if prefix == "" {
		prefix = "google_token_"
	}
	if suffix == "" {
		suffix = ".json"
	}
	return &oauth2app.FileTokenStore{Dir: s.Dir, Prefix: prefix, Suffix: suffix}
}

// Load reads an account's token. A missing file is not an error: an account
// that was never connected is a normal state, not a fault.
func (s *FileTokenStore) Load(account string) (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner().Load(account)
}

// Save writes an account's token at mode 0600, replacing atomically so a
// crash mid-write cannot leave a truncated token where a valid one was.
func (s *FileTokenStore) Save(account string, tok *oauth2.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner().Save(account, tok)
}

// Delete removes an account's token.
func (s *FileTokenStore) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner().Delete(account)
}

// Accounts lists the accounts with a token file, in name order.
func (s *FileTokenStore) Accounts() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner().Accounts()
}
