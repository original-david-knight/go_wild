package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestHistorySnapshotRoundTrip(t *testing.T) {
	prev := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, newHistorySnapshotTestHandler())
	defer func() { globalBrokerClient = prev }()

	history := []loop.Message{
		loop.NewUserMessage("hello"),
		loop.NewModelTextMessage("hi there"),
		loop.NewToolResultMessage("demo_tool", map[string]any{"ok": true}),
	}

	if err := saveHistorySnapshot(context.Background(), history); err != nil {
		t.Fatalf("saveHistorySnapshot failed: %v", err)
	}

	loaded, err := loadHistorySnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadHistorySnapshot failed: %v", err)
	}

	origBytes, err := loop.SerializeHistory(history)
	if err != nil {
		t.Fatalf("SerializeHistory failed: %v", err)
	}
	loadedBytes, err := loop.SerializeHistory(loaded)
	if err != nil {
		t.Fatalf("SerializeHistory(loaded) failed: %v", err)
	}

	if string(origBytes) != string(loadedBytes) {
		t.Fatalf("rehydrated history mismatch\noriginal: %s\nloaded:   %s", origBytes, loadedBytes)
	}
}

func TestHistorySnapshotDelete(t *testing.T) {
	prev := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, newHistorySnapshotTestHandler())
	defer func() { globalBrokerClient = prev }()

	history := []loop.Message{loop.NewUserMessage("keep")}
	if err := saveHistorySnapshot(context.Background(), history); err != nil {
		t.Fatalf("saveHistorySnapshot failed: %v", err)
	}
	if err := saveHistorySnapshot(context.Background(), nil); err != nil {
		t.Fatalf("delete history snapshot failed: %v", err)
	}

	loaded, err := loadHistorySnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadHistorySnapshot failed: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected no history after delete, got %d messages", len(loaded))
	}
}

func TestHistorySnapshotSummarySeparation(t *testing.T) {
	prev := globalBrokerClient
	handler := newHistorySnapshotTestHandler()
	globalBrokerClient = newTestBrokerClient(t, handler)
	defer func() { globalBrokerClient = prev }()

	// Simulate compacted history: summary + remaining messages
	summaryMsg := loop.NewUserMessage("<summary>\nThis is a summary of past conversations.\n</summary>")
	remaining := []loop.Message{
		loop.NewUserMessage("recent question"),
		loop.NewModelTextMessage("recent answer"),
	}
	history := append([]loop.Message{summaryMsg}, remaining...)

	if err := saveHistorySnapshot(context.Background(), history); err != nil {
		t.Fatalf("saveHistorySnapshot failed: %v", err)
	}

	// Load and verify the summary is reconstructed
	loaded, err := loadHistorySnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadHistorySnapshot failed: %v", err)
	}

	if len(loaded) != 3 {
		t.Fatalf("expected 3 messages (summary + 2 remaining), got %d", len(loaded))
	}

	// First message should be the summary
	firstText := loop.ExtractText(loaded[0].Content)
	if !strings.Contains(firstText, "<summary>") {
		t.Fatalf("expected first message to be summary, got: %s", firstText)
	}
	if !strings.Contains(firstText, "This is a summary of past conversations.") {
		t.Fatalf("summary content mismatch: %s", firstText)
	}

	// Remaining messages should round-trip correctly
	origRemaining, _ := loop.SerializeHistory(remaining)
	loadedRemaining, _ := loop.SerializeHistory(loaded[1:])
	if string(origRemaining) != string(loadedRemaining) {
		t.Fatalf("remaining messages mismatch\noriginal: %s\nloaded:   %s", origRemaining, loadedRemaining)
	}
}

func TestHistorySnapshotSummaryOnlyDelete(t *testing.T) {
	prev := globalBrokerClient
	handler := newHistorySnapshotTestHandler()
	globalBrokerClient = newTestBrokerClient(t, handler)
	defer func() { globalBrokerClient = prev }()

	// Save history with summary
	history := []loop.Message{
		loop.NewUserMessage("<summary>\nold summary\n</summary>"),
		loop.NewUserMessage("recent message"),
	}
	if err := saveHistorySnapshot(context.Background(), history); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Delete all history
	if err := saveHistorySnapshot(context.Background(), nil); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	loaded, err := loadHistorySnapshot(context.Background())
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected empty history after delete, got %d messages", len(loaded))
	}
}

func TestDeleteHistorySummary(t *testing.T) {
	prev := globalBrokerClient
	handler := newHistorySnapshotTestHandler()
	globalBrokerClient = newTestBrokerClient(t, handler)
	defer func() { globalBrokerClient = prev }()

	// Save history with summary to populate the summary store
	history := []loop.Message{
		loop.NewUserMessage("<summary>\ntest summary\n</summary>"),
		loop.NewUserMessage("message"),
	}
	if err := saveHistorySnapshot(context.Background(), history); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Delete summary explicitly
	if err := deleteHistorySummary(context.Background()); err != nil {
		t.Fatalf("delete summary failed: %v", err)
	}

	// Load should return only the snapshot messages (no summary prepended)
	loaded, err := loadHistorySnapshot(context.Background())
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 message after summary delete, got %d", len(loaded))
	}
	text := loop.ExtractText(loaded[0].Content)
	if strings.Contains(text, "<summary>") {
		t.Fatalf("expected no summary after delete, got: %s", text)
	}
}

func TestSaveHistorySnapshotDeletePropagatesSummaryDeleteError(t *testing.T) {
	prev := globalBrokerClient
	mux := http.NewServeMux()
	mux.HandleFunc("/broker/v1/tools/", func(w http.ResponseWriter, r *http.Request) {
		toolName := strings.TrimPrefix(r.URL.Path, "/broker/v1/tools/")
		defer r.Body.Close()

		switch toolName {
		case "delete_history_summary":
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "summary delete failed"})
		case "delete_history_snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	})
	globalBrokerClient = newTestBrokerClient(t, mux)
	defer func() { globalBrokerClient = prev }()

	err := saveHistorySnapshot(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error when delete_history_summary fails")
	}
	if !strings.Contains(err.Error(), "summary delete failed") {
		t.Fatalf("expected summary delete error, got: %v", err)
	}
}

func newHistorySnapshotTestHandler() http.Handler {
	var payload string
	var summaryContent string

	mux := http.NewServeMux()
	mux.HandleFunc("/broker/v1/tools/", func(w http.ResponseWriter, r *http.Request) {
		toolName := strings.TrimPrefix(r.URL.Path, "/broker/v1/tools/")
		defer r.Body.Close()

		switch toolName {
		case "save_history_snapshot":
			var input struct {
				Payload string `json:"payload"`
			}
			_ = json.NewDecoder(r.Body).Decode(&input)
			payload = input.Payload
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "get_history_snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": payload})
		case "delete_history_snapshot":
			payload = ""
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "save_history_summary":
			var input struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&input)
			summaryContent = input.Content
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "get_history_summary":
			_ = json.NewEncoder(w).Encode(map[string]any{"content": summaryContent})
		case "delete_history_summary":
			summaryContent = ""
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unknown tool"})
		}
	})

	return mux
}
