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
	pending map[string]stateEntry
}

// stateEntry is what the jar remembers about an outstanding state: when it
// was minted, and whatever the minting caller bound to it.
type stateEntry struct {
	started time.Time
	payload string
}

// NewStates builds a jar whose states expire after ttl.
func NewStates(ttl time.Duration) *States {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &States{ttl: ttl, now: time.Now, pending: map[string]stateEntry{}}
}

// New mints a random state and remembers it.
func (s *States) New() (string, error) {
	return s.NewWith("")
}

// NewWith mints a random state carrying a payload the taker gets back — how
// a callback route recovers what a consent attempt was for without trusting
// anything the redirect says. Same TTL and single-use rules as New.
func (s *States) NewWith(payload string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	s.pending[state] = stateEntry{started: s.now(), payload: payload}
	return state, nil
}

// Take reports whether state is one this jar issued and still current, and
// forgets it either way — a state is single-use.
func (s *States) Take(state string) bool {
	_, ok := s.TakeWith(state)
	return ok
}

// TakeWith is Take returning the payload the state was minted with. A state
// minted by New carries the empty payload.
func (s *States) TakeWith(state string) (payload string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	entry, ok := s.pending[state]
	delete(s.pending, state)
	return entry.payload, ok
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
	s.pending = map[string]stateEntry{}
}

// pendingMatch reports whether any still-current state's payload satisfies
// match. It consumes nothing: a ceremony asking "is this account waiting?"
// must not spend the state the answer is about.
func (s *States) pendingMatch(match func(payload string) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	for _, entry := range s.pending {
		if match(entry.payload) {
			return true
		}
	}
	return false
}

// cancelMatch forgets every outstanding state whose payload satisfies match —
// Cancel scoped to one ceremony's attempts in a shared jar.
func (s *States) cancelMatch(match func(payload string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for state, entry := range s.pending {
		if match(entry.payload) {
			delete(s.pending, state)
		}
	}
}

func (s *States) expireLocked() {
	for state, entry := range s.pending {
		if s.now().Sub(entry.started) > s.ttl {
			delete(s.pending, state)
		}
	}
}
