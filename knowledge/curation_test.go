package gowild_knowledge

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// tick is a clock that advances one second per reading, so records written
// in sequence sort in that order.
func tick() func() time.Time {
	var mu sync.Mutex
	now := t0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	}
}

func noteIDs(ns []NoteView) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

func factIDs(fs []FactView) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

func entityNames(es []Entity) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestNotesLifecycle(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New(WithClock(tick()))
		alice, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Alice")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr("No body")}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("note without body = %v", err)
		}
		if _, err := s.CreateNote(ctx, db, Actor{Kind: AuthorAgent}, NoteInput{Title: ptr("t"), Body: ptr("b")}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("unnamed agent wrote a note = %v", err)
		}
		if _, err := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr("  "), Body: ptr("b")}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("blank title = %v", err)
		}
		if _, err := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr("t"), Body: ptr("b"), Context: ptr("Bad Context!")}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad context = %v", err)
		}

		trip, err := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr(" Trip "), Body: ptr("Packing list"), About: &[]string{alice.ID}, Tags: &[]string{"#Travel"}})
		if err != nil {
			t.Fatal(err)
		}
		if trip.Title != "Trip" || trip.Context != DefaultContext || !slices.Equal(trip.Tags, []string{"travel"}) || len(trip.About) != 1 || trip.About[0].Name != "Alice" {
			t.Fatalf("created note = %+v", trip)
		}
		gift, err := s.CreateNote(ctx, db, Agent("fable"), NoteInput{Title: ptr("Gift ideas"), Body: ptr("Books"), Context: ptr("Family"), About: &[]string{alice.ID}})
		if err != nil {
			t.Fatal(err)
		}
		work, err := s.CreateNote(ctx, db, Agent("fable"), NoteInput{Title: ptr("Standup"), Body: ptr("Notes"), Context: ptr("work"), Tags: &[]string{"travel"}})
		if err != nil {
			t.Fatal(err)
		}

		// The write fence: an agent edits agents' notes, not the owner's.
		if _, err := s.UpdateNote(ctx, db, Agent("opus"), trip.ID, NoteInput{Body: ptr("mine now")}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent edited the owner's note = %v", err)
		}
		if _, err := s.UpdateNote(ctx, db, Owner, "not_missing", NoteInput{Body: ptr("x")}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("edit of a missing note = %v", err)
		}
		if _, err := s.UpdateNote(ctx, db, Agent("opus"), gift.ID, NoteInput{Body: ptr(" ")}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("blank body = %v", err)
		}
		edited, err := s.UpdateNote(ctx, db, Agent("opus"), gift.ID, NoteInput{Body: ptr("Books and tea"), Tags: &[]string{"shopping"}})
		if err != nil {
			t.Fatal(err)
		}
		if edited.Body != "Books and tea" || edited.Title != "Gift ideas" || edited.Context != "family" || !slices.Equal(edited.Tags, []string{"shopping"}) || len(edited.About) != 1 {
			t.Fatalf("edited note = %+v", edited)
		}

		got, err := s.GetNote(ctx, db, gift.ID)
		if err != nil || got.Body != "Books and tea" || got.Author != "fable" || got.About[0].ID != alice.ID {
			t.Fatalf("read back = %+v, %v", got, err)
		}
		if _, err := s.GetNote(ctx, db, "not_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing note = %v", err)
		}

		// Listing: newest update first, with each filter and their overlap.
		for name, tc := range map[string]struct {
			f    NoteFilter
			want []string
		}{
			"all":              {NoteFilter{}, []string{gift.ID, work.ID, trip.ID}},
			"entity":           {NoteFilter{EntityID: alice.ID}, []string{gift.ID, trip.ID}},
			"tag":              {NoteFilter{Tag: "Travel"}, []string{work.ID, trip.ID}},
			"entity and tag":   {NoteFilter{EntityID: alice.ID, Tag: "travel"}, []string{trip.ID}},
			"no overlap":       {NoteFilter{EntityID: alice.ID, Tag: "nothing"}, nil},
			"author kind":      {NoteFilter{AuthorKind: AuthorAgent}, []string{gift.ID, work.ID}},
			"context":          {NoteFilter{Context: "work"}, []string{work.ID}},
			"limit and offset": {NoteFilter{Limit: 1, Offset: 1}, []string{work.ID}},
		} {
			notes, err := s.ListNotes(ctx, db, tc.f)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !slices.Equal(noteIDs(notes), tc.want) {
				t.Errorf("%s = %v, want %v", name, noteIDs(notes), tc.want)
			}
		}
		if _, err := s.ListNotes(ctx, db, NoteFilter{EntityID: "ent_missing"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("list by a missing entity = %v", err)
		}

		// The entity page names its notes, newest first.
		view, err := s.GetEntity(ctx, db, alice.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Notes) != 2 || view.Notes[0] != (NoteRef{gift.ID, "Gift ideas", AuthorAgent, "fable"}) || view.Notes[1].ID != trip.ID {
			t.Fatalf("entity notes = %+v", view.Notes)
		}

		// Deleting: missing, fenced, then gone from reads, lists and search.
		if err := s.DeleteNote(ctx, db, Owner, "not_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete of a missing note = %v", err)
		}
		if err := s.DeleteNote(ctx, db, Agent("opus"), gift.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetNote(ctx, db, gift.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted note still readable: %v", err)
		}
		r, err := s.Search(ctx, db, SearchQuery{Text: "tea"})
		if err != nil || len(r.Hits) != 0 {
			t.Fatalf("deleted note still searchable: %v, %v", hitIDs(r), err)
		}
		if view, _ = s.GetEntity(ctx, db, alice.ID); len(view.Notes) != 1 || view.Notes[0].ID != trip.ID {
			t.Fatalf("entity notes after delete = %+v", view.Notes)
		}
	})
}

