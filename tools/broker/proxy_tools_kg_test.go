package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	kg "github.com/original-david-knight/go_wild/knowledge_graph"
)

// --- KGTools ---

func TestKGTools_Search(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgSearchTool(context.Background(), kg.KgSearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/tools/kg_search" {
		t.Errorf("expected tools/kg_search path, got %s", gotPath)
	}
}

func TestKGTools_Add(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "node-1"})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgAddTool(context.Background(), kg.KgAddInput{
		Name: "test-node", Type: "concept",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestKGTools_Get(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "node-1", "name": "test"})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgGetTool(context.Background(), kg.KgGetInput{ID: "node-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestKGTools_Update(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgUpdateTool(context.Background(), kg.KgUpdateInput{
		ID: "node-1", Name: "updated",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestKGTools_Delete(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgDeleteTool(context.Background(), kg.KgDeleteInput{ID: "node-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestKGTools_Explore(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"nodes": []any{}, "edges": []any{}})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgExploreTool(context.Background(), kg.KgExploreInput{
		StartNodeID: "node-1", MaxDepth: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestKGTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
	}))

	kt := NewKGTools(c)
	result, err := kt.KgGetTool(context.Background(), kg.KgGetInput{ID: "missing"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestKGTools_DescribeTool(t *testing.T) {
	kt := NewKGTools(nil)
	for _, name := range []string{"kg_search", "kg_add", "kg_get", "kg_update", "kg_delete", "kg_explore"} {
		if kt.DescribeTool(name) == "" {
			t.Errorf("expected non-empty description for %s", name)
		}
	}
}
