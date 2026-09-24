package gowild_knowledge

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

func setupSource(t *testing.T, s *Service, db data.Database, id string) {
	t.Helper()
	if _, err := s.PutSource(context.Background(), db, Owner, id, SourceInput{}); err != nil {
		t.Fatal(err)
	}
}

func email(ext, title, body string, at time.Time, from string) IngestItem {
	return IngestItem{ExternalID: ext, Kind: "email", Title: title, Body: body, OccurredAt: at,
		Participants: []Participant{{Role: "from", Name: "Someone", Alias: "email:" + from}}}
}

func hitIDs(r *SearchResult) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.ID)
	}
	return out
}

func TestIngestIsIdempotentAndMirrorsDeletes(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		setupSource(t, s, db, "gmail:personal")
		batch := IngestBatch{Items: []IngestItem{
			email("m1", "Dentist appointment", "Your cleaning is on Tuesday at 3pm.", t0, "front@dental.example"),
			email("m2", "Invoice", "Invoice 42 for September.", t0.Add(time.Hour), "billing@example.com"),
		}, Cursor: ptr("h100")}
		res, err := s.Ingest(ctx, db, Agent("importer"), "gmail:personal", batch)
		if err != nil {
			t.Fatal(err)
		}
		if res.Created != 2 {
			t.Fatalf("created = %d, want 2", res.Created)
		}
		res, err = s.Ingest(ctx, db, Agent("importer"), "gmail:personal", batch)
		if err != nil {
			t.Fatal(err)
		}
		if res.Unchanged != 2 || res.Created != 0 {
			t.Fatalf("second push = %+v, want 2 unchanged", res)
		}
		batch.Items[0].Body = "Moved: your cleaning is on Wednesday at 3pm."
		batch.Items = batch.Items[:1]
		if res, err = s.Ingest(ctx, db, Agent("importer"), "gmail:personal", batch); err != nil || res.Updated != 1 {
			t.Fatalf("update push = %+v, %v", res, err)
		}
		src, err := s.GetSource(ctx, db, "gmail:personal")
		if err != nil {
			t.Fatal(err)
		}
		if src.Cursor != "h100" || src.ItemCount != 2 {
			t.Fatalf("source = cursor %q count %d", src.Cursor, src.ItemCount)
		}

		m1 := ItemID("gmail:personal", "m1")
		fact, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Dentist cleaning is Wednesday at 3pm"), Sources: &[]string{m1}})
		if err != nil {
			t.Fatal(err)
		}
		if res, err = s.Ingest(ctx, db, Agent("importer"), "gmail:personal", IngestBatch{Deletes: []string{"m1", "nope"}}); err != nil || res.Deleted != 1 {
			t.Fatalf("delete push = %+v, %v", res, err)
		}
		if _, err := s.GetItem(ctx, db, m1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted item still readable: %v", err)
		}
		got, err := s.GetFact(ctx, db, fact.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !got.SourceGone || len(got.Sources) != 0 {
			t.Fatalf("fact after source delete: gone=%v sources=%v", got.SourceGone, got.Sources)
		}
		r, err := s.Search(ctx, db, SearchQuery{Text: "cleaning"})
		if err != nil {
			t.Fatal(err)
		}
		if ids := hitIDs(r); !slices.Equal(ids, []string{fact.ID}) {
			t.Fatalf("search after delete = %v, want only the fact", ids)
		}
	})
}

