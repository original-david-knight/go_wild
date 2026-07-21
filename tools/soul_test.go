package tools

import (
	"context"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/data"
)

func newTestSoulTools(t *testing.T) *SoulTools {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("failed to register tables: %v", err)
	}
	svc := data.NewAgentService(db, "test-agent")
	return NewSoulTools(svc)
}

func TestSoulTools_ReadNonexistent(t *testing.T) {
	tools := newTestSoulTools(t)
	result, err := tools.ReadSoulTool(context.Background(), ReadSoulInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success even for nonexistent soul")
	}

	content := result.Content.(map[string]any)
	if content["exists"].(bool) {
		t.Error("expected exists=false for nonexistent soul")
	}
}

func TestSoulTools_CreateAndRead(t *testing.T) {
	tools := newTestSoulTools(t)

	// Create soul
	soulContent := `# My Soul

## Core Values
- Curiosity
- Helpfulness
- Honesty

## Goals
- Learn and grow
- Help my user effectively
`
	updateResult, err := tools.UpdateSoulTool(context.Background(), UpdateSoulInput{
		Content: soulContent,
		Reason:  "Initial soul creation",
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !updateResult.Success {
		t.Errorf("expected success, got error: %s", updateResult.Error)
	}

	content := updateResult.Content.(map[string]any)
	if !content["created"].(bool) {
		t.Error("expected created=true for new soul")
	}

	// Read it back
	readResult, err := tools.ReadSoulTool(context.Background(), ReadSoulInput{})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	readContent := readResult.Content.(map[string]any)
	if !readContent["exists"].(bool) {
		t.Error("expected exists=true after creation")
	}
	if readContent["content"].(string) != soulContent {
		t.Error("content mismatch after read")
	}
}

func TestSoulTools_Update(t *testing.T) {
	tools := newTestSoulTools(t)

	// Create initial soul
	tools.UpdateSoulTool(context.Background(), UpdateSoulInput{
		Content: "Initial soul",
	})

	// Update it
	updateResult, _ := tools.UpdateSoulTool(context.Background(), UpdateSoulInput{
		Content: "Updated soul with new insights",
		Reason:  "Learned something new",
	})

	content := updateResult.Content.(map[string]any)
	if content["created"].(bool) {
		t.Error("expected created=false for update")
	}
	if !content["updated"].(bool) {
		t.Error("expected updated=true for changed content")
	}
}

func TestSoulTools_EmptyContentRejected(t *testing.T) {
	tools := newTestSoulTools(t)

	result, _ := tools.UpdateSoulTool(context.Background(), UpdateSoulInput{
		Content: "   ",
	})

	if result.Success {
		t.Error("expected failure for empty content")
	}
}
