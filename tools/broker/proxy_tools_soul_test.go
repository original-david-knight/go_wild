package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/original-david-knight/go_wild/tools"
)

// --- SoulTools ---

func TestSoulTools_ReadSoul(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"content": "I am helpful"})
	}))

	st := NewSoulTools(c)
	result, err := st.ReadSoulTool(context.Background(), tools.ReadSoulInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestSoulTools_UpdateSoul(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	st := NewSoulTools(c)
	result, err := st.UpdateSoulTool(context.Background(), tools.UpdateSoulInput{
		Content: "new soul content", Reason: "personality update",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestSoulTools_Error(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "db error"})
	}))

	st := NewSoulTools(c)
	result, err := st.ReadSoulTool(context.Background(), tools.ReadSoulInput{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

func TestSoulTools_DescribeTool(t *testing.T) {
	st := NewSoulTools(nil)
	if st.DescribeTool("read_soul") == "" {
		t.Error("expected non-empty description for read_soul")
	}
}
