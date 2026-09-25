package gowild_knowledge

import (
	"context"
	"time"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// extractedCut separates never-extracted items (zero time) from extracted
// ones in SQL on both backends.
var extractedCut = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// PendingExtraction lists items no extractor has mined since they last
// changed, newest first, so fresh mail is distilled before the backfill.
func (s *Service) PendingExtraction(ctx context.Context, db data.Database, sourceID string, limit int) ([]ItemView, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	ids, err := s.pendingIDs(ctx, db, sourceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ItemView, 0, len(ids))
	for _, id := range ids {
		v, err := s.GetItem(ctx, db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// ClaimExtraction leases up to limit pending items to actor for lease, newest
// first, skipping items another extractor holds. The lease lapses on its own,
// so a crashed extractor's items return to the queue.
func (s *Service) ClaimExtraction(ctx context.Context, db data.Database, actor Actor, sourceID string, limit int, lease time.Duration) ([]ItemView, error) {
	if !actor.valid() {
		return nil, ErrForbidden
	}
	if lease <= 0 || lease > 6*time.Hour {
		lease = 30 * time.Minute
	}
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	// Read a wider window than the claim: some of it may be leased.
	candidates, err := s.pendingIDs(ctx, db, sourceID, limit*4)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	out := []ItemView{}
	for _, id := range candidates {
		if len(out) >= limit {
			break
		}
		it, err := dbx.Get[Item](ctx, db, id)
		if err != nil {
			return nil, err
		}
		if it == nil || it.ExtractLeaseUntil.After(now) {
			continue
		}
		prevBy, prevUntil := it.ExtractLeaseBy, it.ExtractLeaseUntil
		it.ExtractLeaseBy, it.ExtractLeaseUntil = actor.Name, now.Add(lease)
		ok, err := dbx.UpdateIf(ctx, db, it, map[string]any{"extract_lease_by": prevBy, "extract_lease_until": prevUntil, "content_hash": it.ContentHash})
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		v, err := s.GetItem(ctx, db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// ExtractedMark names an item an extractor finished and the content it saw.
type ExtractedMark struct {
	ID          string `json:"id"`
	ContentHash string `json:"content_hash"`
}

// MarkExtracted records items as mined. An item whose content changed since
// the extractor read it (another hash) stays pending.
func (s *Service) MarkExtracted(ctx context.Context, db data.Database, actor Actor, marks []ExtractedMark) (int, error) {
	if !actor.valid() {
		return 0, ErrForbidden
	}
	if len(marks) > 200 {
		return 0, invalidf("mark at most 200 items at once")
	}
	done := 0
	for _, m := range marks {
		it, err := dbx.Get[Item](ctx, db, m.ID)
		if err != nil {
			return done, err
		}
		if it == nil || it.ContentHash != m.ContentHash {
			continue
		}
		it.ExtractedAt = s.clock()
		it.ExtractLeaseBy, it.ExtractLeaseUntil = "", time.Time{}
		ok, err := dbx.UpdateIf(ctx, db, it, map[string]any{"content_hash": m.ContentHash})
		if err != nil {
			return done, err
		}
		if ok {
			done++
		}
	}
	return done, nil
}

func (s *Service) pendingIDs(ctx context.Context, db data.Database, sourceID string, limit int) ([]string, error) {
	exec, backend, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	cut := any(extractedCut)
	if backend == data.BackendSqlite {
		cut = extractedCut.Format(time.RFC3339)
	}
	query := `SELECT id FROM kb_items WHERE extracted_at < ?`
	args := []any{cut}
	if sourceID != "" {
		query += ` AND source_id = ?`
		args = append(args, sourceID)
	}
	query += ` ORDER BY occurred_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, rebind(backend, query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) pendingExtractionCount(ctx context.Context, db data.Database) (int, error) {
	exec, backend, err := data.Raw(db)
	if err != nil {
		return 0, err
	}
	cut := any(extractedCut)
	if backend == data.BackendSqlite {
		cut = extractedCut.Format(time.RFC3339)
	}
	var n int
	err = exec.QueryRowContext(ctx, rebind(backend, `SELECT count(*) FROM kb_items WHERE extracted_at < ?`), cut).Scan(&n)
	return n, err
}

// itemTimeArg formats a time for a raw comparison against a kb_items time
// column, which SQLite stores as RFC 3339 text.
func itemTimeArg(backend data.Backend, t time.Time) any {
	if backend == data.BackendSqlite {
		return t.UTC().Format(time.RFC3339)
	}
	return t.UTC()
}

// RequeueExtraction puts the items that occurred in [since, until) back in
// the extraction queue, whether mined or leased, so a worker mines them
// again. The owner uses it after changing what extraction is asked for. A
// zero until means no upper bound. It reports how many items changed.
func (s *Service) RequeueExtraction(ctx context.Context, db data.Database, actor Actor, since, until time.Time) (int, error) {
	if !actor.owner() {
		return 0, ErrForbidden
	}
	if since.IsZero() {
		return 0, invalidf("since is required")
	}
	if !until.IsZero() && !until.After(since) {
		return 0, invalidf("until must be after since")
	}
	exec, backend, err := data.Raw(db)
	if err != nil {
		return 0, err
	}
	// Mined items return to the queue; a pending item under lease loses the
	// lease, so it is claimable at once. A pending, unleased item is already
	// where it should be and does not count.
	query := `UPDATE kb_items SET extracted_at = ?, extract_lease_by = '', extract_lease_until = ? WHERE occurred_at >= ? AND (extracted_at >= ? OR extract_lease_until >= ?)`
	zero := itemTimeArg(backend, time.Time{})
	args := []any{zero, zero, itemTimeArg(backend, since), itemTimeArg(backend, extractedCut), itemTimeArg(backend, s.clock())}
	if !until.IsZero() {
		query += ` AND occurred_at < ?`
		args = append(args, itemTimeArg(backend, until))
	}
	res, err := exec.ExecContext(ctx, rebind(backend, query), args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// SkipExtraction marks every pending item that occurred before the given
// time as extracted without mining it, so the queue holds only what the
// owner wants distilled. An item that changes later is pending again. It
// reports how many items it marked.
func (s *Service) SkipExtraction(ctx context.Context, db data.Database, actor Actor, before time.Time) (int, error) {
	if !actor.owner() {
		return 0, ErrForbidden
	}
	if before.IsZero() {
		return 0, invalidf("before is required")
	}
	exec, backend, err := data.Raw(db)
	if err != nil {
		return 0, err
	}
	query := `UPDATE kb_items SET extracted_at = ?, extract_lease_by = '', extract_lease_until = ? WHERE occurred_at < ? AND extracted_at < ?`
	args := []any{itemTimeArg(backend, s.clock()), itemTimeArg(backend, time.Time{}), itemTimeArg(backend, before), itemTimeArg(backend, extractedCut)}
	res, err := exec.ExecContext(ctx, rebind(backend, query), args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
