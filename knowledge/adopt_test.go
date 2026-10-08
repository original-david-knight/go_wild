package gowild_knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

// An agent fact the owner dictated through a client becomes his: it keeps
// the client as its author, takes confidence 1, comes back from expiry and
// outranks an agent's fact. Adopting it again changes nothing.
func TestOwnerAdoptsAnAgentFact(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		now := t0
		s := New(WithClock(func() time.Time { return now }))
		dictated, err := s.CreateFact(ctx, db, Agent("mcp:claude"), FactInput{Text: ptr("David's dentist is Dr. Okafor")})
		if err != nil {
			t.Fatal(err)
		}
		mined, err := s.CreateFact(ctx, db, Agent("fable"), FactInput{Text: ptr("David's dentist is Dr. Lee")})
		if err != nil {
			t.Fatal(err)
		}
		now = t0.Add(100 * 24 * time.Hour)
		if n, err := s.ExpireUnread(ctx, db, now, 90*24*time.Hour, time.Time{}); err != nil || n != 2 {
			t.Fatalf("expired %d, %v; want 2", n, err)
		}

		if _, err := s.AdoptFact(ctx, db, Agent("fable"), dictated.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("an agent adopting: %v, want ErrForbidden", err)
		}
		now = t0.Add(101 * 24 * time.Hour)
		got, err := s.AdoptFact(ctx, db, Owner, dictated.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.AuthorKind != "owner" || got.Author != "mcp:claude" || got.Confidence != 1 || got.Expired || !got.UpdatedAt.Equal(now) {
			t.Fatalf("adopted = %s/%s %.2f expired=%v updated %v; want owner/mcp:claude 1.00 expired=false updated %v",
				got.AuthorKind, got.Author, got.Confidence, got.Expired, got.UpdatedAt, now)
		}

		now = t0.Add(102 * 24 * time.Hour)
		again, err := s.AdoptFact(ctx, db, Owner, dictated.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again.AuthorKind != "owner" || again.Author != "mcp:claude" || !again.UpdatedAt.Equal(t0.Add(101*24*time.Hour)) {
			t.Fatalf("second adopt = %s/%s updated %v; want it unchanged", again.AuthorKind, again.Author, again.UpdatedAt)
		}

		if _, err := s.UpdateFact(ctx, db, Agent("fable"), dictated.ID, FactInput{Text: ptr("David's dentist is Dr. Lee")}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("an agent editing the adopted fact: %v, want ErrForbidden", err)
		}
		if _, err := s.AdoptFact(ctx, db, Owner, "fct_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("adopting a missing fact: %v, want ErrNotFound", err)
		}

		s.NoteRead(ctx, db, Agent("sol"), mined.ID)
		r, err := s.Search(ctx, db, SearchQuery{Text: "dentist", Mode: ModeKeyword})
		if err != nil || len(r.Hits) != 2 || r.Hits[0].ID != dictated.ID || r.Hits[1].ID != mined.ID {
			t.Fatalf("search = %+v, %v; want the adopted fact, then the agent's", r, err)
		}
	})
}