func TestIngestValidation(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		if _, err := s.Ingest(ctx, db, Owner, "slack:none", IngestBatch{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown source: %v", err)
		}
		setupSource(t, s, db, "slack:acme")
		for name, item := range map[string]IngestItem{
			"no id":   {Kind: "message", OccurredAt: t0},
			"no time": {ExternalID: "a", Kind: "message"},
			"alias":   {ExternalID: "a", Kind: "message", OccurredAt: t0, Participants: []Participant{{Alias: "bogus"}}},
		} {
			if _, err := s.Ingest(ctx, db, Owner, "slack:acme", IngestBatch{Items: []IngestItem{item}}); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s: %v, want invalid", name, err)
			}
		}
		if _, err := s.PutSource(ctx, db, Agent("x"), "slack:other", SourceInput{}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent configured a source: %v", err)
		}
		if _, err := s.PutSource(ctx, db, Owner, "slack:acme", SourceInput{Enabled: ptr(false)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Ingest(ctx, db, Owner, "slack:acme", IngestBatch{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("disabled source accepted a push: %v", err)
		}
		if _, err := s.PutSource(ctx, db, Owner, "slack:acme", SourceInput{Enabled: ptr(true)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Ingest(ctx, db, Owner, "slack:acme", IngestBatch{Error: ptr("token expired")}); err != nil {
			t.Fatal(err)
		}
		if src, _ := s.GetSource(ctx, db, "slack:acme"); src.LastError != "token expired" || !src.LastSyncAt.IsZero() {
			t.Fatalf("failed sync recorded as fresh: %+v", src.Source)
		}
		work, err := s.PutSource(ctx, db, Owner, "gmail:work", SourceInput{Work: ptr(true)})
		if err != nil || work.Context != "work" {
			t.Fatalf("work source context = %q, %v", work.Context, err)
		}
	})
}

func TestWriteFence(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		agent := Agent("fable")
		mine, err := s.CreateFact(ctx, db, Owner, FactInput{Text: ptr("My passport expires in 2031")})
		if err != nil {
			t.Fatal(err)
		}
		if !mine.Verified || mine.Confidence != 1 {
			t.Fatalf("owner fact not verified: %+v", mine.Fact)
		}
		theirs, err := s.CreateFact(ctx, db, agent, FactInput{Text: ptr("David prefers aisle seats")})
		if err != nil {
			t.Fatal(err)
		}
		if theirs.Verified || theirs.Author != "fable" || theirs.Confidence != 0.8 {
			t.Fatalf("agent fact = %+v", theirs.Fact)
		}
		if _, err := s.UpdateFact(ctx, db, agent, mine.ID, FactInput{Text: ptr("changed")}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent edited owner fact: %v", err)
		}
		if _, err := s.UpdateFact(ctx, db, agent, theirs.ID, FactInput{Verified: ptr(true)}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent verified: %v", err)
		}
		if _, err := s.CreateFact(ctx, db, agent, FactInput{Text: ptr("Passport expires 2032"), Supersedes: ptr(mine.ID)}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent superseded owner fact: %v", err)
		}
		if err := s.DeleteFact(ctx, db, agent, theirs.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent deleted: %v", err)
		}
		if _, err := s.UpdateFact(ctx, db, agent, theirs.ID, FactInput{Retracted: ptr(true)}); err != nil {
			t.Fatal(err)
		}
		// Once the owner verifies an agent fact, agents can no longer touch it.
		other, _ := s.CreateFact(ctx, db, agent, FactInput{Text: ptr("Gym closes at 10pm")})
		if _, err := s.UpdateFact(ctx, db, Owner, other.ID, FactInput{Verified: ptr(true)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateFact(ctx, db, agent, other.ID, FactInput{Retracted: ptr(true)}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent retracted a verified fact: %v", err)
		}
		note, err := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr("Trip"), Body: ptr("Packing list")})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteNote(ctx, db, agent, note.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent deleted owner note: %v", err)
		}
		facts, err := s.ListFacts(ctx, db, FactFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(facts) != 2 {
			t.Fatalf("active facts = %d, want 2 (retracted hidden)", len(facts))
		}
	})
}

func TestSupersedeHidesOldFact(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		old, _ := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("Alice lives in Portland")})
		nu, err := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("Alice lives in Seattle"), Supersedes: ptr(old.ID)})
		if err != nil {
			t.Fatal(err)
		}
		r, _ := s.Search(ctx, db, SearchQuery{Text: "Alice lives"})
		if ids := hitIDs(r); !slices.Equal(ids, []string{nu.ID}) {
			t.Fatalf("hits = %v, want only the new fact", ids)
		}
		if err := s.DeleteFact(ctx, db, Owner, nu.ID); err != nil {
			t.Fatal(err)
		}
		r, _ = s.Search(ctx, db, SearchQuery{Text: "Alice lives"})
		if ids := hitIDs(r); !slices.Equal(ids, []string{old.ID}) {
			t.Fatalf("hits after deleting successor = %v, want the old fact back", ids)
		}
	})
}

