package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestPostObjectResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tasks": []string{"a", "b"}})
	}))

	result, err := c.Post(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tasks, ok := result["tasks"].([]any)
	if !ok {
		t.Fatalf("expected tasks array, got %T", result["tasks"])
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestPostArrayResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"node": "a", "score": 1.0},
			{"node": "b", "score": 0.5},
		})
	}))

	result, err := c.Post(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items, ok := result["result"].([]any)
	if !ok {
		t.Fatalf("expected result array, got %T", result["result"])
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestPostStringResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode("deleted node xyz")
	}))

	result, err := c.Post(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	val, ok := result["result"].(string)
	if !ok {
		t.Fatalf("expected result string, got %T", result["result"])
	}
	if val != "deleted node xyz" {
		t.Fatalf("expected 'deleted node xyz', got %q", val)
	}
}

func TestPostErrorResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "unknown tool: bad_tool"})
	}))

	_, err := c.Post(context.Background(), "/test", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "broker error (500): unknown tool: bad_tool" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestPostErrorArrayResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`["not","a","map"]`))
	}))

	_, err := c.Post(context.Background(), "/test", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCallToolSendsToken(t *testing.T) {
	var gotToken string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		if gotToken != "" {
			gotToken = gotToken[len("Bearer "):]
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	c.CallTool(context.Background(), "kg_search", map[string]any{"query": "test"})
	if gotToken != "test-token" {
		t.Fatalf("expected 'test-token', got %q", gotToken)
	}
}

func TestCallToolPath(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	c.CallTool(context.Background(), "kg_search", nil)
	if gotPath != "/broker/v1/tools/kg_search" {
		t.Fatalf("expected /broker/v1/tools/kg_search, got %s", gotPath)
	}
}
