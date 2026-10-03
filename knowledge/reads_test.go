package gowild_knowledge

import (
	"context"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

func TestSearchesAndFetchesCountReads(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		now := t0
		s := New(WithClock(func() time.Time { return now }))
		fact, err := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("David owns a red kayak"), Tags: &[]string{"hobbies"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateNote(ctx, db, Owner, NoteInput{Title: ptr("Paddling"), Body: ptr("Best kayak launch is at the north beach.")}); err != nil {
			t.Fatal(err)
		}
		score := func() float64 {
			r, err := s.Search(ctx, db, SearchQuery{Text: "red kayak", Kinds: []string{KindFact}})
			if err != nil || len(r.Hits) != 1 {
				t.Fatalf("fact search = %+v, %v", r, err)
			}
			return r.Hits[0].Score
		}
		before := score()

		now = t0.Add(time.Hour)
		r, err := s.Search(ctx, db, SearchQuery{Text: "kayak", Contexts: []string{"personal"}, Reader: Agent("sol")})
		if err != nil || len(r.Hits) != 2 {
			t.Fatalf("search = %+v, %v", r, err)
		}
		// Neither a browse, a list, nor a search without a reader is a read.
		if _, err := s.Search(ctx, db, SearchQuery{Tag: "hobbies", Reader: Agent("sol")}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListFacts(ctx, db, FactFilter{}); err != nil {
			t.Fatal(err)
		}
		now = t0.Add(2 * time.Hour)
		s.NoteRead(ctx, db, Owner, fact.ID, "not_elsewhere", "fct_missing")
		s.NoteRead(ctx, db, Actor{}, fact.ID)

		got, err := s.GetFact(ctx, db, fact.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ReadCount != 2 || !got.LastReadAt.Equal(t0.Add(2*time.Hour)) || !got.UpdatedAt.Equal(t0) {
			t.Fatalf("reads = %d, last read %v, updated %v; want 2, %v, %v", got.ReadCount, got.LastReadAt, got.UpdatedAt, t0.Add(2*time.Hour), t0)
		}
		if after := score(); after != before {
			t.Fatalf("score after reads = %v, want %v", after, before)
		}

		queries, err := s.ListQueries(ctx, db, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(queries) != 1 {
			t.Fatalf("queries = %+v, want one", queries)
		}
		q := queries[0]
		q.ID = ""
		want := Query{At: t0.Add(time.Hour), ReaderKind: "agent", Reader: "sol", Text: "kayak", Context: "personal", Hits: 2, FactHits: 1}
		if !q.At.Equal(want.At) || q.ReaderKind != want.ReaderKind || q.Reader != want.Reader || q.Text != want.Text ||
			q.Context != want.Context || q.Hits != want.Hits || q.FactHits != want.FactHits {
			t.Fatalf("query = %+v, want %+v", q, want)
		}
	})
}

func TestAFailedRecordingNeverFailsTheSearch(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		fact, err := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("David owns a red kayak")})
		if err != nil {
			t.Fatal(err)
		}
		exec, _, _ := data.Raw(db)
		if _, err := exec.ExecContext(ctx, "DROP TABLE kb_queries"); err != nil {
			t.Fatal(err)
		}
		r, err := s.Search(ctx, db, SearchQuery{Text: "kayak", Reader: Agent("sol")})
		if err != nil || len(r.Hits) != 1 || r.Hits[0].ID != fact.ID {
			t.Fatalf("search = %+v, %v; want the fact", r, err)
		}
		got, _ := s.GetFact(ctx, db, fact.ID)
		if got.ReadCount != 0 {
			t.Fatalf("read count = %d after a failed recording, want 0", got.ReadCount)
		}
	})
}

// Facts from before the read columns existed hold NULL in them; the schema
// pass backfills, so their reads count from zero.
func TestFactsFromBeforeReadsCountFromZero(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		fact, err := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("David owns a red kayak")})
		if err != nil {
			t.Fatal(err)
		}
		exec, _, _ := data.Raw(db)
		if _, err := exec.ExecContext(ctx, "UPDATE kb_facts SET read_count = NULL, expired = NULL"); err != nil {
			t.Fatal(err)
		}
		if err := EnsureSearchSchema(db); err != nil {
			t.Fatal(err)
		}
		s.NoteRead(ctx, db, Agent("sol"), fact.ID)
		got, _ := s.GetFact(ctx, db, fact.ID)
		if got.ReadCount != 1 || got.Expired {
			t.Fatalf("read count %d, expired %v; want 1, false", got.ReadCount, got.Expired)
		}
	})
}

func TestUnreadAgentFactsExpireUntilSomeoneFetchesThem(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		now := t0
		s := New(WithClock(func() time.Time { return now }))
		day := 24 * time.Hour
		add := func(actor Actor, text string) string {
			f, err := s.CreateFact(ctx, db, actor, FactInput{Text: ptr(text)})
			if err != nil {
				t.Fatal(err)
			}
			return f.ID
		}
		kayak := add(Agent("opus"), "David owns a red kayak")
		canoe := add(Agent("opus"), "David rents a blue canoe")
		add(Owner, "David keeps a green paddle")
		now = t0.Add(50 * day)
		add(Agent("opus"), "David stores a yellow raft")
		now = t0.Add(60 * day)
		s.NoteRead(ctx, db, Agent("sol"), canoe)

		now = t0.Add(100 * day)
		n, err := s.ExpireUnread(ctx, db, now, 90*day)
		if err != nil || n != 1 {
			t.Fatalf("expired %d, %v; want 1 (the never-read kayak)", n, err)
		}
		if n, err := s.ExpireUnread(ctx, db, now, 90*day); err != nil || n != 0 {
			t.Fatalf("second pass expired %d, %v; want 0", n, err)
		}

		r, err := s.Search(ctx, db, SearchQuery{Text: "kayak"})
		if err != nil || len(r.Hits) != 0 {
			t.Fatalf("search for the expired fact = %+v, %v; want no hits", r, err)
		}
		expired, err := s.ListFacts(ctx, db, FactFilter{Expired: ptr(true)})
		if err != nil || len(expired) != 1 || expired[0].ID != kayak {
			t.Fatalf("expired facts = %+v, %v; want the kayak", expired, err)
		}
		all, _ := s.ListFacts(ctx, db, FactFilter{})
		live, _ := s.ListFacts(ctx, db, FactFilter{Expired: ptr(false)})
		if len(all) != 4 || len(live) != 3 {
			t.Fatalf("listed %d facts, %d live; want 4 and 3", len(all), len(live))
		}

		s.NoteRead(ctx, db, Agent("sol"), kayak)
		back, _ := s.GetFact(ctx, db, kayak)
		if back.Expired || back.ReadCount != 1 {
			t.Fatalf("after a fetch: expired %v, reads %d; want false, 1", back.Expired, back.ReadCount)
		}
		r, _ = s.Search(ctx, db, SearchQuery{Text: "kayak"})
		if len(r.Hits) != 1 || r.Hits[0].ID != kayak {
			t.Fatalf("search after the fetch = %+v, want the kayak", r.Hits)
		}
	})
}
