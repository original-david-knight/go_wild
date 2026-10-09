package gowild_knowledge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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

// UpdateEntity edits an entity within the write fence. An entity the
// actor may not modify still takes an edit that only adds associations
// (aliases or tags): an agent that sees the owner's Slack messages may
// record that they are his, but not rename or resummarise him.
func (s *Service) UpdateEntity(ctx context.Context, db data.Database, actor Actor, id string, in EntityInput) (*EntityView, error) {
	var out *EntityView
	err := transact(ctx, db, func(tx data.Database) error {
		e, err := resolveEntity(ctx, tx, id)
		if err != nil {
			return err
		}
		if !actor.canModify(e.AuthorKind) {
			ok, err := onlyAddsAssociations(ctx, tx, e, in)
			if err != nil {
				return err
			}
			if !ok {
				return ErrForbidden
			}
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

// onlyAddsAssociations reports whether an edit touches nothing but the
// entity's aliases and tags, and removes none of them.
func onlyAddsAssociations(ctx context.Context, db data.Database, e *Entity, in EntityInput) (bool, error) {
	if in.Kind != nil || in.Name != nil || in.Summary != nil || in.Context != nil {
		return false, nil
	}
	if in.Aliases != nil {
		var want []string
		for _, a := range *in.Aliases {
			n, err := NormalizeAlias(a)
			if err != nil {
				return false, err
			}
			want = append(want, n)
		}
		for _, have := range e.Aliases {
			if !slices.Contains(want, have) {
				return false, nil
			}
		}
	}
	if in.Tags != nil {
		want, err := normTags(*in.Tags)
		if err != nil {
			return false, err
		}
		have, err := tagsOf(ctx, db, e.ID)
		if err != nil {
			return false, err
		}
		for _, t := range have {
			if !slices.Contains(want, t) {
				return false, nil
			}
		}
	}
	return true, nil
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
		if !f.Retracted && !f.Superseded && !f.Ended {
			v.Facts = append(v.Facts, f)
		}
	}
	if v.Notes, err = noteRefs(ctx, db, noteIDs); err != nil {
		return nil, err
	}
	if len(e.Aliases) > 0 {
		aliases := make([]any, len(e.Aliases))
		for i, a := range e.Aliases {
			aliases[i] = a
		}
		rows, err := dbx.All[ItemParticipant](ctx, db, data.QueryOpts{WhereIn: map[string][]any{"alias": aliases}})
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
	if v.Items, err = itemRefs(ctx, db, itemIDs, entityItems); err != nil {
		return nil, err
	}
	return v, nil
}

// entityItems is how many of its newest items an entity view carries.
const entityItems = 25

// GetEntity reads an entity, following merges.
func (s *Service) GetEntity(ctx context.Context, db data.Database, id string) (*EntityView, error) {
	e, err := resolveEntity(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return s.entityView(ctx, db, e)
}

// EntityStats is a live entity with how much the knowledge base holds about
// it, counted as GetEntity counts: its live facts, the items it took part in
// or that are about it, and when the newest of those occurred.
type EntityStats struct {
	Entity
	FactCount  int       `json:"fact_count"`
	ItemCount  int       `json:"item_count"`
	LastItemAt time.Time `json:"last_item_at,omitzero"`
}

// entityStatsSQL counts every live entity's facts and items in one pass.
// Inactive flags are compared with IS NOT TRUE because columns added after a
// row was written read as NULL, which GetEntity treats as false.
const entityStatsSQL = `
WITH about AS (
	SELECT l.to_id AS entity_id, l.from_id
	FROM kb_links l JOIN kb_entities e ON e.id = l.to_id
	WHERE l.rel = 'about' AND e.merged_into = ''
),
facts AS (
	SELECT a.entity_id, count(*) AS n
	FROM about a JOIN kb_facts f ON f.id = a.from_id
	WHERE f.retracted IS NOT TRUE AND f.ended IS NOT TRUE AND COALESCE(f.superseded_by, '') = ''
	GROUP BY a.entity_id
),
pairs AS (
	SELECT entity_id, from_id AS item_id FROM about WHERE substr(from_id, 1, 4) = 'itm_'
	UNION
	SELECT c.entity_id, p.item_id
	FROM kb_entity_aliases c
	JOIN kb_entities e ON e.id = c.entity_id
	JOIN kb_item_participants p ON p.alias = c.id
	WHERE e.merged_into = ''
),
items AS (
	SELECT pairs.entity_id, count(*) AS n, max(i.occurred_at) AS last
	FROM pairs LEFT JOIN kb_items i ON i.id = pairs.item_id
	GROUP BY pairs.entity_id
)
SELECT coalesce(facts.entity_id, items.entity_id), coalesce(facts.n, 0), coalesce(items.n, 0), items.last
FROM facts FULL OUTER JOIN items ON items.entity_id = facts.entity_id`

// ListEntityStats lists every live entity by name with its counts, in two
// queries however many entities there are.
func (s *Service) ListEntityStats(ctx context.Context, db data.Database) ([]EntityStats, error) {
	rows, err := dbx.All[Entity](ctx, db, data.QueryOpts{Where: map[string]any{"merged_into": ""}, OrderBy: "name"})
	if err != nil {
		return nil, err
	}
	exec, _, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	counted, err := exec.QueryContext(ctx, entityStatsSQL)
	if err != nil {
		return nil, err
	}
	defer counted.Close()
	stats := map[string]EntityStats{}
	for counted.Next() {
		var id string
		var st EntityStats
		var last any
		if err := counted.Scan(&id, &st.FactCount, &st.ItemCount, &last); err != nil {
			return nil, err
		}
		// kb_items is a gowild_data table: SQLite keeps its times as RFC 3339
		// text, PostgreSQL as timestamptz.
		switch v := last.(type) {
		case time.Time:
			st.LastItemAt = v
		case string:
			st.LastItemAt, _ = time.Parse(time.RFC3339, v)
		case []byte:
			st.LastItemAt, _ = time.Parse(time.RFC3339, string(v))
		}
		stats[id] = st
	}
	if err := counted.Err(); err != nil {
		return nil, err
	}
	out := make([]EntityStats, 0, len(rows))
	for _, e := range rows {
		st := stats[e.ID]
		st.Entity = *e
		out = append(out, st)
	}
	return out, nil
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
		if !actor.canModify(from.AuthorKind) || !actor.canModify(into.AuthorKind) {
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

// entityRefMap names the entity each id refers to, after merges. Unknown
// ids are absent.
type entityRefMap map[string]EntityRef

// resolveEntityRefs reads ids' entities in one query, and follows a merge
// only for an id that was merged away.
func resolveEntityRefs(ctx context.Context, db data.Database, ids []string) (entityRefMap, error) {
	out := entityRefMap{}
	if len(ids) == 0 {
		return out, nil
	}
	in := make([]any, len(ids))
	for i, id := range ids {
		in[i] = id
	}
	rows, err := dbx.All[Entity](ctx, db, data.QueryOpts{WhereIn: map[string][]any{"id": in}})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		e := r
		if e.MergedInto != "" {
			if e, err = resolveEntity(ctx, db, e.MergedInto); errors.Is(err, ErrNotFound) {
				continue
			} else if err != nil {
				return nil, err
			}
		}
		out[r.ID] = EntityRef{e.ID, e.Kind, e.Name}
	}
	return out, nil
}

// list names ids' entities in order, skipping unknown ones.
func (m entityRefMap) list(ids []string) []EntityRef {
	out := []EntityRef{}
	for _, id := range ids {
		if ref, ok := m[id]; ok {
			out = append(out, ref)
		}
	}
	return out
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
