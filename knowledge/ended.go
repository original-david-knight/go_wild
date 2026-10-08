package gowild_knowledge

import (
	"context"
	"time"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// EndElapsed ends the facts whose ValidUntil has passed by now, so they
// leave search and lists the way a superseded fact does. A fact written or
// edited after its validity passed is ended by that write; this pass catches
// the ones whose validity ran out since. It reports how many facts ended.
func (s *Service) EndElapsed(ctx context.Context, db data.Database, now time.Time) (int, error) {
	rows, err := dbx.All[Fact](ctx, db, data.QueryOpts{Where: map[string]any{"ended": false}})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range rows {
		if !f.endedAt(now) {
			continue
		}
		err := transact(ctx, db, func(tx data.Database) error {
			f.Ended = true
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
