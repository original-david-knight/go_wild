package gowild_knowledge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// ListEntityStats counts what GetEntity counts, for every live entity at
// once: merges, inactive facts, items reached through an alias or an about
// link, deleted items and rows that predate a column all count the same way.
func TestEntityStatsAgreeWithGetEntity(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New(WithClock(tick()))
		agent := Agent("fable")
		entity := func(name string, aliases ...string) string {
			t.Helper()
			v, err := s.CreateEntity(ctx, db, agent, EntityInput{Name: ptr(name), Aliases: &aliases})
			if err != nil {
				t.Fatal(err)
			}
			return v.ID
		}
		ann := entity("Ann", "email:ann@example.com", "slack:t1/ann")
		bob := entity("Bob", "email:bob@example.com")
		sam := entity("Sam", "email:sam@example.com")
		entity("Cleo")
		acme, err := s.CreateEntity(ctx, db, Owner, EntityInput{Name: ptr("Acme"), Kind: ptr("org")})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := s.PutSource(ctx, db, Owner, "gmail:me", SourceInput{Kind: ptr("gmail")}); err != nil {
			t.Fatal(err)
		}
		mail := func(id string, day int, aliases ...string) IngestItem {
			it := IngestItem{ExternalID: id, Kind: "email", Title: "Mail " + id, Body: "Hello", OccurredAt: time.Date(2026, 3, day, 9, 30, 0, 0, time.UTC)}
			for _, a := range aliases {
				it.Participants = append(it.Participants, Participant{Role: "to", Alias: a})
			}
			return it
		}
		batch := IngestBatch{Items: []IngestItem{
			mail("m1", 1, "email:ann@example.com"),
			mail("m2", 2, "slack:t1/ann", "email:bob@example.com"),
			mail("m3", 3, "email:sam@example.com"),
			mail("m4", 4),
			mail("m5", 20, "email:bob@example.com"),
		}}
		if _, err := s.Ingest(ctx, db, agent, "gmail:me", batch); err != nil {
			t.Fatal(err)
		}
		// An item can be about an entity without naming it as a participant.
		exec, backend, _ := data.Raw(db)
		for _, l := range []struct{ item, entity string }{{"m4", bob}, {"m2", ann}} {
			from := ItemID("gmail:me", l.item)
			if err := db.Table(Link{}).Insert(ctx, &Link{ID: linkID(from, RelAbout, l.entity), FromID: from, Rel: RelAbout, ToID: l.entity, CreatedAt: t0}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.DeleteItem(ctx, db, Owner, ItemID("gmail:me", "m5")); err != nil {
			t.Fatal(err)
		}

		fact := func(actor Actor, text string, in FactInput) string {
			t.Helper()
			in.Text = ptr(text)
			v, err := s.CreateFact(ctx, db, actor, in)
			if err != nil {
				t.Fatal(err)
			}
			return v.ID
		}
		fact(Owner, "Ann lives in Lisbon", FactInput{About: &[]string{ann}})
		gone := fact(Owner, "Ann lives in Porto", FactInput{About: &[]string{ann}})
		if _, err := s.UpdateFact(ctx, db, Owner, gone, FactInput{Retracted: ptr(true)}); err != nil {
			t.Fatal(err)
		}
		old := fact(Owner, "Ann works at Acme", FactInput{About: &[]string{ann, acme.ID}})
		fact(Owner, "Ann runs Acme", FactInput{About: &[]string{ann, acme.ID}, Supersedes: &old})
		fact(Owner, "Ann was in Rome", FactInput{About: &[]string{ann}, ValidUntil: ptr(t0.AddDate(0, -1, 0))})
		fact(agent, "Sam plays chess", FactInput{About: &[]string{sam}})
		fact(Owner, "Bob plays go", FactInput{About: &[]string{bob}})
		if _, err := s.ExpireUnread(ctx, db, t0.AddDate(1, 0, 0), 24*time.Hour, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MergeEntities(ctx, db, agent, sam, ann); err != nil {
			t.Fatal(err)
		}
		// Rows written before a column existed read it as NULL.
		for _, stmt := range []string{
			`UPDATE kb_facts SET ended = NULL WHERE ended = ?`,
			`UPDATE kb_facts SET retracted = NULL WHERE retracted = ?`,
		} {
			if _, err := exec.ExecContext(ctx, rebind(backend, stmt), false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := exec.ExecContext(ctx, `UPDATE kb_facts SET superseded_by = NULL WHERE superseded_by = ''`); err != nil {
			t.Fatal(err)
		}

		stats, err := s.ListEntityStats(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, st := range stats {
			last := "-"
			if !st.LastItemAt.IsZero() {
				last = st.LastItemAt.UTC().Format(time.RFC3339)
			}
			got = append(got, fmt.Sprintf("%s facts=%d items=%d last=%s", st.Name, st.FactCount, st.ItemCount, last))

			v, err := s.GetEntity(ctx, db, st.ID)
			if err != nil {
				t.Fatal(err)
			}
			var newest time.Time
			if len(v.Items) > 0 {
				newest = v.Items[0].OccurredAt
			}
			if st.FactCount != len(v.Facts) || st.ItemCount != v.ItemCount || !st.LastItemAt.Equal(newest) {
				t.Errorf("%s: stats facts=%d items=%d last=%s, GetEntity facts=%d items=%d last=%s",
					st.Name, st.FactCount, st.ItemCount, st.LastItemAt, len(v.Facts), v.ItemCount, newest)
			}
		}
		want := []string{
			"Acme facts=1 items=0 last=-",
			"Ann facts=3 items=3 last=2026-03-03T09:30:00Z",
			"Bob facts=1 items=2 last=2026-03-04T09:30:00Z",
			"Cleo facts=0 items=0 last=-",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("stats =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
}
