package main

import (
	"net/http"
	"testing"
)

func TestBrokerHistorySnapshotClientRoundTrip(t *testing.T) {
	db := setupManagerTestDB(t)
	toolsHandler := NewBrokerToolsHandler(db)

	secret := []byte("test-secret")
	agentID := "agent-1"
	token := testSessionTokenForAgent(t, db, secret, agentID)

	brokerMux := http.NewServeMux()
	brokerMux.HandleFunc("/broker/v1/tools/", toolsHandler.Handle)
	handler := brokerSessionAuthMiddleware(testBrokerAuth(t, db, secret), brokerMux)

	client := newTestBrokerClient(t, handler, token)

	if _, err := client.CallTool(t.Context(), "save_history_snapshot", map[string]any{"payload": "[1]"}); err != nil {
		t.Fatalf("save_history_snapshot failed: %v", err)
	}

	result, err := client.CallTool(t.Context(), "get_history_snapshot", map[string]any{})
	if err != nil {
		t.Fatalf("get_history_snapshot failed: %v", err)
	}
	if result["payload"] != "[1]" {
		t.Fatalf("unexpected payload: %v", result["payload"])
	}

	if _, err := client.CallTool(t.Context(), "delete_history_snapshot", map[string]any{}); err != nil {
		t.Fatalf("delete_history_snapshot failed: %v", err)
	}

	result, err = client.CallTool(t.Context(), "get_history_snapshot", map[string]any{})
	if err != nil {
		t.Fatalf("get_history_snapshot after delete failed: %v", err)
	}
	if result["payload"] != "" {
		t.Fatalf("expected empty payload after delete, got: %v", result["payload"])
	}
}
