// Package gowild_knowledge is a personal knowledge base: raw items pushed by
// importers (email, chat, anything with an external ID), distilled facts that
// cite them, free-form notes, and lightweight entities (people, orgs,
// projects) that merge one identity across sources through aliases.
//
// Records are ordinary gowild_data tables. Search runs over one derived
// table, kb_search, which holds every searchable record's text, its
// full-text vector on PostgreSQL and, when pgvector is installed, its
// embedding. Ranking fuses keyword and semantic hits (reciprocal rank
// fusion) and weights distilled knowledge above raw material. SQLite is
// supported for tests and small deployments with a substring keyword match
// and an in-process cosine scan.
package gowild_knowledge

import (
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// Record kinds. IDs carry the kind as a prefix, so any ID names its table.
const (
	KindItem   = "item"
	KindFact   = "fact"
	KindNote   = "note"
	KindEntity = "entity"
)

// Author kinds. The owner's writes are authoritative; agents can change only
// what agents wrote.
const (
	AuthorOwner = "owner"
	AuthorAgent = "agent"
)

// Entity kinds.
var EntityKinds = []string{"person", "org", "project", "place", "other"}

// Source is one importer's feed: "gmail:personal", "slack:acme". Importers
// push items under it and keep their sync position in Cursor.
type Source struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	Context      string         `json:"context"`
	Work         bool           `json:"work"`
	Enabled      bool           `json:"enabled"`
	BackfillDays int            `json:"backfill_days"`
	Settings     map[string]any `json:"settings"`
	Cursor       string         `json:"cursor"`
	LastSyncAt   time.Time      `json:"last_sync_at,omitzero"`
	LastError    string         `json:"last_error"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (Source) TableName() string { return "kb_sources" }

// Participant is someone on an item, named by an alias ("email:a@b.com",
// "slack:T01/U02") so entities can claim them across sources.
type Participant struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

// Attachment is an item's attachment: its extracted text is indexed, the
// file itself stays at URL.
type Attachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	URL      string `json:"url"`
	Text     string `json:"text"`
}

// Item is one imported thing, kept as it arrived. Its ID derives from
// (source, external ID), so a re-import updates rather than duplicates.
type Item struct {
	ID           string         `json:"id"`
	SourceID     string         `json:"source_id"`
	ExternalID   string         `json:"external_id"`
	Kind         string         `json:"kind"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	URL          string         `json:"url"`
	OccurredAt   time.Time      `json:"occurred_at"`
	Context      string         `json:"context"`
	Participants []Participant  `json:"participants"`
	Attachments  []Attachment   `json:"attachments"`
	Metadata     map[string]any `json:"metadata"`
	ContentHash  string         `json:"content_hash"`
	ExtractedAt  time.Time      `json:"extracted_at,omitzero"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (Item) TableName() string { return "kb_items" }

// ItemParticipant indexes an item's participant aliases for entity lookups.
type ItemParticipant struct {
	ID     string `json:"id"` // item ID + "|" + alias
	ItemID string `json:"item_id"`
	Alias  string `json:"alias"`
	Role   string `json:"role"`
}

func (ItemParticipant) TableName() string { return "kb_item_participants" }

// Fact is one distilled statement. Verified facts are the owner's word and
// outrank agent facts; SourceGone marks a fact whose cited items were all
// deleted at their source.
type Fact struct {
	ID           string    `json:"id"`
	Text         string    `json:"text"`
	Context      string    `json:"context"`
	Confidence   float64   `json:"confidence"`
	ValidFrom    time.Time `json:"valid_from,omitzero"`
	ValidUntil   time.Time `json:"valid_until,omitzero"`
	Supersedes   string    `json:"supersedes"`
	SupersededBy string    `json:"superseded_by"`
	AuthorKind   string    `json:"author_kind"`
	Author       string    `json:"author"`
	Verified     bool      `json:"verified"`
	SourceGone   bool      `json:"source_gone"`
	Retracted    bool      `json:"retracted"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Fact) TableName() string { return "kb_facts" }

// Note is a longer free-form page.
type Note struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Context    string    `json:"context"`
	AuthorKind string    `json:"author_kind"`
	Author     string    `json:"author"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Note) TableName() string { return "kb_notes" }

// Entity is a person, org, project or place. Aliases claim participants
// across sources; MergedInto points at the surviving entity after a merge.
type Entity struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary"`
	Aliases    []string  `json:"aliases"`
	Context    string    `json:"context"`
	AuthorKind string    `json:"author_kind"`
	Author     string    `json:"author"`
	MergedInto string    `json:"merged_into"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Entity) TableName() string { return "kb_entities" }

// EntityAlias is the unique alias → entity claim.
type EntityAlias struct {
	ID       string `json:"id"` // the alias
	EntityID string `json:"entity_id"`
}

func (EntityAlias) TableName() string { return "kb_entity_aliases" }

// Link relations.
const (
	RelSource = "source" // fact → item it was drawn from
	RelAbout  = "about"  // fact/note/item → entity
	RelTag    = "tag"    // fact/note/entity → "tag:<name>"
)

// Link is a typed edge from a record to an item, an entity or a tag.
type Link struct {
	ID        string    `json:"id"` // from|rel|to
	FromID    string    `json:"from_id"`
	Rel       string    `json:"rel"`
	ToID      string    `json:"to_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (Link) TableName() string { return "kb_links" }

func init() {
	data.RegisterFunc(func(db data.Database) error {
		for _, table := range []any{Source{}, Item{}, ItemParticipant{}, Fact{}, Note{}, Entity{}, EntityAlias{}, Link{}} {
			if err := db.AddTable(table); err != nil {
				return err
			}
		}
		for _, idx := range []struct {
			model   any
			name    string
			columns []string
		}{
			{ItemParticipant{}, "kb_item_participants_alias", []string{"alias", "item_id"}},
			{Link{}, "kb_links_to", []string{"to_id", "rel", "from_id"}},
			{Link{}, "kb_links_from", []string{"from_id", "rel", "to_id"}},
		} {
			if err := data.EnsureUniqueIndex(db, idx.model, idx.name, idx.columns...); err != nil {
				return err
			}
		}
		return EnsureSearchSchema(db)
	})
}
