package gowild_knowledge

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/data"
)

func chatDay(ext string, at time.Time) IngestItem {
	return IngestItem{ExternalID: ext, Kind: "slack_day", Title: ext, Body: "10:00 Ann: hi " + ext, OccurredAt: at,
		Metadata: map[string]any{"messages": []any{map[string]any{"ts": "1", "text": ext}}}}
}

func externalIDs(items []*Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ExternalID)
	}
	return out
}

func TestItemsInRangeListsOnePrefixNewestFirst(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		now := t0
		s := New(WithClock(func() time.Time { return now }))
		setupSource(t, s, db, "slack:acme")
		setupSource(t, s, db, "slack:other")
		push := func(src string, items ...IngestItem) {
			t.Helper()
			if _, err := s.Ingest(ctx, db, Agent("importer"), src, IngestBatch{Items: items}); err != nil {
				t.Fatal(err)
			}
		}
		push("slack:acme", chatDay("C1/2026-08-30", t0), chatDay("C1/2026-08-31", t0), chatDay("C10/2026-08-31", t0), chatDay("C2/2026-08-31", t0))
		push("slack:other", chatDay("C1/2026-09-01", t0))
		now = t0.Add(2 * time.Hour)
		push("slack:acme", chatDay("C1/2026-09-01", t0))

		all, err := s.ItemsInRange(ctx, db, ItemRange{SourceID: "slack:acme", Prefix: "C1/"})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := externalIDs(all), []string{"C1/2026-09-01", "C1/2026-08-31", "C1/2026-08-30"}; !slices.Equal(got, want) {
			t.Fatalf("range = %v, want %v", got, want)
		}
		recent, err := s.ItemsInRange(ctx, db, ItemRange{SourceID: "slack:acme", Prefix: "C1/", ChangedSince: t0.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if got := externalIDs(recent); !slices.Equal(got, []string{"C1/2026-09-01"}) {
			t.Fatalf("changed since = %v, want only the day written after", got)
		}
		msgs, _ := recent[0].Metadata["messages"].([]any)
		if len(msgs) != 1 {
			t.Fatalf("metadata messages = %v, want the pushed one", recent[0].Metadata)
		}
		one, err := s.ItemsInRange(ctx, db, ItemRange{SourceID: "slack:acme", Prefix: "C1/", Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got := externalIDs(one); !slices.Equal(got, []string{"C1/2026-09-01"}) {
			t.Fatalf("limit 1 = %v", got)
		}
		wild, err := s.ItemsInRange(ctx, db, ItemRange{SourceID: "slack:acme", Prefix: "C_/"})
		if err != nil {
			t.Fatal(err)
		}
		if len(wild) != 0 {
			t.Fatalf("an underscore matched as a wildcard: %v", externalIDs(wild))
		}
		if _, err := s.ItemsInRange(ctx, db, ItemRange{SourceID: "slack:acme"}); err == nil {
			t.Fatal("a range with no prefix was accepted")
		}
	})
}

func TestIngestAcceptsMetadataUpToOneMiB(t *testing.T) {
	eachBackend(t, func(t *testing.T, db data.Database) {
		ctx := context.Background()
		s := New()
		setupSource(t, s, db, "slack:acme")
		it := chatDay("C1/2026-09-01", t0)
		it.Metadata = map[string]any{"messages": strings.Repeat("x", 200<<10)}
		if _, err := s.Ingest(ctx, db, Agent("importer"), "slack:acme", IngestBatch{Items: []IngestItem{it}}); err != nil {
			t.Fatalf("200 KiB of metadata refused: %v", err)
		}
		it.Metadata = map[string]any{"messages": strings.Repeat("x", MaxItemMetadata)}
		if _, err := s.Ingest(ctx, db, Agent("importer"), "slack:acme", IngestBatch{Items: []IngestItem{it}}); err == nil {
			t.Fatal("metadata over 1 MiB was accepted")
		}
	})
}
