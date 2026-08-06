package oauth2app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// TokenStore is where an application keeps its refresh tokens. The package
// never chooses this: a token is the most sensitive thing the flow produces,
// and where it lands is the consumer's privacy decision.
type TokenStore interface {
	// Load returns the account's token, or (nil, nil) when there is none.
	Load(account string) (*oauth2.Token, error)
	// Save writes the account's token, replacing any existing one.
	Save(account string, tok *oauth2.Token) error
	// Delete removes the account's token. Deleting an absent one is not an error.
	Delete(account string) error
	// Accounts lists the accounts holding a token.
	Accounts() ([]string, error)
}

// FileTokenStore keeps one JSON file per account in a directory, at mode
// 0600, written atomically so a crash mid-write cannot leave a truncated
// token where a valid one was.
type FileTokenStore struct {
	// Dir holds the token files. Created at 0700 on first write.
	Dir string
	// Prefix and Suffix bracket the account name in the filename. Zero values
	// give "token_<account>.json"; a provider package sets its own.
	Prefix string
	Suffix string

	mu sync.Mutex
}

// NewFileTokenStore returns a store writing into dir with the generic naming.
func NewFileTokenStore(dir string) *FileTokenStore {
	return &FileTokenStore{Dir: dir, Prefix: "token_", Suffix: ".json"}
}

func (s *FileTokenStore) prefix() string {
	if s.Prefix == "" {
		return "token_"
	}
	return s.Prefix
}

func (s *FileTokenStore) suffix() string {
	if s.Suffix == "" {
		return ".json"
	}
	return s.Suffix
}

func (s *FileTokenStore) path(account string) (string, error) {
	name, err := NormalizeAccount(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, s.prefix()+name+s.suffix()), nil
}

// Load reads an account's token. A missing file is not an error: an account
// that was never connected is a normal state, not a fault.
func (s *FileTokenStore) Load(account string) (*oauth2.Token, error) {
	path, err := s.path(account)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read token for %s: %w", account, err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("parse token for %s: %w", account, err)
	}
	return &tok, nil
}

// Save writes an account's token at mode 0600, replacing atomically.
func (s *FileTokenStore) Save(account string, tok *oauth2.Token) error {
	if tok == nil {
		return fmt.Errorf("refusing to store a nil token for %s", account)
	}
	path, err := s.path(account)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".token-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	// Mode is set before the bytes land, so the secret is never briefly
	// readable by anyone else.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write token for %s: %w", account, err)
	}
	return os.Chmod(path, 0o600)
}

// Delete removes an account's token.
func (s *FileTokenStore) Delete(account string) error {
	path, err := s.path(account)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Accounts lists the accounts with a token file, in name order.
func (s *FileTokenStore) Accounts() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix, suffix := s.prefix(), s.suffix()
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix))
	}
	sort.Strings(out)
	return out, nil
}

// MemoryTokenStore keeps tokens in memory. For tests, and for a caller that
// wants a flow without persistence.
type MemoryTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*oauth2.Token
}

// NewMemoryTokenStore returns an empty in-memory store.
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{tokens: map[string]*oauth2.Token{}}
}

func (m *MemoryTokenStore) Load(account string) (*oauth2.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tokens[account], nil
}

func (m *MemoryTokenStore) Save(account string, tok *oauth2.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens == nil {
		m.tokens = map[string]*oauth2.Token{}
	}
	copied := *tok
	m.tokens[account] = &copied
	return nil
}

func (m *MemoryTokenStore) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, account)
	return nil
}

func (m *MemoryTokenStore) Accounts() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.tokens))
	for account := range m.tokens {
		out = append(out, account)
	}
	sort.Strings(out)
	return out, nil
}
