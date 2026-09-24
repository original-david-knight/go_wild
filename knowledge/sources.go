package gowild_knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// Limits on one ingested item.
const (
	MaxItemBody        = 1 << 20
	MaxAttachmentText  = 1 << 20
	MaxAttachments     = 32
	MaxParticipants    = 200
	MaxIngestBatch     = 500
	defaultBackfillDay = 365
)

// SourceInput creates or updates a source. Nil fields keep their value.
type SourceInput struct {
	Kind         *string         `json:"kind,omitempty"`
	Name         *string         `json:"name,omitempty"`
	Context      *string         `json:"context,omitempty"`
	Work         *bool           `json:"work,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
	BackfillDays *int            `json:"backfill_days,omitempty"`
	Settings     *map[string]any `json:"settings,omitempty"`
	Cursor       *string         `json:"cursor,omitempty"`
}

// SourceView is a source with its item count.
type SourceView struct {
	Source
	ItemCount int `json:"item_count"`
}

// PutSource creates or updates a source. Only the owner configures sources.
func (s *Service) PutSource(ctx context.Context, db data.Database, actor Actor, id string, in SourceInput) (*SourceView, error) {
	if !actor.owner() {
		return nil, ErrForbidden
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if !sourcePattern.MatchString(id) {
		return nil, invalidf("source id %q must look like kind:account, e.g. gmail:personal", id)
	}
	src, err := dbx.Get[Source](ctx, db, id)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	fresh := src == nil
	if fresh {
		kind, _, _ := strings.Cut(id, ":")
		src = &Source{ID: id, Kind: kind, Name: id, Enabled: true, BackfillDays: defaultBackfillDay, Settings: map[string]any{}, CreatedAt: now}
	}
	if in.Kind != nil {
		if !kindPattern.MatchString(*in.Kind) {
			return nil, invalidf("source kind %q is not a valid kind", *in.Kind)
		}
		src.Kind = *in.Kind
	}
	if in.Name != nil {
		if err := checkLen("name", strings.TrimSpace(*in.Name), 1, 200); err != nil {
			return nil, err
		}
		src.Name = strings.TrimSpace(*in.Name)
	}
	if in.Work != nil {
		src.Work = *in.Work
	}
	if in.Context != nil || fresh {
		c := ""
		if in.Context != nil {
			c = *in.Context
		}
		if c == "" && src.Work {
			c = "work"
		}
		if src.Context, err = normContext(c); err != nil {
			return nil, err
		}
	}
	if in.Enabled != nil {
		src.Enabled = *in.Enabled
	}
	if in.BackfillDays != nil {
		if *in.BackfillDays < 0 || *in.BackfillDays > 36500 {
			return nil, invalidf("backfill_days must be between 0 and 36500")
		}
		src.BackfillDays = *in.BackfillDays
	}
	if in.Settings != nil {
		raw, err := json.Marshal(*in.Settings)
		if err != nil || len(raw) > 64<<10 {
			return nil, invalidf("settings must be a JSON object under 64 KiB")
		}
		src.Settings = *in.Settings
	}
	if in.Cursor != nil {
		src.Cursor = *in.Cursor
	}
	src.UpdatedAt = now
	if err := dbx.Upsert(ctx, db, id, src); err != nil {
		return nil, err
	}
	return s.sourceView(ctx, db, src)
}

func (s *Service) sourceView(ctx context.Context, db data.Database, src *Source) (*SourceView, error) {
	counts, err := itemCounts(ctx, db)
	if err != nil {
		return nil, err
	}
	return &SourceView{Source: *src, ItemCount: counts[src.ID]}, nil
}

func itemCounts(ctx context.Context, db data.Database) (map[string]int, error) {
	exec, _, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT source_id, count(*) FROM kb_items GROUP BY source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// GetSource reads one source.
func (s *Service) GetSource(ctx context.Context, db data.Database, id string) (*SourceView, error) {
	src, err := dbx.Get[Source](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, notFound("source", id)
	}
	return s.sourceView(ctx, db, src)
}

// ListSources lists every source with its item count.
func (s *Service) ListSources(ctx context.Context, db data.Database) ([]SourceView, error) {
	rows, err := dbx.All[Source](ctx, db, data.QueryOpts{OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	counts, err := itemCounts(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make([]SourceView, 0, len(rows))
	for _, r := range rows {
		out = append(out, SourceView{Source: *r, ItemCount: counts[r.ID]})
	}
	return out, nil
}

// DeleteSource removes a source and every item imported under it.
func (s *Service) DeleteSource(ctx context.Context, db data.Database, actor Actor, id string) (int, error) {
	if !actor.owner() {
		return 0, ErrForbidden
	}
	src, err := dbx.Get[Source](ctx, db, id)
	if err != nil {
		return 0, err
	}
	if src == nil {
		return 0, notFound("source", id)
	}
	removed := 0
	for {
		items, err := dbx.All[Item](ctx, db, data.QueryOpts{Where: map[string]any{"source_id": id}, Limit: 200})
		if err != nil {
			return removed, err
		}
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			if err := transact(ctx, db, func(tx data.Database) error { return s.removeItem(ctx, tx, it.ID) }); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, db.Table(Source{}).Delete(ctx, id)
}

// IngestItem is one item as an importer pushes it.
type IngestItem struct {
	ExternalID   string         `json:"external_id"`
	Kind         string         `json:"kind"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	URL          string         `json:"url"`
	OccurredAt   time.Time      `json:"occurred_at"`
	Participants []Participant  `json:"participants"`
	Attachments  []Attachment   `json:"attachments"`
	Metadata     map[string]any `json:"metadata"`
}