func TestEntitiesAcrossSources(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		setupSource(t, s, db, "gmail:personal")
		setupSource(t, s, db, "slack:acme")
		if _, err := s.Ingest(ctx, db, Owner, "gmail:personal", IngestBatch{Items: []IngestItem{
			email("m1", "Lunch?", "Want to grab lunch Friday?", t0, "alice@example.com"),
		}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Ingest(ctx, db, Owner, "slack:acme", IngestBatch{Items: []IngestItem{{
			ExternalID: "c1/1", Kind: "message", Title: "#general", Body: "deploy is done", OccurredAt: t0,
			Participants: []Participant{{Role: "author", Name: "alice", Alias: "slack:T1/U1"}},
		}}}); err != nil {
			t.Fatal(err)
		}
		a, err := s.CreateEntity(ctx, db, Agent("fable"), EntityInput{Name: ptr("Alice Smith"), Aliases: &[]string{"Email:Alice@Example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		dup, err := s.CreateEntity(ctx, db, Agent("fable"), EntityInput{Name: ptr("alice (slack)"), Aliases: &[]string{"slack:T1/U1"}})
		if err != nil {
			t.Fatal(err)
		}
		var conflict *AliasConflictError
		if _, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Other"), Aliases: &[]string{"email:alice@example.com"}}); !errors.As(err, &conflict) || conflict.EntityID != a.ID {
			t.Fatalf("alias conflict = %v", err)
		}
		fact, _ := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("Alice is vegetarian"), About: &[]string{dup.ID}})
		merged, err := s.MergeEntities(ctx, db, Agent("fable"), dup.ID, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if merged.ItemCount != 2 || len(merged.Facts) != 1 || len(merged.Aliases) != 2 {
			t.Fatalf("merged view: items=%d facts=%d aliases=%v", merged.ItemCount, len(merged.Facts), merged.Aliases)
		}
		// The old ID still resolves, and search by entity spans both sources and the fact.
		view, err := s.GetEntity(ctx, db, dup.ID)
		if err != nil || view.ID != a.ID {
			t.Fatalf("merged-away entity resolves to %v, %v", view, err)
		}
		r, err := s.Search(ctx, db, SearchQuery{EntityID: dup.ID, Kinds: []string{KindItem, KindFact}})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Hits) != 3 {
			t.Fatalf("entity browse = %v, want 2 items and the fact", hitIDs(r))
		}
		item, err := s.GetItem(ctx, db, ItemID("gmail:personal", "m1"))
		if err != nil {
			t.Fatal(err)
		}
		if item.Participants[0].Entity == nil || item.Participants[0].Entity.ID != a.ID {
			t.Fatalf("participant not resolved: %+v", item.Participants)
		}
		f, _ := s.GetFact(ctx, db, fact.ID)
		if len(f.About) != 1 || f.About[0].ID != a.ID {
			t.Fatalf("fact about after merge = %+v", f.About)
		}
	})
}

