package gowild_knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// Errors callers map to responses. Every error this package returns for a
// caller mistake wraps one of these.
var (
	ErrInvalid   = errors.New("invalid")
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrConflict  = errors.New("conflict")
)

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func notFound(what, id string) error { return fmt.Errorf("%w: %s %s", ErrNotFound, what, id) }

// Actor is who a write speaks as. The owner is authoritative; an agent is
// named so its writes can be attributed and fenced.
type Actor struct {
	Kind string // AuthorOwner or AuthorAgent
	Name string
}

// Owner is the owner actor.
var Owner = Actor{Kind: AuthorOwner, Name: "owner"}

// Agent names an agent actor.
func Agent(name string) Actor { return Actor{Kind: AuthorAgent, Name: name} }

func (a Actor) owner() bool { return a.Kind == AuthorOwner }

func (a Actor) valid() bool {
	return a.Kind == AuthorOwner || (a.Kind == AuthorAgent && strings.TrimSpace(a.Name) != "")
}

// canModify is the write fence: the owner changes anything; an agent only
// what agents wrote and the owner has not verified.
func (a Actor) canModify(authorKind string, verified bool) bool {
	if a.owner() {
		return true
	}
	return authorKind == AuthorAgent && !verified
}

// Service is the knowledge base over any gowild_data database.
type Service struct {
	embedder Embedder
	now      func() time.Time

	mu      sync.Mutex
	vectors *vectorCaps
}

type vectorCaps struct{ ok, iterative bool }

// Option configures a Service.
type Option func(*Service)

// WithEmbedder enables semantic search and embedding of new rows.
func WithEmbedder(e Embedder) Option { return func(s *Service) { s.embedder = e } }

// WithClock overrides time.Now.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// New builds a Service.
func New(opts ...Option) *Service {
	s := &Service{now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) clock() time.Time { return s.now().UTC() }

// SemanticEnabled reports whether an embedder is configured.
func (s *Service) SemanticEnabled() bool { return s.embedder != nil }

func (s *Service) vectorCaps(ctx context.Context, db data.Database) vectorCaps {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vectors != nil {
		return *s.vectors
	}
	ok, iterative, err := vectorSupport(ctx, db)
	if err != nil {
		return vectorCaps{}
	}
	s.vectors = &vectorCaps{ok, iterative}
	return *s.vectors
}

// --- identifiers and validation ---

func newID(prefix string) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}

// ItemID is the stable ID for (source, external ID).
func ItemID(sourceID, externalID string) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + externalID))
	return "itm_" + hex.EncodeToString(sum[:16])
}

// KindOf names the record kind an ID belongs to, or "".
func KindOf(id string) string {
	switch {
	case strings.HasPrefix(id, "itm_"):
		return KindItem
	case strings.HasPrefix(id, "fct_"):
		return KindFact
	case strings.HasPrefix(id, "not_"):
		return KindNote
	case strings.HasPrefix(id, "ent_"):
		return KindEntity
	}
	return ""
}

