package gowild_knowledge

import (
	"context"
	"strings"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// MaxNoteBody bounds a note's body.
const MaxNoteBody = 200_000

// NoteInput creates or edits a note. Nil fields keep their value on edit.
type NoteInput struct {
	Title   *string   `json:"title,omitempty"`
	Body    *string   `json:"body,omitempty"`
	Context *string   `json:"context,omitempty"`
	About   *[]string `json:"about,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
}

// NoteView is a note with its entities and tags.
type NoteView struct {
	Note
	About []EntityRef `json:"about"`
	Tags  []string    `json:"tags"`
}

// NoteRef is a note named in another record's view.
type NoteRef struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	AuthorKind string `json:"author_kind"`
	Author     string `json:"author"`
}

// CreateNote saves a note.
func (s *Service) CreateNote(ctx context.Context, db data.Database, actor Actor, in NoteInput) (*NoteView, error) {
	if !actor.valid() {
		return nil, ErrForbidden
	}
	if in.Title == nil || in.Body == nil {
		return nil, invalidf("title and body are required")
	}
	now := s.clock()
	n := &Note{ID: newID("not_"), Context: DefaultContext, AuthorKind: actor.Kind, Author: actor.Name, CreatedAt: now, UpdatedAt: now}
	var out *NoteView
	err := transact(ctx, db, func(tx data.Database) error {
		if err := applyNote(n, in); err != nil {
			return err
		}
		if err := tx.Table(Note{}).Insert(ctx, n); err != nil {
			return err
		}
		v, err := s.saveNoteLinks(ctx, tx, n, in)
		out = v
		return err
	})
	return out, err
}

// UpdateNote edits a note within the write fence.
func (s *Service) UpdateNote(ctx context.Context, db data.Database, actor Actor, id string, in NoteInput) (*NoteView, error) {
	var out *NoteView
	err := transact(ctx, db, func(tx data.Database) error {
		n, err := dbx.Get[Note](ctx, tx, id)
		if err != nil {
			return err
		}
		if n == nil {
			return notFound("note", id)
		}
		if !actor.canModify(n.AuthorKind, false) {
			return ErrForbidden
		}
		if err := applyNote(n, in); err != nil {
			return err
		}
		n.UpdatedAt = s.clock()
		if err := tx.Table(Note{}).Update(ctx, n); err != nil {
			return err
		}
		v, err := s.saveNoteLinks(ctx, tx, n, in)
		out = v
		return err
	})
	return out, err
}

func applyNote(n *Note, in NoteInput) error {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if err := checkLen("title", t, 1, 300); err != nil {
			return err
		}
		n.Title = t
	}
	if in.Body != nil {
		if err := checkLen("body", strings.TrimSpace(*in.Body), 1, MaxNoteBody); err != nil {
			return err
		}
		n.Body = *in.Body
	}
	if in.Context != nil {
		c, err := normContext(*in.Context)
		if err != nil {
			return err
		}
		n.Context = c
	}
	return nil
}

func (s *Service) saveNoteLinks(ctx context.Context, tx data.Database, n *Note, in NoteInput) (*NoteView, error) {
	if in.About != nil {
		ids, err := s.resolveEntities(ctx, tx, *in.About)
		if err != nil {
			return nil, err
		}
		if err := s.setLinks(ctx, tx, n.ID, RelAbout, ids); err != nil {
			return nil, err
		}
	}
	if in.Tags != nil {
		tags, err := normTags(*in.Tags)
		if err != nil {
			return nil, err
		}
		if err := s.setLinks(ctx, tx, n.ID, RelTag, tagLinks(tags)); err != nil {
			return nil, err
		}
	}
	v, err := s.noteView(ctx, tx, n)
	if err != nil {
		return nil, err
	}
	return v, s.putSearch(ctx, tx, noteSearchRow(n, v.Tags))
}

func (s *Service) noteView(ctx context.Context, db data.Database, n *Note) (*NoteView, error) {
	v := &NoteView{Note: *n}
	about, err := linksFrom(ctx, db, n.ID, RelAbout)
	if err != nil {
		return nil, err
	}
	if v.About, err = entityRefs(ctx, db, about); err != nil {
		return nil, err
	}
	if v.Tags, err = tagsOf(ctx, db, n.ID); err != nil {
		return nil, err
	}
	return v, nil
}

// GetNote reads one note.
func (s *Service) GetNote(ctx context.Context, db data.Database, id string) (*NoteView, error) {
	n, err := dbx.Get[Note](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, notFound("note", id)
	}
	return s.noteView(ctx, db, n)
}

// DeleteNote removes a note: the owner any, an agent only agents' notes.
func (s *Service) DeleteNote(ctx context.Context, db data.Database, actor Actor, id string) error {
	return transact(ctx, db, func(tx data.Database) error {
		n, err := dbx.Get[Note](ctx, tx, id)
		if err != nil {
			return err
		}
		if n == nil {
			return notFound("note", id)
		}
		if !actor.canModify(n.AuthorKind, false) {
			return ErrForbidden
		}
		if err := dropLinks(ctx, tx, id); err != nil {
			return err
		}
		if err := dropSearch(ctx, tx, id); err != nil {
			return err
		}
		return tx.Table(Note{}).Delete(ctx, id)
	})
}

// NoteFilter narrows ListNotes.
type NoteFilter struct {
	EntityID   string
	Tag        string
	AuthorKind string
	Context    string
	Limit      int
	Offset     int
}

// ListNotes lists notes, most recently updated first.
func (s *Service) ListNotes(ctx context.Context, db data.Database, filter NoteFilter) ([]NoteView, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	opts := data.QueryOpts{Where: map[string]any{}, OrderBy: "updated_at", OrderDesc: true, Limit: limit, Offset: filter.Offset}
	var ids []string
	restricted := false
	if filter.EntityID != "" {
		e, err := resolveEntity(ctx, db, filter.EntityID)
		if err != nil {
			return nil, err
		}
		if ids, err = linksTo(ctx, db, e.ID, RelAbout); err != nil {
			return nil, err
		}
		restricted = true
	}
	if filter.Tag != "" {
		tagged, err := linksTo(ctx, db, tagID(strings.ToLower(filter.Tag)), RelTag)
		if err != nil {
			return nil, err
		}
		if restricted {
			ids = intersect(ids, tagged)
		} else {
			ids = tagged
		}
		restricted = true
	}
	if restricted {
		in := []any{}
		for _, id := range ids {
			if KindOf(id) == KindNote {
				in = append(in, id)
			}
		}
		if len(in) == 0 {
			return []NoteView{}, nil
		}
		opts.WhereIn = map[string][]any{"id": in}
	}
	if filter.AuthorKind != "" {
		opts.Where["author_kind"] = filter.AuthorKind
	}
	if filter.Context != "" {
		opts.Where["context"] = filter.Context
	}
	rows, err := dbx.All[Note](ctx, db, opts)
	if err != nil {
		return nil, err
	}
	out := make([]NoteView, 0, len(rows))
	for _, n := range rows {
		v, err := s.noteView(ctx, db, n)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func intersect(a, b []string) []string {
	keep := map[string]bool{}
	for _, x := range b {
		keep[x] = true
	}
	out := []string{}
	for _, x := range a {
		if keep[x] {
			out = append(out, x)
		}
	}
	return out
}

func noteRefs(ctx context.Context, db data.Database, ids []string) ([]NoteRef, error) {
	out := []NoteRef{}
	in := []any{}
	for _, id := range ids {
		if KindOf(id) == KindNote {
			in = append(in, id)
		}
	}
	if len(in) == 0 {
		return out, nil
	}
	rows, err := dbx.All[Note](ctx, db, data.QueryOpts{WhereIn: map[string][]any{"id": in}, OrderBy: "updated_at", OrderDesc: true})
	if err != nil {
		return nil, err
	}
	for _, n := range rows {
		out = append(out, NoteRef{n.ID, n.Title, n.AuthorKind, n.Author})
	}
	return out, nil
}
