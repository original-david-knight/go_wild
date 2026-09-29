package gowild_knowledge

import (
	"context"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// ItemRange selects one source's items whose external ID starts with a
// prefix, such as every day of one Slack conversation ("C0123/").
type ItemRange struct {
	SourceID string
	Prefix   string
	// ChangedSince keeps items written (created or updated) at or after it;
	// zero keeps all.
	ChangedSince time.Time
	Limit        int
}

// MaxRangeItems bounds one ItemsInRange read.
const MaxRangeItems = 200

// ItemsInRange lists the items in r, highest external ID first: for IDs that
// end in a day key, newest day first. PostgreSQL answers it from the
// (source_id, external_id text_pattern_ops) index.
func (s *Service) ItemsInRange(ctx context.Context, db data.Database, r ItemRange) ([]*Item, error) {
	if r.SourceID == "" || r.Prefix == "" {
		return nil, invalidf("an item range needs a source and a prefix")
	}
	if r.Limit <= 0 || r.Limit > MaxRangeItems {
		r.Limit = MaxRangeItems
	}
	exec, backend, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	query := `SELECT id FROM kb_items WHERE source_id = ? AND external_id LIKE ? ESCAPE '\'`
	args := []any{r.SourceID, likePrefix(r.Prefix)}
	if !r.ChangedSince.IsZero() {
		query += ` AND updated_at >= ?`
		args = append(args, itemTimeArg(backend, r.ChangedSince))
	}
	query += ` ORDER BY external_id DESC LIMIT ?`
	args = append(args, r.Limit)
	rows, err := exec.QueryContext(ctx, rebind(backend, query), args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Item, 0, len(ids))
	for _, id := range ids {
		it, err := dbx.Get[Item](ctx, db, id)
		if err != nil {
			return nil, err
		}
		if it != nil {
			out = append(out, it)
		}
	}
	return out, nil
}

func likePrefix(p string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(p) + "%"
}