func TestEntityListingAndDeletion(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		for name, in := range map[string]EntityInput{
			"bad kind":     {Name: ptr("X"), Kind: ptr("robot")},
			"blank name":   {Name: ptr(" ")},
			"long summary": {Name: ptr("X"), Summary: ptr(strings.Repeat("a", 4001))},
			"bad context":  {Name: ptr("X"), Context: ptr("Not OK")},
			"bad alias":    {Name: ptr("X"), Aliases: &[]string{"nocolon"}},
			"bad tag":      {Name: ptr("X"), Tags: &[]string{"no spaces allowed"}},
		} {
			if _, err := s.CreateEntity(ctx, db, Owner, in); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s = %v, want invalid", name, err)
			}
		}
		if _, err := s.CreateEntity(ctx, db, Owner, EntityInput{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("nameless entity = %v", err)
		}
		if _, err := s.CreateEntity(ctx, db, Actor{}, EntityInput{Name: ptr("X")}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("no actor = %v", err)
		}
		many := make([]string, 101)
		for i := range many {
			many[i] = "email:a" + strings.Repeat("x", i) + "@example.com"
		}
		if _, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("X"), Aliases: &many}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("101 aliases = %v", err)
		}

		acme, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Acme"), Kind: ptr("org"), Summary: ptr(" Widgets "), Context: ptr("work")})
		if err != nil {
			t.Fatal(err)
		}
		if acme.Summary != "Widgets" || acme.Context != "work" || acme.Kind != "org" {
			t.Fatalf("created entity = %+v", acme.Entity)
		}
		bob, err := s.CreateEntity(ctx, db, Agent("fable"), EntityInput{Name: ptr("Bob"), Aliases: &[]string{"email:bob@example.com", "EMAIL:bob@example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(bob.Aliases, []string{"email:bob@example.com"}) {
			t.Fatalf("aliases not deduplicated: %v", bob.Aliases)
		}
		carol, err := s.CreateEntity(ctx, db, Agent("fable"), EntityInput{Name: ptr("Carol"), Aliases: &[]string{"slack:t1/u9"}})
		if err != nil {
			t.Fatal(err)
		}

		var conflict *AliasConflictError
		_, err = s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Robert"), Aliases: &[]string{"email:bob@example.com"}})
		if !errors.As(err, &conflict) || !errors.Is(err, ErrConflict) {
			t.Fatalf("alias conflict = %v", err)
		}
		if want := "alias email:bob@example.com already belongs to entity " + bob.ID; conflict.Error() != want {
			t.Fatalf("conflict message = %q, want %q", conflict.Error(), want)
		}

		// Merges: not into itself, not across the fence.
		if _, err := s.MergeEntities(ctx, db, Agent("fable"), bob.ID, bob.ID); !errors.Is(err, ErrInvalid) {
			t.Fatalf("self merge = %v", err)
		}
		if _, err := s.MergeEntities(ctx, db, Agent("fable"), bob.ID, acme.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent merged into the owner's entity = %v", err)
		}
		if _, err := s.MergeEntities(ctx, db, Agent("fable"), "ent_missing", acme.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("merge from a missing entity = %v", err)
		}
		if _, err := s.MergeEntities(ctx, db, Agent("fable"), bob.ID, "ent_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("merge into a missing entity = %v", err)
		}
		merged, err := s.MergeEntities(ctx, db, Agent("fable"), carol.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(merged.Aliases, []string{"email:bob@example.com", "slack:t1/u9"}) {
			t.Fatalf("merged aliases = %v", merged.Aliases)
		}

		for name, tc := range map[string]struct {
			f    EntityFilter
			want []string
		}{
			"live by name":  {EntityFilter{}, []string{"Acme", "Bob"}},
			"kind":          {EntityFilter{Kind: "person"}, []string{"Bob"}},
			"alias":         {EntityFilter{Alias: " Slack:T1/U9 "}, []string{"Bob"}},
			"unknown alias": {EntityFilter{Alias: "email:nobody@example.com"}, nil},
			"offset":        {EntityFilter{Offset: 1}, []string{"Bob"}},
		} {
			es, err := s.ListEntities(ctx, db, tc.f)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !slices.Equal(entityNames(es), tc.want) {
				t.Errorf("%s = %v, want %v", name, entityNames(es), tc.want)
			}
		}
		if _, err := s.ListEntities(ctx, db, EntityFilter{Alias: "bogus"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("list by a malformed alias = %v", err)
		}

		// Deleting is the owner's; it frees the aliases and drops links.
		fact, err := s.CreateFact(ctx, db, Owner, FactInput{Text: ptr("Bob likes chess"), About: &[]string{bob.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteEntity(ctx, db, Agent("fable"), bob.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent deleted an entity = %v", err)
		}
		if err := s.DeleteEntity(ctx, db, Owner, "ent_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete of a missing entity = %v", err)
		}
		if err := s.DeleteEntity(ctx, db, Owner, bob.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetEntity(ctx, db, bob.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted entity still readable: %v", err)
		}
		if f, _ := s.GetFact(ctx, db, fact.ID); len(f.About) != 0 {
			t.Fatalf("fact still about the deleted entity: %+v", f.About)
		}
		again, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Robert"), Aliases: &[]string{"email:bob@example.com"}})
		if err != nil || again.Name != "Robert" {
			t.Fatalf("freed alias not reusable: %v", err)
		}
	})
}

func TestFactCurationFilters(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New(WithClock(tick()))
		setupSource(t, s, db, "gmail:facts")
		if _, err := s.Ingest(ctx, db, Agent("desk"), "gmail:facts", IngestBatch{Items: []IngestItem{
			email("m1", "Chess", "Bob plays chess", t0, "bob@example.com"),
			email("m2", "Tennis", "Bob plays tennis", t0, "bob@example.com"),
		}}); err != nil {
			t.Fatal(err)
		}
		m1, m2 := ItemID("gmail:facts", "m1"), ItemID("gmail:facts", "m2")
		bob, _ := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Bob")})

		for name, tc := range map[string]struct {
			in   FactInput
			want error
		}{
			"blank text":         {FactInput{Text: ptr("")}, ErrInvalid},
			"confidence":         {FactInput{Text: ptr("x"), Confidence: ptr(1.5)}, ErrInvalid},
			"window":             {FactInput{Text: ptr("x"), ValidFrom: ptr(t0), ValidUntil: ptr(t0.Add(-time.Hour))}, ErrInvalid},
			"context":            {FactInput{Text: ptr("x"), Context: ptr("No Good")}, ErrInvalid},
			"tag":                {FactInput{Text: ptr("x"), Tags: &[]string{"bad tag"}}, ErrInvalid},
			"supersedes missing": {FactInput{Text: ptr("x"), Supersedes: ptr("fct_missing")}, ErrNotFound},
		} {
			if _, err := s.CreateFact(ctx, db, Agent("fable"), tc.in); !errors.Is(err, tc.want) {
				t.Errorf("%s = %v, want %v", name, err, tc.want)
			}
		}
		if _, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("fact without text = %v", err)
		}
		if _, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("x"), Sources: &[]string{"itm_missing"}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("fact citing a missing item = %v", err)
		}
		if _, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("x"), About: &[]string{"ent_missing"}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("fact about a missing entity = %v", err)
		}
		if _, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("x"), Verified: ptr(true)}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent created a verified fact = %v", err)
		}

		chess, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Bob plays chess"), Sources: &[]string{m1, " "}, About: &[]string{bob.ID, bob.ID}, Tags: &[]string{"hobby"}, Confidence: ptr(0.6), Context: ptr("Family")})
		if err != nil {
			t.Fatal(err)
		}
		if chess.Confidence != 0.6 || chess.Context != "family" || len(chess.About) != 1 || len(chess.Sources) != 1 {
			t.Fatalf("created fact = %+v", chess)
		}
		tennis, err := s.CreateFact(ctx, db, Owner, FactInput{Text: ptr("Bob plays tennis"), Sources: &[]string{m2}, About: &[]string{bob.ID}})
		if err != nil {
			t.Fatal(err)
		}
		old, _ := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Bob plays golf"), Tags: &[]string{"hobby"}})
		golf, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Bob quit golf"), Supersedes: ptr(old.ID)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateFact(ctx, db, Agent("fable"), golf.ID, FactInput{Supersedes: ptr(golf.ID)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("fact superseding itself = %v", err)
		}
		retracted, _ := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Bob hates chess"), Retracted: ptr(true)})

		for name, tc := range map[string]struct {
			f    FactFilter
			want []string
		}{
			"active":             {FactFilter{}, []string{golf.ID, tennis.ID, chess.ID}},
			"inactive too":       {FactFilter{IncludeInactive: true}, []string{retracted.ID, golf.ID, old.ID, tennis.ID, chess.ID}},
			"entity":             {FactFilter{EntityID: bob.ID}, []string{tennis.ID, chess.ID}},
			"item":               {FactFilter{ItemID: m1}, []string{chess.ID}},
			"tag":                {FactFilter{Tag: "HOBBY", IncludeInactive: true}, []string{old.ID, chess.ID}},
			"entity and tag":     {FactFilter{EntityID: bob.ID, Tag: "hobby"}, []string{chess.ID}},
			"entity, empty item": {FactFilter{EntityID: bob.ID, ItemID: "itm_none"}, nil},
			"verified":           {FactFilter{Verified: ptr(true)}, []string{tennis.ID}},
			"author kind":        {FactFilter{AuthorKind: AuthorAgent}, []string{golf.ID, chess.ID}},
			"context":            {FactFilter{Context: "family"}, []string{chess.ID}},
			"limit":              {FactFilter{Limit: 1, Offset: 1}, []string{tennis.ID}},
		} {
			fs, err := s.ListFacts(ctx, db, tc.f)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !slices.Equal(factIDs(fs), tc.want) {
				t.Errorf("%s = %v, want %v", name, factIDs(fs), tc.want)
			}
		}
		if _, err := s.ListFacts(ctx, db, FactFilter{EntityID: "ent_missing"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("list by a missing entity = %v", err)
		}

		// Deleting the item under a fact marks it source-gone; citing a new
		// item clears the mark.
		if err := s.DeleteItem(ctx, db, Agent("fable"), m1); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent deleted an item = %v", err)
		}
		if err := s.DeleteItem(ctx, db, Owner, "itm_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete of a missing item = %v", err)
		}
		if err := s.DeleteItem(ctx, db, Owner, m1); err != nil {
			t.Fatal(err)
		}
		gone, _ := s.ListFacts(ctx, db, FactFilter{SourceGone: ptr(true)})
		if !slices.Equal(factIDs(gone), []string{chess.ID}) {
			t.Fatalf("source-gone facts = %v", factIDs(gone))
		}
		cited, err := s.UpdateFact(ctx, db, Agent("fable"), chess.ID, FactInput{Sources: &[]string{m2}})
		if err != nil || cited.SourceGone || len(cited.Sources) != 1 || cited.Sources[0].ID != m2 {
			t.Fatalf("recited fact = %+v, %v", cited, err)
		}

		if _, err := s.GetFact(ctx, db, "fct_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing fact = %v", err)
		}
		if _, err := s.UpdateFact(ctx, db, Owner, "fct_missing", FactInput{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("edit of a missing fact = %v", err)
		}
		if err := s.DeleteFact(ctx, db, Owner, "fct_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete of a missing fact = %v", err)
		}
	})
}

