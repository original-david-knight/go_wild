package data

import (
	"context"
	"testing"
	"time"
)

func TestAddMarketNoteWithMetadataStoresStructuredThesis(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	probability := 0.62
	confidence := 0.88
	metadata := &MarketNoteMetadata{
		Kind:                 "builtin_polymarket_manage_position",
		Status:               "neutral",
		Question:             "Will this note keep metadata?",
		Reasoning:            "Structured thesis metadata should round-trip through storage.",
		Invalidation:         "Reevaluate if the market reprices materially.",
		EstimatedProbability: &probability,
		Confidence:           &confidence,
		CapturedAt:           time.Now().UTC(),
	}

	created, err := AddMarketNoteWithMetadata(ctx, db, "company-1", "agent-1", "cond-1", "note body", metadata)
	if err != nil {
		t.Fatalf("AddMarketNoteWithMetadata failed: %v", err)
	}
	if created == nil {
		t.Fatal("expected created note")
	}
	if created.MetadataJSON == "" {
		t.Fatal("expected metadata_json to be stored")
	}

	notes, err := ListMarketNotes(ctx, db, "company-1", "cond-1", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}

	parsed := ParseMarketNoteMetadata(notes[0])
	if parsed == nil {
		t.Fatal("expected structured metadata to parse")
	}
	if parsed.Kind != metadata.Kind {
		t.Fatalf("expected kind %q, got %q", metadata.Kind, parsed.Kind)
	}
	if parsed.Question != metadata.Question {
		t.Fatalf("expected question %q, got %q", metadata.Question, parsed.Question)
	}
	if parsed.EstimatedProbability == nil || *parsed.EstimatedProbability != probability {
		t.Fatalf("expected estimated_probability %.2f, got %#v", probability, parsed.EstimatedProbability)
	}
	if parsed.Confidence == nil || *parsed.Confidence != confidence {
		t.Fatalf("expected confidence %.2f, got %#v", confidence, parsed.Confidence)
	}
}
