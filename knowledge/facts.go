package gowild_knowledge

import (
	"context"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// FactInput creates or edits a fact. Nil fields keep their value on edit.
type FactInput struct {
	Text       *string    `json:"text,omitempty"`
	Context    *string    `json:"context,omitempty"`
	Confidence *float64   `json:"confidence,omitempty"`
	ValidFrom  *time.Time `json:"valid_from,omitempty"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
	Supersedes *string    `json:"supersedes,omitempty"`
	Verified   *bool      `json:"verified,omitempty"`
	Retracted  *bool      `json:"retracted,omitempty"`
	About      *[]string  `json:"about,omitempty"`
	Sources    *[]string  `json:"sources,omitempty"`
	Tags       *[]string  `json:"tags,omitempty"`
}

// FactRef is a fact named in another record's view.
type FactRef struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	AuthorKind string  `json:"author_kind"`
	Author     string  `json:"author"`
	Verified   bool    `json:"verified"`
	SourceGone bool    `json:"source_gone"`
	Retracted  bool    `json:"retracted"`
	Superseded bool    `json:"superseded"`
}

func factRef(f *Fact) FactRef {
	return FactRef{f.ID, f.Text, f.Confidence, f.AuthorKind, f.Author, f.Verified, f.SourceGone, f.Retracted, f.SupersededBy != ""}
}

// FactView is a fact with what it is about, where it came from and its tags.
// AsOf is the date the fact speaks from: valid_from when the author set it,
// else the earliest cited item's date. A fact with neither has no AsOf.
type FactView struct {
	Fact
	AsOf    time.Time   `json:"as_of,omitzero"`
	About   []EntityRef `json:"about"`
	Sources []ItemRef   `json:"sources"`
	Tags    []string    `json:"tags"`
}

// factAsOf is the date a fact speaks from, given its cited items newest
// first.
func factAsOf(f *Fact, sources []ItemRef) time.Time {
	if !f.ValidFrom.IsZero() {
		return f.ValidFrom
	}
	if len(sources) > 0 {
		return sources[len(sources)-1].OccurredAt
	}
	return time.Time{}
}

// CreateFact records a fact. The owner's facts are verified; an agent's are
// live immediately with the confidence it gives (0.8 when omitted).
func (s *Service) CreateFact(ctx context.Context, db data.Database, actor Actor, in FactInput) (*FactView, error) {
	if !actor.valid() {
		return nil, ErrForbidden
	}
	if in.Text == nil {
		return nil, invalidf("text is required")
	}
	now := s.clock()
	f := &Fact{ID: newID("fct_"), AuthorKind: actor.Kind, Author: actor.Name, Confidence: 0.8, CreatedAt: now, UpdatedAt: now}
	if actor.owner() {
		f.Confidence, f.Verified = 1, true
	}
	if in.Context == nil {
		f.Context = DefaultContext
	}
	var out *FactView
	err := transact(ctx, db, func(tx data.Database) error {
		if err := s.applyFact(ctx, tx, actor, f, in, true); err != nil {
			return err
		}
		if err := tx.Table(Fact{}).Insert(ctx, f); err != nil {
			return err
		}
		if err := s.applyFactLinks(ctx, tx, f, in); err != nil {
			return err
		}
		if err := s.applySupersede(ctx, tx, actor, f, in); err != nil {
			return err
		}
		if err := s.reindexFact(ctx, tx, f); err != nil {
			return err
		}
		v, err := s.factView(ctx, tx, f)
		out = v
		return err
	})
	return out, err
}

// UpdateFact edits a fact within the write fence: an agent may change only
// unverified agent facts, and only the owner verifies.
func (s *Service) UpdateFact(ctx context.Context, db data.Database, actor Actor, id string, in FactInput) (*FactView, error) {
	var out *FactView
	err := transact(ctx, db, func(tx data.Database) error {
		f, err := dbx.Get[Fact](ctx, tx, id)
		if err != nil {
			return err
		}
		if f == nil {
			return notFound("fact", id)
		}
		if !actor.canModify(f.AuthorKind, f.Verified) {
			return ErrForbidden
		}
		if err := s.applyFact(ctx, tx, actor, f, in, false); err != nil {
			return err
		}
		f.UpdatedAt = s.clock()
		if err := tx.Table(Fact{}).Update(ctx, f); err != nil {
			return err
		}
		if err := s.applyFactLinks(ctx, tx, f, in); err != nil {
			return err
		}
		if err := s.applySupersede(ctx, tx, actor, f, in); err != nil {
			return err
		}
		if err := s.reindexFact(ctx, tx, f); err != nil {
			return err
		}
		v, err := s.factView(ctx, tx, f)
		out = v
		return err
	})
	return out, err
}

func (s *Service) applyFact(ctx context.Context, tx data.Database, actor Actor, f *Fact, in FactInput, fresh bool) error {
	if in.Text != nil {
		text := strings.TrimSpace(*in.Text)
		if err := checkLen("text", text, 1, 2000); err != nil {
			return err
		}
		f.Text = text
	}
	if in.Context != nil {
		c, err := normContext(*in.Context)
		if err != nil {
			return err
		}
		f.Context = c
	}
	if in.Confidence != nil {
		if *in.Confidence < 0 || *in.Confidence > 1 {
			return invalidf("confidence must be between 0 and 1")
		}
		f.Confidence = *in.Confidence
	}
	if in.ValidFrom != nil {
		f.ValidFrom = in.ValidFrom.UTC()
	}
	if in.ValidUntil != nil {
		f.ValidUntil = in.ValidUntil.UTC()
	}
	if !f.ValidFrom.IsZero() && !f.ValidUntil.IsZero() && f.ValidUntil.Before(f.ValidFrom) {
		return invalidf("valid_until is before valid_from")
	}
	if in.Verified != nil {
		if !actor.owner() {
			return ErrForbidden
		}
		f.Verified = *in.Verified
	}
	if in.Retracted != nil {
		f.Retracted = *in.Retracted
	}
	return nil
}

func (s *Service) applyFactLinks(ctx context.Context, tx data.Database, f *Fact, in FactInput) error {
	if in.About != nil {
		ids, err := s.resolveEntities(ctx, tx, *in.About)
		if err != nil {
			return err
		}
		if err := s.setLinks(ctx, tx, f.ID, RelAbout, ids); err != nil {
			return err
		}
	}
	if in.Sources != nil {
		ids := []string{}
		for _, id := range *in.Sources {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			it, err := dbx.Get[Item](ctx, tx, id)
			if err != nil {
				return err
			}
			if it == nil {
				return notFound("item", id)
			}
			ids = append(ids, id)
		}
		if err := s.setLinks(ctx, tx, f.ID, RelSource, ids); err != nil {
			return err
		}
		if len(ids) > 0 && f.SourceGone {
			f.SourceGone = false
			if err := tx.Table(Fact{}).Update(ctx, f); err != nil {
				return err
			}
		}
	}
	if in.Tags != nil {
		tags, err := normTags(*in.Tags)
		if err != nil {
			return err
		}
		if err := s.setLinks(ctx, tx, f.ID, RelTag, tagLinks(tags)); err != nil {
			return err
		}
	}
	return nil
}

// applySupersede marks the fact f replaces. An agent cannot supersede a fact
// it could not edit.
func (s *Service) applySupersede(ctx context.Context, tx data.Database, actor Actor, f *Fact, in FactInput) error {
	if in.Supersedes == nil {
		return nil
	}
	target := strings.TrimSpace(*in.Supersedes)
	if target == "" || target == f.Supersedes {
		return nil
	}
	if target == f.ID {
		return invalidf("a fact cannot supersede itself")
	}
	old, err := dbx.Get[Fact](ctx, tx, target)
	if err != nil {
		return err
	}
	if old == nil {
		return notFound("fact", target)
	}
	if !actor.canModify(old.AuthorKind, old.Verified) {
		return ErrForbidden
	}
	old.SupersededBy = f.ID
	old.UpdatedAt = s.clock()
	if err := tx.Table(Fact{}).Update(ctx, old); err != nil {
		return err
	}
	if err := s.reindexFact(ctx, tx, old); err != nil {
		return err
	}
	f.Supersedes = target
	return tx.Table(Fact{}).Update(ctx, f)
}

func (s *Service) reindexFact(ctx context.Context, db data.Database, f *Fact) error {
	tags, err := tagsOf(ctx, db, f.ID)
	if err != nil {
		return err
	}
	sources, err := linksFrom(ctx, db, f.ID, RelSource)
	if err != nil {
		return err
	}
	refs, err := itemRefs(ctx, db, sources)
	if err != nil {
		return err
	}
	return s.putSearch(ctx, db, factSearchRow(f, tags, factAsOf(f, refs)))
}

func (s *Service) factView(ctx context.Context, db data.Database, f *Fact) (*FactView, error) {
	v := &FactView{Fact: *f}
	about, err := linksFrom(ctx, db, f.ID, RelAbout)
	if err != nil {
		return nil, err
	}
	if v.About, err = entityRefs(ctx, db, about); err != nil {
		return nil, err
	}
	sources, err := linksFrom(ctx, db, f.ID, RelSource)
	if err != nil {
		return nil, err
	}
	if v.Sources, err = itemRefs(ctx, db, sources); err != nil {
		return nil, err
	}
	v.AsOf = factAsOf(f, v.Sources)
	if v.Tags, err = tagsOf(ctx, db, f.ID); err != nil {
		return nil, err
	}
	return v, nil
}

// GetFact reads one fact with its links.
func (s *Service) GetFact(ctx context.Context, db data.Database, id string) (*FactView, error) {
	f, err := dbx.Get[Fact](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, notFound("fact", id)
	}
	return s.factView(ctx, db, f)
}

// DeleteFact removes a fact outright. Agents retract instead; only the owner
// deletes.
func (s *Service) DeleteFact(ctx context.Context, db data.Database, actor Actor, id string) error {
	if !actor.owner() {
		return ErrForbidden
	}
	return transact(ctx, db, func(tx data.Database) error {
		f, err := dbx.Get[Fact](ctx, tx, id)
		if err != nil {
			return err
		}
		if f == nil {
			return notFound("fact", id)
		}
		// A fact this one replaced becomes current again.
		if f.Supersedes != "" {
			if old, err := dbx.Get[Fact](ctx, tx, f.Supersedes); err != nil {
				return err
			} else if old != nil && old.SupersededBy == f.ID {
				old.SupersededBy = ""
				if err := tx.Table(Fact{}).Update(ctx, old); err != nil {
					return err
				}
				if err := s.reindexFact(ctx, tx, old); err != nil {
					return err
				}
			}
		}
		if err := dropLinks(ctx, tx, id); err != nil {
			return err
		}
		if err := dropSearch(ctx, tx, id); err != nil {
			return err
		}
		return tx.Table(Fact{}).Delete(ctx, id)
	})
}

// FactFilter narrows ListFacts. Zero values do not filter.
type FactFilter struct {
	EntityID        string
	ItemID          string
	Tag             string
	AuthorKind      string
	Context         string
	Verified        *bool
	SourceGone      *bool
	IncludeInactive bool
	Limit           int
	Offset          int
}

// ListFacts lists facts newest first, for curation.
func (s *Service) ListFacts(ctx context.Context, db data.Database, filter FactFilter) ([]FactView, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	opts := data.QueryOpts{Where: map[string]any{}, OrderBy: "created_at", OrderDesc: true}
	restrict := func(ids []string) {
		in := make([]any, len(ids))
		for i, id := range ids {
			in[i] = id
		}
		if prev, ok := opts.WhereIn["id"]; ok {
			keep := map[any]bool{}
			for _, id := range in {
				keep[id] = true
			}
			in = in[:0]
			for _, id := range prev {
				if keep[id] {
					in = append(in, id)
				}
			}
		}
		if opts.WhereIn == nil {
			opts.WhereIn = map[string][]any{}
		}
		opts.WhereIn["id"] = in
	}
	if filter.EntityID != "" {
		e, err := resolveEntity(ctx, db, filter.EntityID)
		if err != nil {
			return nil, err
		}
		ids, err := linksTo(ctx, db, e.ID, RelAbout)
		if err != nil {
			return nil, err
		}
		restrict(ids)
	}
	if filter.ItemID != "" {
		ids, err := linksTo(ctx, db, filter.ItemID, RelSource)
		if err != nil {
			return nil, err
		}
		restrict(ids)
	}
	if filter.Tag != "" {
		ids, err := linksTo(ctx, db, tagID(strings.ToLower(filter.Tag)), RelTag)
		if err != nil {
			return nil, err
		}
		restrict(ids)
	}
	if ids, ok := opts.WhereIn["id"]; ok && len(ids) == 0 {
		return []FactView{}, nil
	}
	if filter.AuthorKind != "" {
		opts.Where["author_kind"] = filter.AuthorKind
	}
	if filter.Context != "" {
		opts.Where["context"] = filter.Context
	}
	if filter.Verified != nil {
		opts.Where["verified"] = *filter.Verified
	}
	if filter.SourceGone != nil {
		opts.Where["source_gone"] = *filter.SourceGone
	}
	if !filter.IncludeInactive {
		opts.Where["retracted"] = false
		opts.Where["superseded_by"] = ""
	}
	opts.Limit, opts.Offset = limit, filter.Offset
	rows, err := dbx.All[Fact](ctx, db, opts)
	if err != nil {
		return nil, err
	}
	out := make([]FactView, 0, len(rows))
	for _, f := range rows {
		v, err := s.factView(ctx, db, f)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func factRefs(ctx context.Context, db data.Database, ids []string) ([]FactRef, error) {
	out := []FactRef{}
	if len(ids) == 0 {
		return out, nil
	}
	in := make([]any, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	rows, err := dbx.All[Fact](ctx, db, data.QueryOpts{WhereIn: map[string][]any{"id": in}, OrderBy: "created_at", OrderDesc: true})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, factRef(r))
	}
	return out, nil
}