func TestSearchRankingAndFilters(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		setupSource(t, s, db, "gmail:personal")
		if _, err := s.PutSource(ctx, db, Owner, "gmail:work", SourceInput{Work: ptr(true)}); err != nil {
			t.Fatal(err)
		}
		var items []IngestItem
		for i := range 5 {
			items = append(items, email("p"+string(rune('a'+i)), "Kayak rental", "Kayak rental confirmation number "+string(rune('A'+i)), t0.Add(time.Duration(i)*time.Hour), "shop@example.com"))
		}
		if _, err := s.Ingest(ctx, db, Owner, "gmail:personal", IngestBatch{Items: items}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Ingest(ctx, db, Owner, "gmail:work", IngestBatch{Items: []IngestItem{email("w1", "Kayak offsite", "Team kayak offsite planning", t0, "boss@corp.example")}}); err != nil {
			t.Fatal(err)
		}
		fact, _ := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("David owns a red kayak"), Tags: &[]string{"#Hobbies"}})
		note, _ := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr("Paddling"), Body: ptr("Best kayak launch is at the north beach."), Tags: &[]string{"hobbies"}})

		r, err := s.Search(ctx, db, SearchQuery{Text: "kayak", Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Hits) != 3 || r.Hits[0].Kind != KindFact {
			t.Fatalf("top hits = %+v, want 3 with the fact first", r.Hits)
		}
		if r.Semantic || r.SemanticError == "" {
			t.Fatalf("semantic without an embedder: %+v", r)
		}
		r, _ = s.Search(ctx, db, SearchQuery{Text: "kayak", Contexts: []string{"work"}})
		if ids := hitIDs(r); !slices.Equal(ids, []string{ItemID("gmail:work", "w1")}) {
			t.Fatalf("work context = %v", ids)
		}
		r, _ = s.Search(ctx, db, SearchQuery{Tag: "hobbies"})
		if ids := hitIDs(r); len(ids) != 2 || !slices.Contains(ids, fact.ID) || !slices.Contains(ids, note.ID) {
			t.Fatalf("tag browse = %v", ids)
		}
		r, _ = s.Search(ctx, db, SearchQuery{SourceID: "gmail:personal", Limit: 2})
		if len(r.Hits) != 2 || r.Hits[0].ID != ItemID("gmail:personal", "pe") {
			t.Fatalf("source browse newest-first = %v", hitIDs(r))
		}
		r, _ = s.Search(ctx, db, SearchQuery{Text: "kayak", Since: t0.Add(150 * time.Minute), Kinds: []string{KindItem}})
		if len(r.Hits) != 2 {
			t.Fatalf("since filter = %v", hitIDs(r))
		}
		for _, h := range r.Hits {
			if h.Snippet == "" {
				t.Fatalf("hit without snippet: %+v", h)
			}
		}
		if _, err := s.Search(ctx, db, SearchQuery{Text: "x", Kinds: []string{"bogus"}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad kind: %v", err)
		}
	})
}

func TestSemanticSearch(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		// The bag-of-words embedder scores far lower than a real model.
		defer func(v, b float64) { MinSimilarity, SimilarityBand = v, b }(MinSimilarity, SimilarityBand)
		MinSimilarity, SimilarityBand = 0.3, 1
		s := New(WithEmbedder(wordEmbedder{}))
		setupSource(t, s, db, "gmail:personal")
		if _, err := s.Ingest(ctx, db, Owner, "gmail:personal", IngestBatch{Items: []IngestItem{
			email("m1", "Flight itinerary", "Your flight departs Denver gate B12", t0, "air@example.com"),
			email("m2", "Garden club", "Tomatoes and basil seedlings available", t0, "garden@example.com"),
		}}); err != nil {
			t.Fatal(err)
		}
		st, err := s.GetStatus(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Semantic || st.PendingEmbedding != 2 || st.Items != 2 {
			t.Fatalf("status before embedding = %+v", st)
		}
		n, err := s.EmbedPending(ctx, db, 10)
		if err != nil || n != 2 {
			t.Fatalf("embedded %d, %v", n, err)
		}
		// "denver departs" shares words with m1 in a different order and
		// form than a phrase match; both halves should find it.
		r, err := s.Search(ctx, db, SearchQuery{Text: "departs denver", Mode: ModeSemantic})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Semantic || len(r.Hits) != 1 || r.Hits[0].ID != ItemID("gmail:personal", "m1") || !r.Hits[0].Semantic {
			t.Fatalf("semantic hits = %+v (err %q)", r.Hits, r.SemanticError)
		}
		// Editing an item re-queues it.
		if _, err := s.Ingest(ctx, db, Owner, "gmail:personal", IngestBatch{Items: []IngestItem{
			email("m2", "Garden club", "Seedlings sold out", t0, "garden@example.com"),
		}}); err != nil {
			t.Fatal(err)
		}
		if st, _ = s.GetStatus(ctx, db); st.PendingEmbedding != 1 {
			t.Fatalf("pending after edit = %d", st.PendingEmbedding)
		}
		// A failing embedder degrades hybrid search to keyword.
		down := New(WithEmbedder(wordEmbedder{fail: true}))
		r, err = down.Search(ctx, db, SearchQuery{Text: "denver"})
		if err != nil || r.Semantic || r.SemanticError == "" || len(r.Hits) != 1 {
			t.Fatalf("degraded search = %+v, %v", r, err)
		}
	})
}

