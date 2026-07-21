package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

// --- SearchTools ---

func TestSearchTools_WebSearch(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"results": []any{
			map[string]any{"title": "Result 1", "url": "https://example.com"},
		}})
	}))

	st := NewSearchTools(c)
	result, err := st.WebSearchTool(context.Background(), tools.WebSearchInput{
		Query: "golang testing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if gotPath != "/broker/v1/search/web" {
		t.Errorf("expected search/web path, got %s", gotPath)
	}
	if gotBody["query"] != "golang testing" {
		t.Errorf("expected query 'golang testing', got %v", gotBody["query"])
	}
}

func TestSearchTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "search API unavailable"})
	}))

	st := NewSearchTools(c)
	result, err := st.WebSearchTool(context.Background(), tools.WebSearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestSearchTools_DescribeTool(t *testing.T) {
	st := NewSearchTools(nil)
	if st.DescribeTool("web_search") == "" {
		t.Error("expected non-empty description")
	}
}