func TestSourcesListAndDelete(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		for name, tc := range map[string]struct {
			id string
			in SourceInput
		}{
			"id":       {"nocolon", SourceInput{}},
			"kind":     {"gmail:x", SourceInput{Kind: ptr("Not A Kind")}},
			"name":     {"gmail:x", SourceInput{Name: ptr(" ")}},
			"context":  {"gmail:x", SourceInput{Context: ptr("Bad Ctx")}},
			"backfill": {"gmail:x", SourceInput{BackfillDays: ptr(-1)}},
			"settings": {"gmail:x", SourceInput{Settings: &map[string]any{"big": strings.Repeat("a", 65<<10)}}},
			"include":  {"gmail:x", SourceInput{Include: ptr(make([]string, 5001))}},
		} {
			if _, err := s.PutSource(ctx, db, Owner, tc.id, tc.in); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s = %v, want invalid", name, err)
			}
		}
		src, err := s.PutSource(ctx, db, Owner, " Slack:Acme ", SourceInput{
			Kind: ptr("slack"), Name: ptr(" Acme Slack "), BackfillDays: ptr(30), Settings: &map[string]any{"team": "T1"},
			Include: &[]string{"C1", " C1 ", "", "C2"}, IncludeKinds: &[]string{"im"}, Cursor: ptr("c9"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if src.ID != "slack:acme" || src.Name != "Acme Slack" || src.BackfillDays != 30 || src.Settings["team"] != "T1" ||
			!slices.Equal(src.Include, []string{"C1", "C2"}) || src.Cursor != "c9" || src.Context != DefaultContext {
			t.Fatalf("source = %+v", src.Source)
		}
		setupSource(t, s, db, "gmail:home")
		if _, err := s.Ingest(ctx, db, Agent("desk"), "gmail:home", IngestBatch{Items: []IngestItem{
			email("m1", "One", "first", t0, "a@example.com"),
			email("m2", "Two", "second", t0, "a@example.com"),
		}}); err != nil {
			t.Fatal(err)
		}
		fact, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Two emails arrived"), Sources: &[]string{ItemID("gmail:home", "m1"), ItemID("gmail:home", "m2")}})
		if err != nil {
			t.Fatal(err)
		}

		list, err := s.ListSources(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].ID != "gmail:home" || list[0].ItemCount != 2 || list[1].ID != "slack:acme" || list[1].ItemCount != 0 {
			t.Fatalf("sources = %+v", list)
		}
		if list[0].Include == nil || list[0].Catalog == nil {
			t.Fatalf("empty lists read back as nil: %+v", list[0].Source)
		}

		if _, err := s.DeleteSource(ctx, db, Agent("desk"), "gmail:home"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent deleted a source = %v", err)
		}
		if _, err := s.DeleteSource(ctx, db, Owner, "gmail:none"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete of a missing source = %v", err)
		}
		n, err := s.DeleteSource(ctx, db, Owner, "gmail:home")
		if err != nil || n != 2 {
			t.Fatalf("deleted %d items, %v", n, err)
		}
		if _, err := s.GetSource(ctx, db, "gmail:home"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted source still readable: %v", err)
		}
		f, _ := s.GetFact(ctx, db, fact.ID)
		if !f.SourceGone || len(f.Sources) != 0 {
			t.Fatalf("fact after its source went = %+v", f)
		}
		st, err := s.GetStatus(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if *st != (Status{Facts: 1, Sources: 1, PendingEmbedding: 1}) {
			t.Fatalf("status = %+v", *st)
		}
	})
}

func TestIngestLimitsAndErrorText(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		setupSource(t, s, db, "gmail:limits")
		// A long error is kept to 2000 bytes without splitting a rune.
		long := strings.Repeat("a", 1999) + "é" + "tail"
		if _, err := s.Ingest(ctx, db, Owner, "gmail:limits", IngestBatch{Error: &long}); err != nil {
			t.Fatal(err)
		}
		src, _ := s.GetSource(ctx, db, "gmail:limits")
		if src.LastError != strings.Repeat("a", 1999) {
			t.Fatalf("stored error is %d bytes ending %q", len(src.LastError), src.LastError[len(src.LastError)-3:])
		}
		big := make([]CatalogEntry, 5001)
		if _, err := s.Ingest(ctx, db, Owner, "gmail:limits", IngestBatch{Catalog: &big}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("oversized catalog = %v", err)
		}
		if _, err := s.Ingest(ctx, db, Owner, "gmail:limits", IngestBatch{Deletes: make([]string, MaxIngestBatch+1)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("oversized push = %v", err)
		}
		if _, err := s.Ingest(ctx, db, Actor{}, "gmail:limits", IngestBatch{}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("push with no actor = %v", err)
		}
	})
}

func TestKindOf(t *testing.T) {
	for id, want := range map[string]string{
		ItemID("gmail:x", "1"): KindItem,
		"fct_1":                KindFact,
		"not_1":                KindNote,
		"ent_1":                KindEntity,
		"tag:x":                "",
	} {
		if got := KindOf(id); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestSemanticEnabled(t *testing.T) {
	if New().SemanticEnabled() {
		t.Fatal("semantic without an embedder")
	}
	if !New(WithEmbedder(wordEmbedder{})).SemanticEnabled() {
		t.Fatal("no semantic with an embedder")
	}
}
