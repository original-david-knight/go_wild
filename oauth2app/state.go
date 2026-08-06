package oauth2app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// States tracks outstanding consent attempts: each state is random,
// single-use, and expires. It is what a fixed-route callback validates
// against — a redirect carrying a state this jar did not issue, already took,
// or let expire is refused. An expired one is a closed tab, not an error
// worth keeping.
type States struct {
	ttl time.Duration
	// now is the clock, replaceable so a test can expire states without
	// sleeping through a TTL.
	now func() time.Time

	mu      sync.Mutex
	pending map[string]time.Time
}

// NewStates builds a jar whose states expire after ttl.
func NewStates(ttl time.Duration) *States {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &States{ttl: ttl, now: time.Now, pending: map[string]time.Time{}}
}

// New mints a random state and remembers it.
func (s *States) New() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	s.pending[state] = s.now()
	return state, nil
}

// Take reports whether state is one this jar issued and still current, and
// forgets it either way — a state is single-use.
func (s *States) Take(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	_, ok := s.pending[state]
	delete(s.pending, state)
	return ok
}

// Pending reports whether any consent attempt is outstanding.
func (s *States) Pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	return len(s.pending) > 0
}

// Cancel forgets every outstanding attempt — the user closing the flow.
func (s *States) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = map[string]time.Time{}
}

func (s *States) expireLocked() {
	for state, started := range s.pending {
		if s.now().Sub(started) > s.ttl {
			delete(s.pending, state)
		}
	}
}
