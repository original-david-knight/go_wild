package gowild_knowledge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// Lists read their records' links together; each record still shows only
// its own entities, cited items (newest first) and tags, and an entity
// merged away shows as the one it merged into.
func TestListedViewsKeepTheirOwnLinks(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New(WithClock(tick()))
		entity := func(name string) string {
			t.Helper()
			v, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr(name)})
			if err != nil {
				t.Fatal(err)
			}
			return v.ID
		}
		ann, bob, sam := entity("Ann"), entity("Bob"), entity("Sam")
		if _, err := s.PutSource(ctx, db, Owner, "gmail:me", SourceInput{Kind: ptr("gmail")}); err != nil {
			t.Fatal(err)
		}
		var batch IngestBatch
		for day := 1; day <= 3; day++ {
			batch.Items = append(batch.Items, IngestItem{ExternalID: fmt.Sprintf("m%d", day), Kind: "email", Title: fmt.Sprintf("Mail %d", day), Body: "Hello", OccurredAt: time.Date(2026, 3, day, 9, 0, 0, 0, time.UTC)})
		}
		if _, err := s.Ingest(ctx, db, Owner, "gmail:me", batch); err != nil {
			t.Fatal(err)
		}
		m := func(n int) string { return ItemID("gmail:me", fmt.Sprintf("m%d", n)) }
		for _, in := range []FactInput{
			{Text: ptr("Ann and Sam met"), About: &[]string{ann, sam}, Sources: &[]string{m(1), m(3)}, Tags: &[]string{"travel"}},
			{Text: ptr("Bob called"), About: &[]string{bob}, Sources: &[]string{m(2)}},
			{Text: ptr("Nobody in particular")},
		} {
			if _, err := s.CreateFact(ctx, db, Owner, in); err != nil {
				t.Fatal(err)
			}
		}
		for _, in := range []NoteInput{
			{Title: ptr("Ann's garden"), Body: ptr("Roses"), About: &[]string{ann}, Tags: &[]string{"home"}},
			{Title: ptr("Bob and Sam"), Body: ptr("Chess"), About: &[]string{bob, sam}},
		} {
			if _, err := s.CreateNote(ctx, db, Owner, in); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.MergeEntities(ctx, db, Owner, sam, ann); err != nil {
			t.Fatal(err)
		}

		names := func(refs []EntityRef) string {
			var out []string
			for _, r := range refs {
				out = append(out, r.Name)
			}
			return strings.Join(out, ",")
		}
		facts, err := s.ListFacts(ctx, db, FactFilter{})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, f := range facts {
			var sources []string
			for _, it := range f.Sources {
				sources = append(sources, it.Title)
			}
			asOf := "-"
			if !f.AsOf.IsZero() {
				asOf = f.AsOf.UTC().Format(time.DateOnly)
			}
			got = append(got, fmt.Sprintf("fact %s about=%s sources=%s tags=%s as_of=%s", f.Text, names(f.About), strings.Join(sources, ","), strings.Join(f.Tags, ","), asOf))
		}
		notes, err := s.ListNotes(ctx, db, NoteFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range notes {
			got = append(got, fmt.Sprintf("note %s about=%s tags=%s", n.Title, names(n.About), strings.Join(n.Tags, ",")))
		}
		want := []string{
			"fact Nobody in particular about= sources= tags= as_of=-",
			"fact Bob called about=Bob sources=Mail 2 tags= as_of=2026-03-02",
			"fact Ann and Sam met about=Ann sources=Mail 3,Mail 1 tags=travel as_of=2026-03-01",
			"note Bob and Sam about=Bob,Ann tags=",
			"note Ann's garden about=Ann tags=home",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("views =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		for _, f := range facts {
			if f.About == nil || f.Sources == nil || f.Tags == nil {
				t.Fatalf("fact %s has a nil list: %+v", f.ID, f)
			}
		}
	})
}
