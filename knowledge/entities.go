package gowild_knowledge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// EntityInput creates or edits an entity. Nil fields keep their value.
type EntityInput struct {
	Kind    *string   `json:"kind,omitempty"`
	Name    *string   `json:"name,omitempty"`
	Summary *string   `json:"summary,omitempty"`
	Aliases *[]string `json:"aliases,omitempty"`
	Context *string   `json:"context,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
}

// EntityRef is an entity named in another record's view.
type EntityRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// EntityView is an entity with what the knowledge base holds about it.
type EntityView struct {
	Entity
	Tags      []string  `json:"tags"`
	Facts     []FactRef `json:"facts"`
	Notes     []NoteRef `json:"notes"`
	Items     []ItemRef `json:"items"`
	ItemCount int       `json:"item_count"`
}

// AliasConflictError names the entity that already claims an alias.
type AliasConflictError struct {
	Alias    string
	EntityID string
}

func (e *AliasConflictError) Error() string {
	return fmt.Sprintf("alias %s already belongs to entity %s", e.Alias, e.EntityID)
}

func (e *AliasConflictError) Unwrap() error { return ErrConflict }

// CreateEntity records an entity and claims its aliases.
func (s *Service) CreateEntity(ctx context.Context, db data.Database, actor Actor, in EntityInput) (*EntityView, error) {
	if !actor.valid() {
		return nil, ErrForbidden
	}
	if in.Name == nil {
		return nil, invalidf("name is required")
	}
	now := s.clock()
	e := &Entity{ID: newID("ent_"), Kind: "person", Context: DefaultContext, Aliases: []string{}, AuthorKind: actor.Kind, Author: actor.Name, CreatedAt: now, UpdatedAt: now}
	var out *EntityView
	err := transact(ctx, db, func(tx data.Database) error {
		if err := applyEntity(e, in); err != nil {
			return err
		}
		if err := tx.Table(Entity{}).Insert(ctx, e); err != nil {
			return err
		}
		v, err := s.saveEntity(ctx, tx, e, nil, in)
		out = v
		return err
	})
	return out, err
}

// UpdateEntity edits an entity within the write fence.
func (s *Service) UpdateEntity(ctx context.Context, db data.Database, actor Actor, id string, in EntityInput) (*EntityView, error) {
	var out *EntityView
	err := transact(ctx, db, func(tx data.Database) error {
		e, err := resolveEntity(ctx, tx, id)
		if err != nil {
			return err
		}
		if !actor.canModify(e.AuthorKind, false) {
			return ErrForbidden
		}
		before := slices.Clone(e.Aliases)
		if err := applyEntity(e, in); err != nil {
			return err
		}
		e.UpdatedAt = s.clock()
		if err := tx.Table(Entity{}).Update(ctx, e); err != nil {
			return err
		}
		v, err := s.saveEntity(ctx, tx, e, before, in)
		out = v
		return err
	})
	return out, err
}

func applyEntity(e *Entity, in EntityInput) error {
	if in.Kind != nil {
		if !slices.Contains(EntityKinds, *in.Kind) {
			return invalidf("kind must be one of %s", strings.Join(EntityKinds, ", "))
		}
		e.Kind = *in.Kind
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if err := checkLen("name", n, 1, 200); err != nil {
			return err
		}
		e.Name = n
	}
	if in.Summary != nil {
		if err := checkLen("summary", *in.Summary, 0, 4000); err != nil {
			return err
		}
		e.Summary = strings.TrimSpace(*in.Summary)
	}
	if in.Context != nil {
		c, err := normContext(*in.Context)
		if err != nil {
			return err
		}
		e.Context = c
	}
	if in.Aliases != nil {
		aliases := []string{}
		for _, a := range *in.Aliases {
			n, err := NormalizeAlias(a)
			if err != nil {
				return err
			}
			if !slices.Contains(aliases, n) {
				aliases = append(aliases, n)
			}
		}
		if len(aliases) > 100 {
			return invalidf("at most 100 aliases")
		}
		e.Aliases = aliases
	}
	return nil
}

// saveEntity reconciles alias claims and tags, then reindexes.
func (s *Service) saveEntity(ctx context.Context, tx data.Database, e *Entity, before []string, in EntityInput) (*EntityView, error) {
	if in.Aliases != nil {
		for _, a := range before {
			if !slices.Contains(e.Aliases, a) {
				if _, err := dbx.DeleteIf[EntityAlias](ctx, tx, a, map[string]any{"entity_id": e.ID}); err != nil {
					return nil, err
				}
			}
		}
		for _, a := range e.Aliases {
			if err := claimAlias(ctx, tx, a, e.ID); err != nil {
				return nil, err
			}
		}
	}
	if in.Tags != nil {
		tags, err := normTags(*in.Tags)
		if err != nil {
			return nil, err
		}
		if err := s.setLinks(ctx, tx, e.ID, RelTag, tagLinks(tags)); err != nil {
			return nil, err
		}
	}
	if err := s.reindexEntity(ctx, tx, e); err != nil {
		return nil, err
	}
	return s.entityView(ctx, tx, e)
}

func claimAlias(ctx context.Context, tx data.Database, alias, entityID string) error {
	existing, err := dbx.Get[EntityAlias](ctx, tx, alias)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.EntityID == entityID {
			return nil
		}
		return &AliasConflictError{Alias: alias, EntityID: existing.EntityID}
	}
	return tx.Table(EntityAlias{}).Insert(ctx, &EntityAlias{ID: alias, EntityID: entityID})
}

func (s *Service) reindexEntity(ctx context.Context, db data.Database, e *Entity) error {
	tags, err := tagsOf(ctx, db, e.ID)
	if err != nil {
		return err
	}
	return s.putSearch(ctx, db, entitySearchRow(e, tags))
}

func (s *Service) entityView(ctx context.Context, db data.Database, e *Entity) (*EntityView, error) {
	v := &EntityView{Entity: *e}
	var err error
	if v.Tags, err = tagsOf(ctx, db, e.ID); err != nil {
		return nil, err
	}
	about, err := linksTo(ctx, db, e.ID, RelAbout)
	if err != nil {
		return nil, err
	}
	var factIDs, noteIDs, itemIDs []string
	for _, id := range about {
		switch KindOf(id) {
		case KindFact:
			factIDs = append(factIDs, id)
		case KindNote:
			noteIDs = append(noteIDs, id)
		case KindItem:
			itemIDs = append(itemIDs, id)
		}
	}
	facts, err := factRefs(ctx, db, factIDs)
	if err != nil {
		return nil, err
	}
	v.Facts = []FactRef{}
	for _, f := range facts {
		if !f.Retracted && !f.Superseded {
			v.Facts = append(v.Facts, f)
		}
	}
	if v.Notes, err = noteRefs(ctx, db, noteIDs); err != nil {
		return nil, err
	}
	for _, a := range e.Aliases {
		rows, err := dbx.All[ItemParticipant](ctx, db, data.QueryOpts{Where: map[string]any{"alias": a}})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			itemIDs = append(itemIDs, r.ItemID)
		}
	}
	slices.Sort(itemIDs)
	itemIDs = slices.Compact(itemIDs)
	v.ItemCount = len(itemIDs)
	items, err := itemRefs(ctx, db, itemIDs)
	if err != nil {
		return nil, err
	}
	if len(items) > 25 {
		items = items[:25]
	}
	v.Items = items
	return v, nil
}

// GetEntity reads an entity, following merges.
func (s *Service) GetEntity(ctx context.Context, db data.Database, id string) (*EntityView, error) {
	e, err := resolveEntity(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return s.entityView(ctx, db, e)
}

// EntityFilter narrows ListEntities.
type EntityFilter struct {
	Kind   string
	Alias  string
	Limit  int
	Offset int
}

// ListEntities lists live entities by name.
func (s *Service) ListEntities(ctx context.Context, db data.Database, filter EntityFilter) ([]Entity, error) {
	if filter.Alias != "" {
		alias, err := NormalizeAlias(filter.Alias)
		if err != nil {
			return nil, err
		}
		claim, err := dbx.Get[EntityAlias](ctx, db, alias)
		if err != nil || claim == nil {
			return []Entity{}, err
		}
		e, err := resolveEntity(ctx, db, claim.EntityID)
		if err != nil {
			return nil, err
		}
		return []Entity{*e}, nil
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	opts := data.QueryOpts{Where: map[string]any{"merged_into": ""}, OrderBy: "name", Limit: limit, Offset: filter.Offset}
	if filter.Kind != "" {
		opts.Where["kind"] = filter.Kind
	}
	rows, err := dbx.All[Entity](ctx, db, opts)
	if err != nil {
		return nil, err
	}
	out := make([]Entity, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}

// MergeEntities folds from into into: aliases, links and tags move, and from
// remains as a pointer so old references resolve. An agent may merge only
// entities agents created.
func (s *Service) MergeEntities(ctx context.Context, db data.Database, actor Actor, fromID, intoID string) (*EntityView, error) {
	var out *EntityView
	err := transact(ctx, db, func(tx data.Database) error {
		from, err := resolveEntity(ctx, tx, fromID)
		if err != nil {
			return err
		}
		into, err := resolveEntity(ctx, tx, intoID)
		if err != nil {
			return err
		}
		if from.ID == into.ID {
			return invalidf("an entity cannot merge into itself")
		}
		if !actor.canModify(from.AuthorKind, false) || !actor.canModify(into.AuthorKind, false) {
			return ErrForbidden
		}
		for _, a := range from.Aliases {
			if _, err := dbx.DeleteIf[EntityAlias](ctx, tx, a, map[string]any{"entity_id": from.ID}); err != nil {
				return err
			}
			if !slices.Contains(into.Aliases, a) {
				into.Aliases = append(into.Aliases, a)
			}
			if err := claimAlias(ctx, tx, a, into.ID); err != nil {
				return err
			}
		}
		if into.Summary == "" {
			into.Summary = from.Summary
		}
		// Move links pointing at from (facts and notes about it) and from's tags.
		for _, col := range []string{"to_id", "from_id"} {
			rows, err := dbx.All[Link](ctx, tx, data.QueryOpts{Where: map[string]any{col: from.ID}})
			if err != nil {
				return err
			}
			for _, l := range rows {
				if err := tx.Table(Link{}).Delete(ctx, l.ID); err != nil {
					return err
				}
				moved := *l
				if col == "to_id" {
					moved.ToID = into.ID
				} else {
					moved.FromID = into.ID
				}
				moved.ID = linkID(moved.FromID, moved.Rel, moved.ToID)
				if _, err := dbx.InsertNew(ctx, tx, moved.ID, &moved); err != nil {
					return err
				}
			}
		}
		from.MergedInto, from.Aliases, from.UpdatedAt = into.ID, []string{}, s.clock()
		into.UpdatedAt = from.UpdatedAt
		if err := tx.Table(Entity{}).Update(ctx, from); err != nil {
			return err
		}
		if err := tx.Table(Entity{}).Update(ctx, into); err != nil {
			return err
		}
		if err := s.reindexEntity(ctx, tx, from); err != nil {
			return err
		}
		if err := s.reindexEntity(ctx, tx, into); err != nil {
			return err
		}
		v, err := s.entityView(ctx, tx, into)
		out = v
		return err
	})
	return out, err
}

// DeleteEntity removes an entity and its alias claims. Owner only.
func (s *Service) DeleteEntity(ctx context.Context, db data.Database, actor Actor, id string) error {
	if !actor.owner() {
		return ErrForbidden
	}
	return transact(ctx, db, func(tx data.Database) error {
		e, err := dbx.Get[Entity](ctx, tx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return notFound("entity", id)
		}
		for _, a := range e.Aliases {
			if _, err := dbx.DeleteIf[EntityAlias](ctx, tx, a, map[string]any{"entity_id": e.ID}); err != nil {
				return err
			}
		}
		if err := dropLinks(ctx, tx, id); err != nil {
			return err
		}
		if err := dropSearch(ctx, tx, id); err != nil {
			return err
		}
		return tx.Table(Entity{}).Delete(ctx, id)
	})
}

func entityRefs(ctx context.Context, db data.Database, ids []string) ([]EntityRef, error) {
	out := []EntityRef{}
	for _, id := range ids {
		e, err := resolveEntity(ctx, db, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, EntityRef{e.ID, e.Kind, e.Name})
	}
	return out, nil
}

func entityForAlias(ctx context.Context, db data.Database, alias string) (*EntityRef, error) {
	claim, err := dbx.Get[EntityAlias](ctx, db, alias)
	if err != nil || claim == nil {
		return nil, err
	}
	e, err := resolveEntity(ctx, db, claim.EntityID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &EntityRef{e.ID, e.Kind, e.Name}, nil
}