func TestCatalogAndExtraction(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		setupSource(t, s, db, "slack:acme")
		cat := []CatalogEntry{{ID: "C1", Name: "general", Kind: "channel"}, {ID: "D1", Name: "Alice", Kind: "im"}}
		if _, err := s.Ingest(ctx, db, Agent("desk"), "slack:acme", IngestBatch{Catalog: &cat, Items: []IngestItem{
			{ExternalID: "C1/2026-09-01", Kind: "channel_day", Title: "#general", Body: "hello", OccurredAt: t0},
			{ExternalID: "C1/2026-09-02", Kind: "channel_day", Title: "#general", Body: "again", OccurredAt: t0.Add(24 * time.Hour)},
		}}); err != nil {
			t.Fatal(err)
		}
		src, err := s.PutSource(ctx, db, Owner, "slack:acme", SourceInput{Include: &[]string{"C1"}, IncludeKinds: &[]string{"im"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(src.Catalog) != 2 || !src.Selected("C1", "channel") || !src.Selected("D9", "im") || src.Selected("C2", "channel") {
			t.Fatalf("selection = %+v", src.Source)
		}
		pending, err := s.PendingExtraction(ctx, db, "", 10)
		if err != nil || len(pending) != 2 || pending[0].ExternalID != "C1/2026-09-02" {
			t.Fatalf("pending = %d %v", len(pending), err)
		}
		n, err := s.MarkExtracted(ctx, db, Agent("fable"), []ExtractedMark{{pending[0].ID, pending[0].ContentHash}, {pending[1].ID, "stale"}})
		if err != nil || n != 1 {
			t.Fatalf("marked %d, %v", n, err)
		}
		// Leases keep two extractors off the same item.
		first, err := s.ClaimExtraction(ctx, db, Agent("fable"), "", 5, time.Minute)
		if err != nil || len(first) != 1 {
			t.Fatalf("first claim = %d, %v", len(first), err)
		}
		if second, err := s.ClaimExtraction(ctx, db, Agent("opus"), "", 5, time.Minute); err != nil || len(second) != 0 {
			t.Fatalf("second claim = %d, %v; want nothing while leased", len(second), err)
		}
		later := New(WithClock(func() time.Time { return time.Now().Add(2 * time.Minute) }))
		if again, err := later.ClaimExtraction(ctx, db, Agent("opus"), "", 5, time.Minute); err != nil || len(again) != 1 {
			t.Fatalf("claim after lapse = %d, %v", len(again), err)
		}
		st, _ := s.GetStatus(ctx, db)
		if st.PendingExtraction != 1 {
			t.Fatalf("pending extraction = %d", st.PendingExtraction)
		}
		// A changed item needs mining again.
		if _, err := s.Ingest(ctx, db, Agent("desk"), "slack:acme", IngestBatch{Items: []IngestItem{
			{ExternalID: "C1/2026-09-02", Kind: "channel_day", Title: "#general", Body: "again, edited", OccurredAt: t0.Add(24 * time.Hour)},
		}}); err != nil {
			t.Fatal(err)
		}
		if st, _ = s.GetStatus(ctx, db); st.PendingExtraction != 2 {
			t.Fatalf("pending after edit = %d", st.PendingExtraction)
		}
	})
}
