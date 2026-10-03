package gowild_knowledge

import (
	"context"
	"log/slog"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// Reads (README, "Reads"). A search with text by a named reader logs one
// kb_queries row and counts a read on every fact it returns; an explicit
// fetch of a fact counts through NoteRead. Browsing, listing and writing
// count nothing. A read changes neither ranking nor a fact's UpdatedAt.

// recordSearch logs q and counts a read on each fact among hits.
func (s *Service) recordSearch(ctx context.Context, db data.Database, q SearchQuery, hits []SearchHit) error {
	var facts []string
	for _, h := range hits {
		if h.Kind == KindFact {
			facts = append(facts, h.ID)
		}
	}
	now := s.clock()
	return transact(ctx, db, func(tx data.Database) error {
		row := &Query{
			ID: newID("qry_"), At: now, ReaderKind: q.Reader.Kind, Reader: q.Reader.Name,
			Text: q.Text, Context: strings.Join(q.Contexts, ","), Hits: len(hits), FactHits: len(facts),
		}
		if err := tx.Table(Query{}).Insert(ctx, row); err != nil {
			return err
		}
		return countReads(ctx, tx, now, facts)
	})
}

// NoteRead counts one read by reader on each fact named in ids, for a
// caller that fetched them explicitly, and brings back any of them that had
// expired. Other IDs, unknown IDs and a zero reader count nothing. Like a
// search's, a failed recording is logged and never reaches the caller's
// read.
func (s *Service) NoteRead(ctx context.Context, db data.Database, reader Actor, ids ...string) {
	if !reader.valid() {
		return
	}
	var facts []string
	var in []any
	for _, id := range ids {
		if KindOf(id) == KindFact {
			facts, in = append(facts, id), append(in, id)
		}
	}
	if len(facts) == 0 {
		return
	}
	err := transact(ctx, db, func(tx data.Database) error {
		if err := countReads(ctx, tx, s.clock(), facts); err != nil {
			return err
		}
		expired, err := dbx.All[Fact](ctx, tx, data.QueryOpts{Where: map[string]any{"expired": true}, WhereIn: map[string][]any{"id": in}})
		if err != nil {
			return err
		}
		for _, f := range expired {
			f.Expired = false
			if err := tx.Table(Fact{}).Update(ctx, f); err != nil {
				return err
			}
			if err := s.reindexFact(ctx, tx, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("knowledge: recording a read failed", "reader", reader.Name, "err", err)
	}
}

// ExpireUnread retires the agent facts nobody has read within window: an
// active fact whose last read, or its creation when it was never read, is
// before now-window becomes Expired and leaves search, as a retracted fact
// does. The owner's facts never expire. An explicit read brings a fact back
// (NoteRead). It reports how many facts expired.
func (s *Service) ExpireUnread(ctx context.Context, db data.Database, now time.Time, window time.Duration) (int, error) {
	cutoff := now.Add(-window)
	rows, err := dbx.All[Fact](ctx, db, data.QueryOpts{Where: map[string]any{
		"author_kind": AuthorAgent, "expired": false, "retracted": false, "superseded_by": "",
	}})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range rows {
		last := f.CreatedAt
		if f.LastReadAt.After(last) {
			last = f.LastReadAt
		}
		if !last.Before(cutoff) {
			continue
		}
		// One fact at a time, so an interrupted pass keeps what it did and
		// the next one picks up the rest.
		err := transact(ctx, db, func(tx data.Database) error {
			f.Expired = true
			if err := tx.Table(Fact{}).Update(ctx, f); err != nil {
				return err
			}
			return s.reindexFact(ctx, tx, f)
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// countReads bumps the read count and stamps the last read in one
// statement, so concurrent readers never lose a count and the fact's
// UpdatedAt stays the last edit.
func countReads(ctx context.Context, db data.Database, at time.Time, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	exec, backend, err := data.Raw(db)
	if err != nil {
		return err
	}
	// kb_facts is a gowild_data table, which stores SQLite times as RFC 3339.
	stamp := any(at)
	if backend == data.BackendSqlite {
		stamp = at.Format(time.RFC3339)
	}
	args := []any{stamp}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err = exec.ExecContext(ctx, rebind(backend,
		"UPDATE kb_facts SET read_count = read_count + 1, last_read_at = ? WHERE id IN ("+placeholders(len(ids))+")"), args...)
	return err
}

// ListQueries lists logged searches, newest first.
func (s *Service) ListQueries(ctx context.Context, db data.Database, limit int) ([]Query, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := dbx.All[Query](ctx, db, data.QueryOpts{OrderBy: "at", OrderDesc: true, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]Query, 0, len(rows))
	for _, q := range rows {
		out = append(out, *q)
	}
	return out, nil
}