// IngestBatch is one push: new or changed items, external IDs deleted at the
// source, and the importer's new sync position. Error records a failed sync
// the importer wants surfaced; an empty string clears it.
type IngestBatch struct {
	Items   []IngestItem `json:"items"`
	Deletes []string     `json:"deletes"`
	Cursor  *string      `json:"cursor,omitempty"`
	Error   *string      `json:"error,omitempty"`
}

// IngestResult counts what a push changed.
type IngestResult struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Deleted   int `json:"deleted"`
}

// Ingest applies one importer push. Items are keyed by external ID, so
// pushing the same item twice is a no-op and a changed item is updated in
// place. The cursor advances only after every item has been applied.
func (s *Service) Ingest(ctx context.Context, db data.Database, actor Actor, sourceID string, batch IngestBatch) (*IngestResult, error) {
	if !actor.valid() {
		return nil, ErrForbidden
	}
	src, err := dbx.Get[Source](ctx, db, sourceID)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, notFound("source", sourceID)
	}
	if !src.Enabled {
		return nil, invalidf("source %s is disabled", sourceID)
	}
	if len(batch.Items)+len(batch.Deletes) > MaxIngestBatch {
		return nil, invalidf("a push carries at most %d items and deletes", MaxIngestBatch)
	}
	items := make([]*Item, 0, len(batch.Items))
	for i, in := range batch.Items {
		it, err := buildItem(src, in)
		if err != nil {
			return nil, invalidf("item %d: %v", i, strings.TrimPrefix(err.Error(), "invalid: "))
		}
		items = append(items, it)
	}
	res := &IngestResult{}
	for _, it := range items {
		err := transact(ctx, db, func(tx data.Database) error {
			existing, err := dbx.Get[Item](ctx, tx, it.ID)
			if err != nil {
				return err
			}
			now := s.clock()
			if existing != nil {
				if existing.ContentHash == it.ContentHash {
					res.Unchanged++
					return nil
				}
				it.CreatedAt, it.ExtractedAt = existing.CreatedAt, time.Time{}
				it.UpdatedAt = now
				if err := tx.Table(Item{}).Update(ctx, it); err != nil {
					return err
				}
				res.Updated++
			} else {
				it.CreatedAt, it.UpdatedAt = now, now
				if err := tx.Table(Item{}).Insert(ctx, it); err != nil {
					return err
				}
				res.Created++
			}
			if err := s.putParticipants(ctx, tx, it); err != nil {
				return err
			}
			return s.putSearch(ctx, tx, itemSearchRow(it))
		})
		if err != nil {
			return nil, err
		}
	}
	for _, ext := range batch.Deletes {
		id := ItemID(src.ID, ext)
		var gone bool
		err := transact(ctx, db, func(tx data.Database) error {
			existing, err := dbx.Get[Item](ctx, tx, id)
			if err != nil || existing == nil {
				return err
			}
			gone = true
			return s.removeItem(ctx, tx, id)
		})
		if err != nil {
			return nil, err
		}
		if gone {
			res.Deleted++
		}
	}
	// A push reporting an error keeps the last good sync time, so "synced"
	// always means the data was fresh then.
	now := s.clock()
	if batch.Cursor != nil {
		src.Cursor = *batch.Cursor
	}
	if batch.Error != nil && *batch.Error != "" {
		src.LastError = truncateRunes(*batch.Error, 2000)
	} else {
		src.LastError = ""
		src.LastSyncAt = now
	}
	src.UpdatedAt = now
	if err := db.Table(Source{}).Update(ctx, src); err != nil {
		return nil, err
	}
	return res, nil
}

