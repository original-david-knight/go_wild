package main

import (
	"context"
	"strings"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestCompactionSnapshotPersistsMaskedHistory(t *testing.T) {
	prev := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, newHistorySnapshotTestHandler())
	defer func() { globalBrokerClient = prev }()

	history := []loop.Message{
		loop.NewUserMessage("do the thing"),
		loop.NewModelTextMessage("calling tool"),
		loop.NewToolResultMessage("demo_tool", map[string]any{
			"data": strings.Repeat("x", 200),
		}),
		loop.NewModelTextMessage("done"),
	}

	compacted := maskObservations(history, 0).MaskedHistory
	if err := saveHistorySnapshot(context.Background(), compacted); err != nil {
		t.Fatalf("saveHistorySnapshot failed: %v", err)
	}

	rehydrated, err := loadHistorySnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadHistorySnapshot failed: %v", err)
	}

	origBytes, err := loop.SerializeHistory(compacted)
	if err != nil {
		t.Fatalf("SerializeHistory failed: %v", err)
	}
	rehydratedBytes, err := loop.SerializeHistory(rehydrated)
	if err != nil {
		t.Fatalf("SerializeHistory(rehydrated) failed: %v", err)
	}
	if string(origBytes) != string(rehydratedBytes) {
		t.Fatalf("rehydrated history mismatch\noriginal: %s\nrehydrated: %s", origBytes, rehydratedBytes)
	}

	// Validate tool output is masked after compaction.
	if len(rehydrated) < 3 || rehydrated[2].Role != loop.RoleTool || rehydrated[2].Content == nil {
		t.Fatalf("expected tool message at index 2 after rehydration")
	}
	foundMasked := false
	for _, part := range rehydrated[2].Content.Parts {
		if part.FunctionResponse != nil {
			if _, ok := part.FunctionResponse.Response["_masked"]; ok {
				foundMasked = true
			}
		}
	}
	if !foundMasked {
		t.Fatalf("expected masked tool response in rehydrated history")
	}
}
