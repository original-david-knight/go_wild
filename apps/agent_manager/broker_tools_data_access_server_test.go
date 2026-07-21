package main

import (
	"testing"
)

func TestBrokerHistorySnapshotClientViaServer(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)

	secret := []byte("test-secret")
	brokerHandlers := &BrokerHandlers{
		auth:     NewBrokerAuthHandler(service, secret),
		wallet:   NewBrokerWalletHandler(service),
		email:    NewBrokerEmailHandler(service),
		search:   NewBrokerSearchHandler(),
		telegram: NewBrokerTelegramHandler(service),
		tools:    NewBrokerToolsHandler(db),
		secret:   secret,
	}

	handler := NewHandlers(service, nil, nil, nil, nil)
	server := NewServer("127.0.0.1:0", handler, brokerHandlers, nil)

	agentID := "agent-1"
	token := testSessionTokenForAgent(t, db, secret, agentID)
	client := newTestBrokerClient(t, server.buildHandler(), token)

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