var (
	contextPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:_.-]{0,63}$`)
	sourcePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}:[a-z0-9][a-z0-9_.@+-]{0,127}$`)
	kindPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	tagPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,63}$`)
)

// DefaultContext is where records land when the writer names none.
const DefaultContext = "personal"

func normContext(c string) (string, error) {
	c = strings.ToLower(strings.TrimSpace(c))
	if c == "" {
		return DefaultContext, nil
	}
	if !contextPattern.MatchString(c) {
		return "", invalidf("context %q must be lowercase letters, digits and :_.- (at most 64)", c)
	}
	return c, nil
}

// NormalizeAlias lowercases and trims an alias. Aliases are "scheme:value"
// ("email:a@b.com", "slack:T01/U02", "name:alice smith").
func NormalizeAlias(alias string) (string, error) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	scheme, value, ok := strings.Cut(alias, ":")
	if !ok || scheme == "" || strings.TrimSpace(value) == "" || !kindPattern.MatchString(scheme) || len(alias) > 320 {
		return "", invalidf("alias %q must look like scheme:value, e.g. email:someone@example.com", alias)
	}
	return alias, nil
}

func normTags(tags []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if t == "" || seen[t] {
			continue
		}
		if !tagPattern.MatchString(t) {
			return nil, invalidf("tag %q must be lowercase letters, digits and _./- (at most 64)", t)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 32 {
		return nil, invalidf("at most 32 tags")
	}
	return out, nil
}

func checkLen(field, value string, min, max int) error {
	n := utf8.RuneCountInString(value)
	if n < min {
		return invalidf("%s is required", field)
	}
	if n > max {
		return invalidf("%s is longer than %d characters", field, max)
	}
	return nil
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// max is a byte budget; back up to a rune boundary.
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

func hashText(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// --- links ---

func tagID(tag string) string { return "tag:" + tag }

func linkID(from, rel, to string) string { return from + "|" + rel + "|" + to }

// linksFrom lists the targets of from's links under rel.
func linksFrom(ctx context.Context, db data.Database, from, rel string) ([]string, error) {
	rows, err := dbx.All[Link](ctx, db, data.QueryOpts{Where: map[string]any{"from_id": from, "rel": rel}, OrderBy: "created_at"})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ToID)
	}
	return out, nil
}

// linksTo lists the sources of links under rel that point at to.
func linksTo(ctx context.Context, db data.Database, to, rel string) ([]string, error) {
	rows, err := dbx.All[Link](ctx, db, data.QueryOpts{Where: map[string]any{"to_id": to, "rel": rel}})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.FromID)
	}
	return out, nil
}

// setLinks makes from's rel links exactly targets.
func (s *Service) setLinks(ctx context.Context, db data.Database, from, rel string, targets []string) error {
	current, err := linksFrom(ctx, db, from, rel)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, t := range targets {
		want[t] = true
	}
	have := map[string]bool{}
	for _, t := range current {
		have[t] = true
		if !want[t] {
			if err := db.Table(Link{}).Delete(ctx, linkID(from, rel, t)); err != nil {
				return err
			}
		}
	}
	for _, t := range targets {
		if have[t] {
			continue
		}
		if err := db.Table(Link{}).Insert(ctx, &Link{ID: linkID(from, rel, t), FromID: from, Rel: rel, ToID: t, CreatedAt: s.clock()}); err != nil {
			return err
		}
	}
	return nil
}

// dropLinks removes every link from or to id.
func dropLinks(ctx context.Context, db data.Database, id string) error {
	for _, col := range []string{"from_id", "to_id"} {
		rows, err := dbx.All[Link](ctx, db, data.QueryOpts{Where: map[string]any{col: id}})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := db.Table(Link{}).Delete(ctx, r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func tagsOf(ctx context.Context, db data.Database, id string) ([]string, error) {
	ids, err := linksFrom(ctx, db, id, RelTag)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, t := range ids {
		out = append(out, strings.TrimPrefix(t, "tag:"))
	}
	return out, nil
}

func tagLinks(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, tagID(t))
	}
	return out
}

// resolveEntity follows merges to the surviving entity.
func resolveEntity(ctx context.Context, db data.Database, id string) (*Entity, error) {
	for range 8 {
		e, err := dbx.Get[Entity](ctx, db, id)
		if err != nil {
			return nil, err
		}
		if e == nil {
			return nil, notFound("entity", id)
		}
		if e.MergedInto == "" {
			return e, nil
		}
		id = e.MergedInto
	}
	return nil, fmt.Errorf("entity %s: merge chain too long", id)
}

func (s *Service) resolveEntities(ctx context.Context, db data.Database, ids []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		e, err := resolveEntity(ctx, db, id)
		if err != nil {
			return nil, err
		}
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e.ID)
		}
	}
	return out, nil
}

// transact runs fn in a transaction.
func transact(ctx context.Context, db data.Database, fn func(tx data.Database) error) error {
	return db.RunInTransaction(ctx, fn)
}
