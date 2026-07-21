package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrokerHistorySnapshotToolsHTTP(t *testing.T) {
	db := setupManagerTestDB(t)
	toolsHandler := NewBrokerToolsHandler(db)

	secret := []byte("test-secret")
	agentID := "agent-1"
	token := testSessionTokenForAgent(t, db, secret, agentID)

	brokerMux := http.NewServeMux()
	brokerMux.HandleFunc("/broker/v1/tools/", toolsHandler.Handle)
	handler := brokerSessionAuthMiddleware(testBrokerAuth(t, db, secret), brokerMux)

	callTool := func(t *testing.T, tool string, body any) map[string]any {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode body: %v", err)
			}
		}
		req := httptest.NewRequest(http.MethodPost, "/broker/v1/tools/"+tool, &buf)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status for %s: %d body=%s", tool, rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	callTool(t, "save_history_snapshot", map[string]any{"payload": "[1,2,3]"})

	resp := callTool(t, "get_history_snapshot", nil)
	if resp["payload"] != "[1,2,3]" {
		t.Fatalf("unexpected payload: %v", resp["payload"])
	}

	callTool(t, "delete_history_snapshot", nil)

	resp = callTool(t, "get_history_snapshot", nil)
	if resp["payload"] != "" {
		t.Fatalf("expected empty payload after delete, got: %v", resp["payload"])
	}
}