func buildItem(src *Source, in IngestItem) (*Item, error) {
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	if err := checkLen("external_id", in.ExternalID, 1, 512); err != nil {
		return nil, err
	}
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if !kindPattern.MatchString(in.Kind) {
		return nil, invalidf("kind %q must be a lowercase word such as email or message", in.Kind)
	}
	if err := checkLen("title", in.Title, 0, 1000); err != nil {
		return nil, err
	}
	if len(in.Body) > MaxItemBody {
		return nil, invalidf("body exceeds %d bytes", MaxItemBody)
	}
	if len(in.URL) > 2000 {
		return nil, invalidf("url is longer than 2000 characters")
	}
	if in.OccurredAt.IsZero() {
		return nil, invalidf("occurred_at is required")
	}
	if len(in.Participants) > MaxParticipants {
		return nil, invalidf("at most %d participants", MaxParticipants)
	}
	parts := make([]Participant, 0, len(in.Participants))
	for _, p := range in.Participants {
		alias := ""
		if strings.TrimSpace(p.Alias) != "" {
			a, err := NormalizeAlias(p.Alias)
			if err != nil {
				return nil, err
			}
			alias = a
		}
		if alias == "" && strings.TrimSpace(p.Name) == "" {
			continue
		}
		parts = append(parts, Participant{Role: strings.TrimSpace(p.Role), Name: truncateRunes(strings.TrimSpace(p.Name), 200), Alias: alias})
	}
	if len(in.Attachments) > MaxAttachments {
		return nil, invalidf("at most %d attachments", MaxAttachments)
	}
	for _, a := range in.Attachments {
		if len(a.Text) > MaxAttachmentText {
			return nil, invalidf("attachment %q text exceeds %d bytes", a.Name, MaxAttachmentText)
		}
	}
	if in.Metadata == nil {
		in.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(in.Metadata)
	if err != nil || len(meta) > 64<<10 {
		return nil, invalidf("metadata must be a JSON object under 64 KiB")
	}
	atts, _ := json.Marshal(in.Attachments)
	pjson, _ := json.Marshal(parts)
	if in.Attachments == nil {
		in.Attachments = []Attachment{}
	}
	it := &Item{
		ID: ItemID(src.ID, in.ExternalID), SourceID: src.ID, ExternalID: in.ExternalID,
		Kind: in.Kind, Title: strings.TrimSpace(in.Title), Body: in.Body, URL: in.URL,
		OccurredAt: in.OccurredAt.UTC(), Context: src.Context, Participants: parts,
		Attachments: in.Attachments, Metadata: in.Metadata,
	}
	it.ContentHash = hashText(it.Kind, it.Title, it.Body, it.URL, it.OccurredAt.Format(time.RFC3339Nano), string(pjson), string(atts), string(meta), it.Context)
	return it, nil
}

func (s *Service) putParticipants(ctx context.Context, db data.Database, it *Item) error {
	current, err := dbx.All[ItemParticipant](ctx, db, data.QueryOpts{Where: map[string]any{"item_id": it.ID}})
	if err != nil {
		return err
	}
	for _, p := range current {
		if err := db.Table(ItemParticipant{}).Delete(ctx, p.ID); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, p := range it.Participants {
		if p.Alias == "" || seen[p.Alias] {
			continue
		}
		seen[p.Alias] = true
		row := &ItemParticipant{ID: it.ID + "|" + p.Alias, ItemID: it.ID, Alias: p.Alias, Role: p.Role}
		if err := db.Table(ItemParticipant{}).Insert(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// removeItem deletes an item and everything derived from it. Facts that
// cited it keep their text; a fact left with no remaining source is marked
// SourceGone and down-ranked rather than deleted.
func (s *Service) removeItem(ctx context.Context, tx data.Database, id string) error {
	citing, err := linksTo(ctx, tx, id, RelSource)
	if err != nil {
		return err
	}
	if err := s.putParticipants(ctx, tx, &Item{ID: id}); err != nil {
		return err
	}
	if err := dropLinks(ctx, tx, id); err != nil {
		return err
	}
	if err := dropSearch(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Table(Item{}).Delete(ctx, id); err != nil {
		return err
	}
	for _, factID := range citing {
		remaining, err := linksFrom(ctx, tx, factID, RelSource)
		if err != nil {
			return err
		}
		if len(remaining) > 0 {
			continue
		}
		f, err := dbx.Get[Fact](ctx, tx, factID)
		if err != nil {
			return err
		}
		if f == nil || f.SourceGone {
			continue
		}
		f.SourceGone = true
		f.UpdatedAt = s.clock()
		if err := tx.Table(Fact{}).Update(ctx, f); err != nil {
			return err
		}
		if err := s.reindexFact(ctx, tx, f); err != nil {
			return err
		}
	}
	return nil
}

// ItemRef is an item named in another record's view.
type ItemRef struct {
	ID         string    `json:"id"`
	SourceID   string    `json:"source_id"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	OccurredAt time.Time `json:"occurred_at"`
}

func itemRef(it *Item) ItemRef {
	return ItemRef{it.ID, it.SourceID, it.Kind, it.Title, it.URL, it.OccurredAt}
}

// ParticipantView is a participant with the entity that claims its alias.
type ParticipantView struct {
	Participant
	Entity *EntityRef `json:"entity,omitempty"`
}

// ItemView is an item with its resolved participants and the facts citing it.
type ItemView struct {
	Item
	Participants []ParticipantView `json:"participants"`
	Facts        []FactRef         `json:"facts"`
}

// GetItem reads an item in full.
func (s *Service) GetItem(ctx context.Context, db data.Database, id string) (*ItemView, error) {
	it, err := dbx.Get[Item](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return nil, notFound("item", id)
	}
	view := &ItemView{Item: *it, Participants: []ParticipantView{}, Facts: []FactRef{}}
	for _, p := range it.Participants {
		pv := ParticipantView{Participant: p}
		if p.Alias != "" {
			if ref, err := entityForAlias(ctx, db, p.Alias); err != nil {
				return nil, err
			} else {
				pv.Entity = ref
			}
		}
		view.Participants = append(view.Participants, pv)
	}
	factIDs, err := linksTo(ctx, db, id, RelSource)
	if err != nil {
		return nil, err
	}
	if view.Facts, err = factRefs(ctx, db, factIDs); err != nil {
		return nil, err
	}
	return view, nil
}

// DeleteItem removes an item as if it had been deleted at its source.
func (s *Service) DeleteItem(ctx context.Context, db data.Database, actor Actor, id string) error {
	if !actor.owner() {
		return ErrForbidden
	}
	return transact(ctx, db, func(tx data.Database) error {
		it, err := dbx.Get[Item](ctx, tx, id)
		if err != nil {
			return err
		}
		if it == nil {
			return notFound("item", id)
		}
		return s.removeItem(ctx, tx, id)
	})
}

func itemRefs(ctx context.Context, db data.Database, ids []string) ([]ItemRef, error) {
	out := []ItemRef{}
	if len(ids) == 0 {
		return out, nil
	}
	in := make([]any, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	rows, err := dbx.All[Item](ctx, db, data.QueryOpts{WhereIn: map[string][]any{"id": in}, OrderBy: "occurred_at", OrderDesc: true})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, itemRef(r))
	}
	return out, nil
}
