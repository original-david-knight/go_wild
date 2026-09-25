package gowild_knowledge

import (
	"context"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// searchRow is one record's entry in kb_search.
type searchRow struct {
	ID         string
	Kind       string
	SourceID   string
	Context    string
	OccurredAt time.Time
	Title      string
	Body       string
	Weight     float64
	Active     bool
}

// Ranking weights: distilled knowledge outranks raw material at equal
// relevance, and the owner's word outranks an agent's.
const (
	weightItem        = 1.0
	weightNote        = 1.2
	weightEntity      = 1.3
	weightFact        = 1.5
	weightVerified    = 1.8
	penaltySourceGone = 0.5
)

// sqliteTime is a fixed-width UTC format, so text comparison orders times.
const sqliteTime = "2006-01-02T15:04:05.000000Z"

func timeArg(backend data.Backend, t time.Time) any {
	if backend == data.BackendPostgres {
		return t.UTC()
	}
	return t.UTC().Format(sqliteTime)
}

func (s *Service) putSearch(ctx context.Context, db data.Database, row searchRow) error {
	exec, backend, err := data.Raw(db)
	if err != nil {
		return err
	}
	row.Title = truncateRunes(row.Title, 4000)
	row.Body = truncateRunes(row.Body, maxIndexedText)
	textHash := hashText(row.Title, row.Body)
	active := any(row.Active)
	if backend == data.BackendSqlite {
		active = boolInt(row.Active)
	}
	query := rebind(backend, `INSERT INTO kb_search
		(id, kind, source_id, context, occurred_at, title, body, weight, active, text_hash, embed_state, embed_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', '', ?)
		ON CONFLICT (id) DO UPDATE SET
			kind = excluded.kind, source_id = excluded.source_id, context = excluded.context,
			occurred_at = excluded.occurred_at, title = excluded.title, body = excluded.body,
			weight = excluded.weight, active = excluded.active, updated_at = excluded.updated_at,
			embed_state = CASE WHEN kb_search.text_hash = excluded.text_hash THEN kb_search.embed_state ELSE 'pending' END,
			embed_error = CASE WHEN kb_search.text_hash = excluded.text_hash THEN kb_search.embed_error ELSE '' END,
			text_hash = excluded.text_hash`)
	_, err = exec.ExecContext(ctx, query, row.ID, row.Kind, row.SourceID, row.Context,
		timeArg(backend, row.OccurredAt), row.Title, row.Body, row.Weight, active, textHash,
		timeArg(backend, s.clock()))
	return err
}

func dropSearch(ctx context.Context, db data.Database, id string) error {
	exec, backend, err := data.Raw(db)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, rebind(backend, `DELETE FROM kb_search WHERE id = ?`), id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func itemSearchRow(it *Item) searchRow {
	var body strings.Builder
	body.WriteString(it.Body)
	var names []string
	for _, p := range it.Participants {
		if p.Name != "" {
			names = append(names, p.Name)
		}
		if _, v, ok := strings.Cut(p.Alias, ":"); ok {
			names = append(names, v)
		}
	}
	if len(names) > 0 {
		body.WriteString("\n\n")
		body.WriteString(strings.Join(names, ", "))
	}
	for _, a := range it.Attachments {
		body.WriteString("\n\n")
		body.WriteString(a.Name)
		if a.Text != "" {
			body.WriteString("\n")
			body.WriteString(a.Text)
		}
	}
	return searchRow{
		ID: it.ID, Kind: KindItem, SourceID: it.SourceID, Context: it.Context,
		OccurredAt: it.OccurredAt, Title: it.Title, Body: body.String(),
		Weight: weightItem, Active: true,
	}
}

func factWeight(f *Fact) float64 {
	w := weightFact
	if f.Verified {
		w = weightVerified
	}
	conf := f.Confidence
	if conf <= 0 || conf > 1 {
		conf = 1
	}
	w *= 0.5 + 0.5*conf
	if f.SourceGone {
		w *= penaltySourceGone
	}
	return w
}

// factSearchRow dates a fact by when it speaks from (asOf), so a fact
// mined from old mail sorts with that mail and not with the day it was
// mined; a fact with no date of its own sorts by when it was recorded.
func factSearchRow(f *Fact, tags []string, asOf time.Time) searchRow {
	occurred := f.CreatedAt
	if !asOf.IsZero() {
		occurred = asOf
	}
	return searchRow{
		ID: f.ID, Kind: KindFact, Context: f.Context, OccurredAt: occurred,
		Title: f.Text, Body: strings.Join(tags, " "),
		Weight: factWeight(f), Active: !f.Retracted && f.SupersededBy == "",
	}
}

func noteSearchRow(n *Note, tags []string) searchRow {
	body := n.Body
	if len(tags) > 0 {
		body += "\n\n" + strings.Join(tags, " ")
	}
	return searchRow{
		ID: n.ID, Kind: KindNote, Context: n.Context, OccurredAt: n.CreatedAt,
		Title: n.Title, Body: body, Weight: weightNote, Active: true,
	}
}

func entitySearchRow(e *Entity, tags []string) searchRow {
	parts := []string{e.Kind}
	for _, a := range e.Aliases {
		if _, v, ok := strings.Cut(a, ":"); ok {
			parts = append(parts, v)
		}
	}
	parts = append(parts, tags...)
	body := strings.Join(parts, " ")
	if e.Summary != "" {
		body = e.Summary + "\n\n" + body
	}
	return searchRow{
		ID: e.ID, Kind: KindEntity, Context: e.Context, OccurredAt: e.CreatedAt,
		Title: e.Name, Body: body, Weight: weightEntity, Active: e.MergedInto == "",
	}
}
