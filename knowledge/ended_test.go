package gowild_knowledge

import (
	"context"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// A fact whose valid_until day is over has ended: it leaves search, lists
// and its entity's page the way a superseded fact does, still comes back by
// id and with include-inactive, and returns when its validity is extended.
// A fact valid until today still holds.
func TestFactsEndWhenTheirValidityPasses(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		day := 24 * time.Hour
		now := t0
		s := New(WithClock(func() time.Time { return now }))
		person, err := s.CreateEntity(ctx, db, Owner, EntityInput{Kind: ptr("person"), Name: ptr("Ana Ruiz")})
		if err != nil {
			t.Fatal(err)
		}
		about := &[]string{person.ID}
		past, err := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("Ana Ruiz leads the kayak club"), ValidUntil: ptr(t0.Add(-2 * day)), About: about})
		if err != nil {
			t.Fatal(err)
		}
		soon, err := s.CreateFact(ctx, db, Owner, FactInput{Text: ptr("Ana Ruiz chairs the kayak race"), ValidUntil: ptr(t0.Add(-12 * time.Hour)), About: about})
		if err != nil {
			t.Fatal(err)
		}
		if !past.Ended || soon.Ended {
			t.Fatalf("on create: past ended %v, valid until today ended %v; want true, false", past.Ended, soon.Ended)
		}

		ids := func(r *SearchResult, err error) []string {
			if err != nil {
				t.Fatal(err)
			}
			out := []string{}
			for _, h := range r.Hits {
				out = append(out, h.ID)
			}
			return out
		}
		listed := func(f FactFilter) []string {
			rows, err := s.ListFacts(ctx, db, f)
			if err != nil {
				t.Fatal(err)
			}
			out := []string{}
			for _, r := range rows {
				out = append(out, r.ID)
			}
			return out
		}
		if got := ids(s.Search(ctx, db, SearchQuery{Text: "kayak", Mode: ModeKeyword})); len(got) != 1 || got[0] != soon.ID {
			t.Fatalf("search = %v, want only %s", got, soon.ID)
		}
		if got := ids(s.Search(ctx, db, SearchQuery{Text: "kayak", Mode: ModeKeyword, IncludeInactive: true})); len(got) != 2 {
			t.Fatalf("search with inactive = %v, want both", got)
		}
		if got := listed(FactFilter{}); len(got) != 1 || got[0] != soon.ID {
			t.Fatalf("list = %v, want only %s", got, soon.ID)
		}
		if got := listed(FactFilter{IncludeInactive: true}); len(got) != 2 {
			t.Fatalf("list with inactive = %v, want both", got)
		}
		if v, err := s.GetFact(ctx, db, past.ID); err != nil || !v.Ended || v.Text != "Ana Ruiz leads the kayak club" {
			t.Fatalf("get = %+v, %v; want the ended fact", v, err)
		}
		ent, err := s.GetEntity(ctx, db, person.ID)
		if err != nil || len(ent.Facts) != 1 || ent.Facts[0].ID != soon.ID || ent.Facts[0].Ended {
			t.Fatalf("entity facts = %+v, %v; want only the live one", ent.Facts, err)
		}

		now = t0.Add(day)
		if got := ids(s.Search(ctx, db, SearchQuery{Text: "kayak", Mode: ModeKeyword})); len(got) != 1 {
			t.Fatalf("before the sweep, search = %v; want the fact whose end has not been swept yet", got)
		}
		n, err := s.EndElapsed(ctx, db, now)
		if err != nil || n != 1 {
			t.Fatalf("EndElapsed = %d, %v; want 1", n, err)
		}
		if n, err := s.EndElapsed(ctx, db, now); err != nil || n != 0 {
			t.Fatalf("second EndElapsed = %d, %v; want 0", n, err)
		}
		if got := ids(s.Search(ctx, db, SearchQuery{Text: "kayak", Mode: ModeKeyword})); len(got) != 0 {
			t.Fatalf("after the sweep, search = %v; want none", got)
		}
		if v, _ := s.GetFact(ctx, db, soon.ID); !v.Ended {
			t.Fatalf("the owner's fact past its validity: ended %v, want true", v.Ended)
		}

		back, err := s.UpdateFact(ctx, db, Agent("opus"), past.ID, FactInput{ValidUntil: ptr(t0.Add(30 * day))})
		if err != nil || back.Ended {
			t.Fatalf("extended: %+v, %v; want ended false", back, err)
		}
		if got := ids(s.Search(ctx, db, SearchQuery{Text: "kayak", Mode: ModeKeyword})); len(got) != 1 || got[0] != past.ID {
			t.Fatalf("search after extending = %v, want %s", got, past.ID)
		}
	})
}

// Facts from before the ended column existed hold NULL in it; the schema
// pass backfills, so they still list.
func TestFactsFromBeforeEndingStillList(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		fact, err := s.CreateFact(ctx, db, Agent("opus"), FactInput{Text: ptr("David owns a red kayak")})
		if err != nil {
			t.Fatal(err)
		}
		exec, _, _ := data.Raw(db)
		if _, err := exec.ExecContext(ctx, "UPDATE kb_facts SET ended = NULL"); err != nil {
			t.Fatal(err)
		}
		if err := EnsureSearchSchema(db); err != nil {
			t.Fatal(err)
		}
		rows, err := s.ListFacts(ctx, db, FactFilter{})
		if err != nil || len(rows) != 1 || rows[0].ID != fact.ID {
			t.Fatalf("list = %+v, %v; want the old fact", rows, err)
		}
	})
}
