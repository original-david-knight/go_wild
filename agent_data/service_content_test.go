package data

import (
	"context"
	"testing"
)

func TestMemoryOperations(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// No memory initially
	mem, err := svc.GetMemory(ctx)
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}
	if mem != nil {
		t.Error("expected nil memory initially")
	}

	// Save memory
	if err := svc.SaveMemory(ctx, "first memory"); err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	mem, err = svc.GetMemory(ctx)
	if err != nil {
		t.Fatalf("GetMemory after save failed: %v", err)
	}
	if mem.Content != "first memory" {
		t.Errorf("expected 'first memory', got %q", mem.Content)
	}

	// Update existing memory
	if err := svc.SaveMemory(ctx, "updated memory"); err != nil {
		t.Fatalf("SaveMemory update failed: %v", err)
	}

	mem, _ = svc.GetMemory(ctx)
	if mem.Content != "updated memory" {
		t.Errorf("expected 'updated memory', got %q", mem.Content)
	}
}

func TestArchiveOperations(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// Add entries
	svc.AddArchiveEntry(ctx, "summary1", "tag1", "content1")
	svc.AddArchiveEntry(ctx, "summary2", "tag2", "content2")
	svc.AddArchiveEntry(ctx, "summary3", "tag3", "content3")

	// Get entries
	entries, err := svc.GetArchiveEntries(ctx, 2)
	if err != nil {
		t.Fatalf("GetArchiveEntries failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// Search archive
	matched, err := svc.SearchArchive(ctx, "summary2", 10)
	if err != nil {
		t.Fatalf("SearchArchive failed: %v", err)
	}
	if len(matched) != 1 {
		t.Errorf("expected 1 match, got %d", len(matched))
	}

	// Search by tag
	matched, err = svc.SearchArchive(ctx, "tag3", 10)
	if err != nil {
		t.Fatalf("SearchArchive by tag failed: %v", err)
	}
	if len(matched) != 1 {
		t.Errorf("expected 1 match for tag search, got %d", len(matched))
	}

	// Search by content
	matched, err = svc.SearchArchive(ctx, "content1", 10)
	if err != nil {
		t.Fatalf("SearchArchive by content failed: %v", err)
	}
	if len(matched) != 1 {
		t.Errorf("expected 1 match for content search, got %d", len(matched))
	}
}

func TestSoulOperations(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// No soul initially
	soul, err := svc.GetSoul(ctx)
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}
	if soul != nil {
		t.Error("expected nil soul initially")
	}

	// Save soul
	if err := svc.SaveSoul(ctx, "I am a test agent"); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	soul, _ = svc.GetSoul(ctx)
	if soul.Content != "I am a test agent" {
		t.Errorf("expected soul content, got %q", soul.Content)
	}

	// Update soul
	if err := svc.SaveSoul(ctx, "I am an updated agent"); err != nil {
		t.Fatalf("SaveSoul update failed: %v", err)
	}

	soul, _ = svc.GetSoul(ctx)
	if soul.Content != "I am an updated agent" {
		t.Errorf("expected updated soul content, got %q", soul.Content)
	}
}

func TestHistorySnapshot(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// No snapshot initially
	snap, err := svc.GetHistorySnapshot(ctx)
	if err != nil {
		t.Fatalf("GetHistorySnapshot failed: %v", err)
	}
	if snap != nil {
		t.Error("expected nil history snapshot initially")
	}

	// Save snapshot
	if err := svc.SaveHistorySnapshot(ctx, "[]"); err != nil {
		t.Fatalf("SaveHistorySnapshot failed: %v", err)
	}

	snap, _ = svc.GetHistorySnapshot(ctx)
	if snap == nil || snap.Payload != "[]" {
		t.Errorf("unexpected snapshot payload: %v", snap)
	}

	// Update snapshot
	if err := svc.SaveHistorySnapshot(ctx, "[1,2,3]"); err != nil {
		t.Fatalf("SaveHistorySnapshot update failed: %v", err)
	}

	snap, _ = svc.GetHistorySnapshot(ctx)
	if snap.Payload != "[1,2,3]" {
		t.Errorf("expected updated snapshot payload, got %q", snap.Payload)
	}
}

func TestHistorySummary(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "test-agent")
	ctx := context.Background()

	// No summary initially
	summary, err := svc.GetHistorySummary(ctx)
	if err != nil {
		t.Fatalf("GetHistorySummary failed: %v", err)
	}
	if summary != nil {
		t.Error("expected nil history summary initially")
	}

	// Save summary
	if err := svc.SaveHistorySummary(ctx, "first summary"); err != nil {
		t.Fatalf("SaveHistorySummary failed: %v", err)
	}

	summary, _ = svc.GetHistorySummary(ctx)
	if summary == nil || summary.Content != "first summary" {
		t.Errorf("unexpected summary content: %v", summary)
	}

	// Update summary (upsert)
	if err := svc.SaveHistorySummary(ctx, "updated summary"); err != nil {
		t.Fatalf("SaveHistorySummary update failed: %v", err)
	}

	summary, _ = svc.GetHistorySummary(ctx)
	if summary.Content != "updated summary" {
		t.Errorf("expected updated summary content, got %q", summary.Content)
	}

	// Delete summary
	if err := svc.DeleteHistorySummary(ctx); err != nil {
		t.Fatalf("DeleteHistorySummary failed: %v", err)
	}

	summary, _ = svc.GetHistorySummary(ctx)
	if summary != nil {
		t.Errorf("expected nil after delete, got %v", summary)
	}

	// Agent isolation
	svc2 := NewAgentService(db, "other-agent")
	if err := svc.SaveHistorySummary(ctx, "agent1 summary"); err != nil {
		t.Fatalf("SaveHistorySummary agent1 failed: %v", err)
	}

	summary, _ = svc2.GetHistorySummary(ctx)
	if summary != nil {
		t.Error("expected nil summary for different agent")
	}
}
