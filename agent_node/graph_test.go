package agentnode

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestValidate_DuplicateIDs(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "do A"},
			{ID: "a", Prompt: "do A again"},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
}

func TestValidate_MissingDep(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", DependsOn: []NodeID{"missing"}, Prompt: "do A"},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestValidate_Cycle(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", DependsOn: []NodeID{"b"}, Prompt: "do A"},
			{ID: "b", DependsOn: []NodeID{"c"}, Prompt: "do B"},
			{ID: "c", DependsOn: []NodeID{"a"}, Prompt: "do C"},
		},
	}
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestValidate_Valid(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "do A"},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "do B"},
			{ID: "c", DependsOn: []NodeID{"a"}, Prompt: "do C"},
			{ID: "d", DependsOn: []NodeID{"b", "c"}, Prompt: "do D"},
		},
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTopologicalSort_Linear(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "c", DependsOn: []NodeID{"b"}, Prompt: "do C"},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "do B"},
			{ID: "a", Prompt: "do A"},
		},
	}
	sorted, err := g.topologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(sorted))
	}

	// a must come before b, b before c
	pos := make(map[NodeID]int)
	for i, n := range sorted {
		pos[n.ID] = i
	}
	if pos["a"] >= pos["b"] || pos["b"] >= pos["c"] {
		t.Fatalf("wrong order: %v", sorted)
	}
}

func TestTopologicalSort_Diamond(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "root"},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "left"},
			{ID: "c", DependsOn: []NodeID{"a"}, Prompt: "right"},
			{ID: "d", DependsOn: []NodeID{"b", "c"}, Prompt: "join"},
		},
	}
	sorted, err := g.topologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pos := make(map[NodeID]int)
	for i, n := range sorted {
		pos[n.ID] = i
	}

	// a before b and c; b and c before d
	if pos["a"] >= pos["b"] || pos["a"] >= pos["c"] {
		t.Fatal("a must come before b and c")
	}
	if pos["b"] >= pos["d"] || pos["c"] >= pos["d"] {
		t.Fatal("b and c must come before d")
	}
}

func TestTopologicalSort_Cycle(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", DependsOn: []NodeID{"b"}, Prompt: "A"},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "B"},
		},
	}
	_, err := g.topologicalSort()
	if err == nil {
		t.Fatal("expected error for cycle")
	}
}

func TestLeafNodeIDs(t *testing.T) {
	g := &NodeGraph{
		Nodes: []NodeDef{
			{ID: "a", Prompt: "root"},
			{ID: "b", DependsOn: []NodeID{"a"}, Prompt: "mid"},
			{ID: "c", DependsOn: []NodeID{"a"}, Prompt: "leaf-c"},
			{ID: "d", DependsOn: []NodeID{"b"}, Prompt: "leaf-d"},
		},
	}
	leaves := g.leafNodeIDs()
	got := map[NodeID]bool{}
	for _, id := range leaves {
		got[id] = true
	}
	if len(got) != 2 || !got["c"] || !got["d"] {
		t.Fatalf("expected leaves {c, d}, got %v", leaves)
	}
}

func TestLeafNodeIDs_Empty(t *testing.T) {
	g := &NodeGraph{}
	if leaves := g.leafNodeIDs(); len(leaves) != 0 {
		t.Fatalf("expected no leaves, got %v", leaves)
	}
}

func TestSharedState_SnapshotOnly(t *testing.T) {
	state := NewSharedState()
	state.set("json-done", &NodeResult{NodeID: "json-done", Status: NodeDone, Output: json.RawMessage(`{"k":"a"}`)})
	state.set("text-done", &NodeResult{NodeID: "text-done", Status: NodeDone, Text: "hello"})
	state.set("excluded", &NodeResult{NodeID: "excluded", Status: NodeDone, Output: json.RawMessage(`{"k":"x"}`)})
	state.set("failed", &NodeResult{NodeID: "failed", Status: NodeFailed, Error: "boom"})
	state.set("skipped", &NodeResult{NodeID: "skipped", Status: NodeSkipped, Error: "dep failed"})

	snap := state.snapshotOnly([]NodeID{"json-done", "text-done", "failed", "skipped"})
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries (only successful nodes in filter), got %d: %v", len(snap), snap)
	}
	if got := string(snap["json-done"]); got != `{"k":"a"}` {
		t.Fatalf("expected raw JSON output, got %q", got)
	}
	if got := string(snap["text-done"]); got != `"hello"` {
		t.Fatalf("expected JSON-encoded text, got %q", got)
	}
	if _, ok := snap["excluded"]; ok {
		t.Fatal("expected excluded to be filtered out by id list")
	}
	if _, ok := snap["failed"]; ok {
		t.Fatal("expected failed node to be omitted")
	}
	if _, ok := snap["skipped"]; ok {
		t.Fatal("expected skipped node to be omitted")
	}

	full := state.Snapshot()
	for id, raw := range snap {
		if string(full[id]) != string(raw) {
			t.Fatalf("snapshotOnly[%s] = %s, but Snapshot[%s] = %s", id, raw, id, full[id])
		}
	}
}

func TestSharedState_Concurrency(t *testing.T) {
	state := NewSharedState()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := NodeID(json.Number(string(rune('a' + i%26))))
			state.set(id, &NodeResult{
				NodeID: id,
				Status: NodeDone,
				Output: json.RawMessage(`{"value":true}`),
			})
			_ = state.get(id)
			_ = state.Snapshot()
		}(i)
	}

	wg.Wait()
}

func TestSharedState_Snapshot(t *testing.T) {
	state := NewSharedState()
	state.set("done-json", &NodeResult{
		NodeID: "done-json",
		Status: NodeDone,
		Output: json.RawMessage(`{"key":"val"}`),
	})
	state.set("done-text", &NodeResult{
		NodeID: "done-text",
		Status: NodeDone,
		Text:   "hello world",
	})
	state.set("failed", &NodeResult{
		NodeID: "failed",
		Status: NodeFailed,
		Error:  "boom",
	})

	snap := state.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries in snapshot, got %d", len(snap))
	}
	if string(snap["done-json"]) != `{"key":"val"}` {
		t.Fatalf("unexpected JSON output: %s", snap["done-json"])
	}
	if snap["done-text"] == nil {
		t.Fatal("expected done-text in snapshot")
	}
}
