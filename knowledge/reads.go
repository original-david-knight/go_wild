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
// caller that fetched them explicitly. Other IDs, unknown IDs and a zero
// reader count nothing. Like a search's, a failed recording is logged and
// never reaches the caller's read.
func (s *Service) NoteRead(ctx context.Context, db data.Database, reader Actor, ids ...string) {
	if !reader.valid() {
		return
	}
	var facts []string
	for _, id := range ids {
		if KindOf(id) == KindFact {
			facts = append(facts, id)
		}
	}
	if err := countReads(ctx, db, s.clock(), facts); err != nil {
		slog.Warn("knowledge: recording a read failed", "reader", reader.Name, "err", err)
	}
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
